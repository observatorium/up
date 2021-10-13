package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/observatorium/up/pkg/auth"
	"github.com/observatorium/up/pkg/instr"
	"github.com/observatorium/up/pkg/options"
	"github.com/observatorium/up/pkg/transport"

	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/prompb"
)

// Read executes query against Loki with the same labels to retrieve the written logs back.
func Read(
	ctx context.Context,
	endpoint *url.URL,
	tp auth.TokenProvider,
	labels []prompb.Label, // change to Loki ProtoBufs
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

	client := &http.Client{Transport: rt}

	labelSelectors := make([]string, len(labels))
	for i, label := range labels {
		labelSelectors[i] = fmt.Sprintf(`%s="%s"`, label.Name, label.Value)
	}

	query := fmt.Sprintf("{%s}", strings.Join(labelSelectors, ","))

	params := url.Values{}
	params.Add("query", query)
	endpoint.RawQuery = params.Encode()

	req, err := http.NewRequest(http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return 0, errors.Wrap(err, "creating request")
	}

	res, err := client.Do(req.WithContext(ctx))
	if err != nil {
		if res == nil {
			//Unknown error.
			return 0, errors.Wrap(err, "making request")
		}

		return res.StatusCode, errors.Wrap(err, "making request")
	}

	if res.StatusCode != http.StatusOK {
		err = errors.Errorf(res.Status)
		return res.StatusCode, errors.Wrap(err, "non-200 status")
	}

	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return res.StatusCode, errors.Wrap(err, "reading response body")
	}

	rr := &queryResponse{}

	err = json.Unmarshal(body, rr)
	if err != nil {
		return res.StatusCode, errors.Wrap(err, "unmarshalling response")
	}

	rl := len(rr.Data.Result)
	if rl != 1 {
		return res.StatusCode, errors.Errorf("expected one log entry, got %d", rl)
	}

	return res.StatusCode, nil
}
