package metrics

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/observatorium/up/pkg/api"
	"github.com/observatorium/up/pkg/auth"
	"github.com/observatorium/up/pkg/options"
	"github.com/observatorium/up/pkg/transport"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	promapi "github.com/prometheus/client_golang/api"
	promapiv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// Query executes a query specification, a set of queries, against Prometheus.
func Query(
	ctx context.Context,
	l log.Logger,
	endpoint *url.URL,
	t auth.TokenProvider,
	query options.QuerySpec,
	tls options.TLS,
	defaultStep time.Duration,
) (promapiv1.Warnings, error) {
	var (
		warn promapiv1.Warnings
		err  error
		rt   *auth.BearerTokenRoundTripper
	)

	level.Debug(l).Log("msg", "running specified query", "name", query.Name, "query", query.Query)

	// Copy URL to avoid modifying the passed value.
	u := new(url.URL)
	*u = *endpoint
	u.Path = ""

	if u.Scheme == transport.HTTPS {
		tp, err := transport.NewTLSTransport(l, tls)
		if err != nil {
			return warn, errors.Wrap(err, "create round tripper")
		}

		rt = auth.NewBearerTokenRoundTripper(l, t, tp)
	} else {
		rt = auth.NewBearerTokenRoundTripper(l, t, nil)
	}

	c, err := promapi.NewClient(promapi.Config{
		Address:      u.String(),
		RoundTripper: rt,
	})
	if err != nil {
		err = fmt.Errorf("create new API client: %w", err)
		return warn, err
	}

	a := promapiv1.NewAPI(c)

	var res model.Value

	if query.Duration > 0 {
		step := defaultStep
		if query.Step > 0 {
			step = query.Step
		}

		_, warn, err := api.QueryRange(ctx, c, query.Query, promapiv1.Range{
			Start: time.Now().Add(-time.Duration(query.Duration)),
			End:   time.Now(),
			Step:  step,
		}, query.Cache)
		if err != nil {
			err = fmt.Errorf("querying: %w", err)
			return warn, err
		}

		// Don't log response in range query case because there are a lot.
		level.Debug(l).Log("msg", "request finished", "name", query.Name, "trace-id", rt.TraceID)
		return warn, err
	}

	res, warn, err = a.Query(ctx, query.Query, time.Now())
	if err != nil {
		err = fmt.Errorf("querying: %w", err)
		return warn, err
	}

	level.Debug(l).Log("msg", "request finished", "name", query.Name, "response", res.String(), "trace-id", rt.TraceID)

	return warn, err
}
