package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type ResponsesProvider struct {
	apiKey      string
	baseURL     string
	client      *http.Client
	retry       openAIRetryPolicy
	strictTools bool
}

func NewResponsesProvider(apiKey, baseURL string) (*ResponsesProvider, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is required")
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://api.openai.com/v1"
	}
	baseURL = normalizeOpenAICompatibleBaseURL(baseURL)
	return &ResponsesProvider{
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 0},
		retry: openAIRetryPolicy{
			MaxAttempts: 3,
			Backoff: func(attempt int) time.Duration {
				return time.Duration(attempt) * 200 * time.Millisecond
			},
		},
		strictTools: responsesStrictTools(baseURL),
	}, nil
}

func NewResponsesProviderFromEnv() (*ResponsesProvider, error) {
	return NewResponsesProvider(os.Getenv("OPENAI_API_KEY"), os.Getenv("OPENAI_BASE_URL"))
}

func (p *ResponsesProvider) Complete(ctx context.Context, req Request) (Response, error) {
	if strings.TrimSpace(req.Model) == "" {
		return Response{}, fmt.Errorf("model is required")
	}

	payload := p.newResponsesRequest(req, false)

	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, err
	}

	httpResp, err := p.doResponses(ctx, body, false)
	if err != nil {
		return Response{}, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode > 299 {
		return Response{}, p.decodeResponsesError(httpResp)
	}

	var decoded responsesResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&decoded); err != nil {
		return Response{}, err
	}
	return decoded.toLLMResponse(), nil
}

func (p *ResponsesProvider) Stream(ctx context.Context, req Request, handler StreamHandler) (Response, error) {
	if strings.TrimSpace(req.Model) == "" {
		return Response{}, fmt.Errorf("model is required")
	}

	payload := p.newResponsesRequest(req, true)
	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, err
	}

	httpResp, err := p.doResponses(ctx, body, true)
	if err != nil {
		return Response{}, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode > 299 {
		return Response{}, p.decodeResponsesError(httpResp)
	}

	return readResponsesStream(httpResp.Body, handler)
}

func (p *ResponsesProvider) doResponses(ctx context.Context, body []byte, stream bool) (*http.Response, error) {
	attempts := p.retry.MaxAttempts
	if attempts <= 0 {
		attempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/responses", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
		httpReq.Header.Set("Content-Type", "application/json")
		if stream {
			httpReq.Header.Set("Accept", "text/event-stream")
		}
		httpResp, err := p.client.Do(httpReq)
		if err != nil {
			lastErr = err
			if attempt < attempts && isTransientOpenAIError(err) {
				if waitErr := p.sleepBeforeResponsesRetry(ctx, attempt); waitErr != nil {
					return nil, waitErr
				}
				continue
			}
			return nil, err
		}
		if isRetryableOpenAIStatus(httpResp.StatusCode) && attempt < attempts {
			drainAndClose(httpResp.Body)
			if waitErr := p.sleepBeforeResponsesRetry(ctx, attempt); waitErr != nil {
				return nil, waitErr
			}
			continue
		}
		return httpResp, nil
	}
	return nil, lastErr
}

func (p *ResponsesProvider) sleepBeforeResponsesRetry(ctx context.Context, attempt int) error {
	backoff := p.retry.Backoff
	if backoff == nil {
		backoff = func(attempt int) time.Duration { return time.Duration(attempt) * 200 * time.Millisecond }
	}
	timer := time.NewTimer(backoff(attempt))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (p *ResponsesProvider) decodeResponsesError(httpResp *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(httpResp.Body, 16*1024))
	body := strings.TrimSpace(string(data))
	if body == "" {
		return fmt.Errorf("responses API returned %s with empty body", httpResp.Status)
	}
	var apiErr map[string]any
	if err := json.Unmarshal(data, &apiErr); err == nil && len(apiErr) > 0 {
		return fmt.Errorf("responses API returned %s: %v", httpResp.Status, apiErr)
	}
	return fmt.Errorf("responses API returned %s: %s", httpResp.Status, body)
}

func (p *ResponsesProvider) newResponsesRequest(req Request, stream bool) responsesRequest {
	payload := responsesRequest{
		Model:             req.Model,
		Instructions:      systemInstructions(req.Messages),
		Input:             responseInputItems(req.Messages),
		Tools:             responseTools(req.Tools, p.strictTools),
		ToolChoice:        "auto",
		Store:             req.Store,
		Stream:            stream,
		MaxOutputTokens:   req.MaxOutputTokens,
		ParallelToolCalls: true,
	}
	if strings.TrimSpace(req.ReasoningEffort) != "" {
		payload.Reasoning = &responsesReasoning{Effort: req.ReasoningEffort}
	}
	if len(payload.Tools) == 0 {
		payload.ToolChoice = ""
	}
	return payload
}

func newResponsesRequest(req Request, stream bool) responsesRequest {
	return (&ResponsesProvider{strictTools: true}).newResponsesRequest(req, stream)
}

func responsesStrictTools(baseURL string) bool {
	if raw := strings.TrimSpace(os.Getenv("KLYRA_RESPONSES_STRICT_TOOLS")); raw != "" {
		if parsed, err := strconv.ParseBool(raw); err == nil {
			return parsed
		}
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "api.openai.com"
}

type responsesRequest struct {
	Model             string                 `json:"model"`
	Instructions      string                 `json:"instructions,omitempty"`
	Input             []responseInputItem    `json:"input"`
	Tools             []responseTool         `json:"tools,omitempty"`
	ToolChoice        string                 `json:"tool_choice,omitempty"`
	Store             bool                   `json:"store"`
	Stream            bool                   `json:"stream,omitempty"`
	MaxOutputTokens   int                    `json:"max_output_tokens,omitempty"`
	ParallelToolCalls bool                   `json:"parallel_tool_calls"`
	Reasoning         *responsesReasoning    `json:"reasoning,omitempty"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
}

type responsesReasoning struct {
	Effort string `json:"effort,omitempty"`
}

type responseInputItem struct {
	Type    string                `json:"type,omitempty"`
	Role    string                `json:"role,omitempty"`
	Content []responseContentPart `json:"content,omitempty"`
	CallID  string                `json:"call_id,omitempty"`
	Name    string                `json:"name,omitempty"`
	Args    string                `json:"arguments,omitempty"`
	Output  string                `json:"output,omitempty"`
	Status  string                `json:"status,omitempty"`
}

type responseContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

type responseTool struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
	Strict      bool           `json:"strict"`
}

type responsesResponse struct {
	ID     string               `json:"id"`
	Output []responseOutputItem `json:"output"`
	Usage  responsesUsage       `json:"usage"`
}

type responseOutputItem struct {
	Type      string                  `json:"type"`
	ID        string                  `json:"id"`
	CallID    string                  `json:"call_id"`
	Name      string                  `json:"name"`
	Arguments string                  `json:"arguments"`
	Status    string                  `json:"status"`
	Role      string                  `json:"role"`
	Content   []responseOutputContent `json:"content"`
}

type responseOutputContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responsesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
	InputDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputDetails struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}

type responsesStreamEnvelope struct {
	Type     string                `json:"type"`
	Delta    string                `json:"delta"`
	Text     string                `json:"text"`
	Response responsesResponse     `json:"response"`
	Item     responseOutputItem    `json:"item"`
	Part     responseOutputContent `json:"part"`
	Error    *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func readResponsesStream(reader io.Reader, handler StreamHandler) (Response, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var dataLines []string
	var final Response
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			isDone := len(dataLines) == 1 && strings.TrimSpace(dataLines[0]) == "[DONE]"
			resp, err := processResponsesStreamData(dataLines, handler)
			dataLines = nil
			if err != nil {
				if hasResponsePayload(final) {
					return final, nil
				}
				return final, err
			}
			if hasResponsePayload(resp) {
				final = mergeStreamResponse(final, resp)
			}
			if isDone {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
		}
	}
	if err := scanner.Err(); err != nil {
		if hasResponsePayload(final) {
			return final, nil
		}
		return final, err
	}
	if len(dataLines) > 0 {
		resp, err := processResponsesStreamData(dataLines, handler)
		if err != nil {
			if hasResponsePayload(final) {
				return final, nil
			}
			return final, err
		}
		if hasResponsePayload(resp) {
			final = mergeStreamResponse(final, resp)
		}
	}
	return final, nil
}

func mergeStreamResponse(final, next Response) Response {
	if next.ID != "" {
		final.ID = next.ID
	}
	if strings.TrimSpace(next.Content) != "" {
		final.Content = next.Content
	}
	if len(next.ToolCalls) > 0 {
		final.ToolCalls = next.ToolCalls
	}
	if next.Usage.TotalTokens != 0 || next.Usage.InputTokens != 0 || next.Usage.OutputTokens != 0 {
		final.Usage = next.Usage
	}
	return final
}

func processResponsesStreamData(dataLines []string, handler StreamHandler) (Response, error) {
	if len(dataLines) == 0 {
		return Response{}, nil
	}
	data := strings.Join(dataLines, "\n")
	if strings.TrimSpace(data) == "[DONE]" {
		return Response{}, nil
	}

	var event responsesStreamEnvelope
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return Response{}, err
	}
	if event.Error != nil {
		return Response{}, fmt.Errorf("responses stream error: %s", event.Error.Message)
	}
	switch event.Type {
	case "response.output_text.delta":
		if event.Delta != "" && handler != nil {
			if err := handler(StreamEvent{Delta: event.Delta}); err != nil {
				return Response{}, err
			}
		}
	case "response.output_text.done":
		return Response{Content: event.Text}, nil
	case "response.content_part.done":
		if event.Part.Type == "output_text" {
			return Response{Content: event.Part.Text}, nil
		}
	case "response.output_item.done":
		if event.Item.Type == "message" || event.Item.Type == "function_call" {
			return responsesResponse{Output: []responseOutputItem{event.Item}}.toLLMResponse(), nil
		}
	case "response.reasoning_text.delta":
		if event.Delta != "" && handler != nil {
			if err := handler(StreamEvent{Reasoning: event.Delta}); err != nil {
				return Response{}, err
			}
		}
	case "response.reasoning_summary_text.delta":
		if event.Delta != "" && handler != nil {
			if err := handler(StreamEvent{Reasoning: event.Delta}); err != nil {
				return Response{}, err
			}
		}
	case "response.completed":
		return event.Response.toLLMResponse(), nil
	case "response.failed", "response.incomplete":
		return Response{}, fmt.Errorf("responses stream ended with %s", event.Type)
	}
	return Response{}, nil
}

func hasResponsePayload(resp Response) bool {
	return resp.ID != "" || resp.Content != "" || len(resp.ToolCalls) > 0 || resp.Usage.TotalTokens > 0
}

func systemInstructions(messages []Message) string {
	for _, msg := range messages {
		if msg.Role == RoleSystem {
			return msg.Content
		}
	}
	return ""
}

func responseInputItems(messages []Message) []responseInputItem {
	items := make([]responseInputItem, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case RoleSystem:
			continue
		case RoleUser, RoleAssistant:
			if strings.TrimSpace(msg.Content) != "" || len(msg.Attachments) > 0 {
				role := string(msg.Role)
				content := []responseContentPart{}
				if strings.TrimSpace(msg.Content) != "" {
					content = append(content, responseContentPart{
						Type: "input_text",
						Text: msg.Content,
					})
				}
				if msg.Role == RoleUser {
					content = append(content, responseImageParts(msg.Attachments)...)
				}
				items = append(items, responseInputItem{
					Type:    "message",
					Role:    role,
					Content: content,
				})
			}
			for _, call := range msg.ToolCalls {
				args, _ := json.Marshal(call.Arguments)
				items = append(items, responseInputItem{
					Type:   "function_call",
					CallID: call.ID,
					Name:   call.Name,
					Args:   string(args),
					Status: "completed",
				})
			}
		case RoleTool:
			items = append(items, responseInputItem{
				Type:   "function_call_output",
				CallID: msg.ToolCallID,
				Output: msg.Content,
			})
		}
	}
	return items
}

func responseImageParts(attachments []Attachment) []responseContentPart {
	parts := make([]responseContentPart, 0, len(attachments))
	for _, attachment := range attachments {
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
		parts = append(parts, responseContentPart{
			Type:     "input_image",
			ImageURL: imageURL,
		})
	}
	return parts
}

func responseTools(specs []ToolSpec, strict bool) []responseTool {
	out := make([]responseTool, 0, len(specs))
	for _, spec := range specs {
		out = append(out, responseTool{
			Type:        "function",
			Name:        spec.Name,
			Description: spec.Description,
			Parameters:  spec.Parameters,
			Strict:      strict,
		})
	}
	return out
}

func (r responsesResponse) toLLMResponse() Response {
	var contentParts []string
	var calls []ToolCall
	for _, item := range r.Output {
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				if part.Type == "output_text" && strings.TrimSpace(part.Text) != "" {
					contentParts = append(contentParts, part.Text)
				}
			}
		case "function_call":
			args := map[string]any{}
			if strings.TrimSpace(item.Arguments) != "" {
				_ = json.Unmarshal([]byte(item.Arguments), &args)
			}
			callID := item.CallID
			if callID == "" {
				callID = item.ID
			}
			calls = append(calls, ToolCall{
				ID:        callID,
				Name:      item.Name,
				Arguments: args,
			})
		}
	}
	return Response{
		ID:        r.ID,
		Content:   strings.Join(contentParts, "\n"),
		ToolCalls: calls,
		Usage: Usage{
			InputTokens:     r.Usage.InputTokens,
			CachedTokens:    r.Usage.InputDetails.CachedTokens,
			OutputTokens:    r.Usage.OutputTokens,
			ReasoningTokens: r.Usage.OutputDetails.ReasoningTokens,
			TotalTokens:     r.Usage.TotalTokens,
		},
	}
}
