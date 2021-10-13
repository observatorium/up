package options

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/observatorium/up/pkg/api"
	promapi "github.com/prometheus/client_golang/api"
	promapiv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

const (
	// Labels for query types.
	labelQuery      = "query"
	labelQueryRange = "query_range"
	labelSeries     = "series"
	labelNames      = "label_names"
	labelValues     = "label_values"
)

// Query represents different types of queries.
type Query interface {
	// GetName gets the name of the query.
	GetName() string
	// GetType gets the query type.
	GetType() string
	// GetQuery gets the query statement (promql) or label/matchers of the query.
	GetQuery() string
	// Run executes the query.
	Run(ctx context.Context, c promapi.Client, logger log.Logger, traceID string,
		defaultStep time.Duration) (int, promapiv1.Warnings, error)
}

type QuerySpec struct {
	Name     string         `yaml:"name"`
	Query    string         `yaml:"query"`
	Duration model.Duration `yaml:"duration,omitempty"`
	Step     time.Duration  `yaml:"step,omitempty"`
	Cache    bool           `yaml:"cache,omitempty"`
}

func (q QuerySpec) GetName() string {
	return q.Name
}

func (q QuerySpec) GetType() string {
	if q.Duration > 0 {
		return labelQueryRange
	}
	return labelQuery
}

func (q QuerySpec) GetQuery() string { return q.Query }

func (q QuerySpec) Run(ctx context.Context, c promapi.Client, logger log.Logger, traceID string,
	defaultStep time.Duration) (int, promapiv1.Warnings, error) {
	var (
		warn promapiv1.Warnings
		err  error
	)
	if q.Duration > 0 {
		step := defaultStep
		if q.Step > 0 {
			step = q.Step
		}

		_, httpCode, warn, err := api.QueryRange(ctx, c, q.Query, promapiv1.Range{
			Start: time.Now().Add(-time.Duration(q.Duration)),
			End:   time.Now(),
			Step:  step,
		}, q.Cache)
		if err != nil {
			err = fmt.Errorf("querying: %w", err)
			return httpCode, warn, err
		}

		// Don't log response in range query case because there are a lot.
		level.Debug(logger).Log("msg", "request finished", "name", q.Name, "trace-id", traceID)
		return httpCode, warn, err
	}

	_, httpCode, warn, err := api.Query(ctx, c, q.Query, time.Now(), q.Cache)
	if err != nil {
		err = fmt.Errorf("querying: %w", err)
		return httpCode, warn, err
	}
	level.Debug(logger).Log("msg", "request finished", "name", q.Name, "response code ", httpCode, "trace-id", traceID)

	return httpCode, warn, err
}

type LabelSpec struct {
	Name     string         `yaml:"name"`
	Label    string         `yaml:"label"`
	Duration model.Duration `yaml:"duration"`
	Cache    bool           `yaml:"cache"`
}

func (q LabelSpec) GetName() string { return q.Name }

func (q LabelSpec) GetType() string {
	if len(q.Label) > 0 {
		return labelValues
	}
	return labelNames
}

func (q LabelSpec) GetQuery() string { return q.Label }

func (q LabelSpec) Run(ctx context.Context, c promapi.Client, logger log.Logger, traceID string,
	_ time.Duration) (int, promapiv1.Warnings, error) {
	var (
		warn     promapiv1.Warnings
		err      error
		httpCode int
	)
	if len(q.Label) > 0 {
		_, httpCode, warn, err = api.LabelValues(ctx, c, q.Label, time.Now().Add(-time.Duration(q.Duration)), time.Now(), q.Cache)
	} else {
		_, httpCode, warn, err = api.LabelNames(ctx, c, time.Now().Add(-time.Duration(q.Duration)), time.Now(), q.Cache)
	}
	if err != nil {
		err = fmt.Errorf("querying: %w", err)
		return httpCode, warn, err
	}

	// Don't log responses because there are a lot.
	level.Debug(logger).Log("msg", "request finished", "name", q.Name, "trace-id", traceID)
	return httpCode, warn, err
}

type SeriesSpec struct {
	Name     string         `yaml:"name"`
	Matchers []string       `yaml:"matchers"`
	Duration model.Duration `yaml:"duration"`
	Cache    bool           `yaml:"cache"`
}

func (q SeriesSpec) GetName() string { return q.Name }

func (q SeriesSpec) GetType() string { return labelSeries }

func (q SeriesSpec) GetQuery() string { return strings.Join(q.Matchers, ", ") }

func (q SeriesSpec) Run(ctx context.Context, c promapi.Client, logger log.Logger, traceID string,
	_ time.Duration) (int, promapiv1.Warnings, error) {
	_, httpCode, warn, err := api.Series(ctx, c, q.Matchers, time.Now().Add(-time.Duration(q.Duration)), time.Now(), q.Cache)
	if err != nil {
		err = fmt.Errorf("querying: %w", err)
		return httpCode, warn, err
	}

	// Don't log responses because there are a lot.
	level.Debug(logger).Log("msg", "request finished", "name", q.Name, "trace-id", traceID)
	return httpCode, warn, err
}
