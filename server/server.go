package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time" // Added for http server timeouts

	"job_runner/config"
	"job_runner/db"
	"job_runner/metric"

	"github.com/VictoriaMetrics/metrics"
)

// Server represents the HTTP server that handles metric requests
type Server struct {
	Config config.Config
	server *http.Server
}

// New creates a new server instance
func New(cfg config.Config) *Server {
	return &Server{
		Config: cfg,
	}
}

// Start starts the HTTP server
func (s *Server) Start() error {
	mux := http.NewServeMux()

	mux.HandleFunc("/sql", s.handleSQLQuery)       // Changed from /metrics
	mux.HandleFunc("/metrics", s.handleAppMetrics) // New application metrics endpoint
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/", s.handleRoot)

	addr := fmt.Sprintf("%s:%d", s.Config.HTTPAddr, s.Config.HTTPPort)
	s.server = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  s.Config.ConnOptions.ConnectTimeout.ToStd(), // Use ToStd()
		WriteTimeout: s.Config.ConnOptions.QueryTimeout.ToStd(),   // Use ToStd()
		IdleTimeout:  60 * time.Second,
	}

	slog.Info("Starting Job Runner", "address", addr)
	return s.server.ListenAndServe()
}

// HandleRequest handles an HTTP request - useful for testing
func (s *Server) HandleRequest(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	switch {
	case path == "/sql": // Changed from /metrics
		s.handleSQLQuery(w, r)
	case path == "/metrics": // New application metrics endpoint
		s.handleAppMetrics(w, r)
	case path == "/health":
		s.handleHealth(w, r)
	case path == "/":
		s.handleRoot(w, r)
	default:
		s.incrementRequestCounter(path, r.Method, http.StatusNotFound)
		http.NotFound(w, r)
	}
}

// Stop gracefully stops the HTTP server
func (s *Server) Stop(ctx context.Context) error {
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

func (s *Server) incrementRequestCounter(handlerPath, method string, statusCode int) {
	metrics.GetOrCreateCounter(fmt.Sprintf(`http_requests_total{handler="%s",method="%s",status_code="%d"}`, handlerPath, method, statusCode)).Inc()
}

// handleSQLQuery processes HTTP requests with SQL query parameters
// Renamed from handleMetrics
func (s *Server) handleSQLQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.incrementRequestCounter("/sql", r.Method, http.StatusMethodNotAllowed)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	queryParams := r.URL.Query()
	sqlQuery := queryParams.Get("query")
	if sqlQuery == "" {
		s.incrementRequestCounter("/sql", r.Method, http.StatusBadRequest)
		http.Error(w, "Missing required parameter: query", http.StatusBadRequest)
		return
	}

	dbType := queryParams.Get("type")
	if dbType == "" {
		s.incrementRequestCounter("/sql", r.Method, http.StatusBadRequest)
		http.Error(w, "Missing required parameter: type", http.StatusBadRequest)
		return
	}

	username := queryParams.Get("username")
	password := queryParams.Get("password")
	host := queryParams.Get("host")
	database := queryParams.Get("db")

	if dbType != "sqlite" && dbType != "sqlite3" && (username == "" || host == "" || database == "") {
		// Password can be empty for some DBs, but username, host, and db are generally needed.
		// SQLite only needs 'db' (the file path).
		s.incrementRequestCounter("/sql", r.Method, http.StatusBadRequest)
		http.Error(w, "Missing required connection parameters (username, host, db) for non-SQLite types", http.StatusBadRequest)
		return
	}
	if (dbType == "sqlite" || dbType == "sqlite3") && database == "" {
		s.incrementRequestCounter("/sql", r.Method, http.StatusBadRequest)
		http.Error(w, "Missing required parameter: db (database file path for SQLite)", http.StatusBadRequest)
		return
	}

	port := queryParams.Get("port")
	valueColumn := queryParams.Get("value_column")
	if valueColumn == "" {
		valueColumn = "value"
	}

	metricPrefix := queryParams.Get("metric_prefix")
	if metricPrefix == "" {
		metricPrefix = s.Config.QueryMetricName
	}
	queryStatusMetricName := s.Config.QueryStatusMetricName

	requestScopedMetricSet := metrics.NewSet() // Per-request set for SQL results + status

	dsn, err := db.BuildDSN(dbType, username, password, host, port, database)
	if err != nil {
		metric.RecordQueryStatus(requestScopedMetricSet, queryStatusMetricName, sqlQuery, err)
		s.incrementRequestCounter("/sql", r.Method, http.StatusBadRequest)
		http.Error(w, fmt.Sprintf("Failed to build DSN: %v", err), http.StatusBadRequest)
		w.Header().Set("Content-Type", "text/plain")
		requestScopedMetricSet.WritePrometheus(w)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.Config.ConnOptions.QueryTimeout.ToStd()) // Use ToStd()
	defer cancel()

	conn, err := db.Open(ctx, dsn, s.Config.ConnOptions)
	if err != nil {
		metric.RecordQueryStatus(requestScopedMetricSet, queryStatusMetricName, sqlQuery, err)
		s.incrementRequestCounter("/sql", r.Method, http.StatusInternalServerError)
		http.Error(w, fmt.Sprintf("Failed to connect to database: %v", err), http.StatusInternalServerError)
		w.Header().Set("Content-Type", "text/plain")
		requestScopedMetricSet.WritePrometheus(w)
		return
	}
	defer conn.Close()

	rows, err := conn.ExecuteQuery(ctx, sqlQuery)
	if err != nil {
		metric.RecordQueryStatus(requestScopedMetricSet, queryStatusMetricName, sqlQuery, err)
		s.incrementRequestCounter("/sql", r.Method, http.StatusInternalServerError)
		http.Error(w, fmt.Sprintf("Failed to execute query: %v", err), http.StatusInternalServerError)
		w.Header().Set("Content-Type", "text/plain")
		requestScopedMetricSet.WritePrometheus(w)
		return
	}
	defer rows.Close()

	generator := metric.NewGenerator(metricPrefix, valueColumn)
	err = generator.GenerateFromRows(requestScopedMetricSet, rows)
	if err != nil {
		metric.RecordQueryStatus(requestScopedMetricSet, queryStatusMetricName, sqlQuery, err)
		s.incrementRequestCounter("/sql", r.Method, http.StatusInternalServerError)
		http.Error(w, fmt.Sprintf("Failed to generate metrics: %v", err), http.StatusInternalServerError)
		w.Header().Set("Content-Type", "text/plain")
		requestScopedMetricSet.WritePrometheus(w)
		return
	}

	metric.RecordQueryStatus(requestScopedMetricSet, queryStatusMetricName, sqlQuery, nil)
	s.incrementRequestCounter("/sql", r.Method, http.StatusOK)
	w.Header().Set("Content-Type", "text/plain")
	requestScopedMetricSet.WritePrometheus(w)
}

// handleAppMetrics serves application-level metrics (e.g., request counters)
func (s *Server) handleAppMetrics(w http.ResponseWriter, r *http.Request) {
	s.incrementRequestCounter("/metrics", r.Method, http.StatusOK)
	w.Header().Set("Content-Type", "text/plain")
	metrics.WritePrometheus(w, false) // Corrected: Writes metrics from the global default registry, false for exposeProcessMetrics
}

// handleHealth handles health check requests
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.incrementRequestCounter("/health", r.Method, http.StatusOK)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "OK")
}

// handleRoot serves the root page with usage instructions
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		s.incrementRequestCounter(r.URL.Path, r.Method, http.StatusNotFound)
		http.NotFound(w, r)
		return
	}
	s.incrementRequestCounter("/", r.Method, http.StatusOK)
	w.Header().Set("Content-Type", "text/html")
	// Updated example URL to /sql
	fmt.Fprintf(w, `
	<html>
	<head>
		<title>Job Runner</title>
		<style>
			body { font-family: Arial, sans-serif; line-height: 1.6; max-width: 800px; margin: 0 auto; padding: 20px; }
			h1 { color: #333; }
			code { background-color: #f4f4f4; padding: 2px 5px; border-radius: 3px; }
			table { border-collapse: collapse; width: 100%%; }
			th, td { text-align: left; padding: 8px; border-bottom: 1px solid #ddd; }
			th { background-color: #f2f2f2; }
		</style>
	</head>
	<body>
		<h1>Job Runner</h1>
		<p>Use the /sql endpoint with the following query parameters to execute SQL queries and get results as Prometheus metrics:</p>
		<table>
			<tr>
				<th>Parameter</th>
				<th>Description</th>
				<th>Required</th>
			</tr>
			<tr>
				<td>query</td>
				<td>SQL query to execute</td>
				<td>Yes</td>
			</tr>
			<tr>
				<td>type</td>
				<td>Database type (e.g., pg, mysql, sqlite, oracle, sqlserver)</td>
				<td>Yes</td>
			</tr>
			<tr>
				<td>username</td>
				<td>Database username</td>
				<td>Yes (unless SQLite)</td>
			</tr>
			<tr>
				<td>password</td>
				<td>Database password</td>
				<td>(Yes, for most DBs)</td>
			</tr>
			<tr>
				<td>host</td>
				<td>Database host</td>
				<td>Yes (unless SQLite)</td>
			</tr>
			<tr>
				<td>port</td>
				<td>Database port</td>
				<td>No (default varies by type)</td>
			</tr>
			<tr>
				<td>db</td>
				<td>Database name or file path (for SQLite)</td>
				<td>Yes</td>
			</tr>
			<tr>
				<td>value_column</td>
				<td>Column to use as metric value</td>
				<td>No (default: "value")</td>
			</tr>
			<tr>
				<td>metric_prefix</td>
				<td>Prefix for metric names from SQL query</td>
				<td>No (default: "sql_query_result" from config)</td>
			</tr>
		</table>
		
		<h2>Example for /sql</h2>
		<code>/sql?type=pg&username=user&password=pass&host=localhost&db=postgres&query=SELECT+name,+value+FROM+metrics&value_column=value</code>
		
		<h2>Other Endpoints</h2>
		<ul>
			<li><a href="/metrics">/metrics</a> - Application operational metrics (Prometheus exporter)</li>
			<li><a href="/health">/health</a> - Health Check</li>
		</ul>
	</body>
	</html>
	`)
}
