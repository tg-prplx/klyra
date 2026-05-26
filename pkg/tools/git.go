package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"

	"agentcli/pkg/llm"
)

type GitStatus struct{}

func (GitStatus) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "git_status",
		Description: "Return compact git status for the workspace.",
		Parameters:  objectSchema(map[string]any{}),
	}
}

func (GitStatus) Run(ctx context.Context, inv Invocation) (Result, error) {
	return runGitCommand(ctx, inv.CWD, 120, "status", "--short")
}

type GitDiff struct{}

func (GitDiff) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "git_diff",
		Description: "Return compressed git diff for tracked workspace changes.",
		Parameters: objectSchema(map[string]any{
			"max_lines": integerProperty("Maximum compressed output lines.", 1),
		}),
	}
}

func (GitDiff) Run(ctx context.Context, inv Invocation) (Result, error) {
	maxLines, err := optionalIntArg(inv.Args, "max_lines", 240)
	if err != nil {
		return Result{}, err
	}
	result, err := runGitCommand(ctx, inv.CWD, maxLines, "diff", "--", ".")
	if err != nil {
		return result, err
	}
	if result.Output == "" {
		return Result{Output: "no tracked diff"}, nil
	}
	return result, nil
}

func runGitCommand(ctx context.Context, cwd string, maxLines int, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		output += "\n" + stderr.String()
	}
	compressed := CompressOutput(output, maxLines)
	if err != nil {
		return Result{Output: compressed}, fmt.Errorf("git %v failed: %w", args, err)
	}
	return Result{Output: compressed}, nil
}
