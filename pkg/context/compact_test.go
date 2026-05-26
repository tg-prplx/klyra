package contextmgr

import (
	"strings"
	"testing"

	"agentcli/pkg/llm"
)

func TestPackMessagesAddsSummaryAndFitsBudget(t *testing.T) {
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: "system"},
		{Role: llm.RoleUser, Content: strings.Repeat("old ", 200)},
		{Role: llm.RoleAssistant, Content: strings.Repeat("older ", 200)},
		{Role: llm.RoleUser, Content: "recent question"},
		{Role: llm.RoleAssistant, Content: "recent answer"},
	}

	packed, stats := PackMessages(messages, 80, 10)
	if len(packed) >= len(messages) {
		t.Fatalf("expected messages to be compacted: %+v", packed)
	}
	if packed[0].Role != llm.RoleSystem {
		t.Fatalf("expected system message to be preserved: %+v", packed)
	}
	if !strings.Contains(packed[1].Content, "Context summary") {
		t.Fatalf("expected synthetic summary: %+v", packed)
	}
	if stats.OriginalTokens <= stats.PackedTokens {
		t.Fatalf("expected packed tokens to shrink: %+v", stats)
	}
}

func TestDropOrphanToolMessages(t *testing.T) {
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: "system"},
		{Role: llm.RoleTool, ToolCallID: "missing", Content: "orphan"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "read_file"}}},
		{Role: llm.RoleTool, ToolCallID: "call-1", Content: "kept"},
	}

	packed := DropOrphanToolMessages(messages)
	if len(packed) != 3 {
		t.Fatalf("expected orphan tool output to be dropped: %+v", packed)
	}
	if packed[len(packed)-1].Content != "kept" {
		t.Fatalf("expected valid tool output to remain: %+v", packed)
	}
}
