package errors

import "fmt"

// BaseError represents a basic error with a message
type BaseError struct {
	Message string
}

func (e BaseError) Error() string {
	return e.Message
}

// DBError represents a database-related error
type DBError struct {
	BaseError
}

// NewDBError creates a new database error
func NewDBError(message string) *DBError {
	return &DBError{
		BaseError: BaseError{
			Message: fmt.Sprintf("Database error: %s", message),
		},
	}
}

// QueryError represents an error during query execution
type QueryError struct {
	BaseError
}

// NewQueryError creates a new query error
func NewQueryError(message string) *QueryError {
	return &QueryError{
		BaseError: BaseError{
			Message: fmt.Sprintf("Query error: %s", message),
		},
	}
}

// ConfigError represents a configuration error
type ConfigError struct {
	BaseError
}

// NewConfigError creates a new configuration error
func NewConfigError(message string) *ConfigError {
	return &ConfigError{
		BaseError: BaseError{
			Message: fmt.Sprintf("Configuration error: %s", message),
		},
	}
}

// ServerError represents a server-related error
type ServerError struct {
	BaseError
}

// NewServerError creates a new server error
func NewServerError(message string) *ServerError {
	return &ServerError{
		BaseError: BaseError{
			Message: fmt.Sprintf("Server error: %s", message),
		},
	}
}
