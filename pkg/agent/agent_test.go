package agent

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"agentcli/pkg/llm"
	"agentcli/pkg/tools"
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

type scriptedProvider struct {
	responses []llm.Response
	requests  []llm.Request
}

type streamedProvider struct {
	response llm.Response
	deltas   []string
}

func (p *streamedProvider) Complete(_ context.Context, _ llm.Request) (llm.Response, error) {
	return p.response, nil
}

func (p *streamedProvider) Stream(_ context.Context, _ llm.Request, handler llm.StreamHandler) (llm.Response, error) {
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
