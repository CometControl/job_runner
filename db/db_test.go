package db_test

import (
	"context"
	"testing"

	"job_runner/db"
	"job_runner/tests"
)

func TestSQLiteConnection(t *testing.T) {
	// Setup test database
	conn, _, cleanup := tests.SetupTestDB(t) // Get all 3 return values, path not used here
	defer cleanup()

	// Test executing a query
	ctx := context.Background()
	rows, err := conn.ExecuteQuery(ctx, "SELECT name, rows as value FROM tables")
	if err != nil {
		t.Fatalf("Failed to execute query: %v", err)
	}
	defer rows.Close()

	// Validate results
	rowCount := 0
	for rows.Next() {
		var name string
		var value int
		if err := rows.Scan(&name, &value); err != nil {
			t.Fatalf("Failed to scan row: %v", err)
		}
		rowCount++
	}

	if rowCount != 4 {
		t.Errorf("Expected 4 rows, got %d", rowCount)
	}
}

func TestBuildDSN(t *testing.T) {
	tests := []struct {
		name     string
		dbType   string
		username string
		password string
		host     string
		port     string
		database string
		expected string
		wantErr  bool
	}{
		{
			name:     "PostgreSQL",
			dbType:   "pg",
			username: "user",
			password: "pass",
			host:     "localhost",
			port:     "5432",
			database: "testdb",
			expected: "postgres://user:pass@localhost:5432/testdb",
			wantErr:  false,
		},
		{
			name:     "SQLite",
			dbType:   "sqlite",
			username: "",
			password: "",
			host:     "",
			port:     "",
			database: "/path/to/db.sqlite",
			expected: "/path/to/db.sqlite", // Changed: BuildDSN for sqlite now returns the plain path
			wantErr:  false,
		},
		{
			name:     "Unsupported",
			dbType:   "invalid",
			username: "user",
			password: "pass",
			host:     "localhost",
			port:     "1234",
			database: "testdb",
			expected: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn, err := db.BuildDSN(tt.dbType, tt.username, tt.password, tt.host, tt.port, tt.database)

			if (err != nil) != tt.wantErr {
				t.Errorf("BuildDSN() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && dsn != tt.expected {
				t.Errorf("BuildDSN() = %v, want %v", dsn, tt.expected)
			}
		})
	}
}
