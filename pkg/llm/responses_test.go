package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResponsesProviderParsesFunctionCallsAndUsage(t *testing.T) {
	var captured responsesRequest
	var capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("missing authorization header")
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "resp_123",
			"output": [{
				"type": "function_call",
				"id": "fc_123",
				"call_id": "call_123",
				"name": "project_map",
				"arguments": "{\"max_files\":20}",
				"status": "completed"
			}],
			"usage": {
				"input_tokens": 100,
				"input_tokens_details": { "cached_tokens": 40 },
				"output_tokens": 12,
				"output_tokens_details": { "reasoning_tokens": 5 },
				"total_tokens": 112
			}
		}`))
	}))
	defer server.Close()

	provider, err := NewResponsesProvider("test-key", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := provider.Complete(context.Background(), Request{
		Model:           "gpt-test",
		MaxOutputTokens: 512,
		ReasoningEffort: "low",
		Messages: []Message{
			{Role: RoleSystem, Content: "system prompt"},
			{Role: RoleUser, Content: "inspect"},
		},
		Tools: []ToolSpec{{
			Name:        "project_map",
			Description: "Map project",
			Parameters:  map[string]any{"type": "object", "additionalProperties": false},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if captured.Model != "gpt-test" || captured.Instructions != "system prompt" {
		t.Fatalf("unexpected request metadata: %+v", captured)
	}
	if capturedPath != "/v1/responses" {
		t.Fatalf("unexpected responses path: %s", capturedPath)
	}
	if captured.MaxOutputTokens != 512 || captured.Reasoning == nil || captured.Reasoning.Effort != "low" {
		t.Fatalf("generation controls were not sent: %+v", captured)
	}
	if len(captured.Tools) != 1 || captured.Tools[0].Name != "project_map" || captured.Tools[0].Strict {
		t.Fatalf("tool schema was not sent correctly: %+v", captured.Tools)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "call_123" || resp.ToolCalls[0].Arguments["max_files"].(float64) != 20 {
		t.Fatalf("function call was not parsed: %+v", resp.ToolCalls)
	}
	if resp.Usage.CachedTokens != 40 || resp.Usage.ReasoningTokens != 5 || resp.Usage.TotalTokens != 112 {
		t.Fatalf("usage was not parsed: %+v", resp.Usage)
	}
}

func TestResponsesProviderRetriesTransientStatus(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "resp_retry",
			"output": [{
				"type": "message",
				"role": "assistant",
				"content": [{"type":"output_text","text":"ok"}]
			}],
			"usage": {"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}
		}`))
	}))
	defer server.Close()

	provider, err := NewResponsesProvider("test-key", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	provider.retry.Backoff = func(int) time.Duration { return 0 }
	resp, err := provider.Complete(context.Background(), Request{
		Model:    "gpt-test",
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 || resp.Content != "ok" {
		t.Fatalf("expected retry success, attempts=%d resp=%+v", attempts, resp)
	}
}

func TestResponsesProviderFormatsEmptyGatewayError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	provider, err := NewResponsesProvider("test-key", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	provider.retry.MaxAttempts = 1
	_, err = provider.Complete(context.Background(), Request{
		Model:    "gpt-test",
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
	})
	if err == nil || !strings.Contains(err.Error(), "empty body") || strings.Contains(err.Error(), "map[]") {
		t.Fatalf("expected useful empty-body error, got %v", err)
	}
}

func TestResponsesProviderDoesNotDuplicateV1Endpoint(t *testing.T) {
	var capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_ok","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`))
	}))
	defer server.Close()

	provider, err := NewResponsesProvider("test-key", server.URL+"/v1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Complete(context.Background(), Request{
		Model:    "gpt-test",
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if capturedPath != "/v1/responses" {
		t.Fatalf("unexpected responses path: %s", capturedPath)
	}
}

func TestResponsesProviderUsesStrictToolsForOfficialOpenAI(t *testing.T) {
	provider, err := NewResponsesProvider("test-key", "https://api.openai.com/v1")
	if err != nil {
		t.Fatal(err)
	}
	payload := provider.newResponsesRequest(Request{
		Model:    "gpt-test",
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
		Tools: []ToolSpec{{
			Name:       "read_file",
			Parameters: map[string]any{"type": "object", "additionalProperties": false},
		}},
	}, false)
	if len(payload.Tools) != 1 || !payload.Tools[0].Strict {
		t.Fatalf("expected official OpenAI endpoint to use strict tools: %+v", payload.Tools)
	}
}

func TestResponsesProviderAllowsStrictToolOverride(t *testing.T) {
	t.Setenv("KLYRA_RESPONSES_STRICT_TOOLS", "true")
	provider, err := NewResponsesProvider("test-key", "https://example.test/v1")
	if err != nil {
		t.Fatal(err)
	}
	payload := provider.newResponsesRequest(Request{
		Model:    "gpt-test",
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
		Tools: []ToolSpec{{
			Name:       "read_file",
			Parameters: map[string]any{"type": "object", "additionalProperties": false},
		}},
	}, false)
	if len(payload.Tools) != 1 || !payload.Tools[0].Strict {
		t.Fatalf("expected strict tools override to apply: %+v", payload.Tools)
	}
}

func TestResponseInputItemsIncludesFunctionCallOutputs(t *testing.T) {
	items := responseInputItems([]Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{{
			ID:        "call_123",
			Name:      "read_file",
			Arguments: map[string]any{"path": "main.go"},
		}}},
		{Role: RoleTool, ToolCallID: "call_123", Content: "file contents"},
	})
	if len(items) != 2 {
		t.Fatalf("expected function call and output items, got %+v", items)
	}
	if items[0].Type != "function_call" || items[0].CallID != "call_123" {
		t.Fatalf("unexpected function call item: %+v", items[0])
	}
	if items[1].Type != "function_call_output" || items[1].Output != "file contents" {
		t.Fatalf("unexpected function output item: %+v", items[1])
	}
}

func TestResponseInputItemsSerializesAssistantHistoryAsInputText(t *testing.T) {
	items := responseInputItems([]Message{
		{Role: RoleUser, Content: "first"},
		{Role: RoleAssistant, Content: "answer"},
		{Role: RoleUser, Content: "second"},
	})
	if len(items) != 3 {
		t.Fatalf("expected conversation items, got %+v", items)
	}
	for i, item := range items {
		if len(item.Content) != 1 {
			t.Fatalf("expected one text part for item %d, got %+v", i, item)
		}
		if item.Content[0].Type != "input_text" {
			t.Fatalf("history item %d used unsupported input content type %q", i, item.Content[0].Type)
		}
	}
}

func TestResponseInputItemsIncludesImages(t *testing.T) {
	items := responseInputItems([]Message{
		{
			Role:    RoleUser,
			Content: "what is this?",
			Attachments: []Attachment{{
				Type:     "image",
				MIMEType: "image/png",
				Name:     "screen.png",
				Data:     "aW1hZ2U=",
			}},
		},
	})
	if len(items) != 1 || len(items[0].Content) != 2 {
		t.Fatalf("expected text and image content, got %+v", items)
	}
	if items[0].Content[1].Type != "input_image" || items[0].Content[1].ImageURL != "data:image/png;base64,aW1hZ2U=" {
		t.Fatalf("unexpected image content: %+v", items[0].Content[1])
	}
}

func TestResponsesProviderStreamsDeltasAndFinalResponse(t *testing.T) {
	var captured responsesRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: response.output_text.delta\n")
		fmt.Fprint(w, `data: {"type":"response.output_text.delta","delta":"hel"}`+"\n\n")
		fmt.Fprint(w, "event: response.output_text.delta\n")
		fmt.Fprint(w, `data: {"type":"response.output_text.delta","delta":"lo"}`+"\n\n")
		fmt.Fprint(w, "event: response.completed\n")
		fmt.Fprint(w, `data: {"type":"response.completed","response":{"id":"resp_stream","output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}}`+"\n\n")
	}))
	defer server.Close()

	provider, err := NewResponsesProvider("test-key", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	var deltas strings.Builder
	resp, err := provider.Stream(context.Background(), Request{
		Model: "gpt-test",
		Messages: []Message{
			{Role: RoleUser, Content: "hello"},
		},
	}, func(event StreamEvent) error {
		deltas.WriteString(event.Delta)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !captured.Stream {
		t.Fatal("expected stream flag to be sent")
	}
	if deltas.String() != "hello" {
		t.Fatalf("unexpected deltas: %q", deltas.String())
	}
	if resp.ID != "resp_stream" || resp.Content != "hello" || resp.Usage.TotalTokens != 5 {
		t.Fatalf("unexpected final response: %+v", resp)
	}
}

func TestResponsesProviderStreamsReasoningSummaryDeltas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: response.reasoning_summary_text.delta\n")
		fmt.Fprint(w, `data: {"type":"response.reasoning_summary_text.delta","delta":"checking"}`+"\n\n")
		fmt.Fprint(w, "event: response.output_text.delta\n")
		fmt.Fprint(w, `data: {"type":"response.output_text.delta","delta":"ok"}`+"\n\n")
		fmt.Fprint(w, "event: response.completed\n")
		fmt.Fprint(w, `data: {"type":"response.completed","response":{"id":"resp_stream","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}}`+"\n\n")
	}))
	defer server.Close()

	provider, err := NewResponsesProvider("test-key", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	var deltas strings.Builder
	var reasoning strings.Builder
	_, err = provider.Stream(context.Background(), Request{
		Model:    "gpt-test",
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
	}, func(event StreamEvent) error {
		deltas.WriteString(event.Delta)
		reasoning.WriteString(event.Reasoning)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if deltas.String() != "ok" {
		t.Fatalf("unexpected output deltas: %q", deltas.String())
	}
	if reasoning.String() != "checking" {
		t.Fatalf("unexpected reasoning deltas: %q", reasoning.String())
	}
}

func TestResponsesProviderKeepsStreamTextWhenCompletedHasOnlyUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: response.output_text.delta\n")
		fmt.Fprint(w, `data: {"type":"response.output_text.delta","delta":"hidden"}`+"\n\n")
		fmt.Fprint(w, "event: response.output_text.done\n")
		fmt.Fprint(w, `data: {"type":"response.output_text.done","text":"commentary"}`+"\n\n")
		fmt.Fprint(w, "event: response.output_item.done\n")
		fmt.Fprint(w, `data: {"type":"response.output_item.done","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"final answer"}]}}`+"\n\n")
		fmt.Fprint(w, "event: response.completed\n")
		fmt.Fprint(w, `data: {"type":"response.completed","response":{"id":"resp_stream","usage":{"input_tokens":10,"output_tokens":4,"total_tokens":14}}}`+"\n\n")
	}))
	defer server.Close()

	provider, err := NewResponsesProvider("test-key", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	var deltas strings.Builder
	resp, err := provider.Stream(context.Background(), Request{
		Model:    "gpt-test",
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
	}, func(event StreamEvent) error {
		deltas.WriteString(event.Delta)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if deltas.String() != "hidden" {
		t.Fatalf("unexpected deltas: %q", deltas.String())
	}
	if resp.ID != "resp_stream" || resp.Content != "final answer" || resp.Usage.TotalTokens != 14 {
		t.Fatalf("stream text or usage was lost: %+v", resp)
	}
}

func TestResponsesProviderParsesStreamedFunctionCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: response.output_item.added\n")
		fmt.Fprint(w, `data: {"type":"response.output_item.added","item":{"id":"fc_1","type":"function_call","status":"in_progress","arguments":"","call_id":"call_1","name":"read_file"},"output_index":0}`+"\n\n")
		fmt.Fprint(w, "event: response.function_call_arguments.delta\n")
		fmt.Fprint(w, `data: {"type":"response.function_call_arguments.delta","delta":"{\"path\":\"README.md\"}","item_id":"fc_1","output_index":0}`+"\n\n")
		fmt.Fprint(w, "event: response.function_call_arguments.done\n")
		fmt.Fprint(w, `data: {"type":"response.function_call_arguments.done","arguments":"{\"path\":\"README.md\"}","item_id":"fc_1","output_index":0}`+"\n\n")
		fmt.Fprint(w, "event: response.output_item.done\n")
		fmt.Fprint(w, `data: {"type":"response.output_item.done","item":{"id":"fc_1","type":"function_call","status":"completed","arguments":"{\"path\":\"README.md\"}","call_id":"call_1","name":"read_file"},"output_index":0}`+"\n\n")
		fmt.Fprint(w, "event: response.completed\n")
		fmt.Fprint(w, `data: {"type":"response.completed","response":{"id":"resp_stream","usage":{"input_tokens":10,"output_tokens":4,"total_tokens":14}}}`+"\n\n")
	}))
	defer server.Close()

	provider, err := NewResponsesProvider("test-key", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := provider.Stream(context.Background(), Request{
		Model:    "gpt-test",
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.ID != "resp_stream" || resp.Usage.TotalTokens != 14 {
		t.Fatalf("stream metadata was lost: %+v", resp)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "call_1" || resp.ToolCalls[0].Name != "read_file" || resp.ToolCalls[0].Arguments["path"] != "README.md" {
		t.Fatalf("streamed function call was not parsed: %+v", resp.ToolCalls)
	}
}
