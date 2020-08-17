package instr

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	RemoteWriteRequests     *prometheus.CounterVec
	QueryResponses          *prometheus.CounterVec
	MetricValueDifference   prometheus.Histogram
	CustomQueryExecuted     *prometheus.CounterVec
	CustomQueryErrors       *prometheus.CounterVec
	CustomQueryLastDuration *prometheus.GaugeVec
}

func RegisterMetrics(reg *prometheus.Registry) Metrics {
	m := Metrics{
		RemoteWriteRequests: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "up_remote_writes_total",
			Help: "Total number of remote write requests.",
		}, []string{"result"}),
		QueryResponses: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "up_queries_total",
			Help: "The total number of queries made.",
		}, []string{"result"}),
		MetricValueDifference: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "up_metric_value_difference",
			Help:    "The time difference between the current timestamp and the timestamp in the metrics value.",
			Buckets: prometheus.LinearBuckets(4, 0.25, 16),
		}),
		CustomQueryExecuted: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "up_custom_query_executed_total",
			Help: "The total number of custom specified queries executed.",
		}, []string{"type", "query"}),
		CustomQueryErrors: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "up_custom_query_errors_total",
			Help: "The total number of custom specified queries executed.",
		}, []string{"type", "query"}),
		CustomQueryLastDuration: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name: "up_custom_query_last_duration",
			Help: "The duration of the query execution last time the query was executed successfully.",
		}, []string{"type", "query"}),
	}

	return m
}
