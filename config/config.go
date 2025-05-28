package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Duration is a wrapper around time.Duration to allow for custom JSON unmarshaling.
type Duration time.Duration

// UnmarshalJSON parses a string duration (e.g., "5m", "1h") into a Duration type.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		// If it's not a string, try to unmarshal as a number (nanoseconds)
		// This maintains compatibility if someone was using numbers.
		var num int64
		if errNum := json.Unmarshal(b, &num); errNum != nil {
			return fmt.Errorf("failed to unmarshal duration as string or number: %v, %v", err, errNum)
		}
		*d = Duration(time.Duration(num))
		return nil
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("failed to parse duration string %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// MarshalJSON converts Duration to its string representation for JSON.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// ToStd converts custom Duration to standard time.Duration.
func (d Duration) ToStd() time.Duration {
	return time.Duration(d)
}

// Config represents the server configuration
type Config struct {
	HTTPAddr              string            `json:"http_addr"`
	HTTPPort              int               `json:"http_port"`
	ConnOptions           ConnectionOptions `json:"connection_options"`
	QueryMetricName       string            `json:"query_metric_name"`
	QueryStatusMetricName string            `json:"query_status_metric_name"`
	HTTPCheckTaskTimeout  Duration          `json:"http_check_task_timeout,omitempty"` // Added for HTTP check tasks
}

// ConnectionOptions defines database connection parameters
type ConnectionOptions struct {
	MaxConns        int                          `json:"max_connections"`
	MaxIdleConns    int                          `json:"max_idle_connections"`
	MaxConnLifetime Duration                     `json:"max_connection_lifetime"`
	DriverParams    map[string]map[string]string `json:"driver_params,omitempty"` // Generic parameters per driver
	ConnectTimeout  Duration                     `json:"connect_timeout"`
	QueryTimeout    Duration                     `json:"query_timeout"`
	PreparedStmts   bool                         `json:"prepared_statements"`
	NoPing          bool                         `json:"no_ping"`
}

// DefaultConfig returns a configuration with default settings
func DefaultConfig() Config {
	config := Config{
		HTTPAddr: "0.0.0.0",
		HTTPPort: 8080,
		ConnOptions: ConnectionOptions{
			MaxConns:        5,
			MaxIdleConns:    2,
			MaxConnLifetime: Duration(10 * time.Minute),
			DriverParams:    make(map[string]map[string]string), // Initialize as empty map
			ConnectTimeout:  Duration(10 * time.Second),
			QueryTimeout:    Duration(30 * time.Second),
			PreparedStmts:   true,
		},
		QueryMetricName:       "sql_query_result",
		QueryStatusMetricName: "sql_query_status",
		HTTPCheckTaskTimeout:  Duration(15 * time.Second), // Default timeout for HTTP checks
	}

	return config
}

// LoadConfig loads the server configuration from a file
func LoadConfig(configPath string) (Config, error) {
	config := DefaultConfig()

	// If no config file specified, use defaults
	if configPath == "" {
		return config, nil
	}

	// Read and parse config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return config, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := json.Unmarshal(data, &config); err != nil {
		return config, fmt.Errorf("failed to parse config file: %w", err)
	}

	return config, nil
}
