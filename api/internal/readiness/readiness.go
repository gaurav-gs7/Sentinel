package readiness

import (
	"strings"
	"time"

	"github.com/gauravgs7/sentinel/api/internal/models"
)

func Evaluate(service models.Service) models.ReadinessReport {
	checks := []models.ReadinessCheck{
		passIf("ownership", service.Owner != "" && service.Team != "", "owner and team metadata present", "set owner and team metadata", 10),
		passIf("pager", service.Pager != "", "pager/on-call metadata present", "set pager to the owning rotation", 8),
		passIf("tier", validTier(service.Tier), "service tier classified", "set tier to critical, high, medium, or low", 6),
		passIf("runbook", service.RunbookURL != "", "runbook link configured", "generate and publish a runbook link", 8),
		passIf("dashboard", service.DashboardURL != "", "dashboard link configured", "publish the generated Grafana dashboard", 7),
		passIf("slo", service.SLOTarget >= 95 && service.SLOTarget <= 99.99, "availability SLO configured", "set an availability SLO between 95 and 99.99", 10),
		passIf("latency-slo", service.SLOLatencyP95Ms > 0, "p95 latency SLO configured", "set a p95 latency objective", 8),
		passIf("error-rate-policy", service.SLOErrorRate > 0, "error-rate threshold configured", "set max error-rate threshold", 7),
		passIf("deployment-strategy", service.DeploymentStrategy == "canary" || service.DeploymentStrategy == "blue-green" || service.DeploymentStrategy == "rolling", "rollout strategy configured", "set canary or blue-green for production services", 8),
		passIf("rollback-policy", service.RollbackOnFailure, "automated rollback enabled", "enable rollbackOnFailure", 8),
		passIf("namespace", service.Namespace != "", "namespace configured", "set a Kubernetes namespace", 5),
		passIf("repository", service.Repository != "" || service.RepoURL != "", "repository linked", "link the service repository", 5),
		passIf("dependencies", len(service.Dependencies) > 0 || strings.ToLower(service.Tier) != "critical", "dependency metadata acceptable", "list critical service dependencies", 5),
	}

	report := models.ReadinessReport{
		Service:     service.Name,
		Tier:        service.Tier,
		EvaluatedAt: time.Now().UTC(),
	}
	var earned, possible int
	for _, check := range checks {
		possible += check.Weight
		if check.Status == "pass" {
			earned += check.Weight
			report.Passed = append(report.Passed, check)
			continue
		}
		report.Failed = append(report.Failed, check)
		report.Recommendations = append(report.Recommendations, check.Recommendation)
	}
	if possible > 0 {
		report.Score = int(float64(earned) / float64(possible) * 100)
	}
	return report
}

func passIf(name string, ok bool, message, recommendation string, weight int) models.ReadinessCheck {
	status := "fail"
	if ok {
		status = "pass"
		recommendation = ""
	}
	return models.ReadinessCheck{
		Name:           name,
		Status:         status,
		Message:        message,
		Recommendation: recommendation,
		Weight:         weight,
	}
}

func validTier(tier string) bool {
	switch strings.ToLower(tier) {
	case "critical", "high", "medium", "low":
		return true
	default:
		return false
	}
}
