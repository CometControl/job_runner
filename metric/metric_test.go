package metric_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"job_runner/metric"
	"job_runner/tests"

	"github.com/VictoriaMetrics/metrics"
)

func TestMetricGeneration(t *testing.T) {
	// Setup test database
	conn, _, cleanup := tests.SetupTestDB(t) // Get all 3 return values, path not used here
	defer cleanup()

	// Execute a query
	ctx := context.Background()
	rows, err := conn.ExecuteQuery(ctx, "SELECT name, value FROM metrics")
	if err != nil {
		t.Fatalf("Failed to execute query: %v", err)
	}
	defer rows.Close()

	// Create a metric generator
	generator := metric.NewGenerator("test_metric", "value")

	// Generate metrics
	metricSet := metrics.NewSet()
	err = generator.GenerateFromRows(metricSet, rows)
	if err != nil {
		t.Fatalf("Failed to generate metrics: %v", err)
	}

	// Write metrics to a buffer and check the output
	var buf bytes.Buffer
	metricSet.WritePrometheus(&buf)
	if err != nil {
		t.Fatalf("Failed to write metrics: %v", err)
	}

	output := buf.String()

	// Check that we have the expected metrics
	expectedMetrics := []string{
		`test_metric{name="cpu_usage"} `,
		`test_metric{name="memory_usage"} `,
		`test_metric{name="disk_usage"} `,
		`test_metric{name="network_in"} `,
		`test_metric{name="network_out"} `,
	}

	for _, expected := range expectedMetrics {
		if !strings.Contains(output, expected) {
			t.Errorf("Expected metric %q not found in output", expected)
		}
	}
}

func TestMetricGenerationWithDifferentValueColumn(t *testing.T) {
	// Setup test database
	conn, _, cleanup := tests.SetupTestDB(t) // Get all 3 return values, path not used here
	defer cleanup()

	// Execute a query with a different value column
	ctx := context.Background()
	rows, err := conn.ExecuteQuery(ctx, "SELECT name, size as my_value FROM tables")
	if err != nil {
		t.Fatalf("Failed to execute query: %v", err)
	}
	defer rows.Close()
	// Create a metric generator with custom value column
	generator := metric.NewGenerator("table_size", "my_value")

	// Generate metrics
	metricSet := metrics.NewSet()
	err = generator.GenerateFromRows(metricSet, rows)
	if err != nil {
		t.Fatalf("Failed to generate metrics: %v", err)
	}

	// Write metrics to a buffer and check the output
	var buf bytes.Buffer
	metricSet.WritePrometheus(&buf)
	if err != nil {
		t.Fatalf("Failed to write metrics: %v", err)
	}

	output := buf.String()

	// Check that we have the expected metrics
	expectedMetrics := []string{
		`table_size{name="users"} `,
		`table_size{name="orders"} `,
		`table_size{name="products"} `,
		`table_size{name="categories"} `,
	}

	for _, expected := range expectedMetrics {
		if !strings.Contains(output, expected) {
			t.Errorf("Expected metric %q not found in output", expected)
		}
	}
}

func TestMetricGenerationWithSpecialTypes(t *testing.T) {
	// Setup test database
	conn, _, cleanup := tests.SetupTestDB(t)
	defer cleanup()

	// Execute a query
	ctx := context.Background()
	rows, err := conn.ExecuteQuery(ctx, "SELECT name, guid_val, decimal_val, value FROM special_types_table")
	if err != nil {
		t.Fatalf("Failed to execute query: %v", err)
	}
	defer rows.Close()

	// Create a metric generator
	generator := metric.NewGenerator("special_metric", "value")

	// Generate metrics
	metricSet := metrics.NewSet()
	err = generator.GenerateFromRows(metricSet, rows)
	if err != nil {
		t.Fatalf("Failed to generate metrics: %v", err)
	}

	// Write metrics to a buffer and check the output
	var buf bytes.Buffer
	metricSet.WritePrometheus(&buf)
	if err != nil {
		t.Fatalf("Failed to write metrics: %v", err)
	}

	output := buf.String()
	// t.Logf("Generated metrics:\n%s", output) // Optional: Log output for debugging

	// Check that we have the expected metrics
	expectedMetrics := []string{
		`special_metric{name="item1",guid_val="f47ac10b-58cc-4372-a567-0e02b2c3d479",decimal_val="123.456"} 1`,
		`special_metric{name="item2",guid_val="01234567-89ab-cdef-fedc-ba9876543210",decimal_val="7890.12"} 2`,
	}

	for _, expected := range expectedMetrics {
		// Normalize whitespace in output and expected for more robust comparison
		normalizedOutput := strings.Join(strings.Fields(output), " ")
		normalizedExpected := strings.Join(strings.Fields(expected), " ")
		if !strings.Contains(normalizedOutput, normalizedExpected) {
			t.Errorf("Expected metric %q not found in output. Output:\n%s", normalizedExpected, output)
		}
	}
}
