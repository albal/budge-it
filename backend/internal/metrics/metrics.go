// Package metrics exposes Prometheus instrumentation consumed by the
// OpenShift Cluster Observability Operator via a ServiceMonitor.
package metrics

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	HTTPDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "budgeit",
		Name:      "http_request_duration_seconds",
		Help:      "API request latency by route, method and status.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"route", "method", "status"})

	UploadsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "budgeit",
		Name:      "uploads_total",
		Help:      "Statement uploads accepted, by content type.",
	}, []string{"content_type"})

	JobsProcessed = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "budgeit",
		Name:      "processing_jobs_total",
		Help:      "Statement processing jobs finished, by outcome.",
	}, []string{"outcome"})

	JobDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "budgeit",
		Name:      "processing_job_duration_seconds",
		Help:      "Time to extract and categorize one statement (OCR included).",
		Buckets:   []float64{0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120},
	})

	TransactionsExtracted = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "budgeit",
		Name:      "transactions_extracted_total",
		Help:      "Transactions extracted from statements.",
	})

	QueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "budgeit",
		Name:      "processing_queue_depth",
		Help:      "Uploads waiting for a worker.",
	})
)

// GinMiddleware records request latency per templated route.
func GinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		HTTPDuration.WithLabelValues(route, c.Request.Method,
			strconv.Itoa(c.Writer.Status())).Observe(time.Since(start).Seconds())
	}
}
