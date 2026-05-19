package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/gauravgs7/sentinel/api/internal/models"
)

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	store := &PostgresStore{db: db}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *PostgresStore) Close() error {
	return s.db.Close()
}

func (s *PostgresStore) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, schemaSQL)
	return err
}

func (s *PostgresStore) CreateService(ctx context.Context, service models.Service) (models.Service, error) {
	if service.ID == "" {
		service.ID = NewID()
	}
	now := nowUTC()
	service.CreatedAt = now
	service.UpdatedAt = now

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO services (
			id, name, team, owner, tier, language, repo_url, repository, pager,
			runbook_url, dashboard_url, dependencies, namespace, environment,
			slo_target, slo_latency_p95_ms, slo_error_rate, deployment_strategy,
			rollback_on_failure, created_at, updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
	`, service.ID, service.Name, service.Team, service.Owner, service.Tier, service.Language,
		service.RepoURL, service.Repository, service.Pager, service.RunbookURL, service.DashboardURL,
		encodeList(service.Dependencies), service.Namespace, service.Environment, service.SLOTarget,
		service.SLOLatencyP95Ms, service.SLOErrorRate, service.DeploymentStrategy,
		service.RollbackOnFailure, service.CreatedAt, service.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return models.Service{}, ErrConflict
		}
		return models.Service{}, err
	}
	return service, nil
}

func (s *PostgresStore) ListServices(ctx context.Context) ([]models.Service, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, team, owner, tier, language, repo_url, repository, pager,
		       runbook_url, dashboard_url, dependencies, namespace, environment,
		       slo_target, slo_latency_p95_ms, slo_error_rate, deployment_strategy,
		       rollback_on_failure, created_at, updated_at
		FROM services
		ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var services []models.Service
	for rows.Next() {
		var service models.Service
		var dependencies string
		if err := rows.Scan(&service.ID, &service.Name, &service.Team, &service.Owner,
			&service.Tier, &service.Language, &service.RepoURL, &service.Repository,
			&service.Pager, &service.RunbookURL, &service.DashboardURL, &dependencies,
			&service.Namespace, &service.Environment, &service.SLOTarget, &service.SLOLatencyP95Ms,
			&service.SLOErrorRate, &service.DeploymentStrategy, &service.RollbackOnFailure,
			&service.CreatedAt, &service.UpdatedAt); err != nil {
			return nil, err
		}
		service.Dependencies = decodeList(dependencies)
		services = append(services, service)
	}
	return services, rows.Err()
}

func (s *PostgresStore) GetServiceByName(ctx context.Context, name string) (models.Service, error) {
	var service models.Service
	var dependencies string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, team, owner, tier, language, repo_url, repository, pager,
		       runbook_url, dashboard_url, dependencies, namespace, environment,
		       slo_target, slo_latency_p95_ms, slo_error_rate, deployment_strategy,
		       rollback_on_failure, created_at, updated_at
		FROM services
		WHERE name = $1
	`, name).Scan(&service.ID, &service.Name, &service.Team, &service.Owner,
		&service.Tier, &service.Language, &service.RepoURL, &service.Repository,
		&service.Pager, &service.RunbookURL, &service.DashboardURL, &dependencies,
		&service.Namespace, &service.Environment, &service.SLOTarget, &service.SLOLatencyP95Ms,
		&service.SLOErrorRate, &service.DeploymentStrategy, &service.RollbackOnFailure,
		&service.CreatedAt, &service.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Service{}, ErrNotFound
	}
	service.Dependencies = decodeList(dependencies)
	return service, err
}

func (s *PostgresStore) RecordDeployment(ctx context.Context, deployment models.Deployment) (models.Deployment, error) {
	if deployment.ID == "" {
		deployment.ID = NewID()
	}
	if deployment.StartedAt.IsZero() {
		deployment.StartedAt = nowUTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO deployments (
			id, service_id, version, environment, status, strategy, started_at, completed_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	`, deployment.ID, deployment.ServiceID, deployment.Version, deployment.Environment,
		deployment.Status, deployment.Strategy, deployment.StartedAt, nullableTime(deployment.CompletedAt))
	if err != nil {
		return models.Deployment{}, err
	}
	return deployment, nil
}

func (s *PostgresStore) LatestDeployment(ctx context.Context, serviceID string) (models.Deployment, error) {
	var deployment models.Deployment
	var completed sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT id, service_id, version, environment, status, strategy, started_at, completed_at
		FROM deployments
		WHERE service_id = $1
		ORDER BY started_at DESC
		LIMIT 1
	`, serviceID).Scan(&deployment.ID, &deployment.ServiceID, &deployment.Version,
		&deployment.Environment, &deployment.Status, &deployment.Strategy,
		&deployment.StartedAt, &completed)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Deployment{}, ErrNotFound
	}
	if completed.Valid {
		deployment.CompletedAt = &completed.Time
	}
	return deployment, err
}

func (s *PostgresStore) ListDeployments(ctx context.Context, serviceID string) ([]models.Deployment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, service_id, version, environment, status, strategy, started_at, completed_at
		FROM deployments
		WHERE service_id = $1
		ORDER BY started_at DESC
		LIMIT 50
	`, serviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deployments []models.Deployment
	for rows.Next() {
		var deployment models.Deployment
		var completed sql.NullTime
		if err := rows.Scan(&deployment.ID, &deployment.ServiceID, &deployment.Version,
			&deployment.Environment, &deployment.Status, &deployment.Strategy,
			&deployment.StartedAt, &completed); err != nil {
			return nil, err
		}
		if completed.Valid {
			deployment.CompletedAt = &completed.Time
		}
		deployments = append(deployments, deployment)
	}
	return deployments, rows.Err()
}

func (s *PostgresStore) SaveWorkflowRun(ctx context.Context, run models.WorkflowRun) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO workflow_runs (
			id, name, kind, state, service, idempotency_key, attempt, max_attempts,
			started_at, completed_at, error
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			kind = EXCLUDED.kind,
			state = EXCLUDED.state,
			service = EXCLUDED.service,
			idempotency_key = EXCLUDED.idempotency_key,
			attempt = EXCLUDED.attempt,
			max_attempts = EXCLUDED.max_attempts,
			started_at = EXCLUDED.started_at,
			completed_at = EXCLUDED.completed_at,
			error = EXCLUDED.error
	`, run.ID, run.Name, run.Kind, run.State, run.Service, run.IdempotencyKey, run.Attempt,
		run.MaxAttempts, run.StartedAt, nullableTime(run.CompletedAt), run.Error)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM workflow_steps WHERE workflow_id = $1`, run.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM workflow_events WHERE workflow_id = $1`, run.ID); err != nil {
		return err
	}
	for position, step := range run.Steps {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO workflow_steps (
				id, workflow_id, position, name, state, attempt, max_attempts,
				started_at, completed_at, error
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		`, NewID(), run.ID, position, step.Name, step.State, step.Attempt, step.MaxAttempts,
			nullableTime(step.StartedAt), nullableTime(step.CompletedAt), step.Error)
		if err != nil {
			return err
		}
	}
	for _, event := range run.Events {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO workflow_events (id, workflow_id, event_type, message, timestamp)
			VALUES ($1,$2,$3,$4,$5)
		`, NewID(), run.ID, event.Type, event.Message, event.Timestamp)
		if err != nil {
			return err
		}
	}
	err = tx.Commit()
	return err
}

func (s *PostgresStore) GetWorkflowRun(ctx context.Context, id string) (models.WorkflowRun, error) {
	run, err := s.loadWorkflowRun(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return models.WorkflowRun{}, ErrNotFound
	}
	return run, err
}

func (s *PostgresStore) GetWorkflowRunByIdempotencyKey(ctx context.Context, key string) (models.WorkflowRun, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `
		SELECT id
		FROM workflow_runs
		WHERE idempotency_key = $1
		ORDER BY started_at DESC
		LIMIT 1
	`, key).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return models.WorkflowRun{}, ErrNotFound
	}
	if err != nil {
		return models.WorkflowRun{}, err
	}
	return s.loadWorkflowRun(ctx, id)
}

func (s *PostgresStore) ListWorkflowRuns(ctx context.Context) ([]models.WorkflowRun, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id
		FROM workflow_runs
		ORDER BY started_at DESC
		LIMIT 100
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []models.WorkflowRun
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		run, err := s.loadWorkflowRun(ctx, id)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (s *PostgresStore) loadWorkflowRun(ctx context.Context, id string) (models.WorkflowRun, error) {
	var run models.WorkflowRun
	var completed sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, kind, state, service, idempotency_key, attempt, max_attempts,
		       started_at, completed_at, error
		FROM workflow_runs
		WHERE id = $1
	`, id).Scan(&run.ID, &run.Name, &run.Kind, &run.State, &run.Service, &run.IdempotencyKey,
		&run.Attempt, &run.MaxAttempts, &run.StartedAt, &completed, &run.Error)
	if err != nil {
		return models.WorkflowRun{}, err
	}
	if completed.Valid {
		run.CompletedAt = &completed.Time
	}

	steps, err := s.loadWorkflowSteps(ctx, id)
	if err != nil {
		return models.WorkflowRun{}, err
	}
	events, err := s.loadWorkflowEvents(ctx, id)
	if err != nil {
		return models.WorkflowRun{}, err
	}
	run.Steps = steps
	run.Events = events
	return run, nil
}

func (s *PostgresStore) loadWorkflowSteps(ctx context.Context, workflowID string) ([]models.WorkflowStep, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT name, state, attempt, max_attempts, started_at, completed_at, error
		FROM workflow_steps
		WHERE workflow_id = $1
		ORDER BY position ASC
	`, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var steps []models.WorkflowStep
	for rows.Next() {
		var step models.WorkflowStep
		var started, completed sql.NullTime
		if err := rows.Scan(&step.Name, &step.State, &step.Attempt, &step.MaxAttempts,
			&started, &completed, &step.Error); err != nil {
			return nil, err
		}
		if started.Valid {
			step.StartedAt = timePtr(started.Time)
		}
		if completed.Valid {
			step.CompletedAt = timePtr(completed.Time)
		}
		steps = append(steps, step)
	}
	return steps, rows.Err()
}

func (s *PostgresStore) loadWorkflowEvents(ctx context.Context, workflowID string) ([]models.WorkflowEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT timestamp, event_type, message
		FROM workflow_events
		WHERE workflow_id = $1
		ORDER BY timestamp ASC
	`, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []models.WorkflowEvent
	for rows.Next() {
		var event models.WorkflowEvent
		if err := rows.Scan(&event.Timestamp, &event.Type, &event.Message); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *PostgresStore) SaveIncidentRecord(ctx context.Context, record models.IncidentRecord) error {
	incidentJSON, err := json.Marshal(record.Incident)
	if err != nil {
		return err
	}
	serviceJSON, err := json.Marshal(record.Service)
	if err != nil {
		return err
	}
	signalsJSON, err := json.Marshal(record.Signals)
	if err != nil {
		return err
	}
	eventsJSON, err := json.Marshal(record.Events)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO incident_records (
			id, service_id, service_name, incident, service, signals, events, created_at, updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,now())
		ON CONFLICT (id) DO UPDATE SET
			service_id = EXCLUDED.service_id,
			service_name = EXCLUDED.service_name,
			incident = EXCLUDED.incident,
			service = EXCLUDED.service,
			signals = EXCLUDED.signals,
			events = EXCLUDED.events,
			updated_at = now()
	`, record.Incident.ID, record.Service.ID, record.Service.Name, incidentJSON, serviceJSON, signalsJSON,
		eventsJSON, record.Incident.CreatedAt)
	return err
}

func (s *PostgresStore) GetIncidentRecord(ctx context.Context, id string) (models.IncidentRecord, error) {
	record, err := s.loadIncidentRecord(ctx, `
		SELECT incident, service, signals, events
		FROM incident_records
		WHERE id = $1
	`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return models.IncidentRecord{}, ErrNotFound
	}
	return record, err
}

func (s *PostgresStore) ListIncidentRecords(ctx context.Context) ([]models.IncidentRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT incident, service, signals, events
		FROM incident_records
		ORDER BY created_at DESC
		LIMIT 100
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []models.IncidentRecord
	for rows.Next() {
		record, err := scanIncidentRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

type incidentScanner interface {
	Scan(dest ...any) error
}

func (s *PostgresStore) loadIncidentRecord(ctx context.Context, query string, args ...any) (models.IncidentRecord, error) {
	return scanIncidentRecord(s.db.QueryRowContext(ctx, query, args...))
}

func scanIncidentRecord(scanner incidentScanner) (models.IncidentRecord, error) {
	var record models.IncidentRecord
	var incidentJSON, serviceJSON, signalsJSON, eventsJSON []byte
	if err := scanner.Scan(&incidentJSON, &serviceJSON, &signalsJSON, &eventsJSON); err != nil {
		return models.IncidentRecord{}, err
	}
	if err := json.Unmarshal(incidentJSON, &record.Incident); err != nil {
		return models.IncidentRecord{}, err
	}
	if err := json.Unmarshal(serviceJSON, &record.Service); err != nil {
		return models.IncidentRecord{}, err
	}
	if err := json.Unmarshal(signalsJSON, &record.Signals); err != nil {
		return models.IncidentRecord{}, err
	}
	if err := json.Unmarshal(eventsJSON, &record.Events); err != nil {
		return models.IncidentRecord{}, err
	}
	return record, nil
}

func timePtr(t time.Time) *time.Time {
	return &t
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS services (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    team TEXT NOT NULL,
    owner TEXT NOT NULL,
    language TEXT NOT NULL,
    repo_url TEXT NOT NULL DEFAULT '',
    namespace TEXT NOT NULL,
    environment TEXT NOT NULL,
    slo_target NUMERIC NOT NULL,
    deployment_strategy TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE services ADD COLUMN IF NOT EXISTS tier TEXT NOT NULL DEFAULT 'medium';
ALTER TABLE services ADD COLUMN IF NOT EXISTS repository TEXT NOT NULL DEFAULT '';
ALTER TABLE services ADD COLUMN IF NOT EXISTS pager TEXT NOT NULL DEFAULT '';
ALTER TABLE services ADD COLUMN IF NOT EXISTS runbook_url TEXT NOT NULL DEFAULT '';
ALTER TABLE services ADD COLUMN IF NOT EXISTS dashboard_url TEXT NOT NULL DEFAULT '';
ALTER TABLE services ADD COLUMN IF NOT EXISTS dependencies TEXT NOT NULL DEFAULT '';
ALTER TABLE services ADD COLUMN IF NOT EXISTS slo_latency_p95_ms INTEGER NOT NULL DEFAULT 300;
ALTER TABLE services ADD COLUMN IF NOT EXISTS slo_error_rate NUMERIC NOT NULL DEFAULT 1;
ALTER TABLE services ADD COLUMN IF NOT EXISTS rollback_on_failure BOOLEAN NOT NULL DEFAULT true;

CREATE TABLE IF NOT EXISTS deployments (
    id UUID PRIMARY KEY,
    service_id UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    version TEXT NOT NULL,
    environment TEXT NOT NULL,
    status TEXT NOT NULL,
    strategy TEXT NOT NULL,
    started_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS incidents (
    id UUID PRIMARY KEY,
    service_id UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    severity TEXT NOT NULL,
    title TEXT NOT NULL,
    status TEXT NOT NULL,
    root_cause TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS slo_snapshots (
    id UUID PRIMARY KEY,
    service_id UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    current_value NUMERIC NOT NULL,
    error_budget_remaining NUMERIC NOT NULL,
    burn_rate NUMERIC NOT NULL,
    evaluated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS rollout_events (
    id UUID PRIMARY KEY,
    deployment_id UUID NOT NULL REFERENCES deployments(id) ON DELETE CASCADE,
    step TEXT NOT NULL,
    weight INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS incident_events (
    id UUID PRIMARY KEY,
    incident_id UUID NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    description TEXT NOT NULL,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS readiness_checks (
    id UUID PRIMARY KEY,
    service_id UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    check_name TEXT NOT NULL,
    status TEXT NOT NULL,
    message TEXT NOT NULL,
    evaluated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS workflow_runs (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    state TEXT NOT NULL,
    service TEXT NOT NULL DEFAULT '',
    idempotency_key TEXT NOT NULL DEFAULT '',
    attempt INTEGER NOT NULL DEFAULT 1,
    max_attempts INTEGER NOT NULL DEFAULT 1,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    error TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS workflow_steps (
    id UUID PRIMARY KEY,
    workflow_id TEXT NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    position INTEGER NOT NULL DEFAULT 0,
    name TEXT NOT NULL,
    state TEXT NOT NULL,
    attempt INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 1,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    error TEXT NOT NULL DEFAULT ''
);

ALTER TABLE workflow_steps ADD COLUMN IF NOT EXISTS position INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS workflow_events (
    id UUID PRIMARY KEY,
    workflow_id TEXT NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    message TEXT NOT NULL,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_deployments_service_started
    ON deployments(service_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_workflow_runs_service_started
    ON workflow_runs(service, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_workflow_runs_idempotency_key
    ON workflow_runs(idempotency_key)
    WHERE idempotency_key <> '';

CREATE TABLE IF NOT EXISTS incident_records (
    id TEXT PRIMARY KEY,
    service_id UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    service_name TEXT NOT NULL,
    incident JSONB NOT NULL,
    service JSONB NOT NULL,
    signals JSONB NOT NULL,
    events JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_incident_records_service_created
    ON incident_records(service_name, created_at DESC);
`

func encodeList(values []string) string {
	return strings.Join(values, ",")
}

func decodeList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
