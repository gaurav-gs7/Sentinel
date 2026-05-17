package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gauravgs7/sentinel/api/internal/catalog"
	"github.com/gauravgs7/sentinel/api/internal/incidents"
	"github.com/gauravgs7/sentinel/api/internal/models"
	"github.com/gauravgs7/sentinel/api/internal/readiness"
	"github.com/gauravgs7/sentinel/api/internal/reliability"
	"github.com/gauravgs7/sentinel/api/internal/security"
	sloengine "github.com/gauravgs7/sentinel/api/internal/slo"
	"github.com/gauravgs7/sentinel/api/internal/templates"
	"github.com/gauravgs7/sentinel/api/internal/workflows"
)

type Server struct {
	store         catalog.Store
	generator     *templates.Generator
	apiToken      string
	maxBodyBytes  int64
	prometheusURL string
	startedAt     time.Time
	metrics       *serverMetrics
	incidentMu    sync.RWMutex
	incidents     map[string]models.IncidentRecord
	workflows     *workflows.Engine
}

type Option func(*Server)

func WithAPIToken(token string) Option {
	return func(s *Server) {
		s.apiToken = token
	}
}

func WithMaxBodyBytes(bytes int64) Option {
	return func(s *Server) {
		if bytes > 0 {
			s.maxBodyBytes = bytes
		}
	}
}

func WithPrometheusURL(url string) Option {
	return func(s *Server) {
		s.prometheusURL = strings.TrimRight(strings.TrimSpace(url), "/")
	}
}

func NewServer(store catalog.Store, generator *templates.Generator, opts ...Option) *Server {
	server := &Server{
		store:        store,
		generator:    generator,
		maxBodyBytes: 1 << 20,
		startedAt:    time.Now().UTC(),
		metrics:      newServerMetrics(),
		incidents:    make(map[string]models.IncidentRecord),
		workflows:    workflows.NewEngine(store),
	}
	for _, opt := range opts {
		opt(server)
	}
	return server
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /livez", s.live)
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("GET /metrics", s.prometheusMetrics)
	mux.HandleFunc("POST /api/v1/services", s.createService)
	mux.HandleFunc("GET /api/v1/services", s.listServices)
	mux.HandleFunc("GET /api/v1/services/{name}", s.getService)
	mux.HandleFunc("GET /api/v1/services/{name}/status", s.serviceStatus)
	mux.HandleFunc("POST /api/v1/services/{name}/check", s.readiness)
	mux.HandleFunc("GET /api/v1/services/{name}/readiness", s.readiness)
	mux.HandleFunc("GET /api/v1/services/{name}/score", s.score)
	mux.HandleFunc("GET /api/v1/services/{name}/slos", s.slos)
	mux.HandleFunc("GET /api/v1/services/{name}/error-budget", s.slos)
	mux.HandleFunc("GET /api/v1/services/{name}/deployments", s.deployments)
	mux.HandleFunc("POST /api/v1/services/{name}/rollouts", s.startRollout)
	mux.HandleFunc("GET /api/v1/services/{name}/rollouts/{id}", s.getRollout)
	mux.HandleFunc("POST /api/v1/services/{name}/rollouts/{id}/pause", s.pauseRollout)
	mux.HandleFunc("POST /api/v1/services/{name}/rollouts/{id}/rollback", s.rollback)
	mux.HandleFunc("POST /api/v1/services/{name}/health-gate", s.healthGate)
	mux.HandleFunc("POST /api/v1/services/{name}/rollback", s.rollback)
	mux.HandleFunc("GET /api/v1/services/{name}/runbook", s.runbook)
	mux.HandleFunc("POST /api/v1/incidents", s.createIncident)
	mux.HandleFunc("GET /api/v1/incidents", s.listIncidents)
	mux.HandleFunc("GET /api/v1/incidents/{id}", s.getIncident)
	mux.HandleFunc("POST /api/v1/incidents/{id}/resolve", s.resolveIncident)
	mux.HandleFunc("GET /api/v1/incidents/{id}/timeline", s.incidentTimeline)
	mux.HandleFunc("GET /api/v1/incidents/{id}/postmortem", s.incidentPostmortem)
	mux.HandleFunc("GET /api/v1/workflows", s.listWorkflows)
	mux.HandleFunc("GET /api/v1/workflows/{id}", s.getWorkflow)
	return s.logging(s.metricsMiddleware(s.auth(recoverer(mux))))
}

func (s *Server) live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "alive"})
}

func (s *Server) ready(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ready",
		"authEnabled": s.apiToken != "",
		"uptimeSec":   int(time.Since(s.startedAt).Seconds()),
	})
}

func (s *Server) createService(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.maxBodyBytes)
	var req models.CreateServiceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	service, err := requestToService(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if findings := security.ValidateService(service); len(findings) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": "policy validation failed", "findings": findings})
		return
	}

	var created models.Service
	var generated templates.Result
	idempotent := false
	run := s.workflows.Run(r.Context(), workflows.Spec{
		Name:    "service-onboarding",
		Kind:    "sentinel.service.onboarding",
		Service: service.Name,
		Steps: []workflows.Step{
			{
				Name:        "persist-service-metadata",
				MaxAttempts: 1,
				Run: func(ctx context.Context) error {
					var err error
					created, err = s.store.CreateService(ctx, service)
					if err == nil {
						return nil
					}
					if !errors.Is(err, catalog.ErrConflict) {
						return err
					}
					existing, getErr := s.store.GetServiceByName(ctx, service.Name)
					if getErr != nil {
						return getErr
					}
					if !sameServiceSpec(existing, service) {
						return fmt.Errorf("service already exists with different metadata")
					}
					created = existing
					idempotent = true
					return nil
				},
			},
			{
				Name:        "record-initial-deployment",
				MaxAttempts: 1,
				Run: func(ctx context.Context) error {
					if idempotent {
						return nil
					}
					_, err := s.store.RecordDeployment(ctx, models.Deployment{
						ServiceID:   created.ID,
						Version:     "v0.1.0",
						Environment: created.Environment,
						Status:      "generated",
						Strategy:    created.DeploymentStrategy,
						StartedAt:   time.Now().UTC(),
					})
					return err
				},
			},
			{
				Name:        "render-golden-path-scaffold",
				MaxAttempts: 2,
				Run: func(context.Context) error {
					var err error
					generated, err = s.generator.Generate(created)
					return err
				},
			},
		},
	})
	if run.State != workflows.StateSucceeded {
		if strings.Contains(run.Error, "different metadata") {
			writeJSON(w, http.StatusConflict, map[string]any{"error": run.Error, "workflow": run})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": run.Error, "workflow": run})
		return
	}

	status := http.StatusCreated
	if idempotent {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{
		"service":    created,
		"generated":  generated,
		"workflow":   run,
		"idempotent": idempotent,
	})
}

func (s *Server) listServices(w http.ResponseWriter, r *http.Request) {
	services, err := s.store.ListServices(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"services": services})
}

func (s *Server) getService(w http.ResponseWriter, r *http.Request) {
	service, ok := s.loadService(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"service": service})
}

func (s *Server) serviceStatus(w http.ResponseWriter, r *http.Request) {
	service, ok := s.loadService(w, r)
	if !ok {
		return
	}
	deployment, err := s.store.LatestDeployment(r.Context(), service.ID)
	if err != nil && !errors.Is(err, catalog.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	metrics := reliability.Metrics{P99LatencyMs: 240, ErrorRate: 0.2, SuccessCount: 100}
	decision := reliability.EvaluateCanary(reliability.DefaultSLO(service.SLOTarget), metrics)
	last := deployment.StartedAt
	if last.IsZero() {
		last = service.CreatedAt
	}
	writeJSON(w, http.StatusOK, models.ServiceStatus{
		Service:         service.Name,
		Environment:     service.Environment,
		Deployment:      deployment.Status,
		SLO:             service.SLOTarget,
		P99LatencyMs:    metrics.P99LatencyMs,
		ErrorRate:       metrics.ErrorRate,
		LastDeployment:  last,
		HealthGate:      decision.Action,
		RollbackAllowed: service.DeploymentStrategy == "canary",
	})
}

func (s *Server) readiness(w http.ResponseWriter, r *http.Request) {
	service, ok := s.loadService(w, r)
	if !ok {
		return
	}
	var report models.ReadinessReport
	run := s.workflows.Run(r.Context(), workflows.Spec{
		Name:    "production-readiness-check",
		Kind:    "sentinel.readiness.check",
		Service: service.Name,
		Steps: []workflows.Step{{
			Name:        "evaluate-service-production-readiness",
			MaxAttempts: 1,
			Run: func(context.Context) error {
				report = readiness.Evaluate(service)
				return nil
			},
		}},
	})
	writeJSON(w, http.StatusOK, map[string]any{"report": report, "workflow": run})
}

func (s *Server) score(w http.ResponseWriter, r *http.Request) {
	service, ok := s.loadService(w, r)
	if !ok {
		return
	}
	var report models.ReadinessReport
	run := s.workflows.Run(r.Context(), workflows.Spec{
		Name:    "production-readiness-score",
		Kind:    "sentinel.readiness.score",
		Service: service.Name,
		Steps: []workflows.Step{{
			Name: "calculate-score",
			Run: func(context.Context) error {
				report = readiness.Evaluate(service)
				return nil
			},
		}},
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"service":  service.Name,
		"tier":     service.Tier,
		"score":    report.Score,
		"failed":   report.Failed,
		"workflow": run,
	})
}

func (s *Server) slos(w http.ResponseWriter, r *http.Request) {
	service, ok := s.loadService(w, r)
	if !ok {
		return
	}
	var status models.SLOStatus
	run := s.workflows.Run(r.Context(), workflows.Spec{
		Name:    "slo-error-budget-evaluation",
		Kind:    "sentinel.slo.evaluate",
		Service: service.Name,
		Steps: []workflows.Step{{
			Name:        "evaluate-sli-signals-and-error-budget",
			MaxAttempts: 1,
			Run: func(context.Context) error {
				status = s.evaluateSLO(r.Context(), service)
				return nil
			},
		}},
	})
	writeJSON(w, http.StatusOK, map[string]any{"slo": status, "workflow": run})
}

func (s *Server) evaluateSLO(ctx context.Context, service models.Service) models.SLOStatus {
	status := sloengine.Evaluate(service, sloengine.DefaultSignals(service))
	if s.prometheusURL == "" {
		return status
	}
	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, s.prometheusURL+"/-/ready", nil)
	if err != nil {
		status.DeploymentGate = "blocked"
		status.Reason = "Prometheus unavailable; deployment gate fails closed"
		return status
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		status.DeploymentGate = "blocked"
		status.Reason = "Prometheus unavailable; deployment gate fails closed"
		return status
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		status.DeploymentGate = "blocked"
		status.Reason = fmt.Sprintf("Prometheus unavailable; readiness returned %s and deployment gate fails closed", resp.Status)
	}
	return status
}

func (s *Server) deployments(w http.ResponseWriter, r *http.Request) {
	service, ok := s.loadService(w, r)
	if !ok {
		return
	}
	deployments, err := s.store.ListDeployments(r.Context(), service.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"service":     service.Name,
		"deployments": deployments,
	})
}

func (s *Server) startRollout(w http.ResponseWriter, r *http.Request) {
	service, ok := s.loadService(w, r)
	if !ok {
		return
	}
	var req struct {
		Version string `json:"version"`
		Image   string `json:"image"`
		GitSHA  string `json:"gitSha"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if req.Version == "" {
		req.Version = "rollout-" + time.Now().UTC().Format("20060102150405")
	}
	var gate models.SLOStatus
	var deployment models.Deployment
	run := s.workflows.Run(r.Context(), workflows.Spec{
		Name:    "rollout-start",
		Kind:    "sentinel.rollout.start",
		Service: service.Name,
		Steps: []workflows.Step{
			{
				Name:        "evaluate-slo-deployment-gate",
				MaxAttempts: 1,
				Run: func(context.Context) error {
					gate = sloengine.Evaluate(service, sloengine.DefaultSignals(service))
					if strings.EqualFold(gate.DeploymentGate, "blocked") {
						return errors.New(gate.Reason)
					}
					return nil
				},
			},
			{
				Name:        "record-rollout-start",
				MaxAttempts: 1,
				Run: func(ctx context.Context) error {
					var err error
					deployment, err = s.store.RecordDeployment(ctx, models.Deployment{
						ServiceID:   service.ID,
						Version:     req.Version,
						Environment: service.Environment,
						Status:      "rollout-started",
						Strategy:    service.DeploymentStrategy,
						StartedAt:   time.Now().UTC(),
					})
					return err
				},
			},
		},
	})
	if run.State != workflows.StateSucceeded {
		writeJSON(w, http.StatusConflict, map[string]any{"service": service.Name, "gate": gate, "workflow": run})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"service":  service.Name,
		"rollout":  deployment,
		"gate":     gate,
		"workflow": run,
	})
}

func (s *Server) getRollout(w http.ResponseWriter, r *http.Request) {
	service, ok := s.loadService(w, r)
	if !ok {
		return
	}
	deployments, err := s.store.ListDeployments(r.Context(), service.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id := r.PathValue("id")
	for _, deployment := range deployments {
		if deployment.ID == id || deployment.Version == id || id == "latest" {
			writeJSON(w, http.StatusOK, map[string]any{"service": service.Name, "rollout": deployment})
			return
		}
	}
	writeError(w, http.StatusNotFound, "rollout not found")
}

func (s *Server) pauseRollout(w http.ResponseWriter, r *http.Request) {
	service, ok := s.loadService(w, r)
	if !ok {
		return
	}
	var deployment models.Deployment
	run := s.workflows.Run(r.Context(), workflows.Spec{
		Name:    "rollout-pause",
		Kind:    "sentinel.rollout.pause",
		Service: service.Name,
		Steps: []workflows.Step{{
			Name:        "record-rollout-pause",
			MaxAttempts: 1,
			Run: func(ctx context.Context) error {
				var err error
				deployment, err = s.store.RecordDeployment(ctx, models.Deployment{
					ServiceID:   service.ID,
					Version:     "pause-" + r.PathValue("id"),
					Environment: service.Environment,
					Status:      "rollout-paused",
					Strategy:    service.DeploymentStrategy,
					StartedAt:   time.Now().UTC(),
				})
				return err
			},
		}},
	})
	if run.State != workflows.StateSucceeded {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": run.Error, "workflow": run})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"service": service.Name, "rollout": deployment, "workflow": run})
}

func (s *Server) rollback(w http.ResponseWriter, r *http.Request) {
	service, ok := s.loadService(w, r)
	if !ok {
		return
	}
	var deployment models.Deployment
	run := s.workflows.Run(r.Context(), workflows.Spec{
		Name:    "rollout-rollback",
		Kind:    "sentinel.rollout.rollback",
		Service: service.Name,
		Steps: []workflows.Step{{
			Name:        "record-rollback-intent",
			MaxAttempts: 1,
			Run: func(ctx context.Context) error {
				var err error
				deployment, err = s.store.RecordDeployment(ctx, models.Deployment{
					ServiceID:   service.ID,
					Version:     "rollback-" + time.Now().UTC().Format("20060102150405"),
					Environment: service.Environment,
					Status:      "rollback-triggered",
					Strategy:    service.DeploymentStrategy,
					StartedAt:   time.Now().UTC(),
				})
				return err
			},
		}},
	})
	if run.State != workflows.StateSucceeded {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": run.Error, "workflow": run})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"service":    service.Name,
		"deployment": deployment,
		"workflow":   run,
		"nextAction": "GitOps controller should sync the previous stable image tag",
	})
}

func (s *Server) healthGate(w http.ResponseWriter, r *http.Request) {
	service, ok := s.loadService(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.maxBodyBytes)
	var metrics reliability.Metrics
	if err := json.NewDecoder(r.Body).Decode(&metrics); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	var decision reliability.Decision
	status := http.StatusOK
	var rollback *models.Deployment
	run := s.workflows.Run(r.Context(), workflows.Spec{
		Name:    "rollout-health-gate",
		Kind:    "sentinel.rollout.health-gate",
		Service: service.Name,
		Steps: []workflows.Step{
			{
				Name:        "evaluate-canary-sli-window",
				MaxAttempts: 1,
				Run: func(context.Context) error {
					decision = reliability.EvaluateCanary(reliability.DefaultSLO(service.SLOTarget), metrics)
					return nil
				},
			},
			{
				Name:        "record-rollback-when-policy-breaches",
				MaxAttempts: 1,
				Run: func(ctx context.Context) error {
					if decision.Healthy {
						return nil
					}
					deployment, err := s.store.RecordDeployment(ctx, models.Deployment{
						ServiceID:   service.ID,
						Version:     "healthgate-rollback-" + time.Now().UTC().Format("20060102150405"),
						Environment: service.Environment,
						Status:      "rollback-triggered",
						Strategy:    service.DeploymentStrategy,
						StartedAt:   time.Now().UTC(),
					})
					if err != nil {
						return err
					}
					rollback = &deployment
					return nil
				},
			},
		},
	})
	if run.State != workflows.StateSucceeded {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": run.Error, "workflow": run})
		return
	}
	if !decision.Healthy {
		status = http.StatusAccepted
	}
	writeJSON(w, status, map[string]any{
		"service":            service.Name,
		"metrics":            metrics,
		"decision":           decision,
		"rollbackDeployment": rollback,
		"workflow":           run,
	})
}

func (s *Server) createIncident(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.maxBodyBytes)
	var req incidents.AlertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Service == "" {
		writeError(w, http.StatusBadRequest, "service is required")
		return
	}
	service, err := s.store.GetServiceByName(r.Context(), strings.ToLower(strings.TrimSpace(req.Service)))
	if errors.Is(err, catalog.ErrNotFound) {
		writeError(w, http.StatusNotFound, "service not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Severity == "" {
		req.Severity = "critical"
	}
	if req.AlertName == "" {
		req.AlertName = "SLOViolation"
	}
	if req.Title == "" {
		req.Title = req.AlertName
	}
	signals := sloengine.DefaultSignals(service)
	if req.ErrorRate > 0 {
		signals.ErrorRatePct = req.ErrorRate
	}
	if req.Latency > 0 {
		signals.LatencyP95Ms = req.Latency
	}
	var sloStatus models.SLOStatus
	var incident models.Incident
	var events []models.IncidentEvent
	var record models.IncidentRecord
	run := s.workflows.Run(r.Context(), workflows.Spec{
		Name:    "incident-enrichment",
		Kind:    "sentinel.incident.enrichment",
		Service: service.Name,
		Steps: []workflows.Step{
			{
				Name:        "evaluate-alert-slo-impact",
				MaxAttempts: 1,
				Run: func(context.Context) error {
					if signals.ErrorRatePct > service.SLOErrorRate || signals.LatencyP95Ms > float64(service.SLOLatencyP95Ms) {
						signals.AvailabilityPct = service.SLOTarget - 0.4
						signals.BurnRate = 4.2
					}
					sloStatus = sloengine.Evaluate(service, signals)
					return nil
				},
			},
			{
				Name:        "correlate-alert-with-service-owner",
				MaxAttempts: 1,
				Run: func(context.Context) error {
					incident = models.Incident{
						ID:        "inc-" + time.Now().UTC().Format("20060102150405"),
						ServiceID: service.ID,
						Service:   service.Name,
						Severity:  strings.ToLower(req.Severity),
						Title:     req.Title,
						Status:    "active",
						AlertName: req.AlertName,
						CreatedAt: time.Now().UTC(),
					}
					return nil
				},
			},
			{
				Name:        "generate-rca-timeline",
				MaxAttempts: 1,
				Run: func(context.Context) error {
					events = incidents.Timeline(incident, service, sloStatus)
					record = models.IncidentRecord{
						Incident: incident,
						Service:  service,
						Signals:  sloStatus,
						Events:   events,
					}
					return nil
				},
			},
			{
				Name:        "persist-incident-record",
				MaxAttempts: 1,
				Run: func(context.Context) error {
					s.incidentMu.Lock()
					defer s.incidentMu.Unlock()
					s.incidents[incident.ID] = record
					return nil
				},
			},
		},
	})
	if run.State != workflows.StateSucceeded {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": run.Error, "workflow": run})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"incident": record, "workflow": run})
}

func (s *Server) listIncidents(w http.ResponseWriter, _ *http.Request) {
	s.incidentMu.RLock()
	defer s.incidentMu.RUnlock()
	records := make([]models.IncidentRecord, 0, len(s.incidents))
	for _, record := range s.incidents {
		records = append(records, record)
	}
	writeJSON(w, http.StatusOK, map[string]any{"incidents": records})
}

func (s *Server) getIncident(w http.ResponseWriter, r *http.Request) {
	record, ok := s.loadIncident(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (s *Server) resolveIncident(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var record models.IncidentRecord
	found := false
	run := s.workflows.Run(r.Context(), workflows.Spec{
		Name: "incident-resolve",
		Kind: "sentinel.incident.resolve",
		Steps: []workflows.Step{{
			Name:        "mark-incident-resolved",
			MaxAttempts: 1,
			Run: func(context.Context) error {
				s.incidentMu.Lock()
				defer s.incidentMu.Unlock()
				var ok bool
				record, ok = s.incidents[id]
				if !ok {
					return catalog.ErrNotFound
				}
				found = true
				now := time.Now().UTC()
				record.Incident.Status = "resolved"
				record.Incident.ResolvedAt = &now
				record.Events = append(record.Events, models.IncidentEvent{
					Timestamp:   now,
					EventType:   "resolved",
					Description: "incident resolved after rollback and SLO recovery checks",
				})
				s.incidents[id] = record
				return nil
			},
		}},
	})
	if !found {
		writeError(w, http.StatusNotFound, "incident not found")
		return
	}
	if run.State != workflows.StateSucceeded {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": run.Error, "workflow": run})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"incident": record, "workflow": run})
}

func (s *Server) listWorkflows(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"workflows": s.workflows.List()})
}

func (s *Server) getWorkflow(w http.ResponseWriter, r *http.Request) {
	run, ok := s.workflows.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "workflow not found")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) incidentTimeline(w http.ResponseWriter, r *http.Request) {
	record, ok := s.loadIncident(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"incident": record.Incident.ID,
		"timeline": record.Events,
	})
}

func (s *Server) incidentPostmortem(w http.ResponseWriter, r *http.Request) {
	record, ok := s.loadIncident(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	_, _ = w.Write([]byte(incidents.Postmortem(record.Incident, record.Service, record.Signals, record.Events)))
}

func (s *Server) runbook(w http.ResponseWriter, r *http.Request) {
	service, ok := s.loadService(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	_, _ = fmt.Fprintf(w, `# Incident Runbook: %s

## Ownership

- Team: %s
- Owner: %s
- Tier: %s
- Pager: %s
- Environment: %s
- Namespace: %s
- Repository: %s

## SLO

- Availability target: %.2f%%
- p95 latency target: %dms
- Error rate threshold: %.2f%%
- Deployment strategy: %s
- Rollback on failure: %t

## First checks

1. Check current rollout status.
2. Compare p95 latency, availability, burn rate, and error rate against the SLO.
3. Inspect pod restarts, CPU, memory, PDB status, and HPA saturation.
4. Review the latest deployment, Git SHA, and image scan result.
5. Roll back if the health gate fails during canary.
6. Create an incident and attach the Sentinel timeline/postmortem draft.

## Rollback

POST /api/v1/services/%s/rollback
`, service.Name, service.Team, service.Owner, service.Tier, service.Pager, service.Environment, service.Namespace,
		service.Repository, service.SLOTarget, service.SLOLatencyP95Ms, service.SLOErrorRate,
		service.DeploymentStrategy, service.RollbackOnFailure, service.Name)
}

func (s *Server) loadService(w http.ResponseWriter, r *http.Request) (models.Service, bool) {
	service, err := s.store.GetServiceByName(r.Context(), r.PathValue("name"))
	if errors.Is(err, catalog.ErrNotFound) {
		writeError(w, http.StatusNotFound, "service not found")
		return models.Service{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return models.Service{}, false
	}
	return service, true
}

func (s *Server) loadIncident(w http.ResponseWriter, r *http.Request) (models.IncidentRecord, bool) {
	s.incidentMu.RLock()
	defer s.incidentMu.RUnlock()
	record, ok := s.incidents[r.PathValue("id")]
	if !ok {
		writeError(w, http.StatusNotFound, "incident not found")
		return models.IncidentRecord{}, false
	}
	return record, true
}

func requestToService(req models.CreateServiceRequest) (models.Service, error) {
	slo := req.SLOTarget
	if slo == 0 {
		slo = req.SLOAvailability
	}
	if slo == 0 && req.SLO != "" {
		parsed, err := strconv.ParseFloat(req.SLO, 64)
		if err != nil {
			return models.Service{}, fmt.Errorf("slo must be a number")
		}
		slo = parsed
	}
	if slo == 0 {
		slo = 99.9
	}
	latencyP95 := req.SLOLatencyP95Ms
	if latencyP95 == 0 && req.SLOLatencyP95 != "" {
		parsed, err := parseMillis(req.SLOLatencyP95)
		if err != nil {
			return models.Service{}, err
		}
		latencyP95 = parsed
	}
	if latencyP95 == 0 {
		latencyP95 = 300
	}
	errorRate := req.SLOErrorRate
	if errorRate == 0 {
		errorRate = 1
	}
	namespace := req.Namespace
	if namespace == "" {
		namespace = req.Team
	}
	strategy := strings.ToLower(strings.TrimSpace(req.DeploymentStrategy))
	if strategy == "" {
		strategy = strings.ToLower(strings.TrimSpace(req.Deployment))
	}
	if strategy == "" {
		strategy = "canary"
	}
	tier := strings.ToLower(strings.TrimSpace(req.Tier))
	if tier == "" {
		tier = "medium"
	}
	repository := strings.TrimSpace(req.Repository)
	if repository == "" {
		repository = strings.TrimSpace(req.RepoURL)
	}
	if repository == "" {
		repository = "github.com/example/" + strings.ToLower(strings.TrimSpace(req.Name))
	}
	runbookURL := strings.TrimSpace(req.RunbookURL)
	if runbookURL == "" {
		runbookURL = "runbooks/incident-runbook.md"
	}
	dashboardURL := strings.TrimSpace(req.DashboardURL)
	if dashboardURL == "" {
		dashboardURL = "observability/dashboard.json"
	}
	pager := strings.TrimSpace(req.Pager)
	if pager == "" {
		pager = strings.TrimSpace(req.Owner) + "-oncall"
	}
	rollbackOnFailure := req.RollbackOnFailure || strategy == "canary" || strategy == "blue-green"
	return models.Service{
		Name:               strings.ToLower(strings.TrimSpace(req.Name)),
		Team:               strings.ToLower(strings.TrimSpace(req.Team)),
		Owner:              strings.TrimSpace(req.Owner),
		Tier:               tier,
		Language:           strings.ToLower(strings.TrimSpace(req.Language)),
		RepoURL:            strings.TrimSpace(req.RepoURL),
		Repository:         repository,
		Pager:              pager,
		RunbookURL:         runbookURL,
		DashboardURL:       dashboardURL,
		Dependencies:       trimList(req.Dependencies),
		Namespace:          strings.ToLower(strings.TrimSpace(namespace)),
		Environment:        strings.ToLower(strings.TrimSpace(req.Environment)),
		SLOTarget:          slo,
		SLOLatencyP95Ms:    latencyP95,
		SLOErrorRate:       errorRate,
		DeploymentStrategy: strategy,
		RollbackOnFailure:  rollbackOnFailure,
	}, nil
}

func sameServiceSpec(a, b models.Service) bool {
	return a.Name == b.Name &&
		a.Team == b.Team &&
		a.Owner == b.Owner &&
		a.Tier == b.Tier &&
		a.Language == b.Language &&
		a.RepoURL == b.RepoURL &&
		a.Repository == b.Repository &&
		a.Pager == b.Pager &&
		a.RunbookURL == b.RunbookURL &&
		a.DashboardURL == b.DashboardURL &&
		strings.Join(a.Dependencies, ",") == strings.Join(b.Dependencies, ",") &&
		a.Namespace == b.Namespace &&
		a.Environment == b.Environment &&
		a.SLOTarget == b.SLOTarget &&
		a.SLOLatencyP95Ms == b.SLOLatencyP95Ms &&
		a.SLOErrorRate == b.SLOErrorRate &&
		a.DeploymentStrategy == b.DeploymentStrategy &&
		a.RollbackOnFailure == b.RollbackOnFailure
}

func parseMillis(value string) (int, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimSuffix(value, "ms")
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("sloLatencyP95 must be a positive millisecond value")
	}
	return parsed, nil
}

func trimList(values []string) []string {
	var trimmed []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			trimmed = append(trimmed, value)
		}
	}
	return trimmed
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.apiToken == "" || !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		token := r.Header.Get("X-Sentinel-Token")
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.apiToken)) != 1 {
			writeError(w, http.StatusUnauthorized, "missing or invalid Sentinel API token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) logging(next http.Handler) http.Handler {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote_addr", r.RemoteAddr,
		)
	})
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		s.metrics.observe(r.Method, r.URL.Path, recorder.status)
	})
}

func (s *Server) prometheusMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = w.Write([]byte(s.metrics.render(time.Since(s.startedAt))))
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

type serverMetrics struct {
	mu       sync.Mutex
	requests map[string]int64
}

func newServerMetrics() *serverMetrics {
	return &serverMetrics{requests: make(map[string]int64)}
}

func (m *serverMetrics) observe(method, path string, status int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := fmt.Sprintf("%s|%s|%d", method, routeLabel(path), status)
	m.requests[key]++
}

func (m *serverMetrics) render(uptime time.Duration) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	var b strings.Builder
	b.WriteString("# HELP sentinel_uptime_seconds Sentinel API uptime in seconds.\n")
	b.WriteString("# TYPE sentinel_uptime_seconds gauge\n")
	fmt.Fprintf(&b, "sentinel_uptime_seconds %d\n", int(uptime.Seconds()))
	b.WriteString("# HELP sentinel_http_requests_total Total Sentinel HTTP requests.\n")
	b.WriteString("# TYPE sentinel_http_requests_total counter\n")
	for key, count := range m.requests {
		parts := strings.Split(key, "|")
		fmt.Fprintf(&b, "sentinel_http_requests_total{method=%q,route=%q,status=%q} %d\n", parts[0], parts[1], parts[2], count)
	}
	return b.String()
}

func routeLabel(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 4 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "services" {
		if len(parts) == 4 {
			return "/api/v1/services/{name}"
		}
		return "/api/v1/services/{name}/" + strings.Join(parts[4:], "/")
	}
	return path
}
