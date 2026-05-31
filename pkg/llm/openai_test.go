package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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

func TestOpenAIProviderTreatsPrivateIPEndpointsAsLocal(t *testing.T) {
	provider, err := NewOpenAIProvider("", "http://10.171.251.1:1234/v1")
	if err != nil {
		t.Fatal(err)
	}
	if provider.transport.retry.MaxAttempts != 3 {
		t.Fatalf("expected private IP endpoint to use local retry settings: MaxAttempts=%d", provider.transport.retry.MaxAttempts)
	}
}

func TestOpenAIProviderDoesNotGuessLocalEndpointFromHostnameSubstring(t *testing.T) {
	if isLocalOpenAICompatibleBaseURL("https://api.local-models.example/v1") {
		t.Fatal("public hostname containing local should not be treated as a local endpoint")
	}
	if !isLocalOpenAICompatibleBaseURL("http://models.localhost:1234/v1") {
		t.Fatal("localhost subdomain should be treated as a local endpoint")
	}
}

func TestOpenAIProviderNormalizesRootEndpointToV1(t *testing.T) {
	var capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	provider, err := NewOpenAIProvider("", server.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Complete(context.Background(), Request{
		Model:    "local-model",
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if capturedPath != "/v1/chat/completions" {
		t.Fatalf("unexpected chat path: %s", capturedPath)
	}
}

func TestOpenAIProviderDoesNotDuplicateV1Endpoint(t *testing.T) {
	var capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	provider, err := NewOpenAIProvider("", server.URL+"/v1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Complete(context.Background(), Request{
		Model:    "local-model",
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if capturedPath != "/v1/chat/completions" {
		t.Fatalf("unexpected chat path: %s", capturedPath)
	}
}

func TestOpenAIProviderRetriesTransientLocalCompleteError(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Fatal(err)
			}
			_ = conn.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	provider, err := NewOpenAIProvider("", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := provider.Complete(context.Background(), Request{
		Model:    "local-model",
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "ok" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if calls != 2 {
		t.Fatalf("expected one retry, got %d calls", calls)
	}
}

func TestOpenAIProviderRetriesTransientLocalStreamBeforeOutput(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Fatal(err)
			}
			_ = conn.Close()
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"ok"}}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	provider, err := NewOpenAIProvider("", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	var deltas strings.Builder
	resp, err := provider.Stream(context.Background(), Request{
		Model:    "local-model",
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
	}, func(event StreamEvent) error {
		deltas.WriteString(event.Delta)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "ok" || deltas.String() != "ok" {
		t.Fatalf("unexpected stream response=%q deltas=%q", resp.Content, deltas.String())
	}
	if calls != 2 {
		t.Fatalf("expected one stream retry, got %d calls", calls)
	}
}

func TestOpenAIProviderStopsOnLocalStreamIdleTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	provider, err := NewOpenAIProvider("", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	provider.transport.streamIdleTimeout = 20 * time.Millisecond
	start := time.Now()
	_, err = provider.Stream(context.Background(), Request{
		Model:    "local-model",
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
	}, nil)
	if err == nil {
		t.Fatal("expected stream idle timeout")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("stream idle timeout took too long: %s", elapsed)
	}
	if !strings.Contains(err.Error(), "stream idle timeout") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOpenAIProviderReportsPartialStreamWithoutDoneMarker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"partial answer"}}]}`+"\n\n")
	}))
	defer server.Close()

	provider, err := NewOpenAIProvider("", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	var deltas strings.Builder
	resp, err := provider.Stream(context.Background(), Request{
		Model:    "local-model",
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
	}, func(event StreamEvent) error {
		deltas.WriteString(event.Delta)
		return nil
	})
	if err == nil {
		t.Fatal("expected incomplete stream error")
	}
	if resp.Content != "partial answer" || deltas.String() != "partial answer" {
		t.Fatalf("expected partial content to be preserved, resp=%q deltas=%q", resp.Content, deltas.String())
	}
	if !strings.Contains(err.Error(), "ended without done marker") {
		t.Fatalf("unexpected error: %v", err)
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

func TestOpenAIMessagesIncludeImagesForOllamaCompatibleVision(t *testing.T) {
	messages := openAIMessages([]Message{{
		Role:    RoleUser,
		Content: "describe this",
		Attachments: []Attachment{{
			Type:     "image",
			MIMEType: "image/png",
			Name:     "screen.png",
			Data:     "aW1hZ2U=",
		}},
	}})
	if len(messages) != 1 {
		t.Fatalf("expected one message: %+v", messages)
	}
	parts, ok := messages[0].Content.([]openAIContentPart)
	if !ok {
		t.Fatalf("expected multipart content, got %T", messages[0].Content)
	}
	if len(parts) != 2 || parts[0].Type != "text" || parts[1].Type != "image_url" {
		t.Fatalf("unexpected content parts: %+v", parts)
	}
	if parts[1].ImageURL == nil || parts[1].ImageURL.URL != "data:image/png;base64,aW1hZ2U=" {
		t.Fatalf("unexpected image url: %+v", parts[1])
	}
}

func TestOpenAIProviderStreamsDeltasReasoningAndUsage(t *testing.T) {
	var captured openAIChatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"reasoning_content":"thinking "}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"hel"}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"thinking":"more "}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"lo"}}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5,"completion_tokens_details":{"reasoning_tokens":1}}}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	provider, err := NewOpenAIProvider("", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	var deltas strings.Builder
	var reasoning strings.Builder
	resp, err := provider.Stream(context.Background(), Request{
		Model:           "local-model",
		MaxOutputTokens: 123,
		ReasoningEffort: "low",
		Messages:        []Message{{Role: RoleUser, Content: "hello"}},
	}, func(event StreamEvent) error {
		deltas.WriteString(event.Delta)
		reasoning.WriteString(event.Reasoning)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !captured.Stream || captured.StreamOptions == nil || !captured.StreamOptions.IncludeUsage {
		t.Fatalf("expected stream request with usage: %+v", captured)
	}
	if captured.MaxTokens != 123 || captured.ReasoningEffort != "low" {
		t.Fatalf("request limits/reasoning not sent: %+v", captured)
	}
	if deltas.String() != "hello" || resp.Content != "hello" {
		t.Fatalf("unexpected streamed content deltas=%q resp=%q", deltas.String(), resp.Content)
	}
	if reasoning.String() != "thinking more " {
		t.Fatalf("unexpected reasoning stream: %q", reasoning.String())
	}
	if resp.Usage.TotalTokens != 5 || resp.Usage.ReasoningTokens != 1 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}
}

func TestOpenAIProviderStreamsToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"read_file","arguments":"{\"path\""}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"main.go\"}"}}]}}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	provider, err := NewOpenAIProvider("test-key", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	var toolEvents []StreamEvent
	resp, err := provider.Stream(context.Background(), Request{
		Model:    "local-model",
		Messages: []Message{{Role: RoleUser, Content: "read main"}},
	}, func(event StreamEvent) error {
		if event.ToolName != "" || event.ToolArgumentsDelta != "" {
			toolEvents = append(toolEvents, event)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(toolEvents) != 2 {
		t.Fatalf("expected live tool call deltas, got %+v", toolEvents)
	}
	if toolEvents[0].ToolCallIndex != 0 || toolEvents[1].ToolCallIndex != 0 {
		t.Fatalf("expected live tool call index to be preserved: %+v", toolEvents)
	}
	if toolEvents[0].ToolName != "read_file" || !strings.Contains(toolEvents[1].ToolArgumentsDelta, "main.go") {
		t.Fatalf("unexpected live tool events: %+v", toolEvents)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected streamed tool call: %+v", resp.ToolCalls)
	}
	if resp.ToolCalls[0].Name != "read_file" || resp.ToolCalls[0].Arguments["path"] != "main.go" {
		t.Fatalf("unexpected streamed tool call: %+v", resp.ToolCalls[0])
	}
}

func TestOpenAIProviderRoutesThinkTagsToReasoning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"<thi"}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"nk>hidden"}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":" thoughts</thi"}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"nk>visible"}}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	provider, err := NewOpenAIProvider("", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	var deltas strings.Builder
	var reasoning strings.Builder
	resp, err := provider.Stream(context.Background(), Request{
		Model:    "local-model",
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
	}, func(event StreamEvent) error {
		deltas.WriteString(event.Delta)
		reasoning.WriteString(event.Reasoning)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "visible" || deltas.String() != "visible" {
		t.Fatalf("unexpected visible content resp=%q deltas=%q", resp.Content, deltas.String())
	}
	if reasoning.String() != "hidden thoughts" {
		t.Fatalf("unexpected think-tag reasoning: %q", reasoning.String())
	}
}

func TestOpenAIProviderEstimatesUsageWhenServerOmitsIt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Server returns no usage data at all (like old ollama)
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"hello world"}}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	provider, err := NewOpenAIProvider("", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := provider.Stream(context.Background(), Request{
		Model:    "local-model",
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
	}, func(event StreamEvent) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "hello world" {
		t.Fatalf("unexpected content: %q", resp.Content)
	}
	// "hello world" = 11 chars -> (11+3)/4 = 3 estimated tokens
	if resp.Usage.OutputTokens != 3 || resp.Usage.TotalTokens != 3 {
		t.Fatalf("expected estimated usage output=3 total=3, got output=%d total=%d",
			resp.Usage.OutputTokens, resp.Usage.TotalTokens)
	}
}

func TestOpenAIProviderEstimatesUsageForCompleteWhenServerOmitsIt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Response with zero usage
		_, _ = w.Write([]byte(`{
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "this is a test response"
				}
			}],
			"usage": {"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0}
		}`))
	}))
	defer server.Close()

	provider, err := NewOpenAIProvider("", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := provider.Complete(context.Background(), Request{
		Model:    "local-model",
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "this is a test response" {
		t.Fatalf("unexpected content: %q", resp.Content)
	}
	// "this is a test response" = 23 chars -> (23+3)/4 = 6 estimated tokens
	if resp.Usage.OutputTokens != 6 || resp.Usage.TotalTokens != 6 {
		t.Fatalf("expected estimated usage output=6 total=6, got output=%d total=%d",
			resp.Usage.OutputTokens, resp.Usage.TotalTokens)
	}
}
