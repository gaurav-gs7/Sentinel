package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gauravgs7/sentinel/api/internal/catalog"
	"github.com/gauravgs7/sentinel/api/internal/templates"
)

func TestAPITokenRequiredForAPIPaths(t *testing.T) {
	server := testServer(t, WithAPIToken("secret"))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/services", nil)
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateServiceIsIdempotentForSameSpec(t *testing.T) {
	server := testServer(t, WithAPIToken("secret"))
	body := []byte(`{
		"name":"payments-api",
		"team":"platform",
		"owner":"gaurav",
		"language":"go",
		"environment":"staging",
		"slo":"99.9",
		"deploymentStrategy":"canary"
	}`)

	first := authorizedRequest(server, http.MethodPost, "/api/v1/services", body)
	if first.Code != http.StatusCreated {
		t.Fatalf("expected first create 201, got %d: %s", first.Code, first.Body.String())
	}

	second := authorizedRequest(server, http.MethodPost, "/api/v1/services", body)
	if second.Code != http.StatusOK {
		t.Fatalf("expected idempotent create 200, got %d: %s", second.Code, second.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(second.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["idempotent"] != true {
		t.Fatalf("expected idempotent response, got %+v", payload)
	}
}

func TestCreateServiceIdempotencySurvivesEngineRestart(t *testing.T) {
	store := catalog.NewMemoryStore()
	root := filepath.Clean("../../..")
	body := []byte(`{
		"name":"payments-api",
		"team":"platform",
		"owner":"gaurav",
		"language":"go",
		"environment":"staging",
		"slo":"99.9",
		"deploymentStrategy":"canary"
	}`)
	firstServer := NewServer(store, templates.NewGenerator(filepath.Join(root, "templates"), t.TempDir()), WithAPIToken("secret"))
	first := authorizedRequest(firstServer, http.MethodPost, "/api/v1/services", body)
	if first.Code != http.StatusCreated {
		t.Fatalf("expected first create 201, got %d: %s", first.Code, first.Body.String())
	}

	restarted := NewServer(store, templates.NewGenerator(filepath.Join(root, "templates"), t.TempDir()), WithAPIToken("secret"))
	second := authorizedRequest(restarted, http.MethodPost, "/api/v1/services", body)
	if second.Code != http.StatusOK {
		t.Fatalf("expected idempotent create after restart 200, got %d: %s", second.Code, second.Body.String())
	}
	if !bytes.Contains(second.Body.Bytes(), []byte(`"idempotent":true`)) {
		t.Fatalf("expected idempotent response, got %s", second.Body.String())
	}
	deployments := authorizedRequest(restarted, http.MethodGet, "/api/v1/services/payments-api/deployments", nil)
	if deployments.Code != http.StatusOK {
		t.Fatalf("expected deployments 200, got %d: %s", deployments.Code, deployments.Body.String())
	}
	if bytes.Count(deployments.Body.Bytes(), []byte(`"status":"generated"`)) != 1 {
		t.Fatalf("expected only one generated deployment, got %s", deployments.Body.String())
	}
}

func TestHealthGateRecordsRollbackDeployment(t *testing.T) {
	server := testServer(t, WithAPIToken("secret"))
	createBody := []byte(`{
		"name":"payments-api",
		"team":"platform",
		"owner":"gaurav",
		"language":"go",
		"environment":"staging",
		"slo":"99.9",
		"deploymentStrategy":"canary"
	}`)
	created := authorizedRequest(server, http.MethodPost, "/api/v1/services", createBody)
	if created.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d: %s", created.Code, created.Body.String())
	}

	healthBody := []byte(`{"p99LatencyMs":450,"errorRate":0.2,"successCount":100}`)
	health := authorizedRequest(server, http.MethodPost, "/api/v1/services/payments-api/health-gate", healthBody)
	if health.Code != http.StatusAccepted {
		t.Fatalf("expected failed health gate 202, got %d: %s", health.Code, health.Body.String())
	}

	deployments := authorizedRequest(server, http.MethodGet, "/api/v1/services/payments-api/deployments", nil)
	if deployments.Code != http.StatusOK {
		t.Fatalf("expected deployments 200, got %d: %s", deployments.Code, deployments.Body.String())
	}
	if !bytes.Contains(deployments.Body.Bytes(), []byte("rollback-triggered")) {
		t.Fatalf("expected rollback deployment, got %s", deployments.Body.String())
	}
}

func TestSLOEvaluationFailsClosedWhenPrometheusUnavailable(t *testing.T) {
	server := testServer(t, WithAPIToken("secret"), WithPrometheusURL("http://127.0.0.1:1"))
	createBody := []byte(`{
		"name":"payments-api",
		"team":"platform",
		"owner":"gaurav",
		"language":"go",
		"environment":"staging",
		"slo":"99.9",
		"deploymentStrategy":"canary"
	}`)
	created := authorizedRequest(server, http.MethodPost, "/api/v1/services", createBody)
	if created.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d: %s", created.Code, created.Body.String())
	}

	slos := authorizedRequest(server, http.MethodGet, "/api/v1/services/payments-api/slos", nil)
	if slos.Code != http.StatusOK {
		t.Fatalf("expected slos 200, got %d: %s", slos.Code, slos.Body.String())
	}
	if !bytes.Contains(slos.Body.Bytes(), []byte(`"deploymentGate":"blocked"`)) {
		t.Fatalf("expected deployment gate to fail closed, got %s", slos.Body.String())
	}
	if !bytes.Contains(slos.Body.Bytes(), []byte("Prometheus unavailable")) {
		t.Fatalf("expected Prometheus outage reason, got %s", slos.Body.String())
	}
}

func TestIncidentsPersistThroughStore(t *testing.T) {
	store := catalog.NewMemoryStore()
	root := filepath.Clean("../../..")
	generator := templates.NewGenerator(filepath.Join(root, "templates"), t.TempDir())
	server := NewServer(store, generator, WithAPIToken("secret"))
	createBody := []byte(`{
		"name":"payments-api",
		"team":"platform",
		"owner":"gaurav",
		"language":"go",
		"environment":"staging",
		"slo":"99.9",
		"deploymentStrategy":"canary"
	}`)
	created := authorizedRequest(server, http.MethodPost, "/api/v1/services", createBody)
	if created.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d: %s", created.Code, created.Body.String())
	}
	incidentBody := []byte(`{"service":"payments-api","alertName":"HighErrorRate","title":"High error rate","errorRate":8.2,"latencyP95Ms":850}`)
	incident := authorizedRequest(server, http.MethodPost, "/api/v1/incidents", incidentBody)
	if incident.Code != http.StatusCreated {
		t.Fatalf("expected incident 201, got %d: %s", incident.Code, incident.Body.String())
	}
	var payload struct {
		Incident struct {
			Incident struct {
				ID string `json:"id"`
			} `json:"incident"`
		} `json:"incident"`
	}
	if err := json.Unmarshal(incident.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	restarted := NewServer(store, generator, WithAPIToken("secret"))
	got := authorizedRequest(restarted, http.MethodGet, "/api/v1/incidents/"+payload.Incident.Incident.ID, nil)
	if got.Code != http.StatusOK {
		t.Fatalf("expected persisted incident 200, got %d: %s", got.Code, got.Body.String())
	}
}

func testServer(t *testing.T, opts ...Option) *Server {
	t.Helper()
	root := filepath.Clean("../../..")
	generator := templates.NewGenerator(filepath.Join(root, "templates"), t.TempDir())
	return NewServer(catalog.NewMemoryStore(), generator, opts...)
}

func authorizedRequest(server *Server, method, path string, body []byte) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("X-Sentinel-Token", "secret")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	server.Routes().ServeHTTP(rec, req)
	return rec
}
