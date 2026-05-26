package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"

	"agentcli/pkg/llm"
)

type DiffPatcher struct{}

func (DiffPatcher) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "diff_patch",
		Description: "Apply a unified diff patch in the workspace using git apply.",
		Parameters: objectSchema(map[string]any{
			"patch": stringProperty("Unified diff patch."),
		}, "patch"),
	}
}

func (DiffPatcher) Run(ctx context.Context, inv Invocation) (Result, error) {
	patch, err := stringArg(inv.Args, "patch")
	if err != nil {
		return Result{}, err
	}

	result, err := runGitApply(ctx, inv.CWD, patch, 80, "--whitespace=nowarn", "-")
	if err != nil {
		return result, fmt.Errorf("patch failed: %w", err)
	}
	return Result{Output: "patch applied"}, nil
}

type DiffPreview struct{}

func (DiffPreview) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "diff_preview",
		Description: "Validate a unified diff with git apply --check and return a compact diffstat without applying it.",
		Parameters: objectSchema(map[string]any{
			"patch":     stringProperty("Unified diff patch."),
			"max_lines": integerProperty("Maximum compressed output lines.", 1),
		}, "patch"),
	}
}

func (DiffPreview) Run(ctx context.Context, inv Invocation) (Result, error) {
	patch, err := stringArg(inv.Args, "patch")
	if err != nil {
		return Result{}, err
	}
	maxLines, err := optionalIntArg(inv.Args, "max_lines", 120)
	if err != nil {
		return Result{}, err
	}

	check, err := runGitApply(ctx, inv.CWD, patch, maxLines, "--check", "--whitespace=nowarn", "-")
	if err != nil {
		return check, fmt.Errorf("patch check failed: %w", err)
	}
	stat, err := runGitApply(ctx, inv.CWD, patch, maxLines, "--stat", "-")
	if err != nil {
		return stat, fmt.Errorf("patch stat failed: %w", err)
	}
	output := "patch check passed"
	if stat.Output != "" {
		output += "\n" + stat.Output
	}
	return Result{Output: output}, nil
}

func runGitApply(ctx context.Context, cwd, patch string, maxLines int, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"apply"}, args...)...)
	cmd.Dir = cwd
	cmd.Stdin = bytes.NewBufferString(patch)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		output += "\n" + stderr.String()
	}
	if err != nil {
		return Result{Output: CompressOutput(output, maxLines)}, err
	}
	return Result{Output: CompressOutput(output, maxLines)}, nil
}
