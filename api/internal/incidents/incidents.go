package incidents

import (
	"fmt"
	"time"

	"github.com/gauravgs7/sentinel/api/internal/models"
)

type AlertRequest struct {
	Service   string  `json:"service"`
	Severity  string  `json:"severity"`
	AlertName string  `json:"alertName"`
	Title     string  `json:"title"`
	ErrorRate float64 `json:"errorRate"`
	Latency   float64 `json:"latencyP95Ms"`
}

func Timeline(incident models.Incident, service models.Service, signals models.SLOStatus) []models.IncidentEvent {
	start := incident.CreatedAt
	return []models.IncidentEvent{
		{Timestamp: start.Add(-10 * time.Minute), EventType: "deployment", Description: fmt.Sprintf("%s rollout started with %s strategy", service.Name, service.DeploymentStrategy)},
		{Timestamp: start.Add(-7 * time.Minute), EventType: "sli-degradation", Description: fmt.Sprintf("p95 latency increased to %.0fms", signals.LatencyP95CurrentMs)},
		{Timestamp: start.Add(-5 * time.Minute), EventType: "slo-burn", Description: fmt.Sprintf("error budget remaining %.1f%%", signals.ErrorBudgetRemainingPct)},
		{Timestamp: start, EventType: "alert", Description: fmt.Sprintf("%s fired for %s", incident.AlertName, service.Name)},
		{Timestamp: start.Add(1 * time.Minute), EventType: "incident-created", Description: fmt.Sprintf("Sentinel opened %s and linked owner %s", incident.ID, service.Owner)},
		{Timestamp: start.Add(2 * time.Minute), EventType: "rollback-decision", Description: "rollback recommended because incident correlates with recent rollout"},
	}
}

func Postmortem(incident models.Incident, service models.Service, signals models.SLOStatus, events []models.IncidentEvent) string {
	return fmt.Sprintf(`# Incident Postmortem: %s

## Summary
%s experienced %s after a recent %s rollout.

## Impact
Availability was %.2f%% against a %.2f%% SLO. p95 latency was %.0fms against a %dms target.

## Detection
Detected by %s and converted into incident %s by Sentinel.

## Timeline
%s

## Likely Cause
Recent deployment or configuration change correlated with SLO degradation.

## Resolution
Pause the rollout, roll back to the previous stable image, and verify error rate and p95 latency return below thresholds.

## Action Items
- Add regression coverage for the failed path.
- Tighten canary analysis thresholds if this escaped early checks.
- Verify runbook, dashboard, and alert annotations remain linked to this service.
`, service.Name, service.Name, incident.Title, service.DeploymentStrategy, signals.AvailabilityCurrent,
		signals.AvailabilityTarget, signals.LatencyP95CurrentMs, signals.LatencyP95TargetMs,
		incident.AlertName, incident.ID, renderTimeline(events))
}

func renderTimeline(events []models.IncidentEvent) string {
	out := ""
	for _, event := range events {
		out += fmt.Sprintf("- %s: %s\n", event.Timestamp.Format(time.RFC3339), event.Description)
	}
	return out
}
