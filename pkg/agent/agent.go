package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	contextmgr "agentcli/pkg/context"
	"agentcli/pkg/llm"
	"agentcli/pkg/policy"
	"agentcli/pkg/router"
	"agentcli/pkg/tools"
)

type Config struct {
	CWD           string
	Model         string
	ModelRoutes   router.ModelRoutes
	MaxSteps      int
	MaxMessages   int
	MaxContext    int
	MaxOutput     int
	Reasoning     string
	Store         bool
	Stream        bool
	ApprovalMode  string
	Sandbox       string
	Provider      llm.Provider
	Tools         *tools.Registry
	Input         io.Reader
	Output        io.Writer
	SystemMessage string
}

type Agent struct {
	cfg Config
}

type RunResult struct {
	Final    string
	Messages []llm.Message
	Usage    llm.Usage
}

func New(cfg Config) (*Agent, error) {
	if cfg.Provider == nil {
		return nil, fmt.Errorf("provider is required")
	}
	if cfg.Tools == nil {
		cfg.Tools = tools.NewDefaultRegistry()
	}
	if cfg.MaxSteps <= 0 {
		cfg.MaxSteps = 8
	}
	if cfg.MaxMessages <= 0 {
		cfg.MaxMessages = 40
	}
	if cfg.Output == nil {
		cfg.Output = io.Discard
	}
	if cfg.Input == nil {
		cfg.Input = strings.NewReader("")
	}
	if strings.TrimSpace(cfg.ApprovalMode) == "" {
		cfg.ApprovalMode = "auto"
	}
	if cfg.CWD == "" {
		cfg.CWD = "."
	}
	absCWD, err := filepath.Abs(cfg.CWD)
	if err != nil {
		return nil, err
	}
	cfg.CWD = absCWD
	if cfg.SystemMessage == "" {
		cfg.SystemMessage = defaultSystemMessage()
	}
	return &Agent{cfg: cfg}, nil
}

func (a *Agent) Run(ctx context.Context, task string) (string, error) {
	result, err := a.RunConversation(ctx, nil, task)
	return result.Final, err
}

func (a *Agent) RunConversation(ctx context.Context, history []llm.Message, task string) (RunResult, error) {
	task = strings.TrimSpace(task)
	if task == "" {
		return RunResult{}, fmt.Errorf("task cannot be empty")
	}

	window := contextmgr.NewBudgetedWindow(a.cfg.MaxMessages, a.cfg.MaxContext)
	if !hasSystemMessage(history) {
		window.Add(llm.Message{Role: llm.RoleSystem, Content: a.cfg.SystemMessage})
	}
	for _, message := range history {
		window.Add(message)
	}
	window.Add(llm.Message{Role: llm.RoleUser, Content: task})

	var final string
	var lastUsage llm.Usage
	for step := 1; step <= a.cfg.MaxSteps; step++ {
		req := llm.Request{
			Model:           router.SelectModel(a.cfg.Model, a.cfg.ModelRoutes, task),
			Messages:        window.Messages(),
			Tools:           a.cfg.Tools.SpecsForTask(task),
			MaxOutputTokens: a.cfg.MaxOutput,
			ReasoningEffort: a.cfg.Reasoning,
			Store:           a.cfg.Store,
		}
		resp, streamed, err := a.complete(ctx, req)
		if err != nil {
			return RunResult{Final: final, Messages: window.Messages(), Usage: lastUsage}, err
		}
		lastUsage = mergeUsage(lastUsage, resp.Usage)

		if strings.TrimSpace(resp.Content) != "" || len(resp.ToolCalls) > 0 {
			final = strings.TrimSpace(resp.Content)
			if final != "" && !streamed {
				fmt.Fprintf(a.cfg.Output, "\nassistant: %s\n", final)
			}
			window.Add(llm.Message{Role: llm.RoleAssistant, Content: final, ToolCalls: resp.ToolCalls})
		}

		if len(resp.ToolCalls) == 0 {
			a.printUsage(lastUsage)
			return RunResult{Final: final, Messages: window.Messages(), Usage: lastUsage}, nil
		}

		for _, call := range resp.ToolCalls {
			fmt.Fprintf(a.cfg.Output, "tool: %s\n", call.Name)
			if err := a.approveToolCall(call); err != nil {
				observation := toolObservation(call, tools.Result{Output: "tool call rejected by approval policy"}, err)
				window.Add(llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Content: observation})
				fmt.Fprintf(a.cfg.Output, "tool rejected: %v\n", err)
				continue
			}
			result, err := a.cfg.Tools.RunWithSandbox(ctx, a.cfg.CWD, a.cfg.Sandbox, call)
			observation := toolObservation(call, result, err)
			window.Add(llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Content: observation})
			if err != nil {
				fmt.Fprintf(a.cfg.Output, "tool error: %v\n", err)
			}
		}
	}
	return RunResult{Final: final, Messages: window.Messages(), Usage: lastUsage}, fmt.Errorf("agent stopped after %d steps", a.cfg.MaxSteps)
}

func (a *Agent) complete(ctx context.Context, req llm.Request) (llm.Response, bool, error) {
	streamer, ok := a.cfg.Provider.(llm.StreamProvider)
	if !a.cfg.Stream || !ok {
		resp, err := a.cfg.Provider.Complete(ctx, req)
		return resp, false, err
	}

	streamStarted := false
	resp, err := streamer.Stream(ctx, req, func(event llm.StreamEvent) error {
		if event.Delta == "" {
			return nil
		}
		if !streamStarted {
			fmt.Fprint(a.cfg.Output, "\nassistant: ")
			streamStarted = true
		}
		fmt.Fprint(a.cfg.Output, event.Delta)
		return nil
	})
	if streamStarted {
		fmt.Fprintln(a.cfg.Output)
	}
	return resp, streamStarted, err
}

func hasSystemMessage(messages []llm.Message) bool {
	for _, message := range messages {
		if message.Role == llm.RoleSystem {
			return true
		}
	}
	return false
}

func mergeUsage(left, right llm.Usage) llm.Usage {
	return llm.Usage{
		InputTokens:     left.InputTokens + right.InputTokens,
		CachedTokens:    left.CachedTokens + right.CachedTokens,
		OutputTokens:    left.OutputTokens + right.OutputTokens,
		ReasoningTokens: left.ReasoningTokens + right.ReasoningTokens,
		TotalTokens:     left.TotalTokens + right.TotalTokens,
	}
}

func (a *Agent) approveToolCall(call llm.ToolCall) error {
	if !tools.RequiresApproval(call.Name) {
		return nil
	}
	if call.Name == "bash" {
		command, _ := call.Arguments["command"].(string)
		assessment := policy.AssessShellCommand(command)
		fmt.Fprintf(a.cfg.Output, "policy: %s (%s)\n", assessment.Risk, assessment.Reason)
		if strings.ToLower(strings.TrimSpace(a.cfg.ApprovalMode)) == "auto" && assessment.BlockInAuto {
			return fmt.Errorf("blocked by auto policy: %s", assessment.Reason)
		}
	}
	switch strings.ToLower(strings.TrimSpace(a.cfg.ApprovalMode)) {
	case "", "auto", "always":
		return nil
	case "never", "deny":
		return fmt.Errorf("%s requires approval", call.Name)
	case "ask":
		fmt.Fprintf(a.cfg.Output, "approve %s? [y/N]: ", call.Name)
		reader := bufio.NewReader(a.cfg.Input)
		answer, err := reader.ReadString('\n')
		if err != nil && strings.TrimSpace(answer) == "" {
			return fmt.Errorf("approval prompt failed: %w", err)
		}
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer == "y" || answer == "yes" {
			return nil
		}
		return fmt.Errorf("%s rejected by user", call.Name)
	default:
		return fmt.Errorf("unknown approval mode %q", a.cfg.ApprovalMode)
	}
}

func (a *Agent) printUsage(usage llm.Usage) {
	if usage.TotalTokens == 0 {
		return
	}
	fmt.Fprintf(a.cfg.Output, "usage: input=%d cached=%d output=%d reasoning=%d total=%d\n",
		usage.InputTokens,
		usage.CachedTokens,
		usage.OutputTokens,
		usage.ReasoningTokens,
		usage.TotalTokens,
	)
}

func toolObservation(call llm.ToolCall, result tools.Result, runErr error) string {
	payload := map[string]any{
		"tool":   call.Name,
		"output": result.Output,
	}
	if runErr != nil {
		payload["error"] = runErr.Error()
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return result.Output
	}
	return string(data)
}

func defaultSystemMessage() string {
	return strings.TrimSpace(`You are an agentic coding CLI.
Use tools to inspect the workspace before changing files.
Prefer precise retrieval over reading entire large files.
Start broad coding tasks with project_map, then use search/read_go_symbol/read_file slices.
Use diff_patch for edits when possible and bash only when the command is necessary.
Return concise progress and final summaries.
Do not edit files outside the workspace.`)
}
