package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

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
	ContextCockpitMaxCards int
	ContextCockpitDiff     bool
	ContextRetrieval       bool
	ContextRetrievalTokens int
	ContextRetrievalChunks int
	ContextEmbeddings      bool
	ContextReranker        bool
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
	if cfg.ContextCockpitMaxCards <= 0 {
		cfg.ContextCockpitMaxCards = cockpit.DefaultMaxCards
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
	if strings.EqualFold(strings.TrimSpace(a.cfg.Mode), "inspect") && tools.TaskLooksLikeWriteRequest(task) {
		return RunResult{}, fmt.Errorf("mode inspect blocks write-like requests; switch to edit mode with /mode edit or ask for inspection only")
	}

	scopedInstructions, scopedErr := instructions.LoadScoped(a.cfg.CWD, task, a.cfg.ContextFiles, a.cfg.MaxInstructions/2)
	var skillSet skills.Result
	var skillErr error
	if a.cfg.Skills {
		skillSet, skillErr = skills.Load(a.cfg.CWD, task, a.cfg.ContextFiles, a.cfg.MaxInstructions/2)
	}

	cockpitSnapshot, cockpitErr := cockpit.Build(ctx, cockpit.Config{
		Enabled:          a.cfg.ContextCockpitEnabled,
		Inject:           a.cfg.ContextCockpitInject,
		MaxTokens:        a.cfg.ContextCockpitTokens,
		MaxFiles:         a.cfg.ContextCockpitMaxFiles,
		MaxCards:         a.cfg.ContextCockpitMaxCards,
		IncludeDiff:      a.cfg.ContextCockpitDiff,
		IncludeRetrieval: a.cfg.ContextRetrieval,
		RetrievalTokens:  a.cfg.ContextRetrievalTokens,
		RetrievalChunks:  a.cfg.ContextRetrievalChunks,
		UseEmbeddings:    a.cfg.ContextEmbeddings,
		UseReranker:      a.cfg.ContextReranker,
		IncludeRecipes:   a.cfg.ContextRecipes,
		IncludeNegative:  a.cfg.NegativeContext,
		MaxInstructions:  a.cfg.MaxInstructions,
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
	systemMessage = withCurrentTime(systemMessage, time.Now())

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
	failedToolCalls := map[string]tools.Result{}
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

		resp.ToolCalls = ensureToolCallIDs(resp.ToolCalls, step)
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
			signature := toolCallSignature(call)
			if previous, repeated := failedToolCalls[signature]; repeated {
				err := fmt.Errorf("repeated failed tool call suppressed; previous output: %s; change strategy or use a different focused tool", strings.TrimSpace(previous.Output))
				observation := toolObservation(call, tools.Result{Output: err.Error()}, err)
				window.Add(llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Content: observation})
				fmt.Fprintf(a.cfg.Output, "tool error: %v\n", err)
				if progressErr := a.emitToolProgress(ToolProgressEvent{Phase: "error", Tool: call.Name, ID: call.ID, Args: call.Arguments, Output: err.Error(), Error: err.Error()}); progressErr != nil {
					return RunResult{Final: final, Messages: sanitizeMessagesForStorage(window.Messages()), Usage: lastUsage, ContextDebug: lastDebug}, progressErr
				}
				continue
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
				failedToolCalls[signature] = result
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
	if err != nil && !streamStarted && !hasLLMResponsePayload(resp) {
		fallback, fallbackErr := a.cfg.Provider.Complete(ctx, req)
		if fallbackErr == nil {
			return fallback, false, nil
		}
		return resp, streamStarted, fmt.Errorf("%w; non-stream fallback also failed: %v", err, fallbackErr)
	}
	if reasoning.Len() > 0 {
		reasoningText := reasoning.String()
		if strings.TrimSpace(resp.Content) == "" && len(resp.ToolCalls) == 0 {
			resp.Content = reasoningText
			if a.cfg.StreamHandler != nil {
				if err := a.emitStreamEvent(llm.StreamEvent{Delta: reasoningText}); err != nil {
					return resp, streamStarted, err
				}
				streamStarted = true
			}
		} else {
			resp.Reasoning = reasoningText
		}
	}
	return resp, streamStarted, err
}

func hasLLMResponsePayload(resp llm.Response) bool {
	return strings.TrimSpace(resp.Content) != "" || len(resp.ToolCalls) > 0 || resp.ID != "" || resp.Usage.TotalTokens > 0
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
	if usage.TotalTokens == 0 && usage.InputTokens == 0 && usage.OutputTokens == 0 {
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
		if guidance := toolErrorGuidance(call, result, runErr); guidance != "" {
			payload["next_action"] = guidance
		}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return result.Output
	}
	return string(data)
}

func ensureToolCallIDs(calls []llm.ToolCall, step int) []llm.ToolCall {
	if len(calls) == 0 {
		return calls
	}
	out := append([]llm.ToolCall(nil), calls...)
	seen := map[string]bool{}
	for i := range out {
		id := strings.TrimSpace(out[i].ID)
		if id == "" || seen[id] {
			id = fmt.Sprintf("call-%d-%d", step, i+1)
		}
		for seen[id] {
			id += "-x"
		}
		out[i].ID = id
		seen[id] = true
	}
	return out
}

func toolCallSignature(call llm.ToolCall) string {
	data, err := json.Marshal(call.Arguments)
	if err != nil {
		return call.Name + ":" + fmt.Sprint(call.Arguments)
	}
	return call.Name + ":" + string(data)
}

func defaultSystemMessage() string {
	return strings.TrimSpace(`You are Klyra, a coding agent.
Operate through the tools, not guesses.
- Spend tokens like they are expensive. Do not inspect files, sessions, logs, .env, or broad repo maps unless they are needed for the user's concrete task.
- If the user asks to create a new known file, call create_file directly. Do not run project_map/search/bash first just to learn how to create a file.
- For skill creation, call guide if unsure, then create .klyra/skills/<name>.md or .klyra/skills/<name>/SKILL.md. Supporting files must stay inside that skill directory. Stop after creating the requested skill.
- First build a small context slice only when existing code must be understood: project_map/search -> file_outline/read_symbol -> short read_file ranges.
- Keep token use low: prefer guide, symbols, line ranges, repo-map facts, and focused diffs over whole files.
- Use web_search/fetch_url only for current or external internet facts, and cite URLs in the answer.
- Treat mcp_* tools as external capabilities: use them only when their name/description fits the task.
- Existing files: use replace_symbol, replace_lines, insert_lines, or diff_patch. Never overwrite an existing file with write_file.
- New files: use create_file and include a short description explaining why the file exists.
- After any tool failure, read the observation, change strategy once, and do not repeat the same failed call with the same arguments.
- Verify focused changes with the cheapest relevant check, then answer with what changed, what was checked, and remaining risk.
Never edit outside the workspace.`)
}

func toolErrorGuidance(call llm.ToolCall, result tools.Result, runErr error) string {
	text := strings.ToLower(runErr.Error() + "\n" + result.Output)
	switch call.Name {
	case "diff_preview", "diff_patch":
		return "Do not retry the same patch. Inspect the target lines, then use replace_lines/replace_symbol for small edits or rebuild the patch with exact context."
	case "write_file":
		if strings.Contains(text, "overwrite") || strings.Contains(text, "existing file") {
			return "Use replace_lines, insert_lines, replace_symbol, or diff_patch for existing files."
		}
	case "create_file":
		if strings.Contains(text, "overwrite") || strings.Contains(text, "exists") {
			return "The file already exists. Inspect it and edit it with replace_lines/replace_symbol instead of create_file."
		}
	case "read_file", "read_symbol", "file_outline":
		if strings.Contains(text, "no such file") || strings.Contains(text, "not found") {
			return "Refresh the file path with project_map, list_files, or search before trying again."
		}
	case "bash":
		return "Use the command output to choose a narrower fix or a cheaper diagnostic command."
	}
	if strings.Contains(text, "repeated failed tool call suppressed") {
		return "Choose a different tool or change the arguments before continuing."
	}
	return ""
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

func withCurrentTime(base string, now time.Time) string {
	return strings.TrimSpace(base) + "\n\nCurrent time: " + now.Format(time.RFC3339)
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
