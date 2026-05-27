package agent

import (
	"bytes"
	"context"
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
					Name:      "write_file",
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
	if _, err := agent.Run(context.Background(), "write file"); err != nil {
		t.Fatal(err)
	}
	if requestedTool != "write_file" {
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
					Name:      "write_file",
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
	if _, err := agent.Run(context.Background(), "write file"); err != nil {
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
					Name:      "write_file",
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
	if _, err := agent.Run(context.Background(), "write file"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(provider.requests[1].Messages[len(provider.requests[1].Messages)-1].Content, "sandbox read-only blocks write_file") {
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
	result, err := agent.RunConversation(context.Background(), nil, "fix bug")
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
