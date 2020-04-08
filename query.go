package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	promapi "github.com/prometheus/client_golang/api"
	promapiv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

type instantQueryRoundTripper struct {
	l       log.Logger
	r       http.RoundTripper
	t       TokenProvider
	TraceID string
}

func newInstantQueryRoundTripper(l log.Logger, t TokenProvider, r http.RoundTripper) *instantQueryRoundTripper {
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

type querySpec struct {
	Name  string `yaml:"name"`
	Query string `yaml:"query"`
}

func query(
	ctx context.Context,
	l log.Logger,
	endpoint *url.URL,
	t TokenProvider,
	query querySpec,
) (promapiv1.Warnings, error) {
	var (
		warn promapiv1.Warnings
		err  error
	)

	level.Debug(l).Log("msg", "running specified query", "name", query.Name, "query", query.Query)

	// Copy URL to avoid modifying the passed value.
	u := new(url.URL)
	*u = *endpoint
	u.Path = ""

	r := newInstantQueryRoundTripper(l, t, nil)

	c, err := promapi.NewClient(promapi.Config{
		Address:      u.String(),
		RoundTripper: r,
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

	level.Debug(l).Log("msg", "request finished", "name", query.Name, "response", res.String(), "trace-id", r.TraceID)

	return warn, err
}
