package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"klyra/pkg/llm"
	"klyra/pkg/tools"
)

func TestAgentRunExecutesToolAndReturnsFinal(t *testing.T) {
	provider := &scriptedProvider{
		responses: []llm.Response{
			{
				Content: "checking",
				ToolCalls: []llm.ToolCall{{
					ID:        "call-1",
					Name:      "list_files",
					Arguments: map[string]any{"max_files": 5},
				}},
			},
			{Content: "done"},
		},
	}

	agent, err := New(Config{
		CWD:      t.TempDir(),
		Provider: provider,
	})
	if err != nil {
		t.Fatal(err)
	}

	final, err := agent.Run(context.Background(), "inspect")
	if err != nil {
		t.Fatal(err)
	}
	if final != "done" {
		t.Fatalf("unexpected final response: %q", final)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("expected two provider calls, got %d", len(provider.requests))
	}
	if !hasToolMessage(provider.requests[1].Messages) {
		t.Fatalf("expected second request to include tool observation")
	}
}

func TestAgentSuppressesRepeatedFailedToolCall(t *testing.T) {
	call := llm.ToolCall{
		ID:        "call-1",
		Name:      "always_fail",
		Arguments: map[string]any{"path": "missing.txt"},
	}
	provider := &scriptedProvider{
		responses: []llm.Response{
			{Content: "try", ToolCalls: []llm.ToolCall{call}},
			{Content: "retry", ToolCalls: []llm.ToolCall{{
				ID:        "call-2",
				Name:      call.Name,
				Arguments: call.Arguments,
			}}},
			{Content: "done"},
		},
	}
	failing := &failingTool{name: "always_fail"}
	agent, err := New(Config{
		CWD:      t.TempDir(),
		Provider: provider,
		Tools:    tools.NewRegistry(failing),
		Output:   io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := agent.RunConversation(context.Background(), nil, "fix")
	if err != nil {
		t.Fatal(err)
	}
	if result.Final != "done" {
		t.Fatalf("unexpected final response: %q", result.Final)
	}
	if failing.calls != 1 {
		t.Fatalf("expected failed tool to run once, ran %d times", failing.calls)
	}
	var suppressed bool
	for _, msg := range result.Messages {
		if msg.Role == llm.RoleTool && strings.Contains(msg.Content, "repeated failed tool call suppressed") {
			suppressed = true
		}
	}
	if !suppressed {
		t.Fatalf("expected suppression observation in messages: %+v", result.Messages)
	}
}

func TestAgentSuppressesRepeatedReadOnlyCallAndAllowsRecovery(t *testing.T) {
	call := llm.ToolCall{ID: "call-1", Name: "project_map", Arguments: map[string]any{"focus": "empty project"}}
	provider := &scriptedProvider{
		responses: []llm.Response{
			{Content: "inspect", ToolCalls: []llm.ToolCall{call}},
			{Content: "inspect again", ToolCalls: []llm.ToolCall{{ID: "call-2", Name: call.Name, Arguments: call.Arguments}}},
			{Content: "done"},
		},
	}
	projectMap := &countingTool{name: "project_map", output: "files: 0"}
	agent, err := New(Config{
		CWD:      t.TempDir(),
		Provider: provider,
		Tools:    tools.NewRegistry(projectMap),
		Output:   io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := agent.RunConversation(context.Background(), nil, "create project")
	if err != nil {
		t.Fatal(err)
	}
	if result.Final != "done" {
		t.Fatalf("unexpected final response: %q", result.Final)
	}
	if projectMap.calls != 1 {
		t.Fatalf("expected project_map to run once, ran %d times", projectMap.calls)
	}
	var suppressed bool
	for _, msg := range result.Messages {
		if msg.Role == llm.RoleTool && strings.Contains(msg.Content, "repeated read-only tool call suppressed") {
			suppressed = true
		}
	}
	if !suppressed {
		t.Fatalf("expected repeated read-only suppression observation: %+v", result.Messages)
	}
}

func TestAgentUnlocksCapabilitiesAfterDiscovery(t *testing.T) {
	provider := &scriptedProvider{responses: []llm.Response{
		{ToolCalls: []llm.ToolCall{{
			ID:        "call-1",
			Name:      "discover_tools",
			Arguments: map[string]any{"capabilities": []any{"workspace"}},
		}}},
		{Content: "done"},
	}}
	agent, err := New(Config{
		CWD:      t.TempDir(),
		Provider: provider,
		Tools:    tools.NewRegistry(tools.DiscoverTools{}, &countingTool{name: "project_map"}),
		Output:   io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := agent.RunConversation(context.Background(), nil, "arbitrary request"); err != nil {
		t.Fatal(err)
	}
	if hasToolSpecName(provider.requests[0].Tools, "project_map") {
		t.Fatalf("project_map should stay hidden before discovery: %+v", provider.requests[0].Tools)
	}
	if !hasToolSpecName(provider.requests[1].Tools, "project_map") {
		t.Fatalf("project_map should unlock after discovery: %+v", provider.requests[1].Tools)
	}
}

func TestAgentStopsRepeatedReadOnlyLoopEarly(t *testing.T) {
	call := func(id string) llm.Response {
		return llm.Response{Content: "inspect", ToolCalls: []llm.ToolCall{{
			ID: id, Name: "project_map", Arguments: map[string]any{"focus": "empty project"},
		}}}
	}
	provider := &scriptedProvider{responses: []llm.Response{call("call-1"), call("call-2"), call("call-3")}}
	projectMap := &countingTool{name: "project_map", output: "files: 0"}
	agent, err := New(Config{
		CWD:      t.TempDir(),
		Provider: provider,
		Tools:    tools.NewRegistry(projectMap),
		Output:   io.Discard,
		MaxSteps: 20,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = agent.RunConversation(context.Background(), nil, "create project")
	if err == nil || !strings.Contains(err.Error(), "repeated no-progress project_map loop") {
		t.Fatalf("expected no-progress loop error, got %v", err)
	}
	if projectMap.calls != 1 {
		t.Fatalf("expected project_map to run once, ran %d times", projectMap.calls)
	}
}

func TestAgentClearsReadOnlyCacheAfterSuccessfulWrite(t *testing.T) {
	projectCall := func(id string) llm.Response {
		return llm.Response{ToolCalls: []llm.ToolCall{{ID: id, Name: "project_map", Arguments: map[string]any{"focus": "project"}}}}
	}
	provider := &scriptedProvider{responses: []llm.Response{
		projectCall("call-1"),
		{ToolCalls: []llm.ToolCall{{ID: "call-2", Name: "create_file", Arguments: map[string]any{"path": "main.py", "content": "print('ok')"}}}},
		projectCall("call-3"),
		{Content: "done"},
	}}
	projectMap := &countingTool{name: "project_map", output: "map"}
	createFile := &countingTool{name: "create_file", output: "created"}
	agent, err := New(Config{
		CWD:          t.TempDir(),
		Provider:     provider,
		Tools:        tools.NewRegistry(projectMap, createFile),
		Output:       io.Discard,
		ApprovalMode: "always",
		Sandbox:      "workspace-write",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := agent.RunConversation(context.Background(), nil, "create project"); err != nil {
		t.Fatal(err)
	}
	if projectMap.calls != 2 {
		t.Fatalf("expected project_map cache reset after write, ran %d times", projectMap.calls)
	}
}

func TestAgentSuppressesRepeatedGuideAndAllowsRecovery(t *testing.T) {
	provider := &scriptedProvider{
		responses: []llm.Response{
			{Content: "need guidance", ToolCalls: []llm.ToolCall{{
				ID:        "call-1",
				Name:      "guide",
				Arguments: map[string]any{"query": "fix the parser"},
			}}},
			{Content: "asking again", ToolCalls: []llm.ToolCall{{
				ID:        "call-2",
				Name:      "guide",
				Arguments: map[string]any{"query": "fix the parser"},
			}}},
			{Content: "done"},
		},
	}
	agent, err := New(Config{
		CWD:      t.TempDir(),
		Provider: provider,
		Tools:    tools.NewDefaultRegistry(),
		Output:   io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := agent.RunConversation(context.Background(), nil, "fix the parser")
	if err != nil {
		t.Fatal(err)
	}
	if result.Final != "done" {
		t.Fatalf("unexpected final response: %q", result.Final)
	}
	var suppressed bool
	for _, msg := range result.Messages {
		if msg.Role == llm.RoleTool && strings.Contains(msg.Content, "repeated guide call suppressed") {
			suppressed = true
		}
	}
	if !suppressed {
		t.Fatalf("expected repeated guide suppression observation: %+v", result.Messages)
	}
}

func TestAgentStopsRepeatedGuideLoopEarly(t *testing.T) {
	guideCall := func(id string) llm.Response {
		return llm.Response{Content: "need guidance", ToolCalls: []llm.ToolCall{{
			ID:        id,
			Name:      "guide",
			Arguments: map[string]any{"query": "fix the parser"},
		}}}
	}
	provider := &scriptedProvider{
		responses: []llm.Response{
			guideCall("call-1"),
			guideCall("call-2"),
			guideCall("call-3"),
		},
	}
	agent, err := New(Config{
		CWD:      t.TempDir(),
		Provider: provider,
		Tools:    tools.NewDefaultRegistry(),
		Output:   io.Discard,
		MaxSteps: 20,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = agent.RunConversation(context.Background(), nil, "fix the parser")
	if err == nil || !strings.Contains(err.Error(), "repeated guide loop") {
		t.Fatalf("expected repeated guide loop error, got %v", err)
	}
	if len(provider.requests) != 3 {
		t.Fatalf("expected loop to stop after three provider calls, got %d", len(provider.requests))
	}
}

func TestAgentSuppressesRepeatedPlanUpdateAndAllowsRecovery(t *testing.T) {
	planCall := func(id string) llm.Response {
		return llm.Response{Content: "planning", ToolCalls: []llm.ToolCall{{
			ID:   id,
			Name: "update_plan",
			Arguments: map[string]any{"steps": []any{
				map[string]any{"step": "Inspect parser", "status": "in_progress"},
				map[string]any{"step": "Patch parser", "status": "pending"},
			}},
		}}}
	}
	provider := &scriptedProvider{
		responses: []llm.Response{
			planCall("call-1"),
			planCall("call-2"),
			{Content: "done"},
		},
	}
	agent, err := New(Config{
		CWD:      t.TempDir(),
		Provider: provider,
		Tools:    tools.NewDefaultRegistry(),
		Output:   io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := agent.RunConversation(context.Background(), nil, "plan parser refactor")
	if err != nil {
		t.Fatal(err)
	}
	if result.Final != "done" {
		t.Fatalf("unexpected final response: %q", result.Final)
	}
	var suppressed bool
	for _, msg := range result.Messages {
		if msg.Role == llm.RoleTool && strings.Contains(msg.Content, "repeated update_plan call suppressed") {
			suppressed = true
		}
	}
	if !suppressed {
		t.Fatalf("expected repeated update_plan suppression observation: %+v", result.Messages)
	}
}

func TestAgentPlanModeAddsInstructionsAndReadOnlyTools(t *testing.T) {
	provider := &scriptedProvider{responses: []llm.Response{{Content: "done"}}}
	agent, err := New(Config{
		CWD:      t.TempDir(),
		Provider: provider,
		Tools:    tools.NewDefaultRegistry(),
		Output:   io.Discard,
		Mode:     "plan",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := agent.RunConversation(context.Background(), nil, "plan a parser fix"); err != nil {
		t.Fatal(err)
	}
	req := provider.requests[0]
	if !strings.Contains(req.Messages[0].Content, "Plan mode is active") {
		t.Fatalf("expected plan mode system instructions: %s", req.Messages[0].Content)
	}
	if !hasToolSpecName(req.Tools, "update_plan") {
		t.Fatalf("expected update_plan in plan mode: %+v", req.Tools)
	}
	for _, name := range []string{"bash", "create_file", "diff_patch", "replace_lines"} {
		if hasToolSpecName(req.Tools, name) {
			t.Fatalf("plan mode exposed %s: %+v", name, req.Tools)
		}
	}
}

func TestAgentAssignsMissingToolCallID(t *testing.T) {
	provider := &scriptedProvider{
		responses: []llm.Response{
			{
				Content: "checking",
				ToolCalls: []llm.ToolCall{{
					Name:      "file_outline",
					Arguments: map[string]any{"path": "README.md"},
				}},
			},
			{Content: "done"},
		},
	}
	agent, err := New(Config{
		CWD:      t.TempDir(),
		Provider: provider,
		Tools:    tools.NewDefaultRegistry(),
		Output:   io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agent.RunConversation(context.Background(), nil, "what is in README.md?"); err != nil {
		t.Fatal(err)
	}
	messages := provider.requests[1].Messages
	var assistantCallID, toolCallID string
	for _, msg := range messages {
		if msg.Role == llm.RoleAssistant && len(msg.ToolCalls) > 0 {
			assistantCallID = msg.ToolCalls[0].ID
		}
		if msg.Role == llm.RoleTool {
			toolCallID = msg.ToolCallID
		}
	}
	if assistantCallID == "" || toolCallID == "" || assistantCallID != toolCallID {
		t.Fatalf("expected generated matching tool call ids, assistant=%q tool=%q messages=%+v", assistantCallID, toolCallID, messages)
	}
}

func TestDefaultSystemMessageTellsModelToChangeStrategyAfterToolFailure(t *testing.T) {
	system := defaultSystemMessage()
	if !strings.Contains(system, "Do not inspect broad maps") ||
		!strings.Contains(system, "create the first concrete file directly") ||
		!strings.Contains(system, "do not repeat the same call") ||
		!strings.Contains(system, "New files: use create_file") ||
		!strings.Contains(system, "versatile project assistant") {
		t.Fatalf("system prompt does not guide failed tool recovery: %s", system)
	}
}

func TestToolObservationAddsRecoveryGuidance(t *testing.T) {
	observation := toolObservation(
		llm.ToolCall{Name: "diff_preview", Arguments: map[string]any{"patch": "bad"}},
		tools.Result{Output: "git apply output"},
		fmt.Errorf("exit status 128"),
	)
	if !strings.Contains(observation, "next_action") || !strings.Contains(observation, "replace_lines") {
		t.Fatalf("expected recovery guidance in observation: %s", observation)
	}
}

func TestAgentPacksLargeHistoryBeforeProviderRequest(t *testing.T) {
	provider := &scriptedProvider{
		responses: []llm.Response{{Content: "done"}},
	}
	agent, err := New(Config{
		CWD:        t.TempDir(),
		Provider:   provider,
		MaxContext: 80,
		Output:     io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "system"},
		{Role: llm.RoleUser, Content: strings.Repeat("old ", 500)},
		{Role: llm.RoleAssistant, Content: strings.Repeat("older ", 500)},
	}
	if _, err := agent.RunConversation(context.Background(), history, "recent"); err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("expected one request, got %d", len(provider.requests))
	}
	if !strings.Contains(provider.requests[0].Messages[1].Content, "Context summary") {
		t.Fatalf("expected compacted context summary: %+v", provider.requests[0].Messages)
	}
}

func TestAgentLoadsProjectInstructionsIntoSystemPrompt(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("Use table-driven tests."), 0o644); err != nil {
		t.Fatal(err)
	}
	provider := &scriptedProvider{
		responses: []llm.Response{{Content: "done"}},
	}
	agent, err := New(Config{
		CWD:      root,
		Provider: provider,
		Output:   io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agent.Run(context.Background(), "inspect"); err != nil {
		t.Fatal(err)
	}
	system := provider.requests[0].Messages[0]
	if system.Role != llm.RoleSystem || !strings.Contains(system.Content, "Source: AGENTS.md") || !strings.Contains(system.Content, "Use table-driven tests.") {
		t.Fatalf("project instructions were not loaded into system prompt: %+v", provider.requests[0].Messages)
	}
	if !strings.Contains(system.Content, "Current time: ") {
		t.Fatalf("current time was not injected into system prompt: %s", system.Content)
	}
}

func TestAgentInjectsContextCockpitFactCards(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	provider := &scriptedProvider{
		responses: []llm.Response{{Content: "done"}},
	}
	agent, err := New(Config{
		CWD:                   root,
		Provider:              provider,
		Output:                io.Discard,
		ContextCockpitEnabled: true,
		ContextCockpitInject:  true,
		ContextCockpitTokens:  500,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := agent.RunConversation(context.Background(), nil, "inspect repo")
	if err != nil {
		t.Fatal(err)
	}
	system := provider.requests[0].Messages[0]
	if !strings.Contains(system.Content, "Context cockpit fact cards") || !strings.Contains(system.Content, "Repo Map") {
		t.Fatalf("cockpit was not injected into system prompt: %s", system.Content)
	}
	if !strings.Contains(result.ContextDebug.Cockpit, "Repo Map") || result.ContextDebug.CockpitTokens == 0 {
		t.Fatalf("expected cockpit in context debug: %+v", result.ContextDebug)
	}
}

func TestAgentInjectsScopedContextRecipes(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agentcli", "recipes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".agentcli", "recipes", "migration-rules.md"), []byte("Use reversible migrations."), 0o644); err != nil {
		t.Fatal(err)
	}
	provider := &scriptedProvider{
		responses: []llm.Response{{Content: "done"}},
	}
	agent, err := New(Config{
		CWD:            root,
		Provider:       provider,
		Output:         io.Discard,
		ContextRecipes: true,
		ContextFiles:   []string{"db/migrations/202605270001_add_users.sql"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agent.RunConversation(context.Background(), nil, "change migration"); err != nil {
		t.Fatal(err)
	}
	system := provider.requests[0].Messages[0].Content
	if !strings.Contains(system, "Scoped rules matched for this task") || !strings.Contains(system, "Use reversible migrations.") {
		t.Fatalf("scoped recipe was not injected: %s", system)
	}
}

func TestAgentInjectsMatchedSkills(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, ".klyra", "skills")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "frontend.md"), []byte("name: Frontend Cleanup\ntriggers: frontend, css\nAvoid glassmorphism."), 0o644); err != nil {
		t.Fatal(err)
	}
	provider := &scriptedProvider{
		responses: []llm.Response{{Content: "done"}},
	}
	agent, err := New(Config{
		CWD:      root,
		Provider: provider,
		Output:   io.Discard,
		Skills:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agent.RunConversation(context.Background(), nil, "remove glass from frontend css"); err != nil {
		t.Fatal(err)
	}
	system := provider.requests[0].Messages[0].Content
	if !strings.Contains(system, "Skills matched for this task") || !strings.Contains(system, "Avoid glassmorphism.") {
		t.Fatalf("matched skill was not injected: %s", system)
	}
}

func TestAgentInspectModeDoesNotGuessIntentFromTaskText(t *testing.T) {
	provider := &scriptedProvider{
		responses: []llm.Response{{Content: "switch to edit mode to modify files"}},
	}
	agent, err := New(Config{
		CWD:      t.TempDir(),
		Provider: provider,
		Output:   io.Discard,
		Mode:     "inspect",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = agent.RunConversation(context.Background(), nil, "напиши сам себе скилл"); err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("expected provider request, got %+v", provider.requests)
	}
	for _, name := range []string{"create_file", "replace_lines", "bash"} {
		if hasToolSpecName(provider.requests[0].Tools, name) {
			t.Fatalf("inspect mode exposed %s: %+v", name, provider.requests[0].Tools)
		}
	}
}

func TestAgentReplacesSavedSystemMessageWithCurrentPrompt(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("Current repo rule."), 0o644); err != nil {
		t.Fatal(err)
	}
	provider := &scriptedProvider{
		responses: []llm.Response{{Content: "done"}},
	}
	agent, err := New(Config{
		CWD:      root,
		Provider: provider,
		Output:   io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "stale prompt"},
		{Role: llm.RoleUser, Content: "previous"},
	}
	if _, err := agent.RunConversation(context.Background(), history, "next"); err != nil {
		t.Fatal(err)
	}
	messages := provider.requests[0].Messages
	systemCount := 0
	for _, message := range messages {
		if message.Role == llm.RoleSystem {
			systemCount++
			if strings.Contains(message.Content, "stale prompt") || !strings.Contains(message.Content, "Current repo rule.") {
				t.Fatalf("unexpected system prompt: %q", message.Content)
			}
		}
	}
	if systemCount != 1 {
		t.Fatalf("expected one current system prompt, got %d in %+v", systemCount, messages)
	}
}

func TestAgentDoesNotPersistAttachmentData(t *testing.T) {
	provider := &scriptedProvider{
		responses: []llm.Response{{Content: "done"}},
	}
	agent, err := New(Config{
		CWD:      t.TempDir(),
		Provider: provider,
		Output:   io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := agent.RunConversationWithAttachments(context.Background(), nil, "inspect image", []llm.Attachment{{
		Type:     "image",
		MIMEType: "image/png",
		Name:     "screen.png",
		Data:     "base64-data",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.requests[0].Messages[len(provider.requests[0].Messages)-1].Attachments) != 1 {
		t.Fatalf("attachment was not sent to provider: %+v", provider.requests[0].Messages)
	}
	var found bool
	for _, message := range result.Messages {
		for _, attachment := range message.Attachments {
			found = true
			if attachment.Data != "" {
				t.Fatalf("attachment data should not persist: %+v", result.Messages)
			}
		}
	}
	if !found {
		t.Fatalf("expected sanitized attachment metadata in history: %+v", result.Messages)
	}
	if !strings.Contains(result.Messages[len(result.Messages)-2].Content, "[attachments: screen.png image/png]") {
		t.Fatalf("expected attachment marker in stored content: %+v", result.Messages)
	}
}

func TestAgentStreamsProviderOutput(t *testing.T) {
	provider := &streamedProvider{
		response: llm.Response{Content: "hello"},
		deltas:   []string{"he", "llo"},
	}
	var output bytes.Buffer
	agent, err := New(Config{
		CWD:      t.TempDir(),
		Provider: provider,
		Stream:   true,
		Output:   &output,
	})
	if err != nil {
		t.Fatal(err)
	}
	final, err := agent.Run(context.Background(), "say hello")
	if err != nil {
		t.Fatal(err)
	}
	if final != "hello" {
		t.Fatalf("unexpected final response: %q", final)
	}
	if !strings.Contains(output.String(), "assistant: hello") {
		t.Fatalf("expected streamed output, got %q", output.String())
	}
}

func TestAgentRoutesStreamAndReasoningHandlers(t *testing.T) {
	provider := &streamedProvider{
		response:  llm.Response{Content: "hello"},
		deltas:    []string{"he", "llo"},
		reasoning: []string{"thinking"},
	}
	var output bytes.Buffer
	var streamed strings.Builder
	var reasoning strings.Builder
	agent, err := New(Config{
		CWD:      t.TempDir(),
		Provider: provider,
		Stream:   true,
		Output:   &output,
		StreamHandler: func(event llm.StreamEvent) error {
			streamed.WriteString(event.Delta)
			return nil
		},
		ReasoningHandler: func(text string) error {
			reasoning.WriteString(text)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	final, err := agent.Run(context.Background(), "say hello")
	if err != nil {
		t.Fatal(err)
	}
	if final != "hello" {
		t.Fatalf("unexpected final response: %q", final)
	}
	if streamed.String() != "hello" {
		t.Fatalf("expected handler stream, got %q", streamed.String())
	}
	if reasoning.String() != "thinking" {
		t.Fatalf("expected reasoning handler stream, got %q", reasoning.String())
	}
	if strings.Contains(output.String(), "assistant:") || strings.Contains(output.String(), "reasoning:") {
		t.Fatalf("handler output leaked into writer: %q", output.String())
	}
}

func TestAgentPromotesReasoningOnlyStreamToFinalContent(t *testing.T) {
	provider := &streamedProvider{
		reasoning: []string{"reasoning-only answer"},
		response:  llm.Response{},
	}
	var streamed strings.Builder
	agent, err := New(Config{
		CWD:       t.TempDir(),
		Provider:  provider,
		Tools:     tools.NewDefaultRegistry(),
		Stream:    true,
		Output:    io.Discard,
		MaxSteps:  1,
		MaxOutput: 100,
		StreamHandler: func(event llm.StreamEvent) error {
			streamed.WriteString(event.Delta)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := agent.RunConversation(context.Background(), nil, "say it")
	if err != nil {
		t.Fatal(err)
	}
	if result.Final != "reasoning-only answer" {
		t.Fatalf("expected reasoning-only stream to become final content, got %q", result.Final)
	}
	if streamed.String() != "reasoning-only answer" {
		t.Fatalf("expected promoted answer to stream to UI, got %q", streamed.String())
	}
	if len(result.Messages) == 0 || result.Messages[len(result.Messages)-1].Role != llm.RoleAssistant {
		t.Fatalf("expected promoted assistant message in history: %+v", result.Messages)
	}
}

func TestAgentStoresStreamedReasoningOnAssistantMessage(t *testing.T) {
	provider := &streamedProvider{
		response:  llm.Response{Content: "hello"},
		deltas:    []string{"hello"},
		reasoning: []string{"plan\n\n- inspect"},
	}
	agent, err := New(Config{
		CWD:      t.TempDir(),
		Provider: provider,
		Stream:   true,
		Output:   io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := agent.RunConversation(context.Background(), nil, "say hello")
	if err != nil {
		t.Fatal(err)
	}
	var found llm.Message
	for _, message := range result.Messages {
		if message.Role == llm.RoleAssistant {
			found = message
		}
	}
	if found.Reasoning != "plan\n\n- inspect" {
		t.Fatalf("expected stored reasoning, got %#v", found)
	}
}

func TestAgentRejectsApprovalRequiredTool(t *testing.T) {
	provider := &scriptedProvider{
		responses: []llm.Response{
			{
				ToolCalls: []llm.ToolCall{{
					ID:        "call-1",
					Name:      "write_file",
					Arguments: map[string]any{"path": "x.txt", "content": "nope"},
				}},
			},
			{Content: "done"},
		},
	}

	agent, err := New(Config{
		CWD:          t.TempDir(),
		Provider:     provider,
		Tools:        tools.NewDefaultRegistry(),
		ApprovalMode: "never",
		Output:       io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	final, err := agent.Run(context.Background(), "write file")
	if err != nil {
		t.Fatal(err)
	}
	if final != "done" {
		t.Fatalf("unexpected final response: %q", final)
	}
	if !strings.Contains(provider.requests[1].Messages[len(provider.requests[1].Messages)-1].Content, "requires approval") {
		t.Fatalf("expected approval rejection observation: %+v", provider.requests[1].Messages)
	}
}

func TestAgentUsesApprovalCallback(t *testing.T) {
	provider := &scriptedProvider{
		responses: []llm.Response{
			{
				ToolCalls: []llm.ToolCall{{
					ID:        "call-1",
					Name:      "create_file",
					Arguments: map[string]any{"path": "x.txt", "content": "ok"},
				}},
			},
			{Content: "done"},
		},
	}
	var requestedTool string
	agent, err := New(Config{
		CWD:          t.TempDir(),
		Provider:     provider,
		ApprovalMode: "ask",
		Output:       io.Discard,
		Approver: func(req ApprovalRequest) (bool, error) {
			requestedTool = req.Tool
			return false, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agent.Run(context.Background(), "create file"); err != nil {
		t.Fatal(err)
	}
	if requestedTool != "create_file" {
		t.Fatalf("approval callback was not called, got %q", requestedTool)
	}
	if !strings.Contains(provider.requests[1].Messages[len(provider.requests[1].Messages)-1].Content, "rejected by user") {
		t.Fatalf("expected rejection observation: %+v", provider.requests[1].Messages)
	}
}

func TestAgentAlwaysApprovalSkipsCallback(t *testing.T) {
	dir := t.TempDir()
	provider := &scriptedProvider{
		responses: []llm.Response{
			{
				ToolCalls: []llm.ToolCall{{
					ID:        "call-1",
					Name:      "create_file",
					Arguments: map[string]any{"path": "x.txt", "content": "ok"},
				}},
			},
			{Content: "done"},
		},
	}
	called := false
	agent, err := New(Config{
		CWD:          dir,
		Provider:     provider,
		Tools:        tools.NewDefaultRegistry(),
		ApprovalMode: "always",
		Mode:         "edit",
		ContextFiles: []string{"x.txt"},
		Output:       io.Discard,
		Approver: func(ApprovalRequest) (bool, error) {
			called = true
			return false, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agent.Run(context.Background(), "create file"); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("always approval should not call approval callback")
	}
	if data, err := os.ReadFile(filepath.Join(dir, "x.txt")); err != nil || string(data) != "ok" {
		t.Fatalf("expected tool to run under always approval, data=%q err=%v", data, err)
	}
}

func TestAgentAutoPolicyBlocksDestructiveBash(t *testing.T) {
	provider := &scriptedProvider{
		responses: []llm.Response{
			{
				ToolCalls: []llm.ToolCall{{
					ID:        "call-1",
					Name:      "bash",
					Arguments: map[string]any{"command": "git reset --hard HEAD"},
				}},
			},
			{Content: "done"},
		},
	}

	agent, err := New(Config{
		CWD:          t.TempDir(),
		Provider:     provider,
		ApprovalMode: "auto",
		Output:       io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agent.Run(context.Background(), "run risky command"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(provider.requests[1].Messages[len(provider.requests[1].Messages)-1].Content, "blocked by auto policy") {
		t.Fatalf("expected auto policy block observation: %+v", provider.requests[1].Messages)
	}
}

func TestAgentSandboxBlocksWriteTool(t *testing.T) {
	provider := &scriptedProvider{
		responses: []llm.Response{
			{
				ToolCalls: []llm.ToolCall{{
					ID:        "call-1",
					Name:      "create_file",
					Arguments: map[string]any{"path": "x.txt", "content": "x"},
				}},
			},
			{Content: "done"},
		},
	}
	agent, err := New(Config{
		CWD:          t.TempDir(),
		Provider:     provider,
		ApprovalMode: "auto",
		Sandbox:      "read-only",
		ContextFiles: []string{"x.txt"},
		Output:       io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agent.Run(context.Background(), "create file"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(provider.requests[1].Messages[len(provider.requests[1].Messages)-1].Content, "sandbox read-only blocks create_file") {
		t.Fatalf("expected sandbox block observation: %+v", provider.requests[1].Messages)
	}
}

func TestAgentModeShapesVisibleTools(t *testing.T) {
	provider := &scriptedProvider{
		responses: []llm.Response{{Content: "done"}},
	}
	agent, err := New(Config{
		CWD:      t.TempDir(),
		Provider: provider,
		Mode:     "inspect",
		Output:   io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := agent.RunConversation(context.Background(), nil, "inspect bug")
	if err != nil {
		t.Fatal(err)
	}
	if hasToolSpecName(provider.requests[0].Tools, "write_file") || hasToolSpecName(provider.requests[0].Tools, "diff_patch") {
		t.Fatalf("inspect mode exposed edit tools: %+v", provider.requests[0].Tools)
	}
	if result.ContextDebug.Mode != "inspect" || len(result.ContextDebug.Risks) == 0 {
		t.Fatalf("expected context debug for inspect mode: %+v", result.ContextDebug)
	}
}

func TestAgentFallsBackToNonStreamWhenStreamFailsBeforeOutput(t *testing.T) {
	provider := &failingStreamProvider{
		completeResponse: llm.Response{Content: "fallback answer"},
		streamErr:        fmt.Errorf("responses API returned 502 Bad Gateway with empty body"),
	}
	agent, err := New(Config{
		CWD:      t.TempDir(),
		Provider: provider,
		Output:   io.Discard,
		Stream:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := agent.RunConversation(context.Background(), nil, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if result.Final != "fallback answer" || provider.completeCalls != 1 || provider.streamCalls != 1 {
		t.Fatalf("expected non-stream fallback, result=%+v provider=%+v", result, provider)
	}
}

func hasToolSpecName(specs []llm.ToolSpec, name string) bool {
	for _, spec := range specs {
		if spec.Name == name {
			return true
		}
	}
	return false
}

type scriptedProvider struct {
	responses []llm.Response
	requests  []llm.Request
}

type streamedProvider struct {
	response  llm.Response
	deltas    []string
	reasoning []string
}

type failingStreamProvider struct {
	completeResponse llm.Response
	streamErr        error
	completeCalls    int
	streamCalls      int
}

type failingTool struct {
	name  string
	calls int
}

type countingTool struct {
	name   string
	output string
	calls  int
}

func (t *countingTool) Spec() llm.ToolSpec {
	return llm.ToolSpec{Name: t.name, Description: "Counts calls for tests.", Parameters: map[string]any{"type": "object"}}
}

func (t *countingTool) Run(context.Context, tools.Invocation) (tools.Result, error) {
	t.calls++
	return tools.Result{Output: t.output}, nil
}

func (t *failingTool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:        t.name,
		Description: "Always fails for tests.",
		Parameters:  map[string]any{"type": "object"},
	}
}

func (t *failingTool) Run(context.Context, tools.Invocation) (tools.Result, error) {
	t.calls++
	return tools.Result{Output: "synthetic failure"}, fmt.Errorf("synthetic failure")
}

func (p *streamedProvider) Complete(_ context.Context, _ llm.Request) (llm.Response, error) {
	return p.response, nil
}

func (p *streamedProvider) Stream(_ context.Context, _ llm.Request, handler llm.StreamHandler) (llm.Response, error) {
	for _, reasoning := range p.reasoning {
		if err := handler(llm.StreamEvent{Reasoning: reasoning}); err != nil {
			return llm.Response{}, err
		}
	}
	for _, delta := range p.deltas {
		if err := handler(llm.StreamEvent{Delta: delta}); err != nil {
			return llm.Response{}, err
		}
	}
	return p.response, nil
}

func (p *failingStreamProvider) Complete(_ context.Context, _ llm.Request) (llm.Response, error) {
	p.completeCalls++
	return p.completeResponse, nil
}

func (p *failingStreamProvider) Stream(_ context.Context, _ llm.Request, _ llm.StreamHandler) (llm.Response, error) {
	p.streamCalls++
	return llm.Response{}, p.streamErr
}

func (p *scriptedProvider) Complete(_ context.Context, req llm.Request) (llm.Response, error) {
	p.requests = append(p.requests, req)
	resp := p.responses[0]
	p.responses = p.responses[1:]
	return resp, nil
}

func hasToolMessage(messages []llm.Message) bool {
	for _, msg := range messages {
		if msg.Role == llm.RoleTool && strings.Contains(msg.Content, "list_files") {
			return true
		}
	}
	return false
}
