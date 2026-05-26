package tools

import (
	"context"
	"encoding/json"

	"agentcli/pkg/llm"
	"agentcli/pkg/policy"
)

type PolicyCheck struct{}

func (PolicyCheck) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "policy_check",
		Description: "Classify a shell command before running it: read, write, network, or destructive.",
		Parameters: objectSchema(map[string]any{
			"command": stringProperty("Shell command to assess."),
		}, "command"),
	}
}

func (PolicyCheck) Run(_ context.Context, inv Invocation) (Result, error) {
	command, err := stringArg(inv.Args, "command")
	if err != nil {
		return Result{}, err
	}
	data, err := json.MarshalIndent(policy.AssessShellCommand(command), "", "  ")
	if err != nil {
		return Result{}, err
	}
	return Result{Output: string(data)}, nil
}
