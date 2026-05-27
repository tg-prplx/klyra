package llm

import "context"

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role        Role         `json:"role"`
	Content     string       `json:"content"`
	Reasoning   string       `json:"reasoning,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
	ToolCallID  string       `json:"tool_call_id,omitempty"`
	ToolCalls   []ToolCall   `json:"tool_calls,omitempty"`
}

type Attachment struct {
	Type     string `json:"type"`
	MIMEType string `json:"mime_type"`
	Name     string `json:"name,omitempty"`
	Data     string `json:"data,omitempty"`
	URL      string `json:"url,omitempty"`
}

type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type ToolCall struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Arguments        map[string]any `json:"arguments"`
	ProviderMetadata map[string]any `json:"provider_metadata,omitempty"`
}

type Request struct {
	Model           string
	Messages        []Message
	Tools           []ToolSpec
	MaxOutputTokens int
	ReasoningEffort string
	Store           bool
}

type Response struct {
	Content   string
	Reasoning string
	ToolCalls []ToolCall
	Usage     Usage
	ID        string
}

type Usage struct {
	InputTokens     int `json:"input_tokens"`
	CachedTokens    int `json:"cached_tokens"`
	OutputTokens    int `json:"output_tokens"`
	ReasoningTokens int `json:"reasoning_tokens"`
	TotalTokens     int `json:"total_tokens"`
}

type Provider interface {
	Complete(ctx context.Context, req Request) (Response, error)
}

type StreamEvent struct {
	Delta              string
	Reasoning          string
	ToolCallIndex      int
	ToolCallID         string
	ToolName           string
	ToolArgumentsDelta string
}

type StreamHandler func(StreamEvent) error

type StreamProvider interface {
	Provider
	Stream(ctx context.Context, req Request, handler StreamHandler) (Response, error)
}
