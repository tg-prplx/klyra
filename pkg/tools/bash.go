package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"agentcli/pkg/llm"
)

type BashRunner struct{}

func (BashRunner) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        "bash",
		Description: "Run a shell command inside the workspace and return compressed output.",
		Parameters: objectSchema(map[string]any{
			"command":         stringProperty("Command to execute."),
			"timeout_seconds": integerProperty("Timeout in seconds.", 1),
			"max_lines":       integerProperty("Maximum compressed output lines.", 1),
		}, "command"),
	}
}

func (BashRunner) Run(ctx context.Context, inv Invocation) (Result, error) {
	command, err := stringArg(inv.Args, "command")
	if err != nil {
		return Result{}, err
	}
	timeoutSeconds, err := optionalIntArg(inv.Args, "timeout_seconds", 30)
	if err != nil {
		return Result{}, err
	}
	maxLines, err := optionalIntArg(inv.Args, "max_lines", 160)
	if err != nil {
		return Result{}, err
	}

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "bash", "-lc", command)
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
	if runCtx.Err() == context.DeadlineExceeded {
		output += fmt.Sprintf("\ncommand timed out after %ds", timeoutSeconds)
	}
	compressed := CompressOutput(output, maxLines)
	if err != nil {
		return Result{Output: compressed}, fmt.Errorf("command failed: %w", err)
	}
	return Result{Output: compressed}, nil
}
