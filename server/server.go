package server

import (
	"context"
	"encoding/json" // Added for JSON marshalling
	"fmt"
	"log/slog"
	"net/http"
	"strconv" // Added for converting status code to string
	"sync"    // Added for mutex
	"time"

	"job_runner/config"
	"job_runner/tasks"
	"job_runner/tasks/httpcheck"
	"job_runner/tasks/sql"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests.",
		},
		[]string{"code", "handler", "method"}, // Order changed to code, handler, method
	)

	// httpReqDuration is defined here now
	httpReqDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds", // Standard name
			Help:    "HTTP request duration in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"code", "handler", "method"}, // Order changed to code, handler, method
	)
)

// responseData is a wrapper for http.ResponseWriter to capture status code
type responseData struct {
	status int
	http.ResponseWriter
}

// WriteHeader captures the status code before writing it to the actual response writer.
func (r *responseData) WriteHeader(statusCode int) {
	r.status = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

// MetricsMiddleware manually records HTTP request metrics.
func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		path := r.URL.Path

		// Wrap the response writer to capture the status code
		rd := &responseData{
			status:         http.StatusOK, // Default to 200 OK
			ResponseWriter: w,
		}

		next.ServeHTTP(rd, r) // Call the next handler

		duration := time.Since(start)
		statusCodeStr := strconv.Itoa(rd.status)

		// Record metrics
		// For httpRequestsTotal: labels are "code", "handler", "method"
		httpRequestsTotal.WithLabelValues(statusCodeStr, path, r.Method).Inc()
		// For httpReqDuration: labels are "code", "handler", "method"
		httpReqDuration.WithLabelValues(statusCodeStr, path, r.Method).Observe(duration.Seconds())
	})
}

// Server represents the HTTP server that handles metric requests
type Server struct {
	Config       config.Config
	configFile   string // Added to store the config file path
	server       *http.Server
	taskHandlers map[string]tasks.TaskHandler // Map routes to task handlers
	configLock   sync.RWMutex                 // Added for thread-safe config access
}

// New creates a new server instance
func New(cfg config.Config, configFile string) *Server { // Added configFile parameter
	s := &Server{
		Config:       cfg,
		configFile:   configFile, // Store the config file path
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
		// Each task handler endpoint will be wrapped by the MetricsMiddleware
		mux.Handle(p, MetricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s.genericTaskDispatcher(w, r, h)
		})))
	}

	// Wrap non-task handlers with MetricsMiddleware as well
	mux.Handle("/metrics", MetricsMiddleware(http.HandlerFunc(s.handleAppMetrics))) // Application metrics endpoint
	mux.Handle("/health", MetricsMiddleware(http.HandlerFunc(s.handleHealth)))
	mux.Handle("/config", MetricsMiddleware(http.HandlerFunc(s.handleConfig)))       // Added /config endpoint
	mux.Handle("/reload", MetricsMiddleware(http.HandlerFunc(s.handleReloadConfig))) // Corrected /reload endpoint
	mux.Handle("/", MetricsMiddleware(http.HandlerFunc(s.handleRoot)))               // Keep the root handler for now

	s.configLock.RLock()
	addr := fmt.Sprintf("%s:%d", s.Config.HTTPAddr, s.Config.HTTPPort)
	connectTimeout := s.Config.ConnOptions.ConnectTimeout.ToStd()
	queryTimeout := s.Config.ConnOptions.QueryTimeout.ToStd()
	s.configLock.RUnlock()

	s.server = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  connectTimeout,
		WriteTimeout: queryTimeout, // This might be better named as general request timeout
		IdleTimeout:  60 * time.Second,
	}

	slog.Info("Starting Job Runner", "address", addr)
	return s.server.ListenAndServe()
}

// genericTaskDispatcher handles requests by calling the appropriate TaskHandler.
func (s *Server) genericTaskDispatcher(w http.ResponseWriter, r *http.Request, handler tasks.TaskHandler) {
	s.configLock.RLock()
	currentConfig := s.Config
	s.configLock.RUnlock()
	metricContent, statusCode, err := handler.Handle(r.Context(), r, currentConfig)

	// s.incrementRequestCounter(r.URL.Path, r.Method, statusCode) // This is now handled by MetricsMiddleware

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
	// Create a new ServeMux for testing that mirrors the main one with middleware
	testMux := http.NewServeMux()

	// Register task handlers from the map with middleware
	for path, handler := range s.taskHandlers {
		p := path
		h := handler
		testMux.Handle(p, MetricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s.genericTaskDispatcher(w, r, h)
		})))
	}

	// Handle other fixed paths with middleware
	testMux.Handle("/metrics", MetricsMiddleware(http.HandlerFunc(s.handleAppMetrics)))
	testMux.Handle("/health", MetricsMiddleware(http.HandlerFunc(s.handleHealth)))
	testMux.Handle("/", MetricsMiddleware(http.HandlerFunc(s.handleRoot)))

	// Fallback for paths not explicitly handled by taskHandlers or fixed paths
	// This part is tricky because http.NotFound is a function, not a handler.
	// For testing, we might rely on the fact that if a path isn't in taskHandlers or the fixed list,
	// the testMux will serve a 404 automatically, and the middleware would catch that if it wrapped the entire mux.
	// However, for precise metric tagging, explicit handling is better.
	// For simplicity in this refactor, we'll assume test requests hit defined handlers.
	// If a request hits a path not in testMux, it will naturally 404.
	// The MetricsMiddleware will only run if a handler is matched.

	testMux.ServeHTTP(w, r) // Dispatch the request using the test mux
}

// Stop gracefully stops the HTTP server
func (s *Server) Stop(ctx context.Context) error {
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

// handleConfig serves the current configuration as JSON
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	s.configLock.RLock()
	cfgJSON, err := json.MarshalIndent(s.Config, "", "  ")
	s.configLock.RUnlock()

	if err != nil {
		slog.Error("Failed to marshal config to JSON", "error", err)
		http.Error(w, "Failed to marshal config to JSON", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(cfgJSON)
}

// handleReloadConfig reloads the configuration from the config file
func (s *Server) handleReloadConfig(w http.ResponseWriter, r *http.Request) {
	if s.configFile == "" {
		slog.Warn("Config reload requested, but no config file path was provided at startup.")
		http.Error(w, "Configuration reload not supported: no config file path specified at startup.", http.StatusNotImplemented)
		return
	}

	s.configLock.Lock()
	defer s.configLock.Unlock()

	newCfg, err := config.LoadConfig(s.configFile)
	if err != nil {
		slog.Error("Failed to reload configuration", "file", s.configFile, "error", err)
		http.Error(w, fmt.Sprintf("Failed to reload configuration: %v", err), http.StatusInternalServerError)
		return
	}

	s.Config = newCfg
	slog.Info("Configuration reloaded successfully", "file", s.configFile)
	fmt.Fprintln(w, "Configuration reloaded successfully.")
}

// handleAppMetrics serves application-level metrics (e.g., request counters)
func (s *Server) handleAppMetrics(w http.ResponseWriter, r *http.Request) {
	// VictoriaMetrics specific code removed.
	// Use the default Prometheus registry and promhttp.Handler()
	promhttp.Handler().ServeHTTP(w, r)
}

// handleHealth handles health check requests
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	// s.incrementRequestCounter("/health", r.Method, http.StatusOK) // Handled by MetricsMiddleware
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "OK")
}

// handleRoot serves the root page with usage instructions
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" { // This check is important for the middleware to correctly label the handler
		// s.incrementRequestCounter(r.URL.Path, r.Method, http.StatusNotFound) // Handled by MetricsMiddleware if it wraps a NotFoundHandler
		// For now, the middleware wraps specific handlers. If a path is not matched by ServeMux,
		// the middleware for that specific path won't run. The default Go ServeMux will issue a 404.
		// To capture metrics for 404s from unmatched paths, the entire mux would need to be wrapped, or a final catch-all handler.
		// For this iteration, we rely on the mux's default 404 for truly unhandled paths.
		// The MetricsMiddleware will only run if a handler is matched.

		http.NotFound(w, r)
		return
	}
	// s.incrementRequestCounter("/", r.Method, http.StatusOK) // Handled by MetricsMiddleware
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
			<li><a href="/config">/config</a> - View current server configuration (JSON)</li>
			<li><a href="/reload">/reload</a> - Reload server configuration from file</li>
			<li><a href="/metrics">/metrics</a> - Application operational metrics (Prometheus exporter)</li>
			<li><a href="/health">/health</a> - Health Check</li>
		</ul>
	</body>
	</html>
	`) // Ensure the backtick is on its own line if it's the end of the raw string literal
}
