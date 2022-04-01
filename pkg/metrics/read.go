package metrics

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/observatorium/up/pkg/api"
	"github.com/observatorium/up/pkg/auth"
	"github.com/observatorium/up/pkg/instr"
	"github.com/observatorium/up/pkg/options"
	"github.com/observatorium/up/pkg/transport"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	promapi "github.com/prometheus/client_golang/api"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/prompb"
)

// Read executes query against Prometheus with the same labels to retrieve the written metrics back.
func Read(
	ctx context.Context,
	endpoint *url.URL,
	tp auth.TokenProvider,
	labels []prompb.Label,
	ago, latency time.Duration,
	m instr.Metrics,
	l log.Logger,
	tls options.TLS,
) (int, error) {
	var (
		rt  http.RoundTripper
		err error
	)

	if endpoint.Scheme == transport.HTTPS {
		rt, err = transport.NewTLSTransport(l, tls)
		if err != nil {
			return 0, errors.Wrap(err, "create round tripper")
		}

		rt = auth.NewBearerTokenRoundTripper(l, tp, rt)
	} else {
		rt = auth.NewBearerTokenRoundTripper(l, tp, nil)
	}

	client, err := promapi.NewClient(promapi.Config{
		Address:      endpoint.String(),
		RoundTripper: rt,
	})
	if err != nil {
		return 0, err
	}

	labelSelectors := make([]string, len(labels))
	for i, label := range labels {
		labelSelectors[i] = fmt.Sprintf(`%s="%s"`, label.Name, label.Value)
	}

	query := fmt.Sprintf("{%s}", strings.Join(labelSelectors, ","))
	ts := time.Now().Add(ago)

	value, httpCode, _, err := api.Query(ctx, client, query, ts, false)
	if err != nil {
		return httpCode, errors.Wrap(err, "query request failed")
	}

	vec := value.(model.Vector)
	if len(vec) != 1 {
		return httpCode, errors.Errorf("expected one metric, got %d", len(vec))
	}

	t := time.Unix(int64(vec[0].Value/1000), 0)

	diffSeconds := time.Since(t).Seconds()

	m.MetricValueDifference.Observe(diffSeconds)

	if diffSeconds > latency.Seconds() {
		return httpCode, errors.Errorf("metric value is too old: %2.fs", diffSeconds)
	}

	return httpCode, nil
}
