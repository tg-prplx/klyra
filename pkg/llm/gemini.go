package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
)

type GeminiProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

func NewGeminiProvider(apiKey, baseURL string) (*GeminiProvider, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY is required")
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://generativelanguage.googleapis.com/v1beta"
	}
	return &GeminiProvider{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 0},
	}, nil
}

func NewGeminiProviderFromEnv() (*GeminiProvider, error) {
	return NewGeminiProvider(os.Getenv("GEMINI_API_KEY"), os.Getenv("GEMINI_BASE_URL"))
}

func (p *GeminiProvider) Complete(ctx context.Context, req Request) (Response, error) {
	if strings.TrimSpace(req.Model) == "" {
		return Response{}, fmt.Errorf("model is required")
	}
	payload := geminiRequest{
		SystemInstruction: geminiSystemInstruction(req.Messages),
		Contents:          geminiContents(req.Messages),
		Tools:             geminiTools(req.Tools),
		GenerationConfig: geminiGenerationConfig{
			MaxOutputTokens: req.MaxOutputTokens,
		},
	}
	if payload.GenerationConfig.MaxOutputTokens <= 0 {
		payload.GenerationConfig.MaxOutputTokens = 4096
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, err
	}
	endpoint := fmt.Sprintf("%s/models/%s:generateContent?key=%s", p.baseURL, url.PathEscape(geminiModelName(req.Model)), url.QueryEscape(p.apiKey))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	httpReq.Header.Set("content-type", "application/json")

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return Response{}, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode > 299 {
		var apiErr map[string]any
		_ = json.NewDecoder(httpResp.Body).Decode(&apiErr)
		return Response{}, fmt.Errorf("gemini API returned %s: %v", httpResp.Status, apiErr)
	}

	var decoded geminiResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&decoded); err != nil {
		return Response{}, err
	}
	return decoded.toLLMResponse(), nil
}

type geminiRequest struct {
	SystemInstruction geminiContent          `json:"systemInstruction,omitempty"`
	Contents          []geminiContent        `json:"contents"`
	Tools             []geminiTool           `json:"tools,omitempty"`
	GenerationConfig  geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiGenerationConfig struct {
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts,omitempty"`
}

type geminiPart struct {
	Text             string                  `json:"text,omitempty"`
	FunctionCall     *geminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *geminiFunctionResponse `json:"functionResponse,omitempty"`
	InlineData       *geminiInlineData       `json:"inlineData,omitempty"`
	ThoughtSignature string                  `json:"thoughtSignature,omitempty"`
}

type geminiInlineData struct {
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"`
}

type geminiFunctionCall struct {
	ID   string         `json:"id,omitempty"`
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

type geminiFunctionResponse struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type geminiTool struct {
	FunctionDeclarations []geminiFunctionDeclaration `json:"functionDeclarations"`
}

type geminiFunctionDeclaration struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type geminiResponse struct {
	Candidates    []geminiCandidate `json:"candidates"`
	UsageMetadata geminiUsage       `json:"usageMetadata"`
}

type geminiCandidate struct {
	Content geminiContent `json:"content"`
}

type geminiUsage struct {
	PromptTokenCount        int `json:"promptTokenCount"`
	CachedContentTokenCount int `json:"cachedContentTokenCount"`
	CandidatesTokenCount    int `json:"candidatesTokenCount"`
	ThoughtsTokenCount      int `json:"thoughtsTokenCount"`
	TotalTokenCount         int `json:"totalTokenCount"`
}

func geminiSystemInstruction(messages []Message) geminiContent {
	system := strings.TrimSpace(systemInstructions(messages))
	if system == "" {
		return geminiContent{}
	}
	return geminiContent{Parts: []geminiPart{{Text: system}}}
}

func geminiContents(messages []Message) []geminiContent {
	out := make([]geminiContent, 0, len(messages))
	callNames := map[string]string{}
	for _, msg := range messages {
		switch msg.Role {
		case RoleSystem:
			continue
		case RoleUser:
			parts := make([]geminiPart, 0, 1+len(msg.Attachments))
			if strings.TrimSpace(msg.Content) != "" {
				parts = append(parts, geminiPart{Text: msg.Content})
			}
			parts = append(parts, geminiImageParts(msg.Attachments)...)
			if len(parts) > 0 {
				out = append(out, geminiContent{Role: "user", Parts: parts})
			}
		case RoleAssistant:
			parts := make([]geminiPart, 0, 1+len(msg.ToolCalls))
			if strings.TrimSpace(msg.Content) != "" {
				parts = append(parts, geminiPart{Text: msg.Content})
			}
			for _, call := range msg.ToolCalls {
				callNames[call.ID] = call.Name
				parts = append(parts, geminiPart{
					FunctionCall: &geminiFunctionCall{
						ID:   call.ID,
						Name: call.Name,
						Args: call.Arguments,
					},
					ThoughtSignature: geminiThoughtSignature(call.ProviderMetadata),
				})
			}
			if len(parts) > 0 {
				out = append(out, geminiContent{Role: "model", Parts: parts})
			}
		case RoleTool:
			name := callNames[msg.ToolCallID]
			if name == "" {
				name = msg.ToolCallID
			}
			out = append(out, geminiContent{
				Role: "user",
				Parts: []geminiPart{{
					FunctionResponse: &geminiFunctionResponse{
						ID:       msg.ToolCallID,
						Name:     name,
						Response: map[string]any{"output": msg.Content},
					},
				}},
			})
		}
	}
	return out
}

func geminiImageParts(attachments []Attachment) []geminiPart {
	parts := make([]geminiPart, 0, len(attachments))
	for _, attachment := range attachments {
		if attachment.Type != "image" || strings.TrimSpace(attachment.Data) == "" || strings.TrimSpace(attachment.MIMEType) == "" {
			continue
		}
		parts = append(parts, geminiPart{
			InlineData: &geminiInlineData{
				MIMEType: attachment.MIMEType,
				Data:     attachment.Data,
			},
		})
	}
	return parts
}

func geminiTools(specs []ToolSpec) []geminiTool {
	if len(specs) == 0 {
		return nil
	}
	declarations := make([]geminiFunctionDeclaration, 0, len(specs))
	for _, spec := range specs {
		declarations = append(declarations, geminiFunctionDeclaration{
			Name:        spec.Name,
			Description: spec.Description,
			Parameters:  spec.Parameters,
		})
	}
	return []geminiTool{{FunctionDeclarations: declarations}}
}

func (r geminiResponse) toLLMResponse() Response {
	var parts []string
	var calls []ToolCall
	if len(r.Candidates) > 0 {
		for _, part := range r.Candidates[0].Content.Parts {
			if strings.TrimSpace(part.Text) != "" {
				parts = append(parts, part.Text)
			}
			if part.FunctionCall != nil {
				id := part.FunctionCall.ID
				if id == "" {
					id = fmt.Sprintf("gemini_call_%d", len(calls)+1)
				}
				metadata := map[string]any{}
				if part.ThoughtSignature != "" {
					metadata["thoughtSignature"] = part.ThoughtSignature
				}
				calls = append(calls, ToolCall{
					ID:               id,
					Name:             part.FunctionCall.Name,
					Arguments:        part.FunctionCall.Args,
					ProviderMetadata: metadata,
				})
			}
		}
	}
	total := r.UsageMetadata.TotalTokenCount
	if total == 0 {
		total = r.UsageMetadata.PromptTokenCount + r.UsageMetadata.CandidatesTokenCount
	}
	return Response{
		Content:   strings.Join(parts, "\n"),
		ToolCalls: calls,
		Usage: Usage{
			InputTokens:     r.UsageMetadata.PromptTokenCount,
			CachedTokens:    r.UsageMetadata.CachedContentTokenCount,
			OutputTokens:    r.UsageMetadata.CandidatesTokenCount,
			ReasoningTokens: r.UsageMetadata.ThoughtsTokenCount,
			TotalTokens:     total,
		},
	}
}

func geminiModelName(model string) string {
	return strings.TrimPrefix(strings.TrimSpace(model), "models/")
}

func geminiThoughtSignature(metadata map[string]any) string {
	if metadata == nil {
		return ""
	}
	value, _ := metadata["thoughtSignature"].(string)
	return value
}
