package models

import "time"

type Service struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Team               string    `json:"team"`
	Owner              string    `json:"owner"`
	Tier               string    `json:"tier"`
	Language           string    `json:"language"`
	RepoURL            string    `json:"repoUrl,omitempty"`
	Repository         string    `json:"repository,omitempty"`
	Pager              string    `json:"pager,omitempty"`
	RunbookURL         string    `json:"runbookUrl,omitempty"`
	DashboardURL       string    `json:"dashboardUrl,omitempty"`
	Dependencies       []string  `json:"dependencies,omitempty"`
	Namespace          string    `json:"namespace"`
	Environment        string    `json:"environment"`
	SLOTarget          float64   `json:"sloTarget"`
	SLOLatencyP95Ms    int       `json:"sloLatencyP95Ms"`
	SLOErrorRate       float64   `json:"sloErrorRate"`
	DeploymentStrategy string    `json:"deploymentStrategy"`
	RollbackOnFailure  bool      `json:"rollbackOnFailure"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

type CreateServiceRequest struct {
	Name               string   `json:"name"`
	Team               string   `json:"team"`
	Owner              string   `json:"owner"`
	Tier               string   `json:"tier"`
	Language           string   `json:"language"`
	RepoURL            string   `json:"repoUrl"`
	Repository         string   `json:"repository"`
	Pager              string   `json:"pager"`
	RunbookURL         string   `json:"runbookUrl"`
	DashboardURL       string   `json:"dashboardUrl"`
	Dependencies       []string `json:"dependencies"`
	Namespace          string   `json:"namespace"`
	Environment        string   `json:"environment"`
	SLO                string   `json:"slo"`
	SLOTarget          float64  `json:"sloTarget"`
	SLOAvailability    float64  `json:"sloAvailability"`
	SLOLatencyP95      string   `json:"sloLatencyP95"`
	SLOLatencyP95Ms    int      `json:"sloLatencyP95Ms"`
	SLOErrorRate       float64  `json:"sloErrorRate"`
	DeploymentStrategy string   `json:"deploymentStrategy"`
	Deployment         string   `json:"deployment"`
	RollbackOnFailure  bool     `json:"rollbackOnFailure"`
}

type Deployment struct {
	ID          string     `json:"id"`
	ServiceID   string     `json:"serviceId"`
	Version     string     `json:"version"`
	Environment string     `json:"environment"`
	Status      string     `json:"status"`
	Strategy    string     `json:"strategy"`
	StartedAt   time.Time  `json:"startedAt"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
}

type ServiceStatus struct {
	Service         string    `json:"service"`
	Environment     string    `json:"environment"`
	Deployment      string    `json:"deployment"`
	SLO             float64   `json:"slo"`
	P99LatencyMs    float64   `json:"p99LatencyMs"`
	ErrorRate       float64   `json:"errorRate"`
	LastDeployment  time.Time `json:"lastDeployment"`
	HealthGate      string    `json:"healthGate"`
	RollbackAllowed bool      `json:"rollbackAllowed"`
}

type Incident struct {
	ID         string     `json:"id"`
	ServiceID  string     `json:"serviceId"`
	Service    string     `json:"service"`
	Severity   string     `json:"severity"`
	Title      string     `json:"title"`
	Status     string     `json:"status"`
	AlertName  string     `json:"alertName,omitempty"`
	RootCause  string     `json:"rootCause,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	ResolvedAt *time.Time `json:"resolvedAt,omitempty"`
}

type ReadinessCheck struct {
	Name           string `json:"name"`
	Status         string `json:"status"`
	Message        string `json:"message"`
	Recommendation string `json:"recommendation,omitempty"`
	Weight         int    `json:"weight"`
}

type ReadinessReport struct {
	Service         string           `json:"service"`
	Tier            string           `json:"tier"`
	Score           int              `json:"score"`
	Passed          []ReadinessCheck `json:"passed"`
	Failed          []ReadinessCheck `json:"failed"`
	Recommendations []string         `json:"recommendations"`
	EvaluatedAt     time.Time        `json:"evaluatedAt"`
}

type SLOStatus struct {
	Service                 string    `json:"service"`
	AvailabilityTarget      float64   `json:"availabilityTarget"`
	AvailabilityCurrent     float64   `json:"availabilityCurrent"`
	ErrorBudgetRemainingPct float64   `json:"errorBudgetRemainingPct"`
	LatencyP95TargetMs      int       `json:"latencyP95TargetMs"`
	LatencyP95CurrentMs     float64   `json:"latencyP95CurrentMs"`
	ErrorRateThresholdPct   float64   `json:"errorRateThresholdPct"`
	ErrorRateCurrentPct     float64   `json:"errorRateCurrentPct"`
	BurnRate                float64   `json:"burnRate"`
	DeploymentGate          string    `json:"deploymentGate"`
	Reason                  string    `json:"reason"`
	EvaluatedAt             time.Time `json:"evaluatedAt"`
}

type IncidentEvent struct {
	Timestamp   time.Time `json:"timestamp"`
	EventType   string    `json:"eventType"`
	Description string    `json:"description"`
}

type IncidentRecord struct {
	Incident Incident        `json:"incident"`
	Service  Service         `json:"service"`
	Signals  SLOStatus       `json:"signals"`
	Events   []IncidentEvent `json:"events"`
}

type WorkflowRun struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Kind           string          `json:"kind"`
	State          string          `json:"state"`
	Service        string          `json:"service,omitempty"`
	IdempotencyKey string          `json:"idempotencyKey,omitempty"`
	Attempt        int             `json:"attempt"`
	MaxAttempts    int             `json:"maxAttempts"`
	Steps          []WorkflowStep  `json:"steps"`
	Events         []WorkflowEvent `json:"events"`
	StartedAt      time.Time       `json:"startedAt"`
	CompletedAt    *time.Time      `json:"completedAt,omitempty"`
	Error          string          `json:"error,omitempty"`
}

type WorkflowStep struct {
	Name        string     `json:"name"`
	State       string     `json:"state"`
	Attempt     int        `json:"attempt"`
	MaxAttempts int        `json:"maxAttempts"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	Error       string     `json:"error,omitempty"`
}

type WorkflowEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`
	Message   string    `json:"message"`
}
