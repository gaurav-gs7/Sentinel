package workflows

import (
	"context"
	"errors"
	"testing"

	"github.com/gauravgs7/sentinel/api/internal/models"
)

func TestEngineRunsStepsInOrder(t *testing.T) {
	engine := NewEngine()
	var order []string
	run := engine.Run(context.Background(), Spec{
		Name:           "test-workflow",
		Kind:           "test",
		IdempotencyKey: "test-key",
		Steps: []Step{
			{Name: "first", Run: func(context.Context) error {
				order = append(order, "first")
				return nil
			}},
			{Name: "second", Run: func(context.Context) error {
				order = append(order, "second")
				return nil
			}},
		},
	})
	if run.State != StateSucceeded {
		t.Fatalf("expected succeeded workflow, got %+v", run)
	}
	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Fatalf("steps ran out of order: %v", order)
	}
	cached, ok := engine.GetByIdempotencyKey("test-key")
	if !ok || cached.ID != run.ID {
		t.Fatalf("expected idempotent run lookup")
	}
}

func TestEngineRetriesAndFails(t *testing.T) {
	engine := NewEngine()
	attempts := 0
	run := engine.Run(context.Background(), Spec{
		Name: "failure-workflow",
		Kind: "test",
		Steps: []Step{{
			Name:        "retryable",
			MaxAttempts: 2,
			Run: func(context.Context) error {
				attempts++
				return errors.New("nope")
			},
		}},
	})
	if run.State != StateFailed {
		t.Fatalf("expected failed workflow, got %+v", run)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if run.Steps[0].Attempt != 2 {
		t.Fatalf("expected step attempt 2, got %+v", run.Steps[0])
	}
}

func TestEngineFindsIdempotencyKeyFromDurableStore(t *testing.T) {
	store := newWorkflowMemoryStore()
	first := NewEngine(store)
	run := first.Run(context.Background(), Spec{
		Name:           "durable-idempotency",
		Kind:           "test",
		IdempotencyKey: "same-operation",
		Steps: []Step{{
			Name: "once",
			Run:  func(context.Context) error { return nil },
		}},
	})
	second := NewEngine(store)
	cached, ok := second.GetByIdempotencyKey("same-operation")
	if !ok {
		t.Fatal("expected durable idempotency lookup")
	}
	if cached.ID != run.ID {
		t.Fatalf("expected same workflow id %s, got %s", run.ID, cached.ID)
	}
}

type workflowMemoryStore struct {
	runs map[string]models.WorkflowRun
}

func newWorkflowMemoryStore() *workflowMemoryStore {
	return &workflowMemoryStore{runs: make(map[string]models.WorkflowRun)}
}

func (s *workflowMemoryStore) SaveWorkflowRun(_ context.Context, run models.WorkflowRun) error {
	s.runs[run.ID] = run
	return nil
}

func (s *workflowMemoryStore) GetWorkflowRun(_ context.Context, id string) (models.WorkflowRun, error) {
	run, ok := s.runs[id]
	if !ok {
		return models.WorkflowRun{}, errors.New("not found")
	}
	return run, nil
}

func (s *workflowMemoryStore) GetWorkflowRunByIdempotencyKey(_ context.Context, key string) (models.WorkflowRun, error) {
	for _, run := range s.runs {
		if run.IdempotencyKey == key {
			return run, nil
		}
	}
	return models.WorkflowRun{}, errors.New("not found")
}

func (s *workflowMemoryStore) ListWorkflowRuns(context.Context) ([]models.WorkflowRun, error) {
	runs := make([]models.WorkflowRun, 0, len(s.runs))
	for _, run := range s.runs {
		runs = append(runs, run)
	}
	return runs, nil
}
