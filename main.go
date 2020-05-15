package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
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
	"github.com/oklog/run"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/prompb"
	"github.com/prometheus/prometheus/promql/parser"
	"gopkg.in/yaml.v2"
)

const https = "https"

type TokenProvider interface {
	Get() (string, error)
}

type queriesFile struct {
	Queries []querySpec `yaml:"queries"`
}

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

type tlsOptions struct {
	Cert   string
	Key    string
	CACert string
}

type options struct {
	LogLevel          level.Option
	WriteEndpoint     *url.URL
	ReadEndpoint      *url.URL
	Labels            labelArg
	Listen            string
	Name              string
	Token             TokenProvider
	Queries           []querySpec
	Period            time.Duration
	Duration          time.Duration
	Latency           time.Duration
	InitialQueryDelay time.Duration
	SuccessThreshold  float64
	tls               tlsOptions
}

func main() {
	l := log.WithPrefix(log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr)), "name", "up")
	l = log.WithPrefix(l, "ts", log.DefaultTimestampUTC)
	l = log.WithPrefix(l, "caller", log.DefaultCaller)

	opts, err := parseFlags(l)
	if err != nil {
		level.Error(l).Log("msg", "could not parse command line flags", "err", err)
		os.Exit(1)
	}

	l = level.NewFilter(l, opts.LogLevel)
	l = log.WithPrefix(l, "caller", log.DefaultCaller)

	reg := prometheus.NewRegistry()
	m := registerMetrics(reg)

	// Error channel to gather failures
	ch := make(chan error, 2)

	g := &run.Group{}
	{
		// Signal chans must be buffered.
		sig := make(chan os.Signal, 1)
		g.Add(func() error {
			signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
			<-sig
			level.Info(l).Log("msg", "caught interrupt")
			return nil
		}, func(_ error) {
			close(sig)
		})
	}
	// Schedule HTTP server
	scheduleHTTPServer(l, opts, reg, g)

	ctx := context.Background()

	var cancel context.CancelFunc
	if opts.Duration != 0 {
		ctx, cancel = context.WithTimeout(ctx, opts.Duration)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}

	if opts.WriteEndpoint != nil {
		g.Add(func() error {
			l := log.With(l, "component", "writer")
			level.Info(l).Log("msg", "starting the writer")

			return runPeriodically(ctx, opts, m.remoteWriteRequests, l, ch, func(rCtx context.Context) {
				if err := write(rCtx, opts.WriteEndpoint, opts.Token, generate(opts.Labels), l, opts.tls); err != nil {
					m.remoteWriteRequests.WithLabelValues("error").Inc()
					level.Error(l).Log("msg", "failed to make request", "err", err)
				} else {
					m.remoteWriteRequests.WithLabelValues("success").Inc()
				}
			})
		}, func(_ error) {
			cancel()
		})
	}

	if opts.ReadEndpoint != nil && opts.WriteEndpoint != nil {
		g.Add(func() error {
			l := log.With(l, "component", "reader")
			level.Info(l).Log("msg", "starting the reader")

			// Wait for at least one period before start reading metrics.
			level.Info(l).Log("msg", "waiting for initial delay before querying for metrics")
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(opts.InitialQueryDelay):
			}

			level.Info(l).Log("msg", "start querying for metrics")

			return runPeriodically(ctx, opts, m.queryResponses, l, ch, func(rCtx context.Context) {
				if err := read(rCtx, opts.ReadEndpoint, opts.Token, opts.Labels, -1*opts.InitialQueryDelay, opts.Latency, m, l, opts.tls); err != nil {
					m.queryResponses.WithLabelValues("error").Inc()
					level.Error(l).Log("msg", "failed to query", "err", err)
				} else {
					m.queryResponses.WithLabelValues("success").Inc()
				}
			})
		}, func(_ error) {
			cancel()
		})
	}

	if opts.ReadEndpoint != nil && opts.Queries != nil {
		addCustomQueryRunGroup(ctx, g, l, opts, m, cancel)
	}

	if err := g.Run(); err != nil {
		level.Info(l).Log("msg", "run group exited with error", "err", err)
	}

	close(ch)

	fail := false
	for err := range ch {
		fail = true

		level.Error(l).Log("err", err)
	}

	if fail {
		level.Error(l).Log("msg", "up failed")
		os.Exit(1)
	}

	level.Info(l).Log("msg", "up completed its mission!")
}

func addCustomQueryRunGroup(ctx context.Context, g *run.Group, l log.Logger, opts options, m metrics, cancel func()) {
	g.Add(func() error {
		l := log.With(l, "component", "query-reader")
		level.Info(l).Log("msg", "starting the reader for queries")

		// Wait for at least one period before start reading metrics.
		level.Info(l).Log("msg", "waiting for initial delay before querying specified queries")
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(opts.InitialQueryDelay):
		}

		level.Info(l).Log("msg", "start querying for specified queries")

		for {
			select {
			case <-ctx.Done():
				return nil
			default:
				for _, q := range opts.Queries {
					select {
					case <-ctx.Done():
						return nil
					default:
						t := time.Now()
						warn, err := query(
							ctx,
							l,
							opts.ReadEndpoint,
							opts.Token,
							q,
							opts.tls,
						)
						duration := time.Since(t).Seconds()
						if err != nil {
							level.Info(l).Log(
								"msg", "failed to execute specified query",
								"name", q.Name,
								"duration", duration,
								"warnings", fmt.Sprintf("%#+v", warn),
								"err", err,
							)
							m.customQueryErrors.WithLabelValues(q.Name).Inc()
						} else {
							level.Debug(l).Log("msg", "successfully executed specified query",
								"name", q.Name,
								"duration", duration,
								"warnings", fmt.Sprintf("%#+v", warn),
							)
							m.customQueryLastDuration.WithLabelValues(q.Name).Set(duration)
						}
						m.customQueryExecuted.WithLabelValues(q.Name).Inc()
					}
					time.Sleep(100 * time.Millisecond)
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
	}, func(_ error) {
		cancel()
	})
}

func runPeriodically(ctx context.Context, opts options, c *prometheus.CounterVec, l log.Logger, ch chan error,
	f func(rCtx context.Context)) error {
	var (
		t        = time.NewTicker(opts.Period)
		deadline time.Time
		rCtx     context.Context
		rCancel  context.CancelFunc
	)

	for {
		select {
		case <-t.C:
			// NOTICE: Do not propagate parent context to prevent cancellation of in-flight request.
			// It will be cancelled after the deadline.
			deadline = time.Now().Add(opts.Period)
			rCtx, rCancel = context.WithDeadline(context.Background(), deadline)

			// Will only get scheduled once per period and guaranteed to get cancelled after deadline.
			go func() {
				defer rCancel() // Make sure context gets cancelled even if execution panics.

				f(rCtx)
			}()
		case <-ctx.Done():
			t.Stop()

			select {
			// If it gets immediately cancelled, zero value of deadline won't cause a lock!
			case <-time.After(time.Until(deadline)):
				rCancel()
			case <-rCtx.Done():
			}

			return reportResults(l, ch, c, opts.SuccessThreshold)
		}
	}
}

func reportResults(l log.Logger, ch chan error, c *prometheus.CounterVec, threshold float64) error {
	metrics := make(chan prometheus.Metric, 2)
	c.Collect(metrics)
	close(metrics)

	var success, errors float64

	for m := range metrics {
		m1 := &dto.Metric{}
		if err := m.Write(m1); err != nil {
			level.Warn(l).Log("msg", "cannot read success and error count from prometheus counter", "err", err)
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

	level.Info(l).Log("msg", "number of requests", "success", success, "errors", errors)

	ratio := success / (success + errors)
	if ratio < threshold {
		level.Error(l).Log("msg", "ratio is below threshold")

		err := fmt.Errorf("failed with less than %2.f%% success ratio - actual %2.f%%", threshold*100, ratio*100)
		ch <- err

		return err
	}

	return nil
}

// Helpers

func parseFlags(l log.Logger) (options, error) {
	var (
		rawWriteEndpoint string
		rawReadEndpoint  string
		rawLogLevel      string
		queriesFileName  string
		tokenFile        string
		token            string
	)

	opts := options{}

	flag.StringVar(&rawLogLevel, "log.level", "info", "The log filtering level. Options: 'error', 'warn', 'info', 'debug'.")
	flag.StringVar(&rawWriteEndpoint, "endpoint-write", "", "The endpoint to which to make remote-write requests.")
	flag.StringVar(&rawReadEndpoint, "endpoint-read", "", "The endpoint to which to make query requests.")
	flag.Var(&opts.Labels, "labels", "The labels in addition to '__name__' that should be applied to remote-write requests.")
	flag.StringVar(&opts.Listen, "listen", ":8080", "The address on which internal server runs.")
	flag.StringVar(&opts.Name, "name", "up", "The name of the metric to send in remote-write requests.")
	flag.StringVar(&token, "token", "",
		"The bearer token to set in the authorization header on requests. Takes predence over --token-file if set.")
	flag.StringVar(&tokenFile, "token-file", "",
		"The file from which to read a bearer token to set in the authorization header on requests.")
	flag.StringVar(&queriesFileName, "queries-file", "", "A file containing queries to run against the read endpoint.")
	flag.DurationVar(&opts.Period, "period", 5*time.Second, "The time to wait between remote-write requests.")
	flag.DurationVar(&opts.Duration, "duration", 5*time.Minute,
		"The duration of the up command to run until it stops. If 0 it will not stop until the process is terminated.")
	flag.Float64Var(&opts.SuccessThreshold, "threshold", 0.9, "The percentage of successful requests needed to succeed overall. 0 - 1.")
	flag.DurationVar(&opts.Latency, "latency", 15*time.Second, "The maximum allowable latency between writing and reading.")
	flag.DurationVar(&opts.InitialQueryDelay, "initial-query-delay", 5*time.Second, "The time to wait before executing the first query.")
	flag.StringVar(&opts.tls.Cert, "tls-client-cert-file", "",
		"File containing the default x509 Certificate for HTTPS. Leave blank to disable TLS.")
	flag.StringVar(&opts.tls.Key, "tls-client-private-key-file", "",
		"File containing the default x509 private key matching --tls-cert-file. Leave blank to disable TLS.")
	flag.StringVar(&opts.tls.CACert, "tls-ca-file", "",
		"File containing the TLS CA to use against servers for verification. If no CA is specified, there won't be any verification.")
	flag.Parse()

	return buildOptionsFromFlags(l, opts, rawLogLevel, rawWriteEndpoint, rawReadEndpoint, queriesFileName, token, tokenFile)
}

func buildOptionsFromFlags(
	l log.Logger,
	opts options,
	rawLogLevel, rawWriteEndpoint, rawReadEndpoint, queriesFileName, token, tokenFile string,
) (options, error) {
	var err error

	switch rawLogLevel {
	case "error":
		opts.LogLevel = level.AllowError()
	case "warn":
		opts.LogLevel = level.AllowWarn()
	case "info":
		opts.LogLevel = level.AllowInfo()
	case "debug":
		opts.LogLevel = level.AllowDebug()
	default:
		panic("unexpected log level")
	}

	if rawWriteEndpoint != "" {
		writeEndpoint, err := url.ParseRequestURI(rawWriteEndpoint)
		if err != nil {
			return opts, fmt.Errorf("--endpoint-write is invalid: %w", err)
		}

		opts.WriteEndpoint = writeEndpoint
	} else {
		l.Log("msg", "no write endpoint specified, no write tests being performed")
	}

	if rawReadEndpoint != "" {
		var readEndpoint *url.URL
		if rawReadEndpoint != "" {
			readEndpoint, err = url.ParseRequestURI(rawReadEndpoint)
			if err != nil {
				return opts, fmt.Errorf("--endpoint-read is invalid: %w", err)
			}
		}

		opts.ReadEndpoint = readEndpoint
	} else {
		l.Log("msg", "no read endpoint specified, no read tests being performed")
	}

	if queriesFileName != "" {
		b, err := ioutil.ReadFile(queriesFileName)
		if err != nil {
			return opts, fmt.Errorf("--queries-file is invalid: %w", err)
		}

		qf := queriesFile{}
		err = yaml.Unmarshal(b, &qf)

		if err != nil {
			return opts, fmt.Errorf("--queries-file content is invalid: %w", err)
		}

		l.Log("msg", fmt.Sprintf("%d queries configured to be queried periodically", len(qf.Queries)))

		// validate queries
		for _, q := range qf.Queries {
			_, err = parser.ParseExpr(q.Query)
			if err != nil {
				return opts, fmt.Errorf("query %q in --queries-file content is invalid: %w", q.Name, err)
			}
		}

		opts.Queries = qf.Queries
	}

	if opts.Latency <= opts.Period {
		return opts, errors.New("--latency cannot be less than period")
	}

	opts.Labels = append(opts.Labels, prompb.Label{
		Name:  "__name__",
		Value: opts.Name,
	})

	opts.Token = tokenProvider(token, tokenFile)

	return opts, err
}

func tokenProvider(token, tokenFile string) TokenProvider {
	var res TokenProvider

	res = NewNoOpTokenProvider()
	if tokenFile != "" {
		res = NewFileToken(tokenFile)
	}

	if token != "" {
		res = NewStaticToken(token)
	}

	return res
}

func scheduleHTTPServer(l log.Logger, opts options, reg *prometheus.Registry, g *run.Group) {
	logger := log.With(l, "component", "http")
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
