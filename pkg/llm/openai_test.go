package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIProviderParsesToolCalls(t *testing.T) {
	var captured openAIChatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("missing authorization header")
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "checking",
					"tool_calls": [{
						"id": "call-1",
						"type": "function",
						"function": {
							"name": "read_file",
							"arguments": "{\"path\":\"main.go\"}"
						}
					}]
				}
			}]
		}`))
	}))
	defer server.Close()

	provider, err := NewOpenAIProvider("test-key", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := provider.Complete(context.Background(), Request{
		Model: "test-model",
		Messages: []Message{
			{Role: RoleSystem, Content: "system"},
			{Role: RoleUser, Content: "read main"},
		},
		Tools: []ToolSpec{{
			Name:        "read_file",
			Description: "Read file",
			Parameters:  map[string]any{"type": "object"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if captured.Model != "test-model" {
		t.Fatalf("unexpected model: %q", captured.Model)
	}
	if len(captured.Tools) != 1 || captured.Tools[0].Function.Name != "read_file" {
		t.Fatalf("tool schema was not sent: %+v", captured.Tools)
	}
	if resp.Content != "checking" {
		t.Fatalf("unexpected content: %q", resp.Content)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Arguments["path"] != "main.go" {
		t.Fatalf("tool call was not parsed: %+v", resp.ToolCalls)
	}
}

func TestOpenAIMessagesIncludeAssistantToolCalls(t *testing.T) {
	messages := openAIMessages([]Message{{
		Role:    RoleAssistant,
		Content: "checking",
		ToolCalls: []ToolCall{{
			ID:        "call-1",
			Name:      "list_files",
			Arguments: map[string]any{"max_files": 10},
		}},
	}})
	if len(messages) != 1 || len(messages[0].ToolCalls) != 1 {
		t.Fatalf("expected assistant tool call in message: %+v", messages)
	}
	if messages[0].ToolCalls[0].Function.Name != "list_files" {
		t.Fatalf("unexpected tool name: %+v", messages[0].ToolCalls[0])
	}
}
