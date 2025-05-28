package httpcheck_test

import (
	"fmt"
	"job_runner/config"
	"job_runner/tasks/httpcheck"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPCheckTaskHandler_Handle_Success(t *testing.T) {
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "Hello, client")
	}))
	defer targetServer.Close()

	h := httpcheck.NewHTTPCheckTaskHandler()
	cfg := config.DefaultConfig()

	reqURL := fmt.Sprintf("/http_check?target_url=%s&expected_status=200", targetServer.URL)
	req := httptest.NewRequest("GET", reqURL, nil)

	metricContent, statusCode, err := h.Handle(req.Context(), req, cfg)

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if statusCode != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, statusCode)
	}

	metricStr := string(metricContent)
	t.Logf("Metrics returned:\n%s", metricStr) // Log metrics for debugging

	expectedMetrics := []string{
		fmt.Sprintf(`http_check_up{target_url="%s", method="GET", status_code="200"} 1`, targetServer.URL),
		fmt.Sprintf(`http_check_duration_seconds{target_url="%s", method="GET", status_code="200"}`, targetServer.URL), // Check for presence, value varies
		fmt.Sprintf(`http_check_status_code{target_url="%s", method="GET", status_code="200"} 200`, targetServer.URL),
	}

	for _, expected := range expectedMetrics {
		if !strings.Contains(metricStr, expected) {
			t.Errorf("Expected metrics to contain %q, but it didn't. Metrics:\n%s", expected, metricStr)
		}
	}
}

func TestHTTPCheckTaskHandler_Handle_StatusMismatch(t *testing.T) {
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound) // Return 404
	}))
	defer targetServer.Close()

	h := httpcheck.NewHTTPCheckTaskHandler()
	cfg := config.DefaultConfig()

	reqURL := fmt.Sprintf("/http_check?target_url=%s&expected_status=200", targetServer.URL)
	req := httptest.NewRequest("GET", reqURL, nil)

	metricContent, statusCode, err := h.Handle(req.Context(), req, cfg)

	if err != nil {
		t.Fatalf("Expected no error from handler itself, got %v (error should be reflected in metrics)", err)
	}
	if statusCode != http.StatusOK { // The handler itself should return OK, the check result is in metrics
		t.Errorf("Expected handler status code %d, got %d", http.StatusOK, statusCode)
	}

	metricStr := string(metricContent)
	t.Logf("Metrics returned (StatusMismatch):\n%s", metricStr)

	expectedMetrics := []string{
		fmt.Sprintf(`http_check_up{target_url="%s", method="GET", status_code="404"} 0`, targetServer.URL),
		fmt.Sprintf(`http_check_duration_seconds{target_url="%s", method="GET", status_code="404"}`, targetServer.URL),
		fmt.Sprintf(`http_check_status_code{target_url="%s", method="GET", status_code="404"} 404`, targetServer.URL),
	}

	for _, expected := range expectedMetrics {
		if !strings.Contains(metricStr, expected) {
			t.Errorf("Expected metrics to contain %q, but it didn't. Metrics:\n%s", expected, metricStr)
		}
	}
}

func TestHTTPCheckTaskHandler_Handle_TargetDown(t *testing.T) {
	h := httpcheck.NewHTTPCheckTaskHandler()
	cfg := config.DefaultConfig()

	// Use a non-existent URL
	nonExistentURL := "http://localhost:12345/shouldnotexist"
	reqURL := fmt.Sprintf("/http_check?target_url=%s", nonExistentURL)
	req := httptest.NewRequest("GET", reqURL, nil)

	metricContent, statusCode, err := h.Handle(req.Context(), req, cfg)

	if err == nil {
		t.Fatalf("Expected an error when target is down, got nil")
	}
	if statusCode != http.StatusServiceUnavailable {
		t.Errorf("Expected status code %d, got %d", http.StatusServiceUnavailable, statusCode)
	}

	metricStr := string(metricContent)
	t.Logf("Metrics returned (TargetDown):\n%s", metricStr)

	// Check that the error is included in the metrics
	if !strings.Contains(metricStr, fmt.Sprintf(`http_check_up{target_url="%s", method="GET", error=`, nonExistentURL)) {
		t.Errorf("Expected metrics to contain an error label for target_url. Metrics:\n%s", metricStr)
	}
	if !strings.Contains(metricStr, `} 0`) { // up should be 0
		t.Errorf("Expected http_check_up to be 0. Metrics:\n%s", metricStr)
	}
}

func TestHTTPCheckTaskHandler_Handle_Timeout(t *testing.T) {
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond) // Delay to ensure timeout
		w.WriteHeader(http.StatusOK)
	}))
	defer targetServer.Close()

	h := httpcheck.NewHTTPCheckTaskHandler()
	cfg := config.DefaultConfig()
	cfg.HTTPCheckTaskTimeout = config.Duration(50 * time.Millisecond) // Set a short timeout in config

	reqURL := fmt.Sprintf("/http_check?target_url=%s", targetServer.URL)
	req := httptest.NewRequest("GET", reqURL, nil)

	metricContent, statusCode, err := h.Handle(req.Context(), req, cfg)

	if err == nil {
		t.Fatalf("Expected an error when request times out, got nil")
	}
	if statusCode != http.StatusGatewayTimeout { // Expecting Gateway Timeout due to context deadline exceeded
		t.Errorf("Expected status code %d, got %d", http.StatusGatewayTimeout, statusCode)
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("Expected error to be 'context deadline exceeded', got '%v'", err)
	}

	metricStr := string(metricContent)
	t.Logf("Metrics returned (Timeout):\n%s", metricStr)

	if !strings.Contains(metricStr, fmt.Sprintf(`http_check_up{target_url="%s", method="GET", error=`, targetServer.URL)) {
		t.Errorf("Expected metrics to contain an error label for timeout. Metrics:\n%s", metricStr)
	}
	if !strings.Contains(metricStr, `} 0`) { // up should be 0
		t.Errorf("Expected http_check_up to be 0. Metrics:\n%s", metricStr)
	}
}

func TestHTTPCheckTaskHandler_Handle_MissingTargetURL(t *testing.T) {
	h := httpcheck.NewHTTPCheckTaskHandler()
	cfg := config.DefaultConfig()

	req := httptest.NewRequest("GET", "/http_check?method=POST", nil) // Missing target_url

	_, statusCode, err := h.Handle(req.Context(), req, cfg)

	if err == nil {
		t.Fatal("Expected an error for missing target_url, got nil")
	}
	if statusCode != http.StatusBadRequest {
		t.Errorf("Expected status code %d, got %d", http.StatusBadRequest, statusCode)
	}
	if !strings.Contains(err.Error(), "missing required parameter: target_url") {
		t.Errorf("Expected error message to contain 'missing required parameter: target_url', got '%s'", err.Error())
	}
}

func TestHTTPCheckTaskHandler_Handle_InvalidExpectedStatus(t *testing.T) {
	h := httpcheck.NewHTTPCheckTaskHandler()
	cfg := config.DefaultConfig()

	reqURL := "/http_check?target_url=http://example.com&expected_status=notanumber"
	req := httptest.NewRequest("GET", reqURL, nil)

	_, statusCode, err := h.Handle(req.Context(), req, cfg)

	if err == nil {
		t.Fatal("Expected an error for invalid expected_status, got nil")
	}
	if statusCode != http.StatusBadRequest {
		t.Errorf("Expected status code %d, got %d", http.StatusBadRequest, statusCode)
	}
	if !strings.Contains(err.Error(), "invalid expected_status") {
		t.Errorf("Expected error message to contain 'invalid expected_status', got '%s'", err.Error())
	}
}

func TestHTTPCheckTaskHandler_Handle_InvalidTimeout(t *testing.T) {
	h := httpcheck.NewHTTPCheckTaskHandler()
	cfg := config.DefaultConfig()

	reqURL := "/http_check?target_url=http://example.com&timeout=notaduration"
	req := httptest.NewRequest("GET", reqURL, nil)

	_, statusCode, err := h.Handle(req.Context(), req, cfg)

	if err == nil {
		t.Fatal("Expected an error for invalid timeout, got nil")
	}
	if statusCode != http.StatusBadRequest {
		t.Errorf("Expected status code %d, got %d", http.StatusBadRequest, statusCode)
	}
	if !strings.Contains(err.Error(), "invalid timeout duration") {
		t.Errorf("Expected error message to contain 'invalid timeout duration', got '%s'", err.Error())
	}
}
