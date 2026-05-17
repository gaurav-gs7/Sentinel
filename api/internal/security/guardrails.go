package security

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/gauravgs7/sentinel/api/internal/models"
)

var dnsLabel = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

type Finding struct {
	Policy  string `json:"policy"`
	Message string `json:"message"`
}

func ValidateService(service models.Service) []Finding {
	var findings []Finding

	if !dnsLabel.MatchString(service.Name) {
		findings = append(findings, Finding{"service-name", "service name must be a valid Kubernetes DNS label"})
	}
	if !dnsLabel.MatchString(service.Namespace) {
		findings = append(findings, Finding{"namespace", "namespace must be a valid Kubernetes DNS label"})
	}
	switch strings.ToLower(service.Language) {
	case "go", "python":
	default:
		findings = append(findings, Finding{"language", "supported languages are go and python"})
	}
	switch strings.ToLower(service.DeploymentStrategy) {
	case "rolling", "canary", "blue-green":
	default:
		findings = append(findings, Finding{"deployment-strategy", "strategy must be rolling, canary, or blue-green"})
	}
	switch strings.ToLower(service.Environment) {
	case "dev", "staging", "prod":
	default:
		findings = append(findings, Finding{"environment", "environment must be dev, staging, or prod"})
	}
	if service.SLOTarget < 95 || service.SLOTarget > 99.99 {
		findings = append(findings, Finding{"slo", "slo target must be between 95 and 99.99"})
	}
	if service.SLOLatencyP95Ms <= 0 {
		findings = append(findings, Finding{"latency-slo", "p95 latency SLO must be configured"})
	}
	if service.SLOErrorRate <= 0 || service.SLOErrorRate > 25 {
		findings = append(findings, Finding{"error-rate", "error-rate threshold must be between 0 and 25 percent"})
	}
	switch strings.ToLower(service.Tier) {
	case "critical", "high", "medium", "low":
	default:
		findings = append(findings, Finding{"tier", "tier must be critical, high, medium, or low"})
	}
	if service.Team == "" || service.Owner == "" {
		findings = append(findings, Finding{"ownership", "team and owner are required for operational accountability"})
	}
	if service.RepoURL != "" {
		if !validRepository(service.RepoURL) {
			findings = append(findings, Finding{"repo-url", "repo_url must be a valid https URL or owner/repo path when provided"})
		}
	}
	if service.Repository != "" && !validRepository(service.Repository) {
		findings = append(findings, Finding{"repository", "repository must be a valid https URL or owner/repo path when provided"})
	}
	return findings
}

func validRepository(value string) bool {
	if !strings.Contains(value, "://") {
		return strings.Contains(value, "/") && !strings.Contains(value, " ")
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

func RejectIfFindings(findings []Finding) error {
	if len(findings) == 0 {
		return nil
	}
	return fmt.Errorf("%s: %s", findings[0].Policy, findings[0].Message)
}
