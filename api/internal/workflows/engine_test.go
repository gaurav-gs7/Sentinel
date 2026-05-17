package workflows

import (
	"context"
	"errors"
	"testing"
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
