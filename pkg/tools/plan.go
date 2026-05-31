package tools

import (
	"context"
	"fmt"
	"strings"

	"klyra/pkg/llm"
)

const maxPlanSteps = 8

type UpdatePlan struct{}

func (UpdatePlan) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "update_plan",
		Description: "Record or update a short execution plan for a multi-step task. Use only when planning adds clarity, and update only when step status changes.",
		Parameters: objectSchema(map[string]any{
			"explanation": stringProperty("Optional short reason for this plan update."),
			"steps": map[string]any{
				"type":        "array",
				"description": "Ordered plan steps. Keep the list short and concrete.",
				"minItems":    1,
				"maxItems":    maxPlanSteps,
				"items": objectSchema(map[string]any{
					"step": stringProperty("Concrete action or milestone."),
					"status": map[string]any{
						"type":        "string",
						"description": "Current step status.",
						"enum":        []string{"pending", "in_progress", "completed"},
					},
				}, "step", "status"),
			},
		}, "steps"),
	}
}

func (UpdatePlan) Run(_ context.Context, inv Invocation) (Result, error) {
	rawSteps, ok := inv.Args["steps"].([]any)
	if !ok || len(rawSteps) == 0 {
		return Result{}, fmt.Errorf("steps must be a non-empty array")
	}
	if len(rawSteps) > maxPlanSteps {
		return Result{}, fmt.Errorf("plan has %d steps; keep at most %d", len(rawSteps), maxPlanSteps)
	}

	lines := make([]string, 0, len(rawSteps)+2)
	if explanation, _ := inv.Args["explanation"].(string); strings.TrimSpace(explanation) != "" {
		lines = append(lines, "Plan updated: "+strings.TrimSpace(explanation))
	} else {
		lines = append(lines, "Plan updated.")
	}
	inProgress := 0
	for i, raw := range rawSteps {
		step, ok := raw.(map[string]any)
		if !ok {
			return Result{}, fmt.Errorf("step %d must be an object", i+1)
		}
		text, _ := step["step"].(string)
		text = strings.TrimSpace(text)
		if text == "" {
			return Result{}, fmt.Errorf("step %d text cannot be empty", i+1)
		}
		status, _ := step["status"].(string)
		switch status {
		case "pending", "completed":
		case "in_progress":
			inProgress++
		default:
			return Result{}, fmt.Errorf("step %d has invalid status %q", i+1, status)
		}
		lines = append(lines, fmt.Sprintf("%d. [%s] %s", i+1, status, text))
	}
	if inProgress > 1 {
		return Result{}, fmt.Errorf("plan must have at most one in_progress step")
	}
	return Result{Output: strings.Join(lines, "\n")}, nil
}
