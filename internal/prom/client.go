// Package prom is a tiny, dependency-free client for the Prometheus HTTP query
// API. It deliberately covers only what the budget and gate commands need —
// a single instant query reduced to one scalar — rather than pulling in the
// full client library.
package prom

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client queries a Prometheus-compatible HTTP API.
type Client struct {
	baseURL string
	http    *http.Client
}

// New returns a Client for the given Prometheus base URL (e.g.
// "http://localhost:9090"). A trailing slash is tolerated.
func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// WithHTTPClient overrides the underlying HTTP client (used in tests).
func (c *Client) WithHTTPClient(h *http.Client) *Client {
	c.http = h
	return c
}

// apiResponse mirrors the Prometheus query API envelope.
type apiResponse struct {
	Status    string `json:"status"`
	ErrorType string `json:"errorType"`
	Error     string `json:"error"`
	Data      struct {
		ResultType string          `json:"resultType"`
		Result     json.RawMessage `json:"result"`
	} `json:"data"`
}

// QueryScalar runs an instant query and reduces the result to a single float.
//
// It accepts a Prometheus "scalar" result directly, and for "vector" results
// returns the first sample's value. An empty vector is reported as ErrNoData so
// callers can distinguish "no series" from a real value of zero.
func (c *Client) QueryScalar(ctx context.Context, query string) (float64, error) {
	u := c.baseURL + "/api/v1/query?" + url.Values{"query": {query}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("query prometheus at %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	var ar apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return 0, fmt.Errorf("decode prometheus response (HTTP %d): %w", resp.StatusCode, err)
	}
	if ar.Status != "success" {
		return 0, fmt.Errorf("prometheus error (%s): %s", ar.ErrorType, ar.Error)
	}

	switch ar.Data.ResultType {
	case "scalar":
		return parseSample(ar.Data.Result)
	case "vector":
		return parseFirstVectorValue(ar.Data.Result)
	default:
		return 0, fmt.Errorf("unsupported result type %q (expected scalar or vector)", ar.Data.ResultType)
	}
}

// ErrNoData indicates a query returned an empty vector.
var ErrNoData = fmt.Errorf("query returned no data")

// scalar result: [ <unix_ts>, "<value>" ]
func parseSample(raw json.RawMessage) (float64, error) {
	var sample [2]json.RawMessage
	if err := json.Unmarshal(raw, &sample); err != nil {
		return 0, fmt.Errorf("decode scalar: %w", err)
	}
	return valueFromString(sample[1])
}

// vector result: [ { "metric": {...}, "value": [ <ts>, "<value>" ] }, ... ]
func parseFirstVectorValue(raw json.RawMessage) (float64, error) {
	var vec []struct {
		Value [2]json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(raw, &vec); err != nil {
		return 0, fmt.Errorf("decode vector: %w", err)
	}
	if len(vec) == 0 {
		return 0, ErrNoData
	}
	return valueFromString(vec[0].Value[1])
}

func valueFromString(raw json.RawMessage) (float64, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, fmt.Errorf("decode value: %w", err)
	}
	switch s {
	case "NaN":
		return 0, nil
	case "+Inf", "-Inf":
		return 0, fmt.Errorf("query returned %s", s)
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("parse value %q: %w", s, err)
	}
	return f, nil
}
