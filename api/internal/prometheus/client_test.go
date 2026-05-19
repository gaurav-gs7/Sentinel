package prometheus

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gauravgs7/sentinel/api/internal/models"
)

func TestSignalsQueriesPrometheus(t *testing.T) {
	client := NewClient("http://prometheus.test")
	client.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		query := r.URL.Query().Get("query")
		value := "100"
		switch {
		case strings.Contains(query, `status=~"5.."`):
			value = "2"
		case strings.Contains(query, "histogram_quantile"):
			value = "250"
		}
		body := fmt.Sprintf(`{"status":"success","data":{"result":[{"value":[1710000000,"%s"]}]}}`, value)
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})}

	signals, err := client.Signals(context.Background(), models.Service{
		Name:      "payments-api",
		SLOTarget: 99.9,
	})
	if err != nil {
		t.Fatal(err)
	}
	if signals.ErrorRatePct != 2 {
		t.Fatalf("expected 2%% error rate, got %+v", signals)
	}
	if signals.AvailabilityPct != 98 {
		t.Fatalf("expected 98%% availability, got %+v", signals)
	}
	if signals.LatencyP95Ms != 250 {
		t.Fatalf("expected 250ms p95, got %+v", signals)
	}
	if signals.BurnRate < 19.99 || signals.BurnRate > 20.01 {
		t.Fatalf("expected 20x burn rate, got %+v", signals)
	}
}

func TestSignalsFailsWhenPrometheusUnavailable(t *testing.T) {
	_, err := NewClient("http://127.0.0.1:1").Signals(context.Background(), models.Service{Name: "payments-api"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSignalsTreatsMissingErrorSeriesAsZero(t *testing.T) {
	client := NewClient("http://prometheus.test")
	client.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		query := r.URL.Query().Get("query")
		if strings.Contains(query, `status=~"5.."`) {
			return prometheusResponse(`{"status":"success","data":{"result":[]}}`), nil
		}
		value := "100"
		if strings.Contains(query, "histogram_quantile") {
			value = "180"
		}
		return prometheusResponse(fmt.Sprintf(`{"status":"success","data":{"result":[{"value":[1710000000,"%s"]}]}}`, value)), nil
	})}

	signals, err := client.Signals(context.Background(), models.Service{Name: "payments-api", SLOTarget: 99.9})
	if err != nil {
		t.Fatal(err)
	}
	if signals.ErrorRatePct != 0 || signals.AvailabilityPct != 100 {
		t.Fatalf("expected missing error series to mean zero errors, got %+v", signals)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func prometheusResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}
