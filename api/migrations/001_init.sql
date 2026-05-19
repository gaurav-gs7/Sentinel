CREATE TABLE IF NOT EXISTS services (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    team TEXT NOT NULL,
    owner TEXT NOT NULL,
    tier TEXT NOT NULL DEFAULT 'medium',
    language TEXT NOT NULL,
    repo_url TEXT NOT NULL DEFAULT '',
    repository TEXT NOT NULL DEFAULT '',
    pager TEXT NOT NULL DEFAULT '',
    runbook_url TEXT NOT NULL DEFAULT '',
    dashboard_url TEXT NOT NULL DEFAULT '',
    dependencies TEXT NOT NULL DEFAULT '',
    namespace TEXT NOT NULL,
    environment TEXT NOT NULL,
    slo_target NUMERIC NOT NULL,
    slo_latency_p95_ms INTEGER NOT NULL DEFAULT 300,
    slo_error_rate NUMERIC NOT NULL DEFAULT 1,
    deployment_strategy TEXT NOT NULL,
    rollback_on_failure BOOLEAN NOT NULL DEFAULT true,
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
