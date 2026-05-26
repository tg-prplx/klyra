package llm

import (
	"context"
	"fmt"
	"strings"
)

type MockProvider struct{}

func NewMockProvider() *MockProvider {
	return &MockProvider{}
}

func (p *MockProvider) Complete(_ context.Context, req Request) (Response, error) {
	if !hasToolObservation(req.Messages) {
		return Response{
			Content: "Осматриваю рабочую директорию перед следующим шагом.",
			ToolCalls: []ToolCall{{
				ID:   "mock-list-files-1",
				Name: "list_files",
				Arguments: map[string]any{
					"max_files": 80,
				},
			}},
		}, nil
	}

	task := latestUserMessage(req.Messages)
	return Response{
		Content: fmt.Sprintf("MVP agent loop готов: задача принята (%q), tools подключены, контекст и сжатие вывода работают. Для реальных правок подключи LLM-провайдер поверх интерфейса pkg/llm.Provider.", task),
	}, nil
}

func hasToolObservation(messages []Message) bool {
	for _, msg := range messages {
		if msg.Role == RoleTool {
			return true
		}
	}
	return false
}

func latestUserMessage(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == RoleUser {
			return strings.TrimSpace(messages[i].Content)
		}
	}
	return ""
}
