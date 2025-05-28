package tasks

import (
	"context"
	"job_runner/config"
	"net/http"
)

// TaskHandler defines the interface for a component that can handle a specific type of task
// initiated by an HTTP request, process it, and return results suitable for metrics.
type TaskHandler interface {
	// Handle processes the incoming HTTP request, executes the task,
	// and returns the Prometheus-formatted metrics content, an HTTP status code, and any error.
	Handle(ctx context.Context, r *http.Request, appConfig config.Config) (metricContent []byte, httpStatusCode int, err error)
}
