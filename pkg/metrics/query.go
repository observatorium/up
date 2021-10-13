package metrics

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/observatorium/up/pkg/auth"
	"github.com/observatorium/up/pkg/options"
	"github.com/observatorium/up/pkg/transport"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	promapi "github.com/prometheus/client_golang/api"
	promapiv1 "github.com/prometheus/client_golang/api/prometheus/v1"
)

// Query executes a query specification, a set of queries, against Prometheus.
func Query(
	ctx context.Context,
	l log.Logger,
	endpoint *url.URL,
	t auth.TokenProvider,
	query options.Query,
	tls options.TLS,
	defaultStep time.Duration,
) (int, promapiv1.Warnings, error) {
	var (
		warn promapiv1.Warnings
		err  error
		rt   *auth.BearerTokenRoundTripper
	)

	level.Debug(l).Log("msg", "running specified query", "name", query.GetName(), "query", query.GetQuery())

	// Copy URL to avoid modifying the passed value.
	u := new(url.URL)
	*u = *endpoint
	if u.Scheme == transport.HTTPS {
		tp, err := transport.NewTLSTransport(l, tls)
		if err != nil {
			return 0, warn, errors.Wrap(err, "create round tripper")
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
		return 0, warn, err
	}

	return query.Run(ctx, c, l, rt.TraceID, defaultStep)
}
