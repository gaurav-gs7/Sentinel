package slo

import (
	"fmt"
	"math"
	"time"

	"github.com/gauravgs7/sentinel/api/internal/models"
)

type Signals struct {
	AvailabilityPct float64
	LatencyP95Ms    float64
	ErrorRatePct    float64
	BurnRate        float64
}

func DefaultSignals(service models.Service) Signals {
	latency := float64(service.SLOLatencyP95Ms) * 0.8
	if latency == 0 {
		latency = 240
	}
	errorRate := service.SLOErrorRate * 0.2
	if errorRate == 0 {
		errorRate = 0.2
	}
	return Signals{
		AvailabilityPct: math.Min(99.99, service.SLOTarget+0.04),
		LatencyP95Ms:    latency,
		ErrorRatePct:    errorRate,
		BurnRate:        0.7,
	}
}

func Evaluate(service models.Service, signals Signals) models.SLOStatus {
	target := service.SLOTarget
	if target == 0 {
		target = 99.9
	}
	latencyTarget := service.SLOLatencyP95Ms
	if latencyTarget == 0 {
		latencyTarget = 300
	}
	errorThreshold := service.SLOErrorRate
	if errorThreshold == 0 {
		errorThreshold = 1
	}
	allowedBadPct := 100 - target
	actualBadPct := math.Max(0, 100-signals.AvailabilityPct)
	remaining := 100.0
	if allowedBadPct > 0 {
		remaining = math.Max(0, math.Min(100, (allowedBadPct-actualBadPct)/allowedBadPct*100))
	}

	gate := "allowed"
	reason := "SLOs healthy and error budget is above deployment threshold"
	if remaining < 10 {
		gate = "blocked"
		reason = fmt.Sprintf("error budget remaining %.1f%% is below 10%%", remaining)
	} else if signals.LatencyP95Ms > float64(latencyTarget) {
		gate = "blocked"
		reason = fmt.Sprintf("p95 latency %.0fms exceeds %dms target", signals.LatencyP95Ms, latencyTarget)
	} else if signals.ErrorRatePct > errorThreshold {
		gate = "blocked"
		reason = fmt.Sprintf("error rate %.2f%% exceeds %.2f%% threshold", signals.ErrorRatePct, errorThreshold)
	} else if signals.BurnRate >= 2 {
		gate = "blocked"
		reason = fmt.Sprintf("burn rate %.2fx is too high for rollout", signals.BurnRate)
	}

	return models.SLOStatus{
		Service:                 service.Name,
		AvailabilityTarget:      target,
		AvailabilityCurrent:     signals.AvailabilityPct,
		ErrorBudgetRemainingPct: remaining,
		LatencyP95TargetMs:      latencyTarget,
		LatencyP95CurrentMs:     signals.LatencyP95Ms,
		ErrorRateThresholdPct:   errorThreshold,
		ErrorRateCurrentPct:     signals.ErrorRatePct,
		BurnRate:                signals.BurnRate,
		DeploymentGate:          gate,
		Reason:                  reason,
		EvaluatedAt:             time.Now().UTC(),
	}
}
