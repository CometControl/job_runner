package httpcheck

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"job_runner/config"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/VictoriaMetrics/metrics"
)

const (
	DefaultHTTPCheckTimeout   = 15 * time.Second
	DefaultExpectedStatusCode = http.StatusOK
	MetricPrefix              = "http_check"
)

// HTTPCheckTaskHandler handles HTTP check tasks.
// It expects query parameters like "target_url", optionally "method", "expected_status", "timeout".
type HTTPCheckTaskHandler struct{}

// NewHTTPCheckTaskHandler creates a new HTTPCheckTaskHandler.
func NewHTTPCheckTaskHandler() *HTTPCheckTaskHandler {
	return &HTTPCheckTaskHandler{}
}

// Handle processes the HTTP request, performs the HTTP check, and returns Prometheus metrics.
func (h *HTTPCheckTaskHandler) Handle(ctx context.Context, r *http.Request, appConfig config.Config) ([]byte, int, error) {
	if r.Method != http.MethodGet {
		return nil, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed for http_check endpoint, use GET")
	}

	queryParams := r.URL.Query()
	targetURL := queryParams.Get("target_url")
	if targetURL == "" {
		return nil, http.StatusBadRequest, fmt.Errorf("missing required parameter: target_url")
	}

	method := strings.ToUpper(queryParams.Get("method"))
	if method == "" {
		method = http.MethodGet
	}

	expectedStatusStr := queryParams.Get("expected_status")
	expectedStatus := DefaultExpectedStatusCode
	var parseErr error
	if expectedStatusStr != "" {
		expectedStatus, parseErr = strconv.Atoi(expectedStatusStr)
		if parseErr != nil {
			return nil, http.StatusBadRequest, fmt.Errorf("invalid expected_status: %w", parseErr)
		}
	}

	timeoutStr := queryParams.Get("timeout")
	taskTimeout := appConfig.HTTPCheckTaskTimeout.ToStd()
	if taskTimeout <= 0 {
		taskTimeout = DefaultHTTPCheckTimeout // Fallback if config is zero or negative
	}
	if timeoutStr != "" {
		d, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return nil, http.StatusBadRequest, fmt.Errorf("invalid timeout duration: %w", err)
		}
		taskTimeout = d
	}

	// Prepare metric set and buffer
	requestScopedMetricSet := metrics.NewSet()
	var metricBuf bytes.Buffer

	// Create a context with the specified timeout for the HTTP request
	checkCtx, cancel := context.WithTimeout(ctx, taskTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(checkCtx, method, targetURL, nil)
	if err != nil {
		writeMetricsToBuf(requestScopedMetricSet, &metricBuf, targetURL, method, 0, 0, 0, err)
		return metricBuf.Bytes(), http.StatusInternalServerError, fmt.Errorf("failed to create request for target_url %s: %w", targetURL, err)
	}

	client := &http.Client{}
	startTime := time.Now()
	resp, err := client.Do(req)
	duration := time.Since(startTime)

	var actualStatus int

	if err != nil {
		// Handle client.Do errors (e.g., connection refused, DNS lookup failed, context deadline exceeded)
		writeMetricsToBuf(requestScopedMetricSet, &metricBuf, targetURL, method, 0, duration, 0, err)
		// Determine appropriate status code based on error (e.g., context deadline -> Gateway Timeout)
		if strings.Contains(err.Error(), "context deadline exceeded") {
			return metricBuf.Bytes(), http.StatusGatewayTimeout, fmt.Errorf("request to target_url %s timed out: %w", targetURL, err)
		}
		return metricBuf.Bytes(), http.StatusServiceUnavailable, fmt.Errorf("request to target_url %s failed: %w", targetURL, err)
	}
	defer resp.Body.Close()

	actualStatus = resp.StatusCode

	// Drain the body to ensure connection reuse and accurate timing, but ignore content for now
	_, _ = io.Copy(io.Discard, resp.Body)

	// Determine success
	var success float64
	if actualStatus == expectedStatus {
		success = 1
	}

	writeMetricsToBuf(requestScopedMetricSet, &metricBuf, targetURL, method, success, duration, actualStatus, nil)
	return metricBuf.Bytes(), http.StatusOK, nil
}

func writeMetricsToBuf(set *metrics.Set, buf *bytes.Buffer, targetURL, method string, success float64, duration time.Duration, actualStatus int, reqErr error) {
	labels := fmt.Sprintf(`{target_url=%q, method=%q}`, targetURL, method)
	if actualStatus > 0 {
		labels = fmt.Sprintf(`{target_url=%q, method=%q, status_code="%d"}`, targetURL, method, actualStatus)
	}
	if reqErr != nil {
		labels = fmt.Sprintf(`{target_url=%q, method=%q, error=%q}`, targetURL, method, reqErr.Error())
	}

	set.GetOrCreateGauge(MetricPrefix+"_up"+labels, nil).Set(success)
	set.GetOrCreateGauge(MetricPrefix+"_duration_seconds"+labels, nil).Set(duration.Seconds())
	if actualStatus > 0 {
		set.GetOrCreateGauge(MetricPrefix+"_status_code"+labels, nil).Set(float64(actualStatus))
	}
	set.WritePrometheus(buf)
}
