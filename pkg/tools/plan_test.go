package tools

import (
	"context"
	"strings"
	"testing"
)

func TestUpdatePlanRendersStructuredSteps(t *testing.T) {
	result, err := (UpdatePlan{}).Run(context.Background(), Invocation{Args: map[string]any{
		"explanation": "Start with the smallest useful slice.",
		"steps": []any{
			map[string]any{"step": "Inspect parser entrypoint", "status": "in_progress"},
			map[string]any{"step": "Patch the failing branch", "status": "pending"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "[in_progress] Inspect parser entrypoint") || !strings.Contains(result.Output, "[pending] Patch the failing branch") {
		t.Fatalf("unexpected plan output:\n%s", result.Output)
	}
}

func TestUpdatePlanRejectsMultipleInProgressSteps(t *testing.T) {
	_, err := (UpdatePlan{}).Run(context.Background(), Invocation{Args: map[string]any{
		"steps": []any{
			map[string]any{"step": "Inspect", "status": "in_progress"},
			map[string]any{"step": "Patch", "status": "in_progress"},
		},
	}})
	if err == nil || !strings.Contains(err.Error(), "at most one in_progress") {
		t.Fatalf("expected in_progress validation error, got %v", err)
	}
}
