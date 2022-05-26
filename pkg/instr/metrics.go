package instr

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	RemoteWriteRequests        *prometheus.CounterVec
	RemoteWriteRequestDuration prometheus.Histogram
	QueryResponses             *prometheus.CounterVec
	QueryResponseDuration      prometheus.Histogram
	MetricValueDifference      prometheus.Histogram
	CustomQueryExecuted        *prometheus.CounterVec
	CustomQueryErrors          *prometheus.CounterVec
	CustomQueryRequestDuration *prometheus.HistogramVec
	CustomQueryLastDuration    *prometheus.GaugeVec
}

func RegisterMetrics(reg *prometheus.Registry) Metrics {
	m := Metrics{
		RemoteWriteRequests: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "up_remote_writes_total",
			Help: "Total number of remote write requests.",
		}, []string{"result", "http_code"}),
		RemoteWriteRequestDuration: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name: "up_remote_writes_duration_seconds",
			Help: "Duration of remote write requests.",
		}),
		QueryResponses: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "up_queries_total",
			Help: "The total number of queries made.",
		}, []string{"result", "http_code"}),
		QueryResponseDuration: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name: "up_queries_duration_seconds",
			Help: "Duration of up queries.",
		}),
		MetricValueDifference: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "up_metric_value_difference",
			Help:    "The time difference between the current timestamp and the timestamp in the metrics value.",
			Buckets: prometheus.LinearBuckets(4, 0.25, 16),
		}),
		CustomQueryExecuted: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "up_custom_query_executed_total",
			Help: "The total number of custom specified queries executed.",
		}, []string{"type", "query", "http_code"}),
		CustomQueryRequestDuration: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Name: "up_custom_query_duration_seconds",
			Help: "Duration of custom specified queries",
			// We deliberately chose quite large buckets as we want to be able to accurately measure heavy queries.
			Buckets: []float64{0.1, 0.25, 0.5, 1, 5, 10, 20, 30, 45, 60, 100, 120},
		}, []string{"type", "query", "http_code"}),
		CustomQueryErrors: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "up_custom_query_errors_total",
			Help: "The total number of custom specified queries executed.",
		}, []string{"type", "query", "http_code"}),
		CustomQueryLastDuration: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name: "up_custom_query_last_duration",
			Help: "The duration of the query execution last time the query was executed successfully.",
		}, []string{"type", "query", "http_code"}),
	}

	return m
}
