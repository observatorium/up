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
	"syscall"
	"time"

	"github.com/observatorium/up/pkg/auth"
	"github.com/observatorium/up/pkg/instr"
	"github.com/observatorium/up/pkg/logs"
	"github.com/observatorium/up/pkg/metrics"
	"github.com/observatorium/up/pkg/options"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/oklog/run"
	"github.com/pkg/errors"
	promapiv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/prompb"
	"github.com/prometheus/prometheus/promql/parser"
	"gopkg.in/yaml.v2"
)

const (
	numOfEndpoints        = 2
	timeoutBetweenQueries = 100 * time.Millisecond

	labelSuccess = "success"
	labelError   = "error"
)

type callsFile struct {
	Queries []options.QuerySpec  `yaml:"queries"`
	Labels  []options.LabelSpec  `yaml:"labels"`
	Series  []options.SeriesSpec `yaml:"series"`
}

type logsFile struct {
	Spec options.LogsSpec `yaml:"spec"`
}

func main() { //nolint:golint,funlen
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
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	m := instr.RegisterMetrics(reg)

	// Error channel to gather failures
	ch := make(chan error, numOfEndpoints)

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

			return runPeriodically(ctx, opts, m.RemoteWriteRequests, l, ch, func(rCtx context.Context) {
				t := time.Now()
				err := write(rCtx, l, opts)
				duration := time.Since(t).Seconds()
				m.RemoteWriteRequestDuration.Observe(duration)
				if err != nil {
					m.RemoteWriteRequests.WithLabelValues(labelError).Inc()
					level.Error(l).Log("msg", "failed to make request", "err", err)
				} else {
					m.RemoteWriteRequests.WithLabelValues(labelSuccess).Inc()
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
			level.Info(l).Log("msg", "waiting for initial delay before querying", "type", opts.EndpointType)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(opts.InitialQueryDelay):
			}

			level.Info(l).Log("msg", "start querying", "type", opts.EndpointType)

			return runPeriodically(ctx, opts, m.QueryResponses, l, ch, func(rCtx context.Context) {
				t := time.Now()
				httpCode, err := read(rCtx, l, m, opts)
				duration := time.Since(t).Seconds()
				m.QueryResponseDuration.Observe(duration)
				if err != nil {
					if httpCode != 0 {
						m.QueryResponses.WithLabelValues(labelError, strconv.Itoa(httpCode)).Inc()
					}
					level.Error(l).Log("msg", "failed to query", "err", err)
				} else {
					if httpCode != 0 {
						m.QueryResponses.WithLabelValues(labelSuccess, strconv.Itoa(httpCode)).Inc()
					}
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

func write(ctx context.Context, l log.Logger, opts options.Options) error {
	switch opts.EndpointType {
	case options.MetricsEndpointType:
		return metrics.Write(ctx, opts.WriteEndpoint, opts.Token, metrics.Generate(opts.Labels), l, opts.TLS,
			opts.TenantHeader, opts.Tenant)
	case options.LogsEndpointType:
		return logs.Write(ctx, opts.WriteEndpoint, opts.Token, logs.Generate(opts.Labels, opts.Logs), l, opts.TLS)
	}

	return nil
}

func read(ctx context.Context, l log.Logger, m instr.Metrics, opts options.Options) (int, error) {
	switch opts.EndpointType {
	case options.MetricsEndpointType:
		return metrics.Read(ctx, opts.ReadEndpoint, opts.Token, opts.Labels, -1*opts.InitialQueryDelay, opts.Latency, m, l, opts.TLS)
	case options.LogsEndpointType:
		return logs.Read(ctx, opts.ReadEndpoint, opts.Token, opts.Labels, -1*opts.InitialQueryDelay, opts.Latency, m, l, opts.TLS)
	}

	return 0, fmt.Errorf("invalid endpoint-type: %v", opts.EndpointType)
}

func query(ctx context.Context, l log.Logger, q options.Query, opts options.Options) (int, promapiv1.Warnings, error) {
	switch opts.EndpointType {
	case options.MetricsEndpointType:
		return metrics.Query(ctx, l, opts.ReadEndpoint, opts.Token, q, opts.TLS, opts.DefaultStep)
	case options.LogsEndpointType:
		return logs.Query(ctx, l, opts.ReadEndpoint, opts.Token, q, opts.TLS, opts.DefaultStep)
	}

	return 0, nil, fmt.Errorf("invalid endpoint-type: %v", opts.EndpointType)
}

func addCustomQueryRunGroup(ctx context.Context, g *run.Group, l log.Logger, opts options.Options, m instr.Metrics, cancel func()) {
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
						httpCode, warn, err := query(ctx, l, q, opts)
						duration := time.Since(t).Seconds()
						queryType := q.GetType()
						name := q.GetName()
						if err != nil {
							level.Info(l).Log(
								"msg", "failed to execute specified query",
								"type", queryType,
								"name", name,
								"duration", duration,
								"warnings", fmt.Sprintf("%#+v", warn),
								"err", err,
							)
							if httpCode != 0 {
								m.CustomQueryErrors.WithLabelValues(queryType, name, strconv.Itoa(httpCode)).Inc()
							}

						} else {
							level.Debug(l).Log("msg", "successfully executed specified query",
								"type", queryType,
								"name", name,
								"duration", duration,
								"warnings", fmt.Sprintf("%#+v", warn),
							)

							m.CustomQueryLastDuration.WithLabelValues(queryType, name, strconv.Itoa(httpCode)).Set(duration)
						}
						if httpCode != 0 {
							m.CustomQueryExecuted.WithLabelValues(queryType, name, strconv.Itoa(httpCode)).Inc()
							m.CustomQueryRequestDuration.WithLabelValues(queryType, name, strconv.Itoa(httpCode)).Observe(duration)
						}
					}
					time.Sleep(timeoutBetweenQueries)
				}
				time.Sleep(timeoutBetweenQueries)
			}
		}
	}, func(_ error) {
		cancel()
	})
}

func runPeriodically(ctx context.Context, opts options.Options, c *prometheus.CounterVec, l log.Logger, ch chan error,
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

			if rCtx != nil {
				select {
				// If it gets immediately cancelled, zero value of deadline won't cause a lock!
				case <-time.After(time.Until(deadline)):
					rCancel()
				case <-rCtx.Done():
				}
			}

			return reportResults(l, ch, c, opts.SuccessThreshold)
		}
	}
}

func reportResults(l log.Logger, ch chan error, c *prometheus.CounterVec, threshold float64) error {
	metrics := make(chan prometheus.Metric, numOfEndpoints)
	c.Collect(metrics)
	close(metrics)

	var success, failures float64

	for m := range metrics {
		m1 := &dto.Metric{}
		if err := m.Write(m1); err != nil {
			level.Warn(l).Log("msg", "cannot read success and error count from prometheus counter", "err", err)
		}

		for _, l := range m1.Label {
			switch *l.Value {
			case labelError:
				failures = m1.GetCounter().GetValue()
			case labelSuccess:
				success = m1.GetCounter().GetValue()
			}
		}
	}

	level.Info(l).Log("msg", "number of requests", "success", success, "errors", failures)

	ratio := success / (success + failures)
	if ratio < threshold {
		level.Error(l).Log("msg", "ratio is below threshold")

		err := errors.Errorf("failed with less than %2.f%% success ratio - actual %2.f%%", threshold*100, ratio*100)
		ch <- err

		return err
	}

	return nil
}

// Helpers

func parseFlags(l log.Logger) (options.Options, error) {
	var (
		rawEndpointType  string
		rawWriteEndpoint string
		rawReadEndpoint  string
		rawLogLevel      string
		queriesFileName  string
		logsFileName     string
		tokenFile        string
		token            string
	)

	opts := options.Options{}

	flag.StringVar(&rawLogLevel, "log.level", "info", "The log filtering level. Options: 'error', 'warn', 'info', 'debug'.")
	flag.StringVar(&rawEndpointType, "endpoint-type", "metrics", "The endpoint type. Options: 'logs', 'metrics'.")
	flag.StringVar(&rawWriteEndpoint, "endpoint-write", "", "The endpoint to which to make remote-write requests.")
	flag.StringVar(&rawReadEndpoint, "endpoint-read", "", "The endpoint to which to make query requests.")
	flag.Var(&opts.Labels, "labels", "The labels in addition to '__name__' that should be applied to remote-write requests.")
	flag.StringVar(&opts.Listen, "listen", ":8080", "The address on which internal server runs.")
	flag.Var(&opts.Logs, "logs", "The logs that should be sent to remote-write requests.")
	flag.StringVar(&logsFileName, "logs-file", "", "A file containing logs to send against the logs write endpoint.")
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
	flag.DurationVar(&opts.Latency, "latency", 15*time.Second,
		"The maximum allowable latency between writing and reading.")
	flag.DurationVar(&opts.InitialQueryDelay, "initial-query-delay", 10*time.Second,
		"The time to wait before executing the first query.")
	flag.DurationVar(&opts.DefaultStep, "step", 5*time.Minute, "Default step duration for range queries. "+
		"Can be overridden if step is set in query spec.")

	flag.StringVar(&opts.TLS.Cert, "tls-client-cert-file", "",
		"File containing the default x509 Certificate for HTTPS. Leave blank to disable TLS.")
	flag.StringVar(&opts.TLS.Key, "tls-client-private-key-file", "",
		"File containing the default x509 private key matching --tls-cert-file. Leave blank to disable TLS.")
	flag.StringVar(&opts.TLS.CACert, "tls-ca-file", "",
		"File containing the TLS CA to use against servers for verification. If no CA is specified, there won't be any verification.")
	flag.StringVar(&opts.TenantHeader, "tenant-header", "tenant_id",
		"Name of HTTP header used to determine tenant for write requests.")
	flag.StringVar(&opts.Tenant, "tenant", "", "Tenant ID to used to determine tenant for write requests.")
	flag.Parse()

	return buildOptionsFromFlags(
		l, opts, rawLogLevel, rawEndpointType, rawWriteEndpoint, rawReadEndpoint, queriesFileName, logsFileName, token, tokenFile,
	)
}

func buildOptionsFromFlags(
	l log.Logger,
	opts options.Options,
	rawLogLevel, rawEndpointType, rawWriteEndpoint, rawReadEndpoint, queriesFileName, logsFileName, token, tokenFile string,
) (options.Options, error) {
	var err error

	err = parseLogLevel(&opts, rawLogLevel)
	if err != nil {
		return opts, errors.Wrap(err, "parsing log level")
	}

	err = parseEndpointType(&opts, rawEndpointType)
	if err != nil {
		return opts, errors.Wrap(err, "parsing endpoint type")
	}

	err = parseWriteEndpoint(&opts, l, rawWriteEndpoint)
	if err != nil {
		return opts, errors.Wrap(err, "parsing write endpoint")
	}

	err = parseReadEndpoint(&opts, l, rawReadEndpoint)
	if err != nil {
		return opts, errors.Wrap(err, "parsing read endpoint")
	}

	err = parseQueriesFileName(&opts, l, queriesFileName)
	if err != nil {
		return opts, errors.Wrap(err, "parsing queries file name")
	}

	err = parseLogsFileName(&opts, l, logsFileName)
	if err != nil {
		return opts, errors.Wrap(err, "parsing logs file name")
	}

	if opts.Latency <= opts.Period {
		return opts, errors.Errorf("--latency cannot be less than period")
	}

	opts.Labels = append(opts.Labels, prompb.Label{
		Name:  "__name__",
		Value: opts.Name,
	})

	opts.Token = tokenProvider(token, tokenFile)

	return opts, err
}

func parseLogLevel(opts *options.Options, rawLogLevel string) error {
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
		return errors.Errorf("unexpected log level")
	}

	return nil
}

func parseEndpointType(opts *options.Options, rawEndpointType string) error {
	switch options.EndpointType(rawEndpointType) {
	case options.LogsEndpointType:
		opts.EndpointType = options.LogsEndpointType
	case options.MetricsEndpointType:
		opts.EndpointType = options.MetricsEndpointType
	default:
		return errors.Errorf("unexpected endpoint type")
	}

	return nil
}

func parseWriteEndpoint(opts *options.Options, l log.Logger, rawWriteEndpoint string) error {
	if rawWriteEndpoint != "" {
		writeEndpoint, err := url.ParseRequestURI(rawWriteEndpoint)
		if err != nil {
			return fmt.Errorf("--endpoint-write is invalid: %w", err)
		}

		opts.WriteEndpoint = writeEndpoint
	} else {
		l.Log("msg", "no write endpoint specified, no write tests being performed")
	}

	return nil
}

func parseReadEndpoint(opts *options.Options, l log.Logger, rawReadEndpoint string) error {
	if rawReadEndpoint != "" {
		readEndpoint, err := url.ParseRequestURI(rawReadEndpoint)
		if err != nil {
			return fmt.Errorf("--endpoint-read is invalid: %w", err)
		}

		opts.ReadEndpoint = readEndpoint
	} else {
		l.Log("msg", "no read endpoint specified, no read tests being performed")
	}

	return nil
}

func parseQueriesFileName(opts *options.Options, l log.Logger, queriesFileName string) error {
	if queriesFileName != "" {
		b, err := ioutil.ReadFile(queriesFileName)
		if err != nil {
			return fmt.Errorf("--queries-file is invalid: %w", err)
		}

		qf := callsFile{}
		err = yaml.Unmarshal(b, &qf)

		if err != nil {
			return fmt.Errorf("--queries-file content is invalid: %w", err)
		}

		l.Log("msg", fmt.Sprintf("%d queries configured to be queried periodically", len(qf.Queries)))

		// validate queries
		for _, q := range qf.Queries {
			_, err = parser.ParseExpr(q.Query)
			if err != nil {
				return fmt.Errorf("query %q in --queries-file content is invalid: %w", q.Name, err)
			}

			opts.Queries = append(opts.Queries, q)
		}

		for _, q := range qf.Series {
			if len(q.Matchers) == 0 {
				return fmt.Errorf("series query %q in --queries-file matchers cannot be empty", q.Name)
			}

			if len(q.Matchers) > 0 {
				for _, s := range q.Matchers {
					if _, err := parser.ParseMetricSelector(s); err != nil {
						return fmt.Errorf("series query %q in --queries-file matchers are invalid: %w", q.Name, err)
					}
				}
			}

			opts.Queries = append(opts.Queries, q)
		}

		for _, q := range qf.Labels {
			if len(q.Label) > 0 && !model.LabelNameRE.MatchString(q.Label) {
				return fmt.Errorf("label_values query %q in --queries-file label is invalid: %w", q.Name, err)
			}

			opts.Queries = append(opts.Queries, q)
		}
	}

	return nil
}

func parseLogsFileName(opts *options.Options, l log.Logger, logsFileName string) error {
	if logsFileName != "" {
		b, err := ioutil.ReadFile(logsFileName)
		if err != nil {
			return fmt.Errorf("--logs-file is invalid: %w", err)
		}

		lf := logsFile{}
		err = yaml.Unmarshal(b, &lf)

		if err != nil {
			return fmt.Errorf("--logs-file content is invalid: %w", err)
		}

		l.Log("msg", fmt.Sprintf("%d logs configured to be written periodically", len(lf.Spec.Logs)))

		opts.Logs = lf.Spec.Logs
	}

	return nil
}

func tokenProvider(token, tokenFile string) auth.TokenProvider {
	var res auth.TokenProvider

	res = auth.NewNoOpTokenProvider()
	if tokenFile != "" {
		res = auth.NewFileToken(tokenFile)
	}

	if token != "" {
		res = auth.NewStaticToken(token)
	}

	return res
}

func scheduleHTTPServer(l log.Logger, opts options.Options, reg *prometheus.Registry, g *run.Group) {
	logger := log.With(l, "component", "http")
	router := http.NewServeMux()
	router.Handle("/metrics", promhttp.InstrumentMetricHandler(reg, promhttp.HandlerFor(reg, promhttp.HandlerOpts{})))
	router.HandleFunc("/debug/pprof/", pprof.Index)

	srv := &http.Server{Addr: opts.Listen, Handler: router}

	g.Add(func() error {
		level.Info(logger).Log("msg", "starting the HTTP server", "address", opts.Listen)
		return srv.ListenAndServe()
	}, func(err error) {
		if errors.Is(err, http.ErrServerClosed) {
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
