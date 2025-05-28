package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"job_runner/config"
	dberrors "job_runner/errors"

	"github.com/xo/dburl"

	// Import SQL drivers
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/lib/pq"
	_ "github.com/microsoft/go-mssqldb/azuread"
	_ "github.com/sijms/go-ora/v2"
	_ "modernc.org/sqlite"
)

// Connection represents a database connection and its associated settings
type Connection struct {
	DB     *sql.DB
	Config config.ConnectionOptions
}

// SafeParse wraps dburl.Parse method to prevent leaking credentials in error messages
func SafeParse(rawURL string) (*dburl.URL, error) {
	expandedURL := os.ExpandEnv(rawURL)
	parsed, err := dburl.Parse(expandedURL)
	if err != nil {
		if uerr := new(url.Error); errors.As(err, &uerr) {
			return nil, fmt.Errorf("invalid DSN (underlying error: %w)", uerr.Err)
		}
		return nil, fmt.Errorf("invalid DSN (dburl.Parse error: %w)", err)
	}
	return parsed, nil
}

// BuildDSN constructs a data source name (connection string) based on the database type and parameters
func BuildDSN(dbType, username, password, host, port, database string) (string, error) {
	switch strings.ToLower(dbType) {
	case "sqlite", "sqlite3":
		if database == "" {
			return "", errors.New("database path cannot be empty for SQLite")
		}
		// Return the plain database path. The Open function will handle prefixing if necessary for dburl.
		return database, nil
	case "pg", "postgres", "postgresql":
		portVal := "5432"
		if port != "" {
			portVal = port
		}
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s", username, password, host, portVal, database), nil
	case "oracle":
		portVal := "1521"
		if port != "" {
			portVal = port
		}
		return fmt.Sprintf("oracle://%s:%s@%s:%s/%s", username, password, host, portVal, database), nil
	case "sqlserver", "mssql":
		portVal := "1433"
		if port != "" {
			portVal = port
		}
		return fmt.Sprintf("sqlserver://%s:%s@%s:%s?database=%s", username, password, host, portVal, database), nil
	default:
		// Use dburl to build DSN for other types
		dsn := fmt.Sprintf("%s://%s:%s@%s:%s/%s", dbType, username, password, host, port, database)
		u, err := dburl.Parse(dsn) // dburl.Parse, not SafeParse here, as we construct it carefully
		if err != nil {
			return "", fmt.Errorf("failed to parse constructed DSN: %w", err)
		}
		// Ensure the scheme is set correctly for the driver if dburl didn't infer it as expected
		// (though for fully qualified DSNs like above, dburl should get it right)
		// Example: if dbType was an alias dburl didn't know but it's for sqlite
		if strings.ToLower(dbType) == "sqlite" || strings.ToLower(dbType) == "sqlite3" {
			if u.Scheme != "sqlite" { // Only override if not already correct
				u.Scheme = "sqlite"
			}
		}
		return u.String(), nil
	}
}

// Open opens a database connection with the specified parameters
func Open(ctx context.Context, dsn string, connOpts config.ConnectionOptions) (*Connection, error) {
	originalDSN := dsn
	// Check if the DSN is a plain path that might be for SQLite.
	// dburl.Parse might not recognize plain paths as SQLite without a scheme.
	if !strings.Contains(dsn, "://") &&
		(strings.HasSuffix(strings.ToLower(dsn), ".db") ||
			strings.HasSuffix(strings.ToLower(dsn), ".sqlite") ||
			strings.HasSuffix(strings.ToLower(dsn), ".sqlite3") ||
			strings.Contains(dsn, "sqlite") || // less specific, but common in names
			dsn == ":memory:" || strings.HasSuffix(dsn, ":memory:")) {
		// Prepend "sqlite://" to help dburl.Parse identify it as SQLite.
		// For paths like "C:/foo/bar.db", this becomes "sqlite://C:/foo/bar.db"
		// For ":memory:", this becomes "sqlite://:memory:"
		dsn = "sqlite://" + dsn
	}

	parsedURL, err := SafeParse(dsn) // SafeParse calls os.ExpandEnv then dburl.Parse
	if err != nil {
		return nil, dberrors.NewDBError(fmt.Sprintf("failed to parse DSN '%s' (original DSN was '%s'): %v", dsn, originalDSN, err))
	}

	// Get current query values from the parsed DSN
	queryValues, qErr := url.ParseQuery(parsedURL.RawQuery)
	if qErr != nil {
		return nil, dberrors.NewDBError(fmt.Sprintf("failed to parse DSN query parameters from '%s' (in DSN '%s'): %v", parsedURL.RawQuery, dsn, qErr))
	}

	// Apply driver-specific parameters from config, potentially overriding or adding to existing ones
	// parsedURL.Driver is typically the scheme (e.g., "sqlite", "postgres").
	// For SQLite, dburl.Parse might set Driver to "sqlite3".
	lookupDriver := strings.ToLower(parsedURL.Driver)
	if lookupDriver == "sqlite3" { // dburl often uses "sqlite3" as Driver for "sqlite" scheme
		lookupDriver = "sqlite"
	}

	if driverSpecificParams, ok := connOpts.DriverParams[lookupDriver]; ok {
		for key, value := range driverSpecificParams {
			queryValues.Set(key, value) // Add/override parameters
		}
	}

	// Update RawQuery in parsedURL with the merged parameters
	parsedURL.RawQuery = queryValues.Encode()

	// Determine the driver name for sql.Open
	driverToUse := parsedURL.Driver // This is the scheme from dburl, e.g., "postgres", "mysql", "sqlite3"
	if parsedURL.GoDriver != "" {   // GoDriver is specific like "pq", "mysql", "sqlite" (for modernc)
		driverToUse = parsedURL.GoDriver
	}
	// Standardize SQLite driver name to "sqlite" for modernc.org/sqlite
	if driverToUse == "sqlite3" { // If GoDriver wasn't set and Driver was "sqlite3"
		driverToUse = "sqlite"
	}

	var dsnForSqlOpen string
	if driverToUse == "sqlite" {
		// For modernc.org/sqlite, the DSN is the path (from parsedURL.Path)
		// with query parameters appended.
		// dburl.Parse, when given "sqlite://C:/path/to.db?opt=val", sets:
		//   Scheme: "sqlite"
		//   Driver: "sqlite3" (or similar, hence our standardization)
		//   GoDriver: "sqlite"
		//   Path: "/C:/path/to.db" (note the leading slash if drive letter present)
		//   DSN: "C:/path/to.db" (this is what we want for modernc.org/sqlite)
		// If the original DSN was just ":memory:", after prefixing it becomes "sqlite://:memory:".
		// dburl.Parse then gives Path=":memory:" and DSN=":memory:".

		// We use parsedURL.DSN as dburl has already processed it into the form the driver expects.
		// For "sqlite://C:/foo/bar.db", parsedURL.DSN becomes "C:/foo/bar.db".
		// For "sqlite://:memory:", parsedURL.DSN becomes ":memory:".
		dsnForSqlOpen = parsedURL.DSN

		if parsedURL.RawQuery != "" {
			dsnForSqlOpen = dsnForSqlOpen + "?" + parsedURL.RawQuery
		}
	} else {
		// For other drivers, parsedURL.String() reconstructs the full DSN
		// using the scheme, user/pass, host, path, and the *updated* RawQuery.
		dsnForSqlOpen = parsedURL.String()
	}

	db, sqlOpenErr := sql.Open(driverToUse, dsnForSqlOpen)
	if sqlOpenErr != nil {
		return nil, dberrors.NewDBError(fmt.Sprintf("failed to open database connection (driver: %s, dsn: '%s'): %v", driverToUse, dsnForSqlOpen, sqlOpenErr))
	}

	// Configure connection pool
	db.SetMaxOpenConns(connOpts.MaxConns)
	db.SetMaxIdleConns(connOpts.MaxIdleConns)
	db.SetConnMaxLifetime(connOpts.MaxConnLifetime.ToStd())

	// Test the connection with ping unless disabled
	if !connOpts.NoPing {
		pingCtx, pingCancel := context.WithTimeout(ctx, connOpts.ConnectTimeout.ToStd())
		defer pingCancel()

		if pingErr := db.PingContext(pingCtx); pingErr != nil {
			db.Close() // Close the connection if ping fails
			return nil, dberrors.NewDBError(fmt.Sprintf("ping failed (driver: %s, dsn: '%s'): %v", driverToUse, dsnForSqlOpen, pingErr))
		}
	}

	return &Connection{
		DB:     db,
		Config: connOpts,
	}, nil
}

// Close closes the database connection
func (c *Connection) Close() error {
	if c.DB != nil {
		return c.DB.Close()
	}
	return nil
}

// ExecuteQuery runs the SQL query and returns the results
func (c *Connection) ExecuteQuery(ctx context.Context, query string) (*sql.Rows, error) {
	if c.DB == nil {
		return nil, dberrors.NewDBError("database connection is nil")
	}

	if c.Config.PreparedStmts {
		stmt, err := c.DB.PrepareContext(ctx, query) // Use original context
		if err != nil {
			return nil, dberrors.NewQueryError(fmt.Sprintf("prepare query failed: %v", err))
		}
		defer stmt.Close()
		rows, err := stmt.QueryContext(ctx) // Use original context
		if err != nil {
			return nil, dberrors.NewQueryError(fmt.Sprintf("execute prepared query failed: %v", err))
		}
		return rows, nil
	}

	rows, err := c.DB.QueryContext(ctx, query) // Use original context
	if err != nil {
		return nil, dberrors.NewQueryError(fmt.Sprintf("execute query failed: %v", err))
	}
	return rows, nil
}
