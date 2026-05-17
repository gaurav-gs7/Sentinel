package security

import (
	"testing"

	"github.com/gauravgs7/sentinel/api/internal/models"
)

func TestValidateServiceRejectsInvalidName(t *testing.T) {
	findings := ValidateService(models.Service{
		Name:               "Payments_API",
		Team:               "platform",
		Owner:              "gaurav",
		Language:           "go",
		Namespace:          "platform",
		Environment:        "staging",
		SLOTarget:          99.9,
		DeploymentStrategy: "canary",
	})
	if len(findings) == 0 {
		t.Fatal("expected a finding")
	}
}

func TestValidateServiceAcceptsProductionReadyMetadata(t *testing.T) {
	findings := ValidateService(models.Service{
		Name:               "payments-api",
		Team:               "platform",
		Owner:              "gaurav",
		Tier:               "critical",
		Language:           "go",
		Namespace:          "payments",
		Environment:        "staging",
		SLOTarget:          99.9,
		SLOLatencyP95Ms:    300,
		SLOErrorRate:       1,
		DeploymentStrategy: "canary",
		RollbackOnFailure:  true,
	})
	if len(findings) != 0 {
		t.Fatalf("unexpected findings: %+v", findings)
	}
}
