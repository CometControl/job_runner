package sql

import (
	"bytes"
	"context"
	"fmt"
	"job_runner/config"
	"job_runner/db"
	"job_runner/metric"
	"net/http"

	"github.com/VictoriaMetrics/metrics"
)

// SQLTaskHandler handles SQL query tasks.
// It expects query parameters like "query", "type", "host", "db", etc.
type SQLTaskHandler struct{}

// NewSQLTaskHandler creates a new SQLTaskHandler.
func NewSQLTaskHandler() *SQLTaskHandler {
	return &SQLTaskHandler{}
}

// Handle processes the HTTP request, executes the SQL query, and returns Prometheus metrics.
func (h *SQLTaskHandler) Handle(ctx context.Context, r *http.Request, appConfig config.Config) ([]byte, int, error) {
	if r.Method != http.MethodGet {
		return nil, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed")
	}

	queryParams := r.URL.Query()
	sqlQuery := queryParams.Get("query")
	if sqlQuery == "" {
		return nil, http.StatusBadRequest, fmt.Errorf("missing required parameter: query")
	}

	dbType := queryParams.Get("type")
	if dbType == "" {
		return nil, http.StatusBadRequest, fmt.Errorf("missing required parameter: type")
	}

	username := queryParams.Get("username")
	password := queryParams.Get("password")
	host := queryParams.Get("host")
	database := queryParams.Get("db")

	if dbType != "sqlite" && dbType != "sqlite3" && (username == "" || host == "" || database == "") {
		return nil, http.StatusBadRequest, fmt.Errorf("missing required connection parameters (username, host, db) for non-SQLite types")
	}
	if (dbType == "sqlite" || dbType == "sqlite3") && database == "" {
		return nil, http.StatusBadRequest, fmt.Errorf("missing required parameter: db (database file path for SQLite)")
	}

	port := queryParams.Get("port")
	valueColumn := queryParams.Get("value_column")
	if valueColumn == "" {
		valueColumn = "value" // Default value column
	}

	metricPrefix := queryParams.Get("metric_prefix")
	if metricPrefix == "" {
		metricPrefix = appConfig.QueryMetricName // Default from global config
	}
	queryStatusMetricName := appConfig.QueryStatusMetricName

	requestScopedMetricSet := metrics.NewSet()
	var metricBuf bytes.Buffer

	dsn, err := db.BuildDSN(dbType, username, password, host, port, database)
	if err != nil {
		metric.RecordQueryStatus(requestScopedMetricSet, queryStatusMetricName, sqlQuery, err)
		requestScopedMetricSet.WritePrometheus(&metricBuf)
		return metricBuf.Bytes(), http.StatusBadRequest, fmt.Errorf("failed to build DSN: %w", err)
	}

	// Use query timeout from appConfig for the context
	queryCtx, cancel := context.WithTimeout(ctx, appConfig.ConnOptions.QueryTimeout.ToStd())
	defer cancel()

	conn, err := db.Open(queryCtx, dsn, appConfig.ConnOptions)
	if err != nil {
		metric.RecordQueryStatus(requestScopedMetricSet, queryStatusMetricName, sqlQuery, err)
		requestScopedMetricSet.WritePrometheus(&metricBuf)
		return metricBuf.Bytes(), http.StatusInternalServerError, fmt.Errorf("failed to connect to database: %w", err)
	}
	defer conn.Close()

	rows, err := conn.ExecuteQuery(queryCtx, sqlQuery)
	if err != nil {
		metric.RecordQueryStatus(requestScopedMetricSet, queryStatusMetricName, sqlQuery, err)
		requestScopedMetricSet.WritePrometheus(&metricBuf)
		return metricBuf.Bytes(), http.StatusInternalServerError, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	generator := metric.NewGenerator(metricPrefix, valueColumn)
	err = generator.GenerateFromRows(requestScopedMetricSet, rows)
	if err != nil {
		metric.RecordQueryStatus(requestScopedMetricSet, queryStatusMetricName, sqlQuery, err)
		requestScopedMetricSet.WritePrometheus(&metricBuf)
		return metricBuf.Bytes(), http.StatusInternalServerError, fmt.Errorf("failed to generate metrics: %w", err)
	}

	metric.RecordQueryStatus(requestScopedMetricSet, queryStatusMetricName, sqlQuery, nil) // Record success
	requestScopedMetricSet.WritePrometheus(&metricBuf)
	return metricBuf.Bytes(), http.StatusOK, nil
}
