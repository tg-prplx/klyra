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
	"strings"
	"time"
)

type ResponsesProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

func NewResponsesProvider(apiKey, baseURL string) (*ResponsesProvider, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is required")
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &ResponsesProvider{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 90 * time.Second},
	}, nil
}

func NewResponsesProviderFromEnv() (*ResponsesProvider, error) {
	return NewResponsesProvider(os.Getenv("OPENAI_API_KEY"), os.Getenv("OPENAI_BASE_URL"))
}

func (p *ResponsesProvider) Complete(ctx context.Context, req Request) (Response, error) {
	if strings.TrimSpace(req.Model) == "" {
		return Response{}, fmt.Errorf("model is required")
	}

	payload := newResponsesRequest(req, false)

	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return Response{}, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode > 299 {
		var apiErr map[string]any
		_ = json.NewDecoder(httpResp.Body).Decode(&apiErr)
		return Response{}, fmt.Errorf("responses API returned %s: %v", httpResp.Status, apiErr)
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

	payload := newResponsesRequest(req, true)
	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
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
		return Response{}, fmt.Errorf("responses API returned %s: %v", httpResp.Status, apiErr)
	}

	return readResponsesStream(httpResp.Body, handler)
}

func newResponsesRequest(req Request, stream bool) responsesRequest {
	payload := responsesRequest{
		Model:             req.Model,
		Instructions:      systemInstructions(req.Messages),
		Input:             responseInputItems(req.Messages),
		Tools:             responseTools(req.Tools),
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
	Type     string            `json:"type"`
	Delta    string            `json:"delta"`
	Response responsesResponse `json:"response"`
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
			resp, err := processResponsesStreamData(dataLines, handler)
			dataLines = nil
			if err != nil {
				return final, err
			}
			if hasResponsePayload(resp) {
				final = resp
			}
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
		}
	}
	if err := scanner.Err(); err != nil {
		return final, err
	}
	if len(dataLines) > 0 {
		resp, err := processResponsesStreamData(dataLines, handler)
		if err != nil {
			return final, err
		}
		if hasResponsePayload(resp) {
			final = resp
		}
	}
	return final, nil
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
	case "response.reasoning_text.delta":
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
						Type: contentTypeForRole(msg.Role),
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

func contentTypeForRole(role Role) string {
	if role == RoleAssistant {
		return "output_text"
	}
	return "input_text"
}

func responseTools(specs []ToolSpec) []responseTool {
	out := make([]responseTool, 0, len(specs))
	for _, spec := range specs {
		out = append(out, responseTool{
			Type:        "function",
			Name:        spec.Name,
			Description: spec.Description,
			Parameters:  spec.Parameters,
			Strict:      true,
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
