package logs

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/observatorium/up/pkg/auth"
	"github.com/observatorium/up/pkg/options"
	"github.com/observatorium/up/pkg/transport"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"

	promapiv1 "github.com/prometheus/client_golang/api/prometheus/v1"
)

func Query(
	ctx context.Context,
	l log.Logger,
	endpoint *url.URL,
	t auth.TokenProvider,
	q options.Query,
	tls options.TLS,
	defaultStep time.Duration,
) (int, promapiv1.Warnings, error) {
	// TODO: avoid type casting when we need to support all query endpoints for logs.
	query, ok := q.(*options.QuerySpec)
	if !ok {
		return 0, nil, errors.New("Incorrect query type for logs queries")
	}

	level.Debug(l).Log("msg", "running specified query", "name", query.Name, "query", query.Query)

	var (
		rt   http.RoundTripper
		warn promapiv1.Warnings
		err  error
	)

	if endpoint.Scheme == transport.HTTPS {
		rt, err = transport.NewTLSTransport(l, tls)
		if err != nil {
			return 0, warn, errors.Wrap(err, "create round tripper")
		}

		rt = auth.NewBearerTokenRoundTripper(l, t, rt)
	} else {
		rt = auth.NewBearerTokenRoundTripper(l, t, nil)
	}

	client := &http.Client{Transport: rt}

	params := url.Values{}
	params.Add("query", query.Query)

	if query.Duration > 0 {
		step := defaultStep
		if query.Step > 0 {
			step = query.Step
		}

		params.Add("start", time.Now().Add(-time.Duration(query.Duration)).String())
		params.Add("end", time.Now().String())
		params.Add("step", step.String())
	}

	endpoint.RawQuery = params.Encode()

	req, err := http.NewRequest(http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return 0, warn, errors.Wrap(err, "creating request")
	}

	res, err := client.Do(req.WithContext(ctx))
	if err != nil {
		if res == nil {
			return 0, warn, errors.Wrap(err, "making request")
		}

		return res.StatusCode, warn, errors.Wrap(err, "making request")
	}

	if res.StatusCode != http.StatusOK {
		err = errors.Errorf(res.Status)
		return res.StatusCode, warn, errors.Wrap(err, "non-200 status")
	}

	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return res.StatusCode, warn, errors.Wrap(err, "reading response body")
	}

	rr := &queryResponse{}

	err = json.Unmarshal(body, rr)
	if err != nil {
		return res.StatusCode, warn, errors.Wrap(err, "unmarshalling response")
	}

	if len(rr.Data.Result) == 0 {
		return res.StatusCode, warn, errors.Errorf("expected at min one log entry, got none")
	}

	return res.StatusCode, warn, nil
}
