// Copyright 2017 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This is a modified copy from https://github.com/prometheus/client_golang/blob/master/api/prometheus/v1/api.go.

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	promapi "github.com/prometheus/client_golang/api"
	promapiv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

const (
	statusAPIError = 422

	// Possible values for ErrorType.
	ErrBadResponse promapiv1.ErrorType = "bad_response"
	ErrServer      promapiv1.ErrorType = "server_error"
	ErrClient      promapiv1.ErrorType = "client_error"

	epQuery      = "/api/v1/query"
	epQueryRange = "/api/v1/query_range"
)

func errorTypeAndMsgFor(resp *http.Response) (promapiv1.ErrorType, string) {
	switch resp.StatusCode / 100 {
	case 4:
		return ErrClient, fmt.Sprintf("client error: %d", resp.StatusCode)
	case 5:
		return ErrServer, fmt.Sprintf("server error: %d", resp.StatusCode)
	}
	return ErrBadResponse, fmt.Sprintf("bad response code %d", resp.StatusCode)
}

func apiError(code int) bool {
	// These are the codes that Prometheus sends when it returns an error.
	return code == statusAPIError || code == http.StatusBadRequest
}

// 1ueryResult contains result data for a query.
type queryResult struct {
	Type   model.ValueType `json:"resultType"`
	Result interface{}     `json:"result"`

	// The decoded value.
	v model.Value
}

func (qr *queryResult) UnmarshalJSON(b []byte) error {
	v := struct {
		Type   model.ValueType `json:"resultType"`
		Result json.RawMessage `json:"result"`
	}{}

	err := json.Unmarshal(b, &v)
	if err != nil {
		return err
	}

	switch v.Type {
	case model.ValScalar:
		var sv model.Scalar
		err = json.Unmarshal(v.Result, &sv)
		qr.v = &sv

	case model.ValVector:
		var vv model.Vector
		err = json.Unmarshal(v.Result, &vv)
		qr.v = vv

	case model.ValMatrix:
		var mv model.Matrix
		err = json.Unmarshal(v.Result, &mv)
		qr.v = mv

	default:
		err = fmt.Errorf("unexpected value type %q", v.Type)
	}
	return err
}

type apiResponse struct {
	Status    string              `json:"status"`
	Data      json.RawMessage     `json:"data"`
	ErrorType promapiv1.ErrorType `json:"errorType"`
	Error     string              `json:"error"`
	Warnings  []string            `json:"warnings,omitempty"`
}

func do(ctx context.Context, client promapi.Client, req *http.Request) (*http.Response, []byte, promapiv1.Warnings, error) {
	resp, body, err := client.Do(ctx, req)
	if err != nil {
		return resp, body, nil, err
	}

	code := resp.StatusCode

	if code/100 != 2 && !apiError(code) {
		errorType, errorMsg := errorTypeAndMsgFor(resp)
		return resp, body, nil, &promapiv1.Error{
			Type:   errorType,
			Msg:    errorMsg,
			Detail: string(body),
		}
	}

	var result apiResponse

	if http.StatusNoContent != code {
		if jsonErr := json.Unmarshal(body, &result); jsonErr != nil {
			return resp, body, nil, &promapiv1.Error{
				Type: ErrBadResponse,
				Msg:  jsonErr.Error(),
			}
		}
	}

	if apiError(code) && result.Status == "success" {
		err = &promapiv1.Error{
			Type: ErrBadResponse,
			Msg:  "inconsistent body for response code",
		}
	}

	if result.Status == "error" {
		err = &promapiv1.Error{
			Type: result.ErrorType,
			Msg:  result.Error,
		}
	}

	return resp, result.Data, result.Warnings, err
}

// doGetFallback will attempt to do the request as-is, and on a 405 it will fallback to a GET request.
func doGetFallback(
	ctx context.Context,
	client promapi.Client,
	u *url.URL,
	args url.Values,
	cache bool,
) (*http.Response, []byte, promapiv1.Warnings, error) {
	req, err := http.NewRequest(http.MethodPost, u.String(), strings.NewReader(args.Encode()))
	if err != nil {
		return nil, nil, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if !cache {
		req.Header.Set("Cache-Control", "no-store")
	}

	resp, body, warnings, err := do(ctx, client, req)
	if resp != nil && resp.StatusCode == http.StatusMethodNotAllowed {
		u.RawQuery = args.Encode()
		req, err = http.NewRequest(http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, nil, warnings, err
		}

	} else {
		if err != nil {
			return resp, body, warnings, err
		}
		return resp, body, warnings, nil
	}
	return do(ctx, client, req)
}

func QueryRange(ctx context.Context, client promapi.Client, query string, r promapiv1.Range,
	cache bool) (model.Value, promapiv1.Warnings, error) {
	u := client.URL(epQueryRange, nil)
	q := u.Query()

	q.Set("query", query)
	q.Set("start", formatTime(r.Start))
	q.Set("end", formatTime(r.End))
	q.Set("step", strconv.FormatFloat(r.Step.Seconds(), 'f', -1, 64))

	_, body, warnings, err := doGetFallback(ctx, client, u, q, cache) //nolint:bodyclose
	if err != nil {
		return nil, warnings, err
	}

	var qres queryResult

	return qres.v, warnings, json.Unmarshal(body, &qres)
}

func Query(ctx context.Context, client promapi.Client, query string, ts time.Time) (model.Value, promapiv1.Warnings, error) {
	u := client.URL(epQuery, nil)
	q := u.Query()

	q.Set("query", query)
	if !ts.IsZero() {
		q.Set("time", formatTime(ts))
	}

	// Instant query doesn't support cache
	_, body, warnings, err := doGetFallback(ctx, client, u, q, false) //nolint:bodyclose
	if err != nil {
		return nil, warnings, err
	}

	var qres queryResult
	return qres.v, warnings, json.Unmarshal(body, &qres)
}

func formatTime(t time.Time) string {
	return strconv.FormatFloat(float64(t.Unix())+float64(t.Nanosecond())/1e9, 'f', -1, 64)
}
