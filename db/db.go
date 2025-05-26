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
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/lib/pq"
	_ "github.com/microsoft/go-mssqldb/azuread"
	_ "github.com/sijms/go-ora/v2"
	_ "modernc.org/sqlite" // Replaced
)

// Connection represents a database connection and its associated settings
type Connection struct {
	DB     *sql.DB
	Config config.ConnectionOptions
}

// SafeParse wraps dburl.Parse method to prevent leaking credentials in error messages
func SafeParse(rawURL string) (*dburl.URL, error) {
	parsed, err := dburl.Parse(os.ExpandEnv(rawURL))
	if err != nil {
		if uerr := new(url.Error); errors.As(err, &uerr) {
			return nil, uerr.Err
		}
		return nil, fmt.Errorf("invalid URL")
	}
	return parsed, nil
}

// BuildDSN constructs a data source name (connection string) based on the database type and parameters
func BuildDSN(dbType, username, password, host, port, database string) (string, error) {
	switch strings.ToLower(dbType) {
	case "sqlite", "sqlite3": // Added "sqlite"
		// For SQLite, the DSN is typically just the file path.
		// We'll use the 'database' parameter as the file path.
		if database == "" {
			return "", errors.New("database path cannot be empty for SQLite")
		}
		return fmt.Sprintf("sqlite://%s", database), nil // Ensure "sqlite" prefix
	case "pg", "postgres", "postgresql":
		portVal := "5432"
		if port != "" {
			portVal = port
		}
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s", username, password, host, portVal, database), nil
	case "mysql":
		portVal := "3306"
		if port != "" {
			portVal = port
		}
		return fmt.Sprintf("mysql://%s:%s@%s:%s/%s", username, password, host, portVal, database), nil
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
		u, err := dburl.Parse(dsn)
		if err != nil {
			return "", fmt.Errorf("failed to parse DSN: %w", err)
		}
		// Ensure the scheme is set correctly for the driver
		// For modernc.org/sqlite, the scheme should be "sqlite"
		if strings.ToLower(dbType) == "sqlite" || strings.ToLower(dbType) == "sqlite3" {
			u.Scheme = "sqlite"
		}
		return u.String(), nil
	}
}

// Open opens a database connection with the specified parameters
func Open(ctx context.Context, dsn string, connOpts config.ConnectionOptions) (*Connection, error) {
	parsedURL, err := SafeParse(dsn)
	if err != nil {
		return nil, dberrors.NewDBError(fmt.Sprintf("failed to parse DSN: %v", err))
	}

	// Get current query values
	queryValues, err := url.ParseQuery(parsedURL.RawQuery)
	if err != nil {
		return nil, dberrors.NewDBError(fmt.Sprintf("failed to parse DSN query parameters: %v", err))
	}

	// Apply driver-specific parameters from config
	standardizedDriver := strings.ToLower(parsedURL.Driver)
	if driverSpecificParams, ok := connOpts.DriverParams[standardizedDriver]; ok {
		for key, value := range driverSpecificParams {
			queryValues.Set(key, value)
		}
	}

	// Update RawQuery and DSN string in parsedURL
	parsedURL.RawQuery = queryValues.Encode()
	finalDSN := parsedURL.String()

	driverToUse := parsedURL.Driver
	if parsedURL.GoDriver != "" {
		driverToUse = parsedURL.GoDriver
	}

	// Override driver name for sqlite to ensure modernc.org/sqlite is used
	if driverToUse == "sqlite3" {
		driverToUse = "sqlite"
	}

	db, err := sql.Open(driverToUse, finalDSN)
	if err != nil {
		return nil, dberrors.NewDBError(fmt.Sprintf("failed to open database connection: %v, DSN: %s", err, finalDSN))
	}

	// Configure connection pool
	db.SetMaxOpenConns(connOpts.MaxConns)
	db.SetMaxIdleConns(connOpts.MaxIdleConns)
	db.SetConnMaxLifetime(connOpts.MaxConnLifetime.ToStd()) // Use ToStd()

	// Test the connection with ping unless disabled
	if !connOpts.NoPing {
		pingCtx, pingCancel := context.WithTimeout(ctx, connOpts.ConnectTimeout.ToStd()) // Use ToStd()
		defer pingCancel()

		if err := db.PingContext(pingCtx); err != nil {
			db.Close()
			return nil, dberrors.NewDBError(fmt.Sprintf("ping failed: %v", err))
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
