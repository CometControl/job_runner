package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time" // Added for http server timeouts

	"job_runner/config"
	// "job_runner/db" // No longer directly used by server, but by sql_handler
	// "job_runner/metric" // No longer directly used by server, but by sql_handler
	"job_runner/tasks"
	"job_runner/tasks/httpcheck" // Import the new httpcheck handler
	"job_runner/tasks/sql"     // Import the new sql handler package

	"github.com/VictoriaMetrics/metrics"
)

// Server represents the HTTP server that handles metric requests
type Server struct {
	Config       config.Config
	server       *http.Server
	taskHandlers map[string]tasks.TaskHandler // Map routes to task handlers
}

// New creates a new server instance
func New(cfg config.Config) *Server {
	s := &Server{
		Config:       cfg,
		taskHandlers: make(map[string]tasks.TaskHandler),
	}

	// Initialize task handlers
	s.taskHandlers["/sql"] = sql.NewSQLTaskHandler()
	s.taskHandlers["/http_check"] = httpcheck.NewHTTPCheckTaskHandler() // Add new handler

	return s
}

// Start starts the HTTP server
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Register task handlers from the map
	for path, handler := range s.taskHandlers {
		// Capture path and handler for the closure
		p := path
		h := handler
		mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
			s.genericTaskDispatcher(w, r, h)
		})
	}

	mux.HandleFunc("/metrics", s.handleAppMetrics) // Application metrics endpoint
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/", s.handleRoot) // Keep the root handler for now

	addr := fmt.Sprintf("%s:%d", s.Config.HTTPAddr, s.Config.HTTPPort)
	s.server = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  s.Config.ConnOptions.ConnectTimeout.ToStd(),
		WriteTimeout: s.Config.ConnOptions.QueryTimeout.ToStd(), // This might be better named as general request timeout
		IdleTimeout:  60 * time.Second,
	}

	slog.Info("Starting Job Runner", "address", addr)
	return s.server.ListenAndServe()
}

// genericTaskDispatcher handles requests by calling the appropriate TaskHandler.
func (s *Server) genericTaskDispatcher(w http.ResponseWriter, r *http.Request, handler tasks.TaskHandler) {
	metricContent, statusCode, err := handler.Handle(r.Context(), r, s.Config)

	s.incrementRequestCounter(r.URL.Path, r.Method, statusCode)

	if err != nil {
		slog.Error("Task handler error", "path", r.URL.Path, "method", r.Method, "status_code", statusCode, "error", err.Error())
		// If metricContent is available (e.g., a status metric from the handler), write it with the error status.
		if len(metricContent) > 0 {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(statusCode) // Handler determined this status code
			w.Write(metricContent)
		} else {
			// Otherwise, just write the error message with the status code.
			http.Error(w, fmt.Sprintf("Task execution failed: %v", err), statusCode)
		}
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(statusCode)
	w.Write(metricContent)
}

// HandleRequest handles an HTTP request - useful for testing
// This needs to be updated to use the new dispatcher logic or be re-evaluated if still needed.
func (s *Server) HandleRequest(w http.ResponseWriter, r *http.Request) {
	// Check if the path is for a registered task handler
	if handler, ok := s.taskHandlers[r.URL.Path]; ok {
		s.genericTaskDispatcher(w, r, handler)
		return
	}

	// Handle other fixed paths
	switch r.URL.Path {
	case "/metrics":
		s.handleAppMetrics(w, r)
	case "/health":
		s.handleHealth(w, r)
	case "/":
		s.handleRoot(w, r)
	default:
		s.incrementRequestCounter(r.URL.Path, r.Method, http.StatusNotFound)
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
			h1, h2 { color: #333; }
			code { background-color: #f4f4f4; padding: 2px 5px; border-radius: 3px; }
			table { border-collapse: collapse; width: 100%%; margin-bottom: 20px; }
			th, td { text-align: left; padding: 8px; border-bottom: 1px solid #ddd; }
			th { background-color: #f2f2f2; }
		</style>
	</head>
	<body>
		<h1>Job Runner</h1>
		
		<h2>/sql Endpoint</h2>
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
				<td>Database type (e.g., pg, sqlite, oracle, sqlserver)</td>
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
		<h3>Example for /sql</h3>
		<code>/sql?type=pg&username=user&password=pass&host=localhost&db=postgres&query=SELECT+name,+value+FROM+metrics&value_column=value</code>

		<h2>/http_check Endpoint</h2>
		<p>Use the /http_check endpoint to perform an HTTP request to a target URL and get its status and duration as Prometheus metrics.</p>
		<table>
			<tr>
				<th>Parameter</th>
				<th>Description</th>
				<th>Required</th>
			</tr>
			<tr>
				<td>target_url</td>
				<td>The full URL to check (e.g., http://example.com/health)</td>
				<td>Yes</td>
			</tr>
			<tr>
				<td>method</td>
				<td>HTTP method to use (e.g., GET, POST)</td>
				<td>No (default: GET)</td>
			</tr>
			<tr>
				<td>expected_status</td>
				<td>The expected HTTP status code for a successful check</td>
				<td>No (default: 200)</td>
			</tr>
			<tr>
				<td>timeout</td>
				<td>Timeout for the HTTP request (e.g., 5s, 500ms). Overrides global config.</td>
				<td>No (default: from config, typically 15s)</td>
			</tr>
		</table>
		<h3>Example for /http_check</h3>
		<code>/http_check?target_url=https://api.example.com/status&method=GET&expected_status=200&timeout=5s</code>
		
		<h2>Other Endpoints</h2>
		<ul>
			<li><a href="/metrics">/metrics</a> - Application operational metrics (Prometheus exporter)</li>
			<li><a href="/health">/health</a> - Health Check</li>
		</ul>
	</body>
	</html>
	`)
}
