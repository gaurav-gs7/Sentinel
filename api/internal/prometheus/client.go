package prometheus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gauravgs7/sentinel/api/internal/models"
	"github.com/gauravgs7/sentinel/api/internal/slo"
)

type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		http:    &http.Client{Timeout: 2 * time.Second},
	}
}

func (c *Client) Signals(ctx context.Context, service models.Service) (slo.Signals, error) {
	if c.baseURL == "" {
		return slo.Signals{}, fmt.Errorf("prometheus URL is empty")
	}
	totalQuery := fmt.Sprintf(`sum(rate(http_requests_total{service=%q}[5m]))`, service.Name)
	errorQuery := fmt.Sprintf(`sum(rate(http_requests_total{service=%q,status=~"5.."}[5m]))`, service.Name)
	latencyQuery := fmt.Sprintf(`histogram_quantile(0.95, sum by (le) (rate(http_request_duration_seconds_bucket{service=%q}[5m]))) * 1000`, service.Name)

	total, err := c.queryScalar(ctx, totalQuery)
	if err != nil {
		return slo.Signals{}, fmt.Errorf("query request rate: %w", err)
	}
	errorsPerSecond, err := c.queryScalarOrZero(ctx, errorQuery)
	if err != nil {
		return slo.Signals{}, fmt.Errorf("query error rate: %w", err)
	}
	latencyP95Ms, err := c.queryScalar(ctx, latencyQuery)
	if err != nil {
		return slo.Signals{}, fmt.Errorf("query p95 latency: %w", err)
	}

	errorRatePct := 0.0
	if total > 0 {
		errorRatePct = math.Max(0, errorsPerSecond/total*100)
	}
	availability := math.Max(0, math.Min(100, 100-errorRatePct))
	allowedBadPct := 100 - service.SLOTarget
	burnRate := 0.0
	if allowedBadPct > 0 {
		burnRate = errorRatePct / allowedBadPct
	}
	return slo.Signals{
		AvailabilityPct: availability,
		LatencyP95Ms:    latencyP95Ms,
		ErrorRatePct:    errorRatePct,
		BurnRate:        burnRate,
	}, nil
}

func (c *Client) queryScalar(ctx context.Context, query string) (float64, error) {
	value, _, err := c.queryScalarResult(ctx, query)
	return value, err
}

func (c *Client) queryScalarOrZero(ctx context.Context, query string) (float64, error) {
	value, found, err := c.queryScalarResult(ctx, query)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, nil
	}
	return value, nil
}

func (c *Client) queryScalarResult(ctx context.Context, query string) (float64, bool, error) {
	values := url.Values{}
	values.Set("query", query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/query?"+values.Encode(), nil)
	if err != nil {
		return 0, false, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, false, fmt.Errorf("Prometheus returned %s", resp.Status)
	}
	var payload struct {
		Status string `json:"status"`
		Error  string `json:"error"`
		Data   struct {
			Result []struct {
				Value []any `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, false, err
	}
	if payload.Status != "success" {
		if payload.Error == "" {
			payload.Error = "query failed"
		}
		return 0, false, errors.New(payload.Error)
	}
	if len(payload.Data.Result) == 0 {
		return 0, false, nil
	}
	if len(payload.Data.Result[0].Value) < 2 {
		return 0, false, fmt.Errorf("query returned no scalar result")
	}
	raw, ok := payload.Data.Result[0].Value[1].(string)
	if !ok {
		return 0, false, fmt.Errorf("query scalar result was not a string")
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false, fmt.Errorf("parse scalar result %q: %w", raw, err)
	}
	return value, true, nil
}
