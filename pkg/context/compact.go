package contextmgr

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"agentcli/pkg/llm"
)

type PackStats struct {
	OriginalMessages int
	PackedMessages   int
	OriginalTokens   int
	PackedTokens     int
	Summarized       int
}

func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return (utf8.RuneCountInString(text) + 3) / 4
}

func EstimateMessageTokens(message llm.Message) int {
	total := 6 + EstimateTokens(string(message.Role)) + EstimateTokens(message.Content)
	for _, call := range message.ToolCalls {
		total += 8 + EstimateTokens(call.Name)
		for key, value := range call.Arguments {
			total += EstimateTokens(key) + EstimateTokens(fmt.Sprint(value))
		}
	}
	return total
}

func EstimateMessagesTokens(messages []llm.Message) int {
	total := 0
	for _, message := range messages {
		total += EstimateMessageTokens(message)
	}
	return total
}

func PackMessages(messages []llm.Message, maxTokens int, maxMessages int) ([]llm.Message, PackStats) {
	stats := PackStats{
		OriginalMessages: len(messages),
		OriginalTokens:   EstimateMessagesTokens(messages),
	}
	if len(messages) == 0 {
		return nil, stats
	}

	packed := append([]llm.Message(nil), messages...)
	if maxMessages > 0 && len(packed) > maxMessages {
		packed = packByMessageCount(packed, maxMessages)
	}
	if maxTokens > 0 && EstimateMessagesTokens(packed) > maxTokens {
		packed = packByTokenBudget(packed, maxTokens)
	}
	packed = DropOrphanToolMessages(packed)

	stats.PackedMessages = len(packed)
	stats.PackedTokens = EstimateMessagesTokens(packed)
	stats.Summarized = stats.OriginalMessages - stats.PackedMessages
	if hasSyntheticSummary(packed) {
		stats.Summarized++
	}
	return packed, stats
}

func CompactMessages(messages []llm.Message, maxTokens int, keepTail int) ([]llm.Message, PackStats) {
	if keepTail <= 0 {
		keepTail = 12
	}
	return PackMessages(messages, maxTokens, keepTail+2)
}

func DropOrphanToolMessages(messages []llm.Message) []llm.Message {
	seenCalls := map[string]bool{}
	out := make([]llm.Message, 0, len(messages))
	for _, message := range messages {
		if message.Role == llm.RoleTool {
			if !seenCalls[message.ToolCallID] {
				continue
			}
			out = append(out, message)
			continue
		}
		for _, call := range message.ToolCalls {
			seenCalls[call.ID] = true
		}
		out = append(out, message)
	}
	return out
}

func packByMessageCount(messages []llm.Message, maxMessages int) []llm.Message {
	if len(messages) <= maxMessages {
		return messages
	}
	system, rest := splitSystem(messages)
	tailSize := maxMessages - len(system)
	if tailSize < 1 {
		tailSize = 1
	}
	if tailSize > len(rest) {
		tailSize = len(rest)
	}
	removed := rest[:len(rest)-tailSize]
	tail := rest[len(rest)-tailSize:]
	return appendSummary(system, removed, tail)
}

func packByTokenBudget(messages []llm.Message, maxTokens int) []llm.Message {
	system, rest := splitSystem(messages)
	tail := make([]llm.Message, 0, len(rest))
	used := EstimateMessagesTokens(system)
	for i := len(rest) - 1; i >= 0; i-- {
		next := EstimateMessageTokens(rest[i])
		if used+next > maxTokens && len(tail) > 0 {
			break
		}
		tail = append([]llm.Message{rest[i]}, tail...)
		used += next
	}
	removedCount := len(rest) - len(tail)
	if removedCount <= 0 {
		return messages
	}
	return appendSummary(system, rest[:removedCount], tail)
}

func splitSystem(messages []llm.Message) ([]llm.Message, []llm.Message) {
	if len(messages) > 0 && messages[0].Role == llm.RoleSystem {
		return []llm.Message{messages[0]}, messages[1:]
	}
	return nil, messages
}

func appendSummary(system []llm.Message, removed []llm.Message, tail []llm.Message) []llm.Message {
	out := append([]llm.Message(nil), system...)
	if len(removed) > 0 {
		out = append(out, llm.Message{
			Role:    llm.RoleAssistant,
			Content: summarizeRemoved(removed),
		})
	}
	out = append(out, tail...)
	return out
}

func summarizeRemoved(messages []llm.Message) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("Context summary: %d older messages were compacted to reduce token use.", len(messages)))
	for _, message := range messages {
		content := strings.TrimSpace(message.Content)
		if content == "" && len(message.ToolCalls) > 0 {
			var names []string
			for _, call := range message.ToolCalls {
				names = append(names, call.Name)
			}
			content = "tool calls: " + strings.Join(names, ", ")
		}
		content = strings.Join(strings.Fields(content), " ")
		if content == "" {
			continue
		}
		if len(content) > 220 {
			content = content[:220] + "..."
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", message.Role, content))
		if len(lines) >= 12 {
			lines = append(lines, "- ...")
			break
		}
	}
	return strings.Join(lines, "\n")
}

func hasSyntheticSummary(messages []llm.Message) bool {
	for _, message := range messages {
		if strings.HasPrefix(message.Content, "Context summary:") {
			return true
		}
	}
	return false
}
