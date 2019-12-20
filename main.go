package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	stdlog "log"
	"net/http"
	"net/http/pprof"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/oklog/run"
	"github.com/pkg/errors"
	promapi "github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/prompb"
)

type labelArg []prompb.Label

func (la *labelArg) String() string {
	ls := make([]string, len(*la))
	for i, l := range *la {
		ls[i] = l.Name + "=" + l.Value
	}

	return strings.Join(ls, ", ")
}

func (la *labelArg) Set(v string) error {
	labels := strings.Split(v, ",")
	lset := make([]prompb.Label, len(labels))

	for i, l := range labels {
		parts := strings.SplitN(l, "=", 2)
		if len(parts) != 2 {
			return errors.Errorf("unrecognized label %q", l)
		}

		if !model.LabelName.IsValid(model.LabelName(parts[0])) {
			return errors.Errorf("unsupported format for label %s", l)
		}

		val, err := strconv.Unquote(parts[1])
		if err != nil {
			return errors.Wrap(err, "unquote label value")
		}

		lset[i] = prompb.Label{Name: parts[0], Value: val}
	}

	*la = lset

	return nil
}

type options struct {
	WriteEndpoint *url.URL
	ReadAddress   *url.URL
	// ReadEndpoint     *url.URL
	Labels           labelArg
	Listen           string
	Name             string
	Token            string
	Period           time.Duration
	Duration         time.Duration
	Latency          time.Duration
	InitialQueryWait time.Duration
	SuccessThreshold float64
}

type metrics struct {
	remoteWriteRequests   *prometheus.CounterVec
	queryResponses        *prometheus.CounterVec
	metricValueDifference prometheus.Histogram
	reporterCounter       *prometheus.CounterVec
}

func main() {
	logger := log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
	logger = log.WithPrefix(logger, "ts", log.DefaultTimestampUTC)
	logger = log.WithPrefix(logger, "caller", log.DefaultCaller)

	opts, err := parseOptions()
	if err != nil {
		level.Error(logger).Log("msg", "option parse", "err", err)
		os.Exit(1)
	}

	reg := prometheus.NewRegistry()
	m := registerMetrics(reg, opts)
	ctx := context.Background()

	var g run.Group
	{
		// Signal chans must be buffered.
		sig := make(chan os.Signal, 1)
		g.Add(func() error {
			signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
			<-sig
			level.Info(logger).Log("msg", "caught interrupt")
			return nil
		}, func(_ error) {
			close(sig)

			success, errors := successerrors(logger, m)
			level.Info(logger).Log("msg", "number of requests", "success", success, "errors", errors)
		})
	}
	{
		logger := log.With(logger, "component", "http")
		router := http.NewServeMux()
		router.Handle("/metrics", promhttp.InstrumentMetricHandler(reg, promhttp.HandlerFor(reg, promhttp.HandlerOpts{})))
		router.HandleFunc("/debug/pprof/", pprof.Index)

		srv := &http.Server{Addr: opts.Listen, Handler: router}

		g.Add(func() error {
			level.Info(logger).Log("msg", "starting the HTTP server", "address", opts.Listen)
			return srv.ListenAndServe()
		}, func(err error) {
			if err == http.ErrServerClosed {
				level.Warn(logger).Log("msg", "internal server closed unexpectedly")
				return
			}
			level.Info(logger).Log("msg", "shutting down internal server")
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := srv.Shutdown(ctx); err != nil {
				stdlog.Fatal(err)
			}
		})
	}
	{
		logger := log.With(logger, "component", "writer")
		t := time.NewTicker(opts.Period)
		ctx, cancel := context.WithCancel(ctx)

		g.Add(func() error {
			level.Info(logger).Log("msg", "starting the remote-write client")
			for {
				select {
				case <-t.C:
					if err := write(ctx, opts.WriteEndpoint, opts.Token, generate(opts.Labels), m); err != nil {
						level.Error(logger).Log("msg", "failed to make request", "err", err)
					}
				case <-ctx.Done():
					return nil
				}
			}
		}, func(_ error) {
			t.Stop()
			cancel()
		})
	}
	{
		if opts.ReadAddress != nil {
			logger := log.With(logger, "component", "querier")
			t := time.NewTicker(opts.Period)

			var cancel func()
			if opts.Duration != 0 {
				ctx, cancel = context.WithTimeout(ctx, opts.Duration+opts.Period)
			} else {
				ctx, cancel = context.WithCancel(ctx)
			}

			g.Add(func() error {
				level.Info(logger).Log("msg", "waiting for a single period before querying for metrics")
				time.Sleep(opts.InitialQueryWait)

				level.Info(logger).Log("msg", "start querying for metrics")
				for {
					select {
					case <-t.C:
						if err := read(ctx, opts.ReadAddress, opts.Labels, -1*(opts.InitialQueryWait-opts.Period), opts.Latency, m); err != nil {
							m.queryResponses.WithLabelValues("error").Inc()
							level.Error(logger).Log("msg", "failed to query", "err", err)
						} else {
							m.queryResponses.WithLabelValues("success").Inc()
						}
					case <-ctx.Done():
						success, errors := successerrors(logger, m)
						ratio := success / (success + errors)

						if ratio < opts.SuccessThreshold {
							return fmt.Errorf("failed with less than %2.f%% success ratio - actual %2.f%%", opts.SuccessThreshold*100, ratio*100)
						}
						return nil
					}
				}
			}, func(err error) {
				t.Stop()
				cancel()
			})
		}
	}

	if err := g.Run(); err != nil {
		stdlog.Fatal(err)
	}

	level.Info(logger).Log("msg", "up completed its mission!")
}

func registerMetrics(reg *prometheus.Registry, opts options) metrics {
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
	}
	reg.MustRegister(
		prometheus.NewGoCollector(),
		prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
		m.remoteWriteRequests,
		m.queryResponses,
		m.metricValueDifference,
	)

	// Used for successerrors reporting.
	if opts.ReadAddress != nil {
		m.reporterCounter = m.queryResponses
	} else {
		m.reporterCounter = m.remoteWriteRequests
	}

	return m
}

func parseOptions() (options, error) {
	var (
		rawEndpointWrite string
		rawEndpointRead  string
	)

	opts := options{}

	flag.StringVar(&rawEndpointWrite, "endpoint-write", "", "The endpoint to which to make remote-write requests.")
	flag.StringVar(&rawEndpointRead, "endpoint-read", "", "The endpoint to which to make query requests to.")
	flag.Var(&opts.Labels, "labels", "The labels that should be applied to remote-write requests.")
	flag.StringVar(&opts.Listen, "listen", ":8080", "The address on which internal server runs.")
	flag.StringVar(&opts.Name, "name", "up", "The name of the metric to send in remote-write requests.")
	flag.StringVar(&opts.Token, "token", "", "The bearer token to set in the authorization header on remote-write requests.")
	flag.DurationVar(&opts.Period, "period", 5*time.Second, "The time to wait between remote-write requests.")
	flag.DurationVar(&opts.Duration, "duration", 5*time.Minute, "The duration of the up command to run until it stops.")
	flag.Float64Var(&opts.SuccessThreshold, "threshold", 0.9, "The percentage of successful requests needed to succeed overall. 0 - 1.")
	flag.DurationVar(&opts.Latency, "latency", 15*time.Second, "The maximum allowable latency between writing and reading.")
	flag.DurationVar(&opts.InitialQueryWait, "initial-query-wait", 5*time.Second, "The time to wait before executing first query.")
	flag.Parse()

	endpointWrite, err := url.ParseRequestURI(rawEndpointWrite)
	if err != nil {
		return opts, fmt.Errorf("--endpoint-write is invalid: %w", err)
	}

	opts.WriteEndpoint = endpointWrite

	var endpointRead *url.URL
	if rawEndpointRead != "" {
		endpointRead, err = url.ParseRequestURI(rawEndpointRead)
		if err != nil {
			return opts, fmt.Errorf("--endpoint-read is invalid: %w", err)
		}
	}

	opts.ReadAddress = endpointRead

	if opts.Latency <= opts.Period {
		return opts, errors.New("--latency cannot be less than period")
	}

	if opts.InitialQueryWait <= opts.Period {
		return opts, errors.New("--initial-query-wait cannot be less than period")
	}

	opts.Labels = append(opts.Labels, prompb.Label{
		Name:  "__name__",
		Value: opts.Name,
	})

	return opts, err
}

func read(ctx context.Context, endpoint fmt.Stringer, labels []prompb.Label, ago time.Duration, latency time.Duration, m metrics) error {
	client, err := promapi.NewClient(promapi.Config{Address: endpoint.String()})
	if err != nil {
		return err
	}

	a := promv1.NewAPI(client)

	labelSelectors := make([]string, len(labels))
	for i, label := range labels {
		labelSelectors[i] = fmt.Sprintf(`%s="%s"`, label.Name, label.Value)
	}

	query := fmt.Sprintf("{%s}", strings.Join(labelSelectors, ","))

	value, _, err := a.Query(ctx, query, time.Now().Add(ago))
	if err != nil {
		return err
	}

	vec := value.(model.Vector)
	if len(vec) != 1 {
		return fmt.Errorf("expected one metric, got %d", len(vec))
	}

	t := time.Unix(int64(vec[0].Value/1000), 0)

	diffSeconds := time.Since(t).Seconds()

	m.metricValueDifference.Observe(diffSeconds)

	if diffSeconds > latency.Seconds() {
		return fmt.Errorf("metric value is too old: %2.fs", diffSeconds)
	}

	return nil
}

func write(ctx context.Context, endpoint fmt.Stringer, token string, wreq proto.Message, m metrics) error {
	var (
		buf []byte
		err error
		req *http.Request
		res *http.Response
	)

	defer func() {
		if err != nil {
			m.remoteWriteRequests.WithLabelValues("error").Inc()
			return
		}
		m.remoteWriteRequests.WithLabelValues("success").Inc()
	}()

	buf, err = proto.Marshal(wreq)
	if err != nil {
		return errors.Wrap(err, "marshalling proto")
	}

	req, err = http.NewRequest("POST", endpoint.String(), bytes.NewBuffer(snappy.Encode(nil, buf)))
	if err != nil {
		return errors.Wrap(err, "creating request")
	}

	req.Header.Add("Authorization", "Bearer "+token)

	res, err = http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		return errors.Wrap(err, "making request")
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		err = errors.New(res.Status)
		return errors.Wrap(err, "non-200 status")
	}

	return nil
}

func successerrors(logger log.Logger, m metrics) (float64, float64) {
	metrics := make(chan prometheus.Metric, 2)
	m.reporterCounter.Collect(metrics)
	close(metrics)

	var success, errors float64

	for m := range metrics {
		m1 := &dto.Metric{}
		if err := m.Write(m1); err != nil {
			level.Warn(logger).Log(
				"msg", "cannot read success and error count from prometheus counter",
				"err", err,
			)
		}

		for _, l := range m1.Label {
			switch *l.Value {
			case "error":
				errors = m1.GetCounter().GetValue()
			case "success":
				success = m1.GetCounter().GetValue()
			}
		}
	}

	return success, errors
}

func generate(labels []prompb.Label) *prompb.WriteRequest {
	timestamp := time.Now().UnixNano() / int64(time.Millisecond)

	return &prompb.WriteRequest{
		Timeseries: []prompb.TimeSeries{
			{
				Labels: labels,
				Samples: []prompb.Sample{
					{
						Value:     float64(timestamp),
						Timestamp: timestamp,
					},
				},
			},
		},
	}
}
