package server_test

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"job_runner/config"
	"job_runner/server"
	"job_runner/tests"
)

func TestServerIntegration(t *testing.T) {
	_, testDBPath, cleanup := tests.SetupTestDB(t) // dbConn not directly used here, get testDBPath
	defer cleanup()                                // Use the returned cleanup function

	cfg := config.DefaultConfig()
	cfg.HTTPPort = 0                      // Use a dynamic port for testing
	cfg.ConnOptions.PreparedStmts = false // Align with test DB setup

	srv := server.New(cfg)
	// The server's HandleRequest method can be used as the handler for httptest.NewServer
	testServer := httptest.NewServer(http.HandlerFunc(srv.HandleRequest))
	defer testServer.Close()

	// Test cases
	testCases := []struct {
		name             string
		endpoint         string
		query            string
		expectedCode     int
		expectedParts    []string
		isAppMetricsTest bool // Flag to indicate if this is testing the new /metrics endpoint
	}{
		{
			name:         "Health check",
			endpoint:     "/health",
			expectedCode: http.StatusOK,
			expectedParts: []string{
				"OK",
			},
		},
		{
			name:         "Root page",
			endpoint:     "/",
			expectedCode: http.StatusOK,
			expectedParts: []string{
				"Job Runner",
				"/sql endpoint", // Updated to reflect new /sql endpoint info
				"/metrics",      // Ensure the new /metrics endpoint is mentioned
			},
		},
		{
			name:         "SQL Query with table data",
			endpoint:     "/sql", // Changed from /metrics
			query:        fmt.Sprintf("type=sqlite&username=test&password=test&host=localhost&db=%s&query=SELECT%%20name,%%20rows%%20as%%20value%%20FROM%%20tables&value_column=value&metric_prefix=table", testDBPath),
			expectedCode: http.StatusOK,
			expectedParts: []string{
				`table{name="users"} 1250`,
				`table{name="orders"} 5432`,
				`table{name="categories"} 50`,
				`sql_query_status{query="SELECT name, rows as value FROM tables"} 1`,
			},
		},
		{
			name:         "SQL Query with table data and custom metric names",
			endpoint:     "/sql", // Changed from /metrics
			query:        fmt.Sprintf("type=sqlite&username=test&password=test&host=localhost&db=%s&query=SELECT%%20name,%%20rows%%20as%%20value%%20FROM%%20tables&value_column=value&metric_prefix=my_custom_data_metric", testDBPath),
			expectedCode: http.StatusOK,
			expectedParts: []string{
				`my_custom_data_metric{name="users"} 1250`,
				`my_custom_data_metric{name="orders"} 5432`,
				`my_custom_data_metric{name="products"} 842`,
				`my_custom_data_metric{name="categories"} 50`,
				`sql_query_status{query="SELECT name, rows as value FROM tables"} 1`,
			},
		},
		{
			name:         "SQL Query with SQL error",
			endpoint:     "/sql", // Changed from /metrics
			query:        fmt.Sprintf("type=sqlite&username=test&password=test&host=localhost&db=%s&query=SELECT%%20nonexistent_column%%20FROM%%20tables&value_column=value", testDBPath),
			expectedCode: http.StatusInternalServerError,
			expectedParts: []string{
				`sql_query_status{query="SELECT nonexistent_column FROM tables",error="Query error: execute query failed: SQL logic error: no such column: nonexistent_column (1)"} 0`, // Corrected for non-prepared path
			},
		},
		{
			name:         "SQL Query with DSN build error",
			endpoint:     "/sql", // Changed from /metrics
			query:        fmt.Sprintf("type=invalid_db_type&username=test&password=test&host=localhost&db=testdb&query=%s&value_column=value", url.QueryEscape("SELECT 1")),
			expectedCode: http.StatusBadRequest,
			expectedParts: []string{
				// Updated to match the actual error from db.BuildDSN more closely.
				// The key part is "failed to parse constructed DSN" and the problematic DSN string.
				`sql_query_status{query="SELECT 1",error="failed to parse constructed DSN: parse \"invalid_db_type://test:test@localhost:/testdb\": first path segment in URL cannot contain colon"} 0`,
			},
		},
		{
			name:         "SQL Query with missing parameters",
			endpoint:     "/sql", // Changed from /metrics
			query:        "type=sqlite",
			expectedCode: http.StatusBadRequest,
			expectedParts: []string{
				"Missing required parameter", // This error message comes from the handler
			},
		},
		{
			name:         "Application Metrics Endpoint",
			endpoint:     "/metrics",
			expectedCode: http.StatusOK,
			expectedParts: []string{
				`http_requests_total{handler="/health",method="GET",status_code="200"}`,  // From previous health check call
				`http_requests_total{handler="/",method="GET",status_code="200"}`,        // From previous root page call
				`http_requests_total{handler="/sql",method="GET",status_code="200"}`,     // From previous successful /sql calls
				`http_requests_total{handler="/metrics",method="GET",status_code="200"}`, // For this current call
			},
			isAppMetricsTest: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var url string
			if tc.query != "" {
				url = fmt.Sprintf("%s%s?%s", testServer.URL, tc.endpoint, tc.query) // Use initialized testServer
			} else {
				url = fmt.Sprintf("%s%s", testServer.URL, tc.endpoint) // Use initialized testServer
			}

			resp, err := http.Get(url)
			if err != nil {
				t.Fatalf("Failed to make request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tc.expectedCode {
				t.Errorf("Expected status code %d, got %d", tc.expectedCode, resp.StatusCode)
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("Failed to read response body: %v", err)
			}

			bodyStr := string(body)
			for _, part := range tc.expectedParts {
				if !strings.Contains(bodyStr, part) {
					t.Errorf("Expected response to contain %q, but it didn't. Response: %s", part, bodyStr)
				}
			}
		})
	}
}

func TestServerStress(t *testing.T) {
	dbConn, testDBPath, cleanup := tests.SetupTestDB(t) // dbConn IS used here for CreateLargeTable
	defer cleanup()                                     // Use the returned cleanup function

	cfg := config.DefaultConfig()
	cfg.HTTPPort = 0                                                 // Use a dynamic port
	cfg.ConnOptions.QueryTimeout = config.Duration(20 * time.Second) // Longer timeout for stress
	cfg.ConnOptions.ConnectTimeout = config.Duration(10 * time.Second)
	cfg.ConnOptions.PreparedStmts = false // Align with test DB setup

	srv := server.New(cfg)
	// The server's HandleRequest method can be used as the handler for httptest.NewServer
	testServer := httptest.NewServer(http.HandlerFunc(srv.HandleRequest))
	defer testServer.Close()

	// Create a large table for stress testing
	const largeTableRowCount = 100000
	const largeTableName = "large_test_table"
	tests.CreateLargeTable(t, dbConn.DB, largeTableName, largeTableRowCount)

	// Construct the query for the large table
	querySQL := fmt.Sprintf("SELECT name, value FROM %s", largeTableName)
	encodedQuery := url.QueryEscape(querySQL)

	requestURL := fmt.Sprintf("%s/sql?type=sqlite&username=test&password=test&host=localhost&db=%s&query=%s&value_column=value&metric_prefix=stress_test", testServer.URL, testDBPath, encodedQuery) // Changed endpoint to /sql, use initialized testServer

	t.Run("Stress test with large table", func(t *testing.T) {
		startTime := time.Now()

		resp, err := http.Get(requestURL)
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}
		defer resp.Body.Close()

		duration := time.Since(startTime)
		t.Logf("Request to /sql with %d rows took %s", largeTableRowCount, duration) // Changed endpoint to /sql

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status code %d, got %d", http.StatusOK, resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}

		// Check if the status metric is present and successful
		statusMetric := fmt.Sprintf(`sql_query_status{query="%s"} 1`, querySQL)
		if !strings.Contains(string(body), statusMetric) {
			t.Errorf("Expected response to contain successful status metric %q, but it didn't. Response: %s", statusMetric, string(body))
		}

		// Also check that the app metrics endpoint contains a counter for this successful /sql call
		appMetricsURL := fmt.Sprintf("%s/metrics", testServer.URL) // Use initialized testServer
		appResp, appErr := http.Get(appMetricsURL)
		if appErr != nil {
			t.Fatalf("Failed to make request to /metrics: %v", appErr)
		}
		defer appResp.Body.Close()
		appBody, appBodyErr := io.ReadAll(appResp.Body)
		if appBodyErr != nil {
			t.Fatalf("Failed to read /metrics response body: %v", appBodyErr)
		}
		expectedSQLCounter := fmt.Sprintf(`http_requests_total{handler="/sql",method="GET",status_code="200"}`)
		if !strings.Contains(string(appBody), expectedSQLCounter) {
			t.Errorf("/metrics response should contain counter %q. Response: %s", expectedSQLCounter, string(appBody))
		}

		// Optionally, check for a few data metrics if feasible, but avoid checking all 2 million.
		if !strings.Contains(string(body), "stress_test{name=\"item_0\"} 0") {
			t.Errorf("Response should contain at least the first data metric.")
		}

		// Check the last metric to ensure all rows were processed.
		lastItemName := fmt.Sprintf("item_%d", largeTableRowCount-1)
		expectedLastItemValue := float64(largeTableRowCount-1) * 1.1

		lastItemMetricRegex := fmt.Sprintf(`stress_test{name=%q} ([0-9\.]+)`, lastItemName)
		re := regexp.MustCompile(lastItemMetricRegex)
		matches := re.FindStringSubmatch(string(body))
		if len(matches) < 2 {
			t.Errorf("Last item metric stress_test{name=%q} not found or value not captured. Response: %s", lastItemName, string(body))
		} else {
			actualValueStr := matches[1]
			actualValueFloat, convErr := strconv.ParseFloat(actualValueStr, 64)
			if convErr != nil {
				t.Errorf("Could not convert actual value string %q to float: %v", actualValueStr, convErr)
			} else {
				// Compare floats with a small tolerance for precision issues
				const tolerance = 1e-9
				if math.Abs(actualValueFloat-expectedLastItemValue) > tolerance {
					t.Logf("Original expected value for item_%d was %f", largeTableRowCount-1, expectedLastItemValue)
					t.Errorf("Last item metric stress_test{name=%q} has value %f (string: %q), expected approximately %f.", lastItemName, actualValueFloat, actualValueStr, expectedLastItemValue)
				}
			}
		}

		t.Logf("Response body size: %d bytes", len(body))
	})
}
