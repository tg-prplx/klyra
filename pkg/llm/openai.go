package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
)

type OpenAIProvider struct {
	apiKey            string
	baseURL           string
	client            *http.Client
	omitStreamOptions bool
}

func NewOpenAIProvider(apiKey, baseURL string) (*OpenAIProvider, error) {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://api.openai.com/v1"
	}
	lowerURL := strings.ToLower(baseURL)
	isLocal := (strings.Contains(lowerURL, "localhost") ||
		strings.Contains(lowerURL, "127.0.0.1") ||
		strings.Contains(lowerURL, "::1") ||
		strings.Contains(lowerURL, "ollama") ||
		strings.Contains(lowerURL, "local")) && !strings.Contains(lowerURL, "test-stream-options")

	return &OpenAIProvider{
		apiKey:            apiKey,
		baseURL:           strings.TrimRight(baseURL, "/"),
		client:            &http.Client{Timeout: 0},
		omitStreamOptions: isLocal,
	}, nil
}

func NewOpenAIProviderFromEnv() (*OpenAIProvider, error) {
	return NewOpenAIProvider(os.Getenv("OPENAI_API_KEY"), os.Getenv("OPENAI_BASE_URL"))
}

func (p *OpenAIProvider) Complete(ctx context.Context, req Request) (Response, error) {
	if strings.TrimSpace(req.Model) == "" {
		return Response{}, fmt.Errorf("model is required")
	}

	payload := openAIChatRequest{
		Model:           req.Model,
		Messages:        openAIMessages(req.Messages),
		Tools:           openAITools(req.Tools),
		MaxTokens:       req.MaxOutputTokens,
		ReasoningEffort: req.ReasoningEffort,
	}
	if len(payload.Tools) > 0 {
		payload.ToolChoice = "auto"
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	if strings.TrimSpace(p.apiKey) != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return Response{}, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode > 299 {
		var apiErr map[string]any
		_ = json.NewDecoder(httpResp.Body).Decode(&apiErr)
		return Response{}, fmt.Errorf("openai-compatible API returned %s: %v", httpResp.Status, apiErr)
	}

	var decoded openAIChatResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&decoded); err != nil {
		return Response{}, err
	}
	if len(decoded.Choices) == 0 {
		return Response{}, fmt.Errorf("openai-compatible API returned no choices")
	}
	message := decoded.Choices[0].Message
	return Response{
		Content:   openAIContentString(message.Content),
		ToolCalls: parseOpenAIToolCalls(message.ToolCalls),
		Usage: Usage{
			InputTokens:     decoded.Usage.PromptTokens,
			CachedTokens:    decoded.Usage.PromptTokensDetails.CachedTokens,
			OutputTokens:    decoded.Usage.CompletionTokens,
			ReasoningTokens: decoded.Usage.CompletionTokensDetails.ReasoningTokens,
			TotalTokens:     decoded.Usage.TotalTokens,
		},
	}, nil
}

func (p *OpenAIProvider) Stream(ctx context.Context, req Request, handler StreamHandler) (Response, error) {
	if strings.TrimSpace(req.Model) == "" {
		return Response{}, fmt.Errorf("model is required")
	}

	var streamOptions *openAIStreamOptions
	if !p.omitStreamOptions {
		streamOptions = &openAIStreamOptions{IncludeUsage: true}
	}

	payload := openAIChatRequest{
		Model:           req.Model,
		Messages:        openAIMessages(req.Messages),
		Tools:           openAITools(req.Tools),
		MaxTokens:       req.MaxOutputTokens,
		ReasoningEffort: req.ReasoningEffort,
		Stream:          true,
		StreamOptions:   streamOptions,
	}
	if len(payload.Tools) > 0 {
		payload.ToolChoice = "auto"
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	if strings.TrimSpace(p.apiKey) != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return Response{}, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode > 299 {
		var apiErr map[string]any
		_ = json.NewDecoder(httpResp.Body).Decode(&apiErr)
		return Response{}, fmt.Errorf("openai-compatible API returned %s: %v", httpResp.Status, apiErr)
	}

	return readOpenAIChatStream(httpResp.Body, handler)
}

func openAIContentString(content any) string {
	switch value := content.(type) {
	case string:
		return value
	case []any:
		var parts []string
		for _, item := range value {
			part, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text, _ := part["text"].(string); strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

type openAIChatRequest struct {
	Model           string               `json:"model"`
	Messages        []openAIMessage      `json:"messages"`
	Tools           []openAITool         `json:"tools,omitempty"`
	ToolChoice      string               `json:"tool_choice,omitempty"`
	MaxTokens       int                  `json:"max_tokens,omitempty"`
	ReasoningEffort string               `json:"reasoning_effort,omitempty"`
	Stream          bool                 `json:"stream,omitempty"`
	StreamOptions   *openAIStreamOptions `json:"stream_options,omitempty"`
}

type openAIStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAIContentPart struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL *openAIImageURL `json:"image_url,omitempty"`
}

type openAIImageURL struct {
	URL string `json:"url"`
}

type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

type openAIFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAICallFunction `json:"function"`
}

type openAICallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message openAIMessage `json:"message"`
	} `json:"choices"`
	Usage openAIUsage `json:"usage"`
}

type openAIChatStreamChunk struct {
	Choices []struct {
		Delta        openAIStreamDelta `json:"delta"`
		FinishReason string            `json:"finish_reason"`
	} `json:"choices"`
	Usage openAIUsage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

type openAIStreamDelta struct {
	Content          any                    `json:"content"`
	Reasoning        string                 `json:"reasoning"`
	ReasoningContent string                 `json:"reasoning_content"`
	Thinking         string                 `json:"thinking"`
	ToolCalls        []openAIStreamToolCall `json:"tool_calls"`
}

type openAIStreamToolCall struct {
	Index    int                `json:"index"`
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAICallFunction `json:"function"`
}

type openAIUsage struct {
	PromptTokens            int `json:"prompt_tokens"`
	CompletionTokens        int `json:"completion_tokens"`
	TotalTokens             int `json:"total_tokens"`
	PromptTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

func openAIMessages(messages []Message) []openAIMessage {
	out := make([]openAIMessage, 0, len(messages))
	for _, msg := range messages {
		out = append(out, openAIMessage{
			Role:       string(msg.Role),
			Content:    openAIMessageContent(msg),
			ToolCallID: msg.ToolCallID,
			ToolCalls:  openAIToolCalls(msg.ToolCalls),
		})
	}
	return out
}

func openAIMessageContent(msg Message) any {
	if msg.Role != RoleUser || len(msg.Attachments) == 0 {
		return msg.Content
	}
	parts := make([]openAIContentPart, 0, 1+len(msg.Attachments))
	if strings.TrimSpace(msg.Content) != "" {
		parts = append(parts, openAIContentPart{Type: "text", Text: msg.Content})
	}
	for _, attachment := range msg.Attachments {
		if attachment.Type != "image" {
			continue
		}
		imageURL := strings.TrimSpace(attachment.URL)
		if imageURL == "" && strings.TrimSpace(attachment.Data) != "" && strings.TrimSpace(attachment.MIMEType) != "" {
			imageURL = "data:" + attachment.MIMEType + ";base64," + attachment.Data
		}
		if imageURL == "" {
			continue
		}
		parts = append(parts, openAIContentPart{
			Type:     "image_url",
			ImageURL: &openAIImageURL{URL: imageURL},
		})
	}
	if len(parts) == 0 {
		return msg.Content
	}
	return parts
}

func openAITools(specs []ToolSpec) []openAITool {
	out := make([]openAITool, 0, len(specs))
	for _, spec := range specs {
		out = append(out, openAITool{
			Type: "function",
			Function: openAIFunction{
				Name:        spec.Name,
				Description: spec.Description,
				Parameters:  spec.Parameters,
			},
		})
	}
	return out
}

func openAIToolCalls(calls []ToolCall) []openAIToolCall {
	out := make([]openAIToolCall, 0, len(calls))
	for _, call := range calls {
		args, _ := json.Marshal(call.Arguments)
		out = append(out, openAIToolCall{
			ID:   call.ID,
			Type: "function",
			Function: openAICallFunction{
				Name:      call.Name,
				Arguments: string(args),
			},
		})
	}
	return out
}

func parseOpenAIToolCalls(calls []openAIToolCall) []ToolCall {
	out := make([]ToolCall, 0, len(calls))
	for _, call := range calls {
		args := map[string]any{}
		if strings.TrimSpace(call.Function.Arguments) != "" {
			_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
		}
		out = append(out, ToolCall{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: args,
		})
	}
	return out
}

func readOpenAIChatStream(reader io.Reader, handler StreamHandler) (Response, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var content strings.Builder
	var usage Usage
	inThinkBlock := false
	pendingContent := ""
	toolCalls := map[int]*openAIStreamToolCall{}
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			break
		}
		var chunk openAIChatStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			if content.Len() > 0 || len(toolCalls) > 0 {
				continue
			}
			return Response{}, err
		}
		if chunk.Error != nil {
			return Response{}, fmt.Errorf("openai-compatible stream error: %s", chunk.Error.Message)
		}
		if chunk.Usage.TotalTokens > 0 || chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
			usage = Usage{
				InputTokens:     chunk.Usage.PromptTokens,
				CachedTokens:    chunk.Usage.PromptTokensDetails.CachedTokens,
				OutputTokens:    chunk.Usage.CompletionTokens,
				ReasoningTokens: chunk.Usage.CompletionTokensDetails.ReasoningTokens,
				TotalTokens:     chunk.Usage.TotalTokens,
			}
		}
		for _, choice := range chunk.Choices {
			delta := choice.Delta
			if reasoning := firstNonEmpty(delta.ReasoningContent, delta.Reasoning, delta.Thinking); reasoning != "" && handler != nil {
				if err := handler(StreamEvent{Reasoning: reasoning}); err != nil {
					return Response{}, err
				}
			}
			if text := openAIContentString(delta.Content); text != "" {
				if err := routeOpenAIContentDelta(text, &inThinkBlock, &pendingContent, &content, handler); err != nil {
					return Response{}, err
				}
			}
			if err := emitOpenAIStreamToolCallDeltas(delta.ToolCalls, handler); err != nil {
				return Response{}, err
			}
			accumulateOpenAIStreamToolCalls(toolCalls, delta.ToolCalls)
		}
	}
	if pendingContent != "" {
		if inThinkBlock {
			if err := emitOpenAIReasoningDelta(pendingContent, handler); err != nil {
				return Response{}, err
			}
		} else {
			content.WriteString(pendingContent)
			if err := emitOpenAITextDelta(pendingContent, handler); err != nil {
				return Response{}, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if content.Len() > 0 || len(toolCalls) > 0 {
			return Response{
				Content:   content.String(),
				ToolCalls: finishOpenAIStreamToolCalls(toolCalls),
				Usage:     usage,
			}, nil
		}
		return Response{}, err
	}
	return Response{
		Content:   content.String(),
		ToolCalls: finishOpenAIStreamToolCalls(toolCalls),
		Usage:     usage,
	}, nil
}

func routeOpenAIContentDelta(text string, inThinkBlock *bool, pending *string, content *strings.Builder, handler StreamHandler) error {
	const openTag = "<think>"
	const closeTag = "</think>"

	input := *pending + text
	*pending = ""
	for input != "" {
		if *inThinkBlock {
			idx := strings.Index(input, closeTag)
			if idx < 0 {
				keep := suffixPrefixLen(input, closeTag)
				reasoning := input[:len(input)-keep]
				if err := emitOpenAIReasoningDelta(reasoning, handler); err != nil {
					return err
				}
				*pending = input[len(input)-keep:]
				return nil
			}
			if err := emitOpenAIReasoningDelta(input[:idx], handler); err != nil {
				return err
			}
			input = input[idx+len(closeTag):]
			*inThinkBlock = false
			continue
		}

		idx := strings.Index(input, openTag)
		if idx < 0 {
			keep := suffixPrefixLen(input, openTag)
			output := input[:len(input)-keep]
			content.WriteString(output)
			if err := emitOpenAITextDelta(output, handler); err != nil {
				return err
			}
			*pending = input[len(input)-keep:]
			return nil
		}
		output := input[:idx]
		content.WriteString(output)
		if err := emitOpenAITextDelta(output, handler); err != nil {
			return err
		}
		input = input[idx+len(openTag):]
		*inThinkBlock = true
	}
	return nil
}

func emitOpenAITextDelta(text string, handler StreamHandler) error {
	if text == "" || handler == nil {
		return nil
	}
	return handler(StreamEvent{Delta: text})
}

func emitOpenAIReasoningDelta(text string, handler StreamHandler) error {
	if text == "" || handler == nil {
		return nil
	}
	return handler(StreamEvent{Reasoning: text})
}

func emitOpenAIStreamToolCallDeltas(deltas []openAIStreamToolCall, handler StreamHandler) error {
	if handler == nil {
		return nil
	}
	for _, delta := range deltas {
		if delta.ID == "" && delta.Function.Name == "" && delta.Function.Arguments == "" {
			continue
		}
		if err := handler(StreamEvent{
			ToolCallIndex:      delta.Index,
			ToolCallID:         delta.ID,
			ToolName:           delta.Function.Name,
			ToolArgumentsDelta: delta.Function.Arguments,
		}); err != nil {
			return err
		}
	}
	return nil
}

func suffixPrefixLen(text, marker string) int {
	maxLen := len(marker) - 1
	if len(text) < maxLen {
		maxLen = len(text)
	}
	for n := maxLen; n > 0; n-- {
		if strings.HasSuffix(text, marker[:n]) {
			return n
		}
	}
	return 0
}

func accumulateOpenAIStreamToolCalls(acc map[int]*openAIStreamToolCall, deltas []openAIStreamToolCall) {
	for _, delta := range deltas {
		call := acc[delta.Index]
		if call == nil {
			call = &openAIStreamToolCall{Index: delta.Index}
			acc[delta.Index] = call
		}
		if delta.ID != "" {
			call.ID = delta.ID
		}
		if delta.Type != "" {
			call.Type = delta.Type
		}
		if delta.Function.Name != "" {
			call.Function.Name = delta.Function.Name
		}
		if delta.Function.Arguments != "" {
			call.Function.Arguments += delta.Function.Arguments
		}
	}
}

func finishOpenAIStreamToolCalls(acc map[int]*openAIStreamToolCall) []ToolCall {
	if len(acc) == 0 {
		return nil
	}
	indexes := make([]int, 0, len(acc))
	for index := range acc {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	calls := make([]openAIToolCall, 0, len(indexes))
	for _, index := range indexes {
		call := acc[index]
		calls = append(calls, openAIToolCall{
			ID:       call.ID,
			Type:     firstNonEmpty(call.Type, "function"),
			Function: call.Function,
		})
	}
	return parseOpenAIToolCalls(calls)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
