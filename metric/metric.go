package metric

import (
	"database/sql"
	"fmt"
	"io"
	"log/slog" // Changed from log
	"strconv"
	"strings"

	dberrors "job_runner/errors"

	"github.com/VictoriaMetrics/metrics"
)

// Generator handles the generation of metrics from SQL query results
type Generator struct {
	MetricPrefix string
	ValueColumn  string
}

// NewGenerator creates a new metric generator
func NewGenerator(metricPrefix, valueColumn string) *Generator {
	if metricPrefix == "" {
		metricPrefix = "sql_query_result"
	}

	if valueColumn == "" {
		valueColumn = "value"
	}

	return &Generator{
		MetricPrefix: metricPrefix,
		ValueColumn:  valueColumn,
	}
}

// convertToFloat64 attempts to convert an interface{} to float64.
// It handles common numeric types and strings.
func convertToFloat64(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return f, true
		}
		slog.Warn("Could not parse string to float64", "value", v, "error", err)
		return 0, false
	case []byte:
		s := string(v)
		f, err := strconv.ParseFloat(s, 64)
		if err == nil {
			return f, true
		}
		slog.Warn("Could not parse []byte to float64", "value", s, "error", err)
		return 0, false
	default:
		// Try a general string conversion as a last resort
		strVal := fmt.Sprint(v)
		f, err := strconv.ParseFloat(strVal, 64)
		if err == nil {
			return f, true
		}
		slog.Warn("Unsupported type for float64 conversion", "type", fmt.Sprintf("%T", v), "value", strVal, "error", err)
		return 0, false
	}
}

// GenerateFromRows creates metrics from SQL query results
// It adds the generated metrics to the provided set.
func (g *Generator) GenerateFromRows(set *metrics.Set, rows *sql.Rows) error {
	columns, err := rows.Columns()
	if err != nil {
		return dberrors.NewQueryError(fmt.Sprintf("failed to get columns: %v", err))
	}

	// Find the value column index
	valueColIndex := -1
	for i, col := range columns {
		if strings.EqualFold(col, g.ValueColumn) {
			valueColIndex = i
			break
		}
	}
	if valueColIndex == -1 {
		return dberrors.NewQueryError(fmt.Sprintf("value column '%s' not found in result set", g.ValueColumn))
	}

	// Create a destination slice to scan into
	values := make([]interface{}, len(columns))
	for i := range values {
		values[i] = new(interface{})
	}

	// Iterate through rows
	for rows.Next() {
		if err := rows.Scan(values...); err != nil {
			return dberrors.NewQueryError(fmt.Sprintf("failed to scan row: %v", err))
		}

		// Build label string from all non-value columns
		var labelParts []string
		for i, col := range columns {
			if i != valueColIndex {
				val := *(values[i].(*interface{}))
				if val != nil {
					labelParts = append(labelParts, fmt.Sprintf("%s=%q", col, fmt.Sprint(val)))
				}
			}
		}

		// Get the value from the value column
		val := *(values[valueColIndex].(*interface{}))
		if val == nil {
			continue
		}

		// Create metric name with labels
		metricName := g.MetricPrefix
		if len(labelParts) > 0 {
			metricName += fmt.Sprintf("{%s}", strings.Join(labelParts, ","))
		}

		// Set the metric value using the helper function
		if floatVal, ok := convertToFloat64(val); ok {
			gauge := set.GetOrCreateGauge(metricName, nil) // Corrected: pass nil for the callback
			gauge.Set(floatVal)
		} else {
			// convertToFloat64 already logs a warning, so no additional logging here unless desired
			slog.Debug("Skipping metric due to conversion failure", "metricName", metricName, "originalValue", val)
		}
	}

	if err := rows.Err(); err != nil {
		return dberrors.NewQueryError(fmt.Sprintf("error iterating rows: %v", err))
	}

	return nil
}

// WriteMetrics writes the metric set to the provided writer in Prometheus format
// This function is deprecated, use metricSet.WritePrometheus(w) instead
func (g *Generator) WriteMetrics(w io.Writer, set *metrics.Set) error {
	set.WritePrometheus(w)
	return nil
}

// RecordQueryStatus records the status of a query execution.
// It creates a gauge metric with the given name.
// If an error occurs, it sets the value to 0 and adds an 'error' label with the error message.
// Otherwise, it sets the value to 1.
func RecordQueryStatus(set *metrics.Set, metricName string, query string, err error) {
	var statusValue float64 = 1
	labels := fmt.Sprintf(`{query=%q}`, query)

	if err != nil {
		statusValue = 0
		labels = fmt.Sprintf(`{query=%q,error=%q}`, query, err.Error())
	}

	fullMetricName := metricName + labels
	gauge := set.GetOrCreateGauge(fullMetricName, nil)
	gauge.Set(statusValue)
}

// RecordMetrics records metrics from a SQL query result
// This function seems to be a duplicate or an alternative way to generate metrics.
// It might be better to consolidate metric generation logic.
// For now, it's left as is but marked for review.
// TODO: Review and consolidate metric generation logic.
func RecordMetrics(set *metrics.Set, db *sql.DB, metricName, sqlQuery, valueColumn string) error {
	rows, err := db.Query(sqlQuery)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var v interface{}
		if err := rows.Scan(&v); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}

		// Set the metric value using the helper function
		if floatVal, ok := convertToFloat64(v); ok {
			gauge := set.GetOrCreateGauge(metricName, nil) // Corrected: pass nil for the callback
			gauge.Set(floatVal)
		} else {
			// convertToFloat64 already logs a warning
			slog.Debug("Skipping metric due to conversion failure in RecordMetrics", "metricName", metricName, "originalValue", v)
		}
	}
	return rows.Err()
}

// WriteMetrics writes the metrics in Prometheus format to the given writer.
func WriteMetrics(w io.Writer, set *metrics.Set) {
	set.WritePrometheus(w)
}
