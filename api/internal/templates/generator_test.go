package templates

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gauravgs7/sentinel/api/internal/models"
)

func TestGeneratorCreatesServiceScaffold(t *testing.T) {
	root := filepath.Clean("../../..")
	output := t.TempDir()
	generator := NewGenerator(filepath.Join(root, "templates"), output)

	result, err := generator.Generate(models.Service{
		Name:               "payments-api",
		Team:               "platform",
		Owner:              "gaurav",
		Language:           "go",
		Namespace:          "payments",
		Environment:        "staging",
		SLOTarget:          99.9,
		DeploymentStrategy: "canary",
	})
	if err != nil {
		t.Fatal(err)
	}

	required := []string{
		"Dockerfile",
		"sentinel-service.yaml",
		"slo.yaml",
		"rollout.yaml",
		".github/workflows/ci.yml",
		"k8s/base/deployment.yaml",
		"k8s/base/pdb.yaml",
		"observability/dashboard.json",
		"observability/servicemonitor.yaml",
		"observability/prometheus-rules.yaml",
		"runbooks/incident-runbook.md",
		"infra/argocd/application.yaml",
		"observability/otel-collector.yaml",
		"observability/jaeger.yaml",
	}
	for _, rel := range required {
		path := filepath.Join(result.OutputPath, rel)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected generated file %s: %v", path, err)
		}
	}
}
