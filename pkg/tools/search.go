package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"

	"agentcli/pkg/llm"
)

type Search struct{}

func (Search) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "search",
		Description: "Search workspace text with ripgrep and compressed output.",
		Parameters: objectSchema(map[string]any{
			"pattern":   stringProperty("Search pattern."),
			"glob":      stringProperty("Optional file glob."),
			"max_lines": integerProperty("Maximum output lines.", 1),
		}, "pattern"),
	}
}

func (Search) Run(ctx context.Context, inv Invocation) (Result, error) {
	pattern, err := stringArg(inv.Args, "pattern")
	if err != nil {
		return Result{}, err
	}
	glob, err := optionalStringArg(inv.Args, "glob", "")
	if err != nil {
		return Result{}, err
	}
	maxLines, err := optionalIntArg(inv.Args, "max_lines", 120)
	if err != nil {
		return Result{}, err
	}

	args := []string{"--line-number", "--hidden", "--glob", "!.git", pattern}
	if glob != "" {
		args = append([]string{"--line-number", "--hidden", "--glob", "!.git", "--glob", glob}, pattern)
	}
	cmd := exec.CommandContext(ctx, "rg", args...)
	cmd.Dir = inv.CWD
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		output += "\n" + stderr.String()
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return Result{Output: "no matches"}, nil
		}
		return Result{Output: CompressOutput(output, maxLines)}, fmt.Errorf("search failed: %w", err)
	}
	return Result{Output: CompressOutput(output, maxLines)}, nil
}
