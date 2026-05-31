package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"klyra/pkg/llm"
)

const (
	CapabilityWorkspace = "workspace"
	CapabilityEdit      = "edit"
	CapabilityGit       = "git"
	CapabilityShell     = "shell"
	CapabilityWeb       = "web"
	CapabilityPlan      = "plan"
	CapabilityExternal  = "external"
)

var supportedCapabilities = map[string]bool{
	CapabilityWorkspace: true,
	CapabilityEdit:      true,
	CapabilityGit:       true,
	CapabilityShell:     true,
	CapabilityWeb:       true,
	CapabilityPlan:      true,
	CapabilityExternal:  true,
}

type DiscoverTools struct{}

func (DiscoverTools) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "discover_tools",
		Description: "Unlock compact tool groups needed for the current task. Use this when the required tool is not visible; request only the smallest useful groups.",
		Parameters: objectSchema(map[string]any{
			"capabilities": map[string]any{
				"type":        "array",
				"description": "Tool groups to unlock for this run. edit also unlocks workspace reads.",
				"minItems":    1,
				"uniqueItems": true,
				"items": map[string]any{
					"type": "string",
					"enum": []string{
						CapabilityWorkspace,
						CapabilityEdit,
						CapabilityGit,
						CapabilityShell,
						CapabilityWeb,
						CapabilityPlan,
						CapabilityExternal,
					},
				},
			},
		}, "capabilities"),
	}
}

func (DiscoverTools) Run(_ context.Context, inv Invocation) (Result, error) {
	capabilities, err := RequestedCapabilities(inv.Args)
	if err != nil {
		return Result{}, err
	}
	return Result{Output: "Unlocked tool groups: " + strings.Join(capabilities, ", ") + ". Continue with the smallest relevant task tool."}, nil
}

func RequestedCapabilities(args map[string]any) ([]string, error) {
	raw, ok := args["capabilities"]
	if !ok {
		return nil, fmt.Errorf("missing argument %q", "capabilities")
	}
	var requested []string
	switch typed := raw.(type) {
	case []any:
		for _, value := range typed {
			text, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("capabilities must contain strings")
			}
			requested = append(requested, text)
		}
	case []string:
		requested = append(requested, typed...)
	default:
		return nil, fmt.Errorf("argument %q must be an array", "capabilities")
	}
	if len(requested) == 0 {
		return nil, fmt.Errorf("capabilities cannot be empty")
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(requested))
	for _, capability := range requested {
		capability = strings.ToLower(strings.TrimSpace(capability))
		if !supportedCapabilities[capability] {
			return nil, fmt.Errorf("unknown capability %q", capability)
		}
		if !seen[capability] {
			seen[capability] = true
			out = append(out, capability)
		}
	}
	sort.Strings(out)
	return out, nil
}
