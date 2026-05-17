package workflows

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/gauravgs7/sentinel/api/internal/models"
)

const (
	StatePending   = "pending"
	StateRunning   = "running"
	StateSucceeded = "succeeded"
	StateFailed    = "failed"
)

type StepFunc func(context.Context) error

type Step struct {
	Name        string
	MaxAttempts int
	Run         StepFunc
}

type Spec struct {
	Name           string
	Kind           string
	Service        string
	IdempotencyKey string
	MaxAttempts    int
	Steps          []Step
}

type Store interface {
	SaveWorkflowRun(context.Context, models.WorkflowRun) error
	GetWorkflowRun(context.Context, string) (models.WorkflowRun, error)
	ListWorkflowRuns(context.Context) ([]models.WorkflowRun, error)
}

type Engine struct {
	mu    sync.RWMutex
	runs  map[string]models.WorkflowRun
	byKey map[string]string
	store Store
}

func NewEngine(store ...Store) *Engine {
	var durable Store
	if len(store) > 0 {
		durable = store[0]
	}
	return &Engine{
		runs:  make(map[string]models.WorkflowRun),
		byKey: make(map[string]string),
		store: durable,
	}
}

func (e *Engine) Run(ctx context.Context, spec Spec) models.WorkflowRun {
	if spec.MaxAttempts <= 0 {
		spec.MaxAttempts = 1
	}
	if spec.IdempotencyKey != "" {
		if run, ok := e.GetByIdempotencyKey(spec.IdempotencyKey); ok {
			return run
		}
	}

	run := models.WorkflowRun{
		ID:             newID(),
		Name:           spec.Name,
		Kind:           spec.Kind,
		State:          StateRunning,
		Service:        spec.Service,
		IdempotencyKey: spec.IdempotencyKey,
		Attempt:        1,
		MaxAttempts:    spec.MaxAttempts,
		StartedAt:      time.Now().UTC(),
		Events: []models.WorkflowEvent{{
			Timestamp: time.Now().UTC(),
			Type:      "workflow-started",
			Message:   fmt.Sprintf("%s workflow started", spec.Name),
		}},
	}
	for _, step := range spec.Steps {
		maxAttempts := step.MaxAttempts
		if maxAttempts <= 0 {
			maxAttempts = 1
		}
		run.Steps = append(run.Steps, models.WorkflowStep{
			Name:        step.Name,
			State:       StatePending,
			MaxAttempts: maxAttempts,
		})
	}
	e.save(ctx, run)

	for i, step := range spec.Steps {
		maxAttempts := run.Steps[i].MaxAttempts
		var err error
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			now := time.Now().UTC()
			run.Steps[i].State = StateRunning
			run.Steps[i].Attempt = attempt
			run.Steps[i].StartedAt = &now
			run.Events = append(run.Events, models.WorkflowEvent{
				Timestamp: now,
				Type:      "step-started",
				Message:   step.Name,
			})
			e.save(ctx, run)

			if step.Run != nil {
				err = step.Run(ctx)
			}
			done := time.Now().UTC()
			run.Steps[i].CompletedAt = &done
			if err == nil {
				run.Steps[i].State = StateSucceeded
				run.Events = append(run.Events, models.WorkflowEvent{
					Timestamp: done,
					Type:      "step-succeeded",
					Message:   step.Name,
				})
				e.save(ctx, run)
				break
			}
			run.Steps[i].Error = err.Error()
			run.Events = append(run.Events, models.WorkflowEvent{
				Timestamp: done,
				Type:      "step-failed",
				Message:   fmt.Sprintf("%s: %v", step.Name, err),
			})
			e.save(ctx, run)
		}
		if err != nil {
			done := time.Now().UTC()
			run.State = StateFailed
			run.Error = err.Error()
			run.CompletedAt = &done
			run.Events = append(run.Events, models.WorkflowEvent{
				Timestamp: done,
				Type:      "workflow-failed",
				Message:   err.Error(),
			})
			e.save(ctx, run)
			return run
		}
	}

	done := time.Now().UTC()
	run.State = StateSucceeded
	run.CompletedAt = &done
	run.Events = append(run.Events, models.WorkflowEvent{
		Timestamp: done,
		Type:      "workflow-succeeded",
		Message:   fmt.Sprintf("%s workflow completed", spec.Name),
	})
	e.save(ctx, run)
	return run
}

func (e *Engine) Get(id string) (models.WorkflowRun, bool) {
	e.mu.RLock()
	run, ok := e.runs[id]
	e.mu.RUnlock()
	if ok {
		return run, true
	}
	if e.store == nil {
		return models.WorkflowRun{}, false
	}
	run, err := e.store.GetWorkflowRun(context.Background(), id)
	if err != nil {
		return models.WorkflowRun{}, false
	}
	e.cache(run)
	return run, true
}

func (e *Engine) GetByIdempotencyKey(key string) (models.WorkflowRun, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	id, ok := e.byKey[key]
	if !ok {
		return models.WorkflowRun{}, false
	}
	run, ok := e.runs[id]
	return run, ok
}

func (e *Engine) List() []models.WorkflowRun {
	if e.store != nil {
		runs, err := e.store.ListWorkflowRuns(context.Background())
		if err == nil {
			e.mu.Lock()
			for _, run := range runs {
				e.runs[run.ID] = run
				if run.IdempotencyKey != "" {
					e.byKey[run.IdempotencyKey] = run.ID
				}
			}
			e.mu.Unlock()
			return runs
		}
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	runs := make([]models.WorkflowRun, 0, len(e.runs))
	for _, run := range e.runs {
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].StartedAt.After(runs[j].StartedAt)
	})
	return runs
}

func (e *Engine) save(ctx context.Context, run models.WorkflowRun) {
	e.cache(run)
	if e.store != nil {
		_ = e.store.SaveWorkflowRun(ctx, run)
	}
}

func (e *Engine) cache(run models.WorkflowRun) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.runs[run.ID] = run
	if run.IdempotencyKey != "" {
		e.byKey[run.IdempotencyKey] = run.ID
	}
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("wf-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(b[:])
	return "wf-" + encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}
