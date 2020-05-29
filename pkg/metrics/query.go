package metrics

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/observatorium/up/pkg/auth"
	"github.com/observatorium/up/pkg/options"
	"github.com/observatorium/up/pkg/transport"
	"github.com/pkg/errors"
	promapi "github.com/prometheus/client_golang/api"
	promapiv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

type instantQueryRoundTripper struct {
	l       log.Logger
	r       http.RoundTripper
	t       auth.TokenProvider
	TraceID string
}

func newInstantQueryRoundTripper(l log.Logger, t auth.TokenProvider, r http.RoundTripper) *instantQueryRoundTripper {
	if r == nil {
		r = http.DefaultTransport
	}

	return &instantQueryRoundTripper{
		l: l,
		t: t,
		r: r,
	}
}

func (r *instantQueryRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := r.t.Get()
	if err != nil {
		return nil, err
	}

	if token != "" {
		req.Header.Add("Authorization", "Bearer "+token)
	}

	resp, err := r.r.RoundTrip(req)
	if err != nil {
		return resp, err
	}

	r.TraceID = resp.Header.Get("X-Thanos-Trace-Id")

	return resp, err
}

func Query(
	ctx context.Context,
	l log.Logger,
	endpoint *url.URL,
	t auth.TokenProvider,
	query options.QuerySpec,
	tls options.TLS,
) (promapiv1.Warnings, error) {
	var (
		warn promapiv1.Warnings
		err  error
		rt   *instantQueryRoundTripper
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

		rt = newInstantQueryRoundTripper(l, t, tp)
	} else {
		rt = newInstantQueryRoundTripper(l, t, nil)
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

	res, warn, err = a.Query(ctx, query.Query, time.Now())
	if err != nil {
		err = fmt.Errorf("querying: %w", err)
		return warn, err
	}

	level.Debug(l).Log("msg", "request finished", "name", query.Name, "response", res.String(), "trace-id", rt.TraceID)

	return warn, err
}
