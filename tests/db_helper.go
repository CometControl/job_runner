package tests

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"job_runner/config"
	"job_runner/db"
)

// generateTestDBPath creates a unique path for a test SQLite database
func generateTestDBPath(t *testing.T) string {
	tempDir := os.TempDir()
	// Sanitize the test name to be filesystem-friendly
	safeTestName := strings.ReplaceAll(t.Name(), "/", "_")
	safeTestName = strings.ReplaceAll(safeTestName, "\\", "_")
	safeTestName = strings.ReplaceAll(safeTestName, ":", "_")
	fileName := fmt.Sprintf("job_runner_test_%s_data.db", safeTestName)
	return filepath.ToSlash(filepath.Clean(filepath.Join(tempDir, fileName)))
}

// SetupTestDB creates a SQLite database with test data for testing
// It now returns the path to the created database along with the connection and cleanup function.
func SetupTestDB(t *testing.T) (*db.Connection, string, func()) {
	currentTestDBPath := generateTestDBPath(t)
	// Force removal of any existing test database file *before* anything else.
	if err := os.Remove(currentTestDBPath); err != nil && !os.IsNotExist(err) {
		t.Logf("Warning: could not remove existing test database %s: %v", currentTestDBPath, err)
	}

	// Ensure test directory exists (though TempDir should exist)
	dir := filepath.Dir(currentTestDBPath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err := os.MkdirAll(dir, 0755)
		if err != nil {
			t.Fatalf("Failed to create test directory %s: %v", dir, err)
		}
	}

	// Create connection options for testing
	connOpts := config.ConnectionOptions{
		MaxConns:        5,
		MaxIdleConns:    2,
		MaxConnLifetime: config.Duration(0), // No timeout for tests, ensure type compatibility
		DriverParams: map[string]map[string]string{
			"sqlite": {
				"_busy_timeout": "5000", // Wait 5 seconds if the database is locked
				// WAL can improve concurrency but adds complexity to file handling and cleanup,
				// especially on Windows where files might be locked more readily.
				// For simplicity in testing, especially cross-platform, default journal mode is often sufficient.
				// "_journal_mode": "WAL",
			},
		},
		ConnectTimeout: config.Duration(10 * time.Second), // Ensure type compatibility
		QueryTimeout:   config.Duration(60 * time.Second), // Ensure type compatibility
		PreparedStmts:  false,                             // Changed to false for testing
		NoPing:         false,
	}

	dsnForDbOpen := currentTestDBPath // Pass the plain, cleaned, absolute path. db.Open should handle this for SQLite.

	// Open connection to the database
	conn, err := db.Open(context.Background(), dsnForDbOpen, connOpts)
	if err != nil {
		t.Fatalf("Failed to open test database (%s): %v", dsnForDbOpen, err)
	}

	// Create test tables and data
	createTestData(t, conn.DB, currentTestDBPath) // Pass path for logging

	// Return the connection, the path, and a cleanup function
	cleanup := func() {
		if conn != nil {
			conn.Close()
		}
		// Attempt to remove the database file again on cleanup.
		if err := os.Remove(currentTestDBPath); err != nil && !os.IsNotExist(err) {
			t.Logf("Warning: could not remove test database %s during cleanup: %v", currentTestDBPath, err)
		}
	}

	return conn, currentTestDBPath, cleanup
}

// createTestData creates tables and populates them with test data
func createTestData(t *testing.T, db *sql.DB, dbPath string) {
	// Drop tables if they exist to ensure a clean state for each test
	// Using the specific db connection ensures we are acting on the correct database.
	_, err := db.Exec(`DROP TABLE IF EXISTS tables`)
	if err != nil {
		t.Fatalf("Failed to drop tables table in %s: %v", dbPath, err)
	}
	_, err = db.Exec(`DROP TABLE IF EXISTS metrics`)
	if err != nil {
		t.Fatalf("Failed to drop metrics table in %s: %v", dbPath, err)
	}
	_, err = db.Exec(`DROP TABLE IF EXISTS large_test_table`)
	if err != nil {
		t.Fatalf("Failed to drop large_test_table in %s: %v", dbPath, err)
	}

	// Create a tables table
	_, err = db.Exec(`
		CREATE TABLE tables (
			name TEXT PRIMARY KEY,
			rows INTEGER,
			size INTEGER
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create tables table in %s: %v", dbPath, err)
	}

	// Insert test data
	_, err = db.Exec(`
		INSERT INTO tables (name, rows, size) VALUES 
		('users', 1250, 5120),
		('orders', 5432, 25600),
		('products', 842, 3200),
		('categories', 50, 512)
	`)
	if err != nil {
		t.Fatalf("Failed to insert test data into tables table in %s: %v", dbPath, err)
	}

	// Create a metrics table
	_, err = db.Exec(`
		CREATE TABLE metrics (
			name TEXT,
			value REAL,
			timestamp INTEGER
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create metrics table in %s: %v", dbPath, err)
	}

	// Insert test metrics data
	_, err = db.Exec(`
		INSERT INTO metrics (name, value, timestamp) VALUES 
		('cpu_usage', 45.2, strftime('%s','now')),
		('memory_usage', 62.8, strftime('%s','now')),
		('disk_usage', 78.5, strftime('%s','now')),
		('network_in', 1250.45, strftime('%s','now')),
		('network_out', 876.23, strftime('%s','now'))
	`)
	if err != nil {
		t.Fatalf("Failed to insert test data into metrics table in %s: %v", dbPath, err)
	}
}

// CreateLargeTable creates a table with a large number of rows for stress testing.
func CreateLargeTable(t *testing.T, db *sql.DB, tableName string, rowCount int) {
	// Drop table if it exists to ensure a clean state
	_, err := db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", tableName))
	if err != nil {
		t.Fatalf("Failed to drop large table %s: %v", tableName, err)
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	// Attempt to rollback if not committed. If commit is successful, this is a no-op.
	defer tx.Rollback()

	createTableSQL := fmt.Sprintf(`
		CREATE TABLE %s (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT,
			value REAL
		);
	`, tableName)
	_, err = tx.Exec(createTableSQL)
	if err != nil {
		t.Fatalf("Failed to create large table %s: %v", tableName, err)
	}

	stmt, err := tx.Prepare(fmt.Sprintf("INSERT INTO %s (name, value) VALUES (?, ?)", tableName))
	if err != nil {
		t.Fatalf("Failed to prepare insert statement for %s: %v", tableName, err)
	}
	defer stmt.Close()

	for i := 0; i < rowCount; i++ {
		name := fmt.Sprintf("item_%d", i)
		value := float64(i) * 1.1 // Dummy value
		if _, err := stmt.Exec(name, value); err != nil {
			t.Fatalf("Failed to insert row %d into %s: %v", i, tableName, err)
		}
	}

	err = tx.Commit()
	if err != nil {
		t.Fatalf("Failed to commit transaction for %s: %v", tableName, err)
	}
	t.Logf("Successfully created and populated table %s with %d rows", tableName, rowCount)
}
