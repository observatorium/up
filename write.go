package main

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/prompb"
)

func write(ctx context.Context, endpoint *url.URL, t TokenProvider, wreq proto.Message, l log.Logger, tls tlsOptions) error {
	var (
		buf []byte
		err error
		req *http.Request
		res *http.Response
		rt  http.RoundTripper
	)

	if endpoint.Scheme == https {
		rt, err = newTLSTransport(l, tls)
		if err != nil {
			return errors.Wrap(err, "create round tripper")
		}
	} else {
		rt = http.DefaultTransport
	}

	client := &http.Client{Transport: rt}

	buf, err = proto.Marshal(wreq)
	if err != nil {
		return errors.Wrap(err, "marshalling proto")
	}

	req, err = http.NewRequest("POST", endpoint.String(), bytes.NewBuffer(snappy.Encode(nil, buf)))
	if err != nil {
		return errors.Wrap(err, "creating request")
	}

	token, err := t.Get()
	if err != nil {
		return errors.Wrap(err, "retrieving token")
	}

	if token != "" {
		req.Header.Add("Authorization", "Bearer "+token)
	}

	res, err = client.Do(req.WithContext(ctx)) //nolint:bodyclose
	if err != nil {
		return errors.Wrap(err, "making request")
	}

	defer exhaustCloseWithLogOnErr(l, res.Body)

	if res.StatusCode != http.StatusOK {
		err = errors.New(res.Status)
		return errors.Wrap(err, "non-200 status")
	}

	return nil
}

func generate(labels []prompb.Label) *prompb.WriteRequest {
	timestamp := time.Now().UnixNano() / int64(time.Millisecond)

	return &prompb.WriteRequest{
		Timeseries: []prompb.TimeSeries{
			{
				Labels: labels,
				Samples: []prompb.Sample{
					{
						Value:     float64(timestamp),
						Timestamp: timestamp,
					},
				},
			},
		},
	}
}
