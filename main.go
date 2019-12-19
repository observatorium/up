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

var (
	remoteWriteRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "up_remote_writes_total",
		Help: "Total number of remote write requests.",
	}, []string{"result"})
	queryResponses = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "up_queries_total",
		Help: "The total number of queries made",
	}, []string{"result"})
	metricValueDifference = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "up_metric_value_difference",
		Help:    "The time difference of the current time stamp and the time stamp in the metrics value",
		Buckets: prometheus.LinearBuckets(4, 0.25, 16),
	})
)

type labelArg []prompb.Label

func (la *labelArg) String() string {

	var ls []string
	for _, l := range *la {
		ls = append(ls, l.Name+"="+l.Value)
	}
	return strings.Join(ls, ", ")
}

func (la *labelArg) Set(v string) error {
	var lset []prompb.Label
	for _, l := range strings.Split(v, ",") {
		parts := strings.SplitN(l, "=", 2)
		if len(parts) != 2 {
			return errors.Errorf("unrecognized label %q", l)
		}
		if !model.LabelName.IsValid(model.LabelName(string(parts[0]))) {
			return errors.Errorf("unsupported format for label %s", l)
		}
		val, err := strconv.Unquote(parts[1])
		if err != nil {
			return errors.Wrap(err, "unquote label value")
		}
		lset = append(lset, prompb.Label{Name: parts[0], Value: val})
	}
	*la = lset
	return nil
}

func main() {
	opts := struct {
		EndpointWrite   string
		EndpointRead    string
		Labels          labelArg
		Listen          string
		Name            string
		Token           string
		Period          time.Duration
		Duration        time.Duration
		SuccessTreshold float64
		Latency         time.Duration
	}{}

	flag.StringVar(&opts.EndpointWrite, "endpoint-write", "", "The endpoint to which to make remote-write requests.")
	flag.StringVar(&opts.EndpointRead, "endpoint-read", "", "The endpoint to which to make query requests to.")
	flag.Var(&opts.Labels, "labels", "The labels that should be applied to remote-write requests.")
	flag.StringVar(&opts.Listen, "listen", ":8080", "The address on which internal server runs.")
	flag.StringVar(&opts.Name, "name", "up", "The name of the metric to send in remote-write requests.")
	flag.StringVar(&opts.Token, "token", "", "The bearer token to set in the authorization header on remote-write requests.")
	flag.DurationVar(&opts.Period, "period", 5*time.Second, "The time to wait between remote-write requests.")
	flag.DurationVar(&opts.Duration, "duration", 5*time.Minute, "The duration of the up command to run until it stops")
	flag.Float64Var(&opts.SuccessTreshold, "treshold", 0.9, "The percentage of successful requests needed to succeed overall. 0 - 1")
	flag.DurationVar(&opts.Latency, "latency", 15*time.Second, "The maximum allowable latency between writing and reading")
	flag.Parse()

	logger := log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
	logger = log.WithPrefix(logger, "ts", log.DefaultTimestampUTC)
	logger = log.WithPrefix(logger, "caller", log.DefaultCaller)

	endpointWrite, err := url.ParseRequestURI(opts.EndpointWrite)
	if err != nil {
		level.Error(logger).Log("msg", "--endpoint-write is invalid", "err", err)
		return
	}
	endpointRead, err := url.ParseRequestURI(opts.EndpointRead)
	if err != nil {
		level.Error(logger).Log("msg", "--endpoint-read is invalid", "err", err)
		return
	}

	opts.Labels = append(opts.Labels, prompb.Label{
		Name:  "__name__",
		Value: opts.Name,
	})

	reg := prometheus.NewRegistry()
	reg.MustRegister(
		prometheus.NewGoCollector(),
		prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
		remoteWriteRequests,
		queryResponses,
		metricValueDifference,
	)

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

			success, errors := successerrors()
			level.Info(logger).Log("msg", "number of requests", "success", success, "errors", errors)
		})
	}
	{
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
		t := time.NewTicker(opts.Period)
		ctx, cancel := context.WithCancel(ctx)

		g.Add(func() error {
			level.Info(logger).Log("msg", "starting the remote-write client")
			for {
				select {
				case <-t.C:
					if err := write(ctx, endpointWrite, opts.Token, generate(opts.Labels)); err != nil {
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
		var cancel func()

		if opts.Duration != 0 {
			ctx, cancel = context.WithTimeout(ctx, opts.Duration)
		} else {
			ctx, cancel = context.WithCancel(ctx)
		}

		t := time.NewTicker(opts.Period)

		g.Add(func() error {
			level.Info(logger).Log("msg", "start querying for metrics")
			for {
				select {
				case <-t.C:
					if err := read(ctx, endpointRead, opts.Labels, opts.Latency); err != nil {
						queryResponses.WithLabelValues("error").Inc()
						level.Error(logger).Log("msg", "failed to query", "err", err)
					} else {
						queryResponses.WithLabelValues("success").Inc()
					}
				case <-ctx.Done():
					success, errors := successerrors()
					ratio := success / (success + errors)

					if ratio < opts.SuccessTreshold {
						return fmt.Errorf("failed with less than %2.f%% success ratio - actual %2.f%%", opts.SuccessTreshold*100, ratio*100)
					}
					return nil
				}
			}
		}, func(err error) {
			t.Stop()
			cancel()
		})
	}

	if err := g.Run(); err != nil {
		stdlog.Fatal(err)
	}
}

func read(ctx context.Context, endpoint *url.URL, labels []prompb.Label, latency time.Duration) error {
	client, err := promapi.NewClient(promapi.Config{Address: endpoint.String()})
	if err != nil {
		return err
	}
	a := promv1.NewAPI(client)

	var labelSelectors []string
	for _, label := range labels {
		labelSelectors = append(labelSelectors, fmt.Sprintf(`%s="%s"`, label.Name, label.Value))
	}
	query := fmt.Sprintf("{%s}", strings.Join(labelSelectors, ","))

	value, _, err := a.Query(ctx, query, time.Now().Add(-5*time.Second))
	if err != nil {
		return err
	}

	vec := value.(model.Vector)
	if len(vec) != 1 {
		return fmt.Errorf("expected one metric, got %d", len(vec))
	}

	t := time.Unix(int64(vec[0].Value/1000), 0)

	diffSeconds := time.Since(t).Seconds()

	metricValueDifference.Observe(diffSeconds)

	if diffSeconds > latency.Seconds() {
		return fmt.Errorf("metric value is too old: %2.fs", diffSeconds)
	}

	return nil
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

func write(ctx context.Context, endpoint *url.URL, token string, wreq *prompb.WriteRequest) error {
	var (
		buf []byte
		err error
		req *http.Request
		res *http.Response
	)
	defer func() {
		if err != nil {
			remoteWriteRequests.WithLabelValues("error").Inc()
			return
		}
		remoteWriteRequests.WithLabelValues("success").Inc()
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
	if res.StatusCode != http.StatusOK {
		err = errors.New(res.Status)
		return errors.Wrap(err, "non-200 status")
	}
	return nil
}

func successerrors() (float64, float64) {
	metrics := make(chan prometheus.Metric, 2)
	queryResponses.Collect(metrics)
	close(metrics)

	var success, errors float64

	for m := range metrics {
		m1 := &dto.Metric{}
		_ = m.Write(m1)
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
