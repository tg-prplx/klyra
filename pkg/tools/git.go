package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"klyra/pkg/llm"
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
	result, err := runGitCommand(ctx, inv.CWD, 120, "status", "--short")
	if err != nil && isNotGitRepository(result.Output) {
		return Result{Output: "not a git repository"}, nil
	}
	return result, err
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
	if err != nil && isNotGitRepository(result.Output) {
		return Result{Output: "not a git repository; no tracked diff"}, nil
	}
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

func isGitRepository(ctx context.Context, cwd string) bool {
	result, err := runGitCommand(ctx, cwd, 20, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(result.Output) == "true"
}

func isNotGitRepository(output string) bool {
	normalized := strings.ToLower(output)
	return strings.Contains(normalized, "not a git repository") ||
		strings.Contains(normalized, "not a git repo")
}
