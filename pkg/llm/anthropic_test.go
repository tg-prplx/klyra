package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAnthropicProviderParsesToolUse(t *testing.T) {
	var captured anthropicRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Fatalf("missing api key")
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Fatalf("missing anthropic-version")
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_123",
			"type": "message",
			"role": "assistant",
			"content": [
				{"type": "text", "text": "checking"},
				{"type": "tool_use", "id": "toolu_123", "name": "project_map", "input": {"max_files": 20}}
			],
			"usage": {"input_tokens": 100, "cache_read_input_tokens": 30, "output_tokens": 12}
		}`))
	}))
	defer server.Close()

	provider, err := NewAnthropicProvider("test-key", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := provider.Complete(context.Background(), Request{
		Model:           "claude-test",
		MaxOutputTokens: 512,
		Messages: []Message{
			{Role: RoleSystem, Content: "system prompt"},
			{Role: RoleUser, Content: "inspect"},
		},
		Tools: []ToolSpec{{
			Name:        "project_map",
			Description: "Map project",
			Parameters:  map[string]any{"type": "object"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if captured.Model != "claude-test" || captured.MaxTokens != 512 || captured.System != "system prompt" {
		t.Fatalf("unexpected captured request: %+v", captured)
	}
	if len(captured.Tools) != 1 || captured.Tools[0].InputSchema["type"] != "object" {
		t.Fatalf("tool schema was not sent: %+v", captured.Tools)
	}
	if resp.Content != "checking" {
		t.Fatalf("unexpected content: %q", resp.Content)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "toolu_123" || resp.ToolCalls[0].Name != "project_map" {
		t.Fatalf("unexpected tool call: %+v", resp.ToolCalls)
	}
	if resp.Usage.CachedTokens != 30 || resp.Usage.TotalTokens != 112 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}
}

func TestAnthropicMessagesIncludeToolResults(t *testing.T) {
	messages := anthropicMessages([]Message{
		{Role: RoleAssistant, Content: "checking", ToolCalls: []ToolCall{{
			ID:        "toolu_123",
			Name:      "read_file",
			Arguments: map[string]any{"path": "main.go"},
		}}},
		{Role: RoleTool, ToolCallID: "toolu_123", Content: "file contents"},
	})
	if len(messages) != 2 {
		t.Fatalf("unexpected messages: %+v", messages)
	}
	if messages[0].Content[1].Type != "tool_use" || messages[1].Content[0].Type != "tool_result" {
		t.Fatalf("tool use/result blocks not mapped: %+v", messages)
	}
}
