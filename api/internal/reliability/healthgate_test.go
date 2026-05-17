package reliability

import "testing"

func TestEvaluateCanaryRollsBackOnLatency(t *testing.T) {
	decision := EvaluateCanary(DefaultSLO(99.9), Metrics{
		P99LatencyMs: 450,
		ErrorRate:    0.1,
		SuccessCount: 100,
	})
	if decision.Healthy || decision.Action != "rollback" {
		t.Fatalf("expected rollback decision, got %+v", decision)
	}
}

func TestEvaluateCanaryPromotesHealthyCanary(t *testing.T) {
	decision := EvaluateCanary(DefaultSLO(99.9), Metrics{
		P99LatencyMs: 220,
		ErrorRate:    0.2,
		SuccessCount: 100,
	})
	if !decision.Healthy || decision.Action != "promote" {
		t.Fatalf("expected promote decision, got %+v", decision)
	}
}
