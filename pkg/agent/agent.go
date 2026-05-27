package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"klyra/pkg/cockpit"
	contextmgr "klyra/pkg/context"
	"klyra/pkg/instructions"
	"klyra/pkg/llm"
	"klyra/pkg/policy"
	"klyra/pkg/router"
	"klyra/pkg/skills"
	"klyra/pkg/tools"
)

type Config struct {
	CWD                    string
	Model                  string
	ModelRoutes            router.ModelRoutes
	MaxSteps               int
	MaxMessages            int
	MaxContext             int
	MaxInstructions        int
	MaxOutput              int
	Reasoning              string
	Store                  bool
	Stream                 bool
	ApprovalMode           string
	Sandbox                string
	Mode                   string
	ContextFiles           []string
	ContextCockpitEnabled  bool
	ContextCockpitInject   bool
	ContextCockpitTokens   int
	ContextCockpitMaxFiles int
	ContextCockpitDiff     bool
	ContextRecipes         bool
	NegativeContext        bool
	Skills                 bool
	Provider               llm.Provider
	Tools                  *tools.Registry
	Input                  io.Reader
	Output                 io.Writer
	SystemMessage          string
	Approver               ApprovalFunc
	StreamHandler          llm.StreamHandler
	ReasoningHandler       func(string) error
	ToolProgress           func(ToolProgressEvent) error
}

type ApprovalFunc func(ApprovalRequest) (bool, error)

type ApprovalRequest struct {
	Tool   string
	Risk   string
	Reason string
	Args   map[string]any
}

type ToolProgressEvent struct {
	Phase  string
	Tool   string
	ID     string
	Args   map[string]any
	Output string
	Error  string
}

type Agent struct {
	cfg Config
}

type RunResult struct {
	Final        string
	Messages     []llm.Message
	Usage        llm.Usage
	ContextDebug ContextDebug
}

type ContextDebug struct {
	Mode          string
	ContextFiles  []string
	VisibleTools  []string
	Risks         []string
	Cockpit       string
	CockpitTokens int
}

func New(cfg Config) (*Agent, error) {
	if cfg.Provider == nil {
		return nil, fmt.Errorf("provider is required")
	}
	if cfg.Tools == nil {
		cfg.Tools = tools.NewDefaultRegistry()
	}
	if cfg.MaxSteps <= 0 {
		cfg.MaxSteps = 20
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
	if strings.TrimSpace(cfg.Mode) == "" {
		cfg.Mode = "edit"
	}
	if cfg.CWD == "" {
		cfg.CWD = "."
	}
	absCWD, err := filepath.Abs(cfg.CWD)
	if err != nil {
		return nil, err
	}
	cfg.CWD = absCWD
	if cfg.MaxInstructions <= 0 {
		cfg.MaxInstructions = instructions.DefaultMaxBytes
	}
	if cfg.ContextCockpitTokens <= 0 {
		cfg.ContextCockpitTokens = cockpit.DefaultMaxTokens
	}
	if cfg.ContextCockpitMaxFiles <= 0 {
		cfg.ContextCockpitMaxFiles = cockpit.DefaultMaxFiles
	}
	systemMessage := strings.TrimSpace(cfg.SystemMessage)
	if systemMessage == "" {
		systemMessage = defaultSystemMessage()
	}
	projectInstructions, err := instructions.Load(cfg.CWD, cfg.MaxInstructions)
	if err != nil {
		return nil, err
	}
	cfg.SystemMessage = withProjectInstructions(systemMessage, projectInstructions)
	return &Agent{cfg: cfg}, nil
}

func (a *Agent) Run(ctx context.Context, task string) (string, error) {
	result, err := a.RunConversation(ctx, nil, task)
	return result.Final, err
}

func (a *Agent) RunConversation(ctx context.Context, history []llm.Message, task string) (RunResult, error) {
	return a.RunConversationWithAttachments(ctx, history, task, nil)
}

func (a *Agent) RunConversationWithAttachments(ctx context.Context, history []llm.Message, task string, attachments []llm.Attachment) (RunResult, error) {
	task = strings.TrimSpace(task)
	if task == "" {
		return RunResult{}, fmt.Errorf("task cannot be empty")
	}

	scopedInstructions, scopedErr := instructions.LoadScoped(a.cfg.CWD, task, a.cfg.ContextFiles, a.cfg.MaxInstructions/2)
	var skillSet skills.Result
	var skillErr error
	if a.cfg.Skills {
		skillSet, skillErr = skills.Load(a.cfg.CWD, task, a.cfg.ContextFiles, a.cfg.MaxInstructions/2)
	}

	cockpitSnapshot, cockpitErr := cockpit.Build(ctx, cockpit.Config{
		Enabled:         a.cfg.ContextCockpitEnabled,
		Inject:          a.cfg.ContextCockpitInject,
		MaxTokens:       a.cfg.ContextCockpitTokens,
		MaxFiles:        a.cfg.ContextCockpitMaxFiles,
		IncludeDiff:     a.cfg.ContextCockpitDiff,
		IncludeRecipes:  a.cfg.ContextRecipes,
		IncludeNegative: a.cfg.NegativeContext,
		MaxInstructions: a.cfg.MaxInstructions,
	}, a.cfg.CWD, task, a.cfg.ContextFiles)
	systemMessage := a.cfg.SystemMessage
	if scopedErr == nil && a.cfg.ContextRecipes && strings.TrimSpace(scopedInstructions.Content) != "" {
		systemMessage = withScopedInstructions(systemMessage, scopedInstructions)
	}
	if skillErr == nil && a.cfg.Skills && strings.TrimSpace(skillSet.Content) != "" {
		systemMessage = withSkills(systemMessage, skillSet)
	}
	if cockpitErr == nil && cockpitSnapshot.Enabled && cockpitSnapshot.Injected && len(cockpitSnapshot.Cards) > 0 {
		systemMessage = strings.TrimSpace(systemMessage) + "\n\nContext cockpit fact cards. Use these as a compact starting slice, then verify with tools:\n" + cockpitSnapshot.PromptText()
	}

	window := contextmgr.NewBudgetedWindow(a.cfg.MaxMessages, a.cfg.MaxContext)
	window.Add(llm.Message{Role: llm.RoleSystem, Content: systemMessage})
	for _, message := range history {
		if message.Role == llm.RoleSystem {
			continue
		}
		window.Add(message)
	}
	userMessage := llm.Message{Role: llm.RoleUser, Content: task}
	if len(attachments) > 0 {
		userMessage.Attachments = attachments
	}
	window.Add(userMessage)

	var final string
	var lastUsage llm.Usage
	var lastDebug ContextDebug
	for step := 1; step <= a.cfg.MaxSteps; step++ {
		specs := a.cfg.Tools.SpecsForTaskMode(task, a.cfg.Mode, a.cfg.ContextFiles)
		lastDebug = a.contextDebug(specs)
		if scopedErr != nil {
			lastDebug.Risks = append(lastDebug.Risks, "scoped recipes failed: "+scopedErr.Error())
		}
		if skillErr != nil {
			lastDebug.Risks = append(lastDebug.Risks, "skills failed: "+skillErr.Error())
		}
		if cockpitErr != nil {
			lastDebug.Risks = append(lastDebug.Risks, "context cockpit failed: "+cockpitErr.Error())
		} else if cockpitSnapshot.Enabled {
			lastDebug.Cockpit = cockpitSnapshot.Markdown()
			lastDebug.CockpitTokens = cockpitSnapshot.EstimatedTokens
		}
		req := llm.Request{
			Model:           router.SelectModel(a.cfg.Model, a.cfg.ModelRoutes, task),
			Messages:        window.Messages(),
			Tools:           specs,
			MaxOutputTokens: a.cfg.MaxOutput,
			ReasoningEffort: a.cfg.Reasoning,
			Store:           a.cfg.Store,
		}
		resp, streamed, err := a.complete(ctx, req)
		if err != nil {
			return RunResult{Final: final, Messages: window.Messages(), Usage: lastUsage, ContextDebug: lastDebug}, err
		}
		lastUsage = mergeUsage(lastUsage, resp.Usage)

		if strings.TrimSpace(resp.Content) != "" || len(resp.ToolCalls) > 0 {
			final = strings.TrimSpace(resp.Content)
			if final != "" && !streamed {
				fmt.Fprintf(a.cfg.Output, "\nassistant: %s\n", final)
			}
			window.Add(llm.Message{Role: llm.RoleAssistant, Content: final, Reasoning: resp.Reasoning, ToolCalls: resp.ToolCalls})
		}

		if len(resp.ToolCalls) == 0 {
			a.printUsage(lastUsage)
			return RunResult{Final: final, Messages: sanitizeMessagesForStorage(window.Messages()), Usage: lastUsage, ContextDebug: lastDebug}, nil
		}

		for _, call := range resp.ToolCalls {
			fmt.Fprintf(a.cfg.Output, "tool: %s\n", call.Name)
			if err := a.emitToolProgress(ToolProgressEvent{Phase: "queued", Tool: call.Name, ID: call.ID, Args: call.Arguments}); err != nil {
				return RunResult{Final: final, Messages: sanitizeMessagesForStorage(window.Messages()), Usage: lastUsage, ContextDebug: lastDebug}, err
			}
			if err := a.approveToolCall(call); err != nil {
				observation := toolObservation(call, tools.Result{Output: "tool call rejected by approval policy"}, err)
				window.Add(llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Content: observation})
				fmt.Fprintf(a.cfg.Output, "tool rejected: %v\n", err)
				if progressErr := a.emitToolProgress(ToolProgressEvent{Phase: "rejected", Tool: call.Name, ID: call.ID, Args: call.Arguments, Error: err.Error()}); progressErr != nil {
					return RunResult{Final: final, Messages: sanitizeMessagesForStorage(window.Messages()), Usage: lastUsage, ContextDebug: lastDebug}, progressErr
				}
				continue
			}
			if err := a.emitToolProgress(ToolProgressEvent{Phase: "running", Tool: call.Name, ID: call.ID, Args: call.Arguments}); err != nil {
				return RunResult{Final: final, Messages: sanitizeMessagesForStorage(window.Messages()), Usage: lastUsage, ContextDebug: lastDebug}, err
			}
			result, err := a.cfg.Tools.RunWithPolicy(ctx, a.cfg.CWD, a.cfg.Sandbox, a.cfg.Mode, a.cfg.ContextFiles, call)
			observation := toolObservation(call, result, err)
			window.Add(llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Content: observation})
			if err != nil {
				fmt.Fprintf(a.cfg.Output, "tool error: %v\n", err)
			}
			phase := "done"
			errText := ""
			if err != nil {
				phase = "error"
				errText = err.Error()
			}
			if progressErr := a.emitToolProgress(ToolProgressEvent{Phase: phase, Tool: call.Name, ID: call.ID, Args: call.Arguments, Output: result.Output, Error: errText}); progressErr != nil {
				return RunResult{Final: final, Messages: sanitizeMessagesForStorage(window.Messages()), Usage: lastUsage, ContextDebug: lastDebug}, progressErr
			}
		}
	}
	return RunResult{Final: final, Messages: sanitizeMessagesForStorage(window.Messages()), Usage: lastUsage, ContextDebug: lastDebug}, fmt.Errorf("agent stopped after %d steps", a.cfg.MaxSteps)
}

func (a *Agent) emitToolProgress(event ToolProgressEvent) error {
	if a.cfg.ToolProgress == nil {
		return nil
	}
	return a.cfg.ToolProgress(event)
}

func (a *Agent) contextDebug(specs []llm.ToolSpec) ContextDebug {
	tools := make([]string, 0, len(specs))
	for _, spec := range specs {
		tools = append(tools, spec.Name)
	}
	debug := ContextDebug{
		Mode:         a.cfg.Mode,
		ContextFiles: append([]string(nil), a.cfg.ContextFiles...),
		VisibleTools: tools,
	}
	switch strings.ToLower(strings.TrimSpace(a.cfg.Mode)) {
	case "inspect":
		debug.Risks = append(debug.Risks, "inspect mode: model cannot edit files; conclusions may need an edit-mode follow-up")
	case "edit":
		if len(a.cfg.ContextFiles) == 0 {
			debug.Risks = append(debug.Risks, "edit mode: no context cart files, write tools are hidden/blocked")
		}
	case "repair":
		debug.Risks = append(debug.Risks, "repair mode: verify against failing test output and current diff before broad changes")
	case "refactor":
		debug.Risks = append(debug.Risks, "refactor mode: require reference/dry-run evidence before applying broad patches")
	}
	return debug
}

func (a *Agent) complete(ctx context.Context, req llm.Request) (llm.Response, bool, error) {
	streamer, ok := a.cfg.Provider.(llm.StreamProvider)
	if !a.cfg.Stream || !ok {
		resp, err := a.cfg.Provider.Complete(ctx, req)
		return resp, false, err
	}

	streamStarted := false
	var reasoning strings.Builder
	resp, err := streamer.Stream(ctx, req, func(event llm.StreamEvent) error {
		if event.Reasoning != "" {
			reasoning.WriteString(event.Reasoning)
			if a.cfg.ReasoningHandler != nil {
				if err := a.cfg.ReasoningHandler(event.Reasoning); err != nil {
					return err
				}
				return a.emitStreamEvent(event)
			}
			fmt.Fprintf(a.cfg.Output, "reasoning: %s", event.Reasoning)
			return nil
		}
		if event.Delta == "" {
			return a.emitStreamEvent(event)
		}
		if err := a.emitStreamEvent(event); err != nil {
			return err
		}
		if a.cfg.StreamHandler != nil {
			streamStarted = true
			return nil
		}
		if !streamStarted {
			fmt.Fprint(a.cfg.Output, "\nassistant: ")
			streamStarted = true
		}
		fmt.Fprint(a.cfg.Output, event.Delta)
		return nil
	})
	if streamStarted && a.cfg.StreamHandler == nil {
		fmt.Fprintln(a.cfg.Output)
	}
	if reasoning.Len() > 0 {
		resp.Reasoning = reasoning.String()
	}
	return resp, streamStarted, err
}

func (a *Agent) emitStreamEvent(event llm.StreamEvent) error {
	if a.cfg.StreamHandler == nil {
		return nil
	}
	return a.cfg.StreamHandler(event)
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
	risk := ""
	reason := ""
	if call.Name == "bash" {
		command, _ := call.Arguments["command"].(string)
		assessment := policy.AssessShellCommand(command)
		risk = string(assessment.Risk)
		reason = assessment.Reason
		fmt.Fprintf(a.cfg.Output, "policy: %s (%s)\n", assessment.Risk, assessment.Reason)
		if strings.ToLower(strings.TrimSpace(a.cfg.ApprovalMode)) == "auto" && assessment.BlockInAuto {
			return fmt.Errorf("blocked by auto policy: %s", assessment.Reason)
		}
	}
	switch strings.ToLower(strings.TrimSpace(a.cfg.ApprovalMode)) {
	case "", "auto":
		return nil
	case "always", "allow":
		return nil
	case "never", "deny":
		return fmt.Errorf("%s requires approval", call.Name)
	case "ask":
		if a.cfg.Approver != nil {
			approved, err := a.cfg.Approver(ApprovalRequest{
				Tool:   call.Name,
				Risk:   risk,
				Reason: reason,
				Args:   call.Arguments,
			})
			if err != nil {
				return err
			}
			if approved {
				return nil
			}
			return fmt.Errorf("%s rejected by user", call.Name)
		}
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
	return strings.TrimSpace(`You are Klyra, a local coding agent.
Work in small verified steps:
1. Inspect first. Use project_map, search, file_outline, read_symbol, then small read_file slices.
2. Do not guess file contents or project rules; call tools when unsure.
3. Edit narrowly. For existing files use replace_symbol, replace_lines, insert_lines, or diff_patch. Use create_file only for new files; do not rewrite existing files from scratch.
4. Run focused checks after edits when available. Use bash only when needed.
5. Keep answers concise: what changed, what was checked, and any remaining risk.
Never edit outside the workspace.`)
}

func withProjectInstructions(base string, loaded instructions.Result) string {
	if strings.TrimSpace(loaded.Content) == "" {
		return base
	}
	var builder strings.Builder
	builder.WriteString(strings.TrimSpace(base))
	builder.WriteString("\n\nProject rules. Follow unless the user directly overrides them:\n")
	builder.WriteString(strings.TrimSpace(loaded.Content))
	if loaded.Truncated {
		builder.WriteString("\n\nSome project rules were truncated by the configured budget.")
	}
	return builder.String()
}

func withScopedInstructions(base string, loaded instructions.ScopedResult) string {
	if strings.TrimSpace(loaded.Content) == "" {
		return base
	}
	var builder strings.Builder
	builder.WriteString(strings.TrimSpace(base))
	builder.WriteString("\n\nScoped rules matched for this task:\n")
	builder.WriteString(strings.TrimSpace(loaded.Content))
	if loaded.Truncated {
		builder.WriteString("\n\nSome scoped rules were truncated by the configured budget.")
	}
	return builder.String()
}

func withSkills(base string, loaded skills.Result) string {
	if strings.TrimSpace(loaded.Content) == "" {
		return base
	}
	var builder strings.Builder
	builder.WriteString(strings.TrimSpace(base))
	builder.WriteString("\n\nSkills matched for this task. Use them as operational guidance, below direct user and project rules:\n")
	builder.WriteString(strings.TrimSpace(loaded.Content))
	if loaded.Truncated {
		builder.WriteString("\n\nSome skills were truncated by the configured budget.")
	}
	return builder.String()
}

func sanitizeMessagesForStorage(messages []llm.Message) []llm.Message {
	out := make([]llm.Message, 0, len(messages))
	for _, message := range messages {
		if len(message.Attachments) == 0 {
			out = append(out, message)
			continue
		}
		copied := message
		copied.Attachments = make([]llm.Attachment, 0, len(message.Attachments))
		var labels []string
		for _, attachment := range message.Attachments {
			sanitized := attachment
			sanitized.Data = ""
			copied.Attachments = append(copied.Attachments, sanitized)
			labels = append(labels, attachmentLabel(attachment))
		}
		if len(labels) > 0 && !strings.Contains(copied.Content, "[attachments:") {
			copied.Content = strings.TrimSpace(copied.Content + "\n\n[attachments: " + strings.Join(labels, ", ") + "]")
		}
		out = append(out, copied)
	}
	return out
}

func attachmentLabel(attachment llm.Attachment) string {
	name := strings.TrimSpace(attachment.Name)
	if name == "" {
		name = strings.TrimSpace(attachment.URL)
	}
	if name == "" {
		name = "attachment"
	}
	if attachment.MIMEType != "" {
		return fmt.Sprintf("%s %s", name, attachment.MIMEType)
	}
	return name
}
