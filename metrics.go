package main

import "github.com/prometheus/client_golang/prometheus"

type metrics struct {
	remoteWriteRequests     *prometheus.CounterVec
	queryResponses          *prometheus.CounterVec
	metricValueDifference   prometheus.Histogram
	customQueryExecuted     *prometheus.CounterVec
	customQueryErrors       *prometheus.CounterVec
	customQueryLastDuration *prometheus.GaugeVec
}

func registerMetrics(reg *prometheus.Registry) metrics {
	m := metrics{
		remoteWriteRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "up_remote_writes_total",
			Help: "Total number of remote write requests.",
		}, []string{"result"}),
		queryResponses: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "up_queries_total",
			Help: "The total number of queries made.",
		}, []string{"result"}),
		metricValueDifference: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "up_metric_value_difference",
			Help:    "The time difference between the current timestamp and the timestamp in the metrics value.",
			Buckets: prometheus.LinearBuckets(4, 0.25, 16),
		}),
		customQueryExecuted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "up_custom_query_executed_total",
			Help: "The total number of custom specified queries executed.",
		}, []string{"query"}),
		customQueryErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "up_custom_query_errors_total",
			Help: "The total number of custom specified queries executed.",
		}, []string{"query"}),
		customQueryLastDuration: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "up_custom_query_last_duration",
			Help: "The duration of the query execution last time the query was executed successfully.",
		}, []string{"query"}),
	}
	reg.MustRegister(
		prometheus.NewGoCollector(),
		prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
		m.remoteWriteRequests,
		m.queryResponses,
		m.metricValueDifference,
		m.customQueryExecuted,
		m.customQueryErrors,
		m.customQueryLastDuration,
	)

	return m
}
