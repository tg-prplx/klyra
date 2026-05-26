package contextmgr

import "agentcli/pkg/llm"

type Window struct {
	maxMessages int
	maxTokens   int
	messages    []llm.Message
}

func NewWindow(maxMessages int) *Window {
	if maxMessages < 4 {
		maxMessages = 4
	}
	return &Window{maxMessages: maxMessages}
}

func NewBudgetedWindow(maxMessages int, maxTokens int) *Window {
	window := NewWindow(maxMessages)
	window.maxTokens = maxTokens
	return window
}

func (w *Window) Add(message llm.Message) {
	w.messages = append(w.messages, message)
	w.trim()
}

func (w *Window) Messages() []llm.Message {
	out := make([]llm.Message, len(w.messages))
	copy(out, w.messages)
	if w.maxTokens > 0 {
		out, _ = PackMessages(out, w.maxTokens, w.maxMessages)
	}
	return out
}

func (w *Window) trim() {
	if len(w.messages) <= w.maxMessages {
		return
	}

	system := make([]llm.Message, 0, 1)
	if len(w.messages) > 0 && w.messages[0].Role == llm.RoleSystem {
		system = append(system, w.messages[0])
	}

	tailSize := w.maxMessages - len(system)
	tail := w.messages[len(w.messages)-tailSize:]
	w.messages = append(system, tail...)
}
