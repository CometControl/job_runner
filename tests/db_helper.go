package tests

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time" // Added import

	"job_runner/config"
	"job_runner/db"
)

// TestDBPath is the path where the test SQLite database will be created
const TestDBPath = "./test_data.db"

// SetupTestDB creates a SQLite database with test data for testing
func SetupTestDB(t *testing.T) (*db.Connection, func()) {
	// Ensure test directory exists
	dir := filepath.Dir(TestDBPath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err := os.MkdirAll(dir, 0755)
		if err != nil {
			t.Fatalf("Failed to create test directory: %v", err)
		}
	}

	// Remove any existing test database
	_ = os.Remove(TestDBPath)

	// Create connection options for testing
	connOpts := config.ConnectionOptions{
		MaxConns:        5,
		MaxIdleConns:    2,
		MaxConnLifetime: config.Duration(0), // No timeout for tests, ensure type compatibility
		DriverParams: map[string]map[string]string{
			"sqlite": { // Assuming the test DB is SQLite and db.go standardizes driver to 'sqlite'
				// SQLite typically doesn't use SSL, but if it had other params, they'd go here.
				// For example, if we wanted to ensure a specific journal mode for tests:
				// "_journal_mode": "WAL", // SQLite params often start with _
			},
		},
		ConnectTimeout: config.Duration(10 * time.Second), // Ensure type compatibility
		QueryTimeout:   config.Duration(60 * time.Second), // Ensure type compatibility
		PreparedStmts:  false,                             // Changed to false for testing
		NoPing:         false,
	}

	// Create DSN for SQLite
	dsn := "sqlite://" + TestDBPath

	// Open connection to the database
	conn, err := db.Open(context.Background(), dsn, connOpts)
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	// Create test tables and data
	createTestData(t, conn.DB)

	// Return the connection and a cleanup function
	cleanup := func() {
		conn.Close()
		os.Remove(TestDBPath)
	}

	return conn, cleanup
}

// createTestData creates tables and populates them with test data
func createTestData(t *testing.T, db *sql.DB) {
	// Create a tables table
	_, err := db.Exec(`
		CREATE TABLE tables (
			name TEXT PRIMARY KEY,
			rows INTEGER,
			size INTEGER
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create tables table: %v", err)
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
		t.Fatalf("Failed to insert test data into tables table: %v", err)
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
		t.Fatalf("Failed to create metrics table: %v", err)
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
		t.Fatalf("Failed to insert test data into metrics table: %v", err)
	}
}

// CreateLargeTable creates a table with a large number of rows for stress testing.
func CreateLargeTable(t *testing.T, db *sql.DB, tableName string, rowCount int) {
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
