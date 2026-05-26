package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestViewIncludesMetadata(t *testing.T) {
	model := New(Config{SessionID: "s1", Provider: "mock", Model: "mock-agent"})
	view := model.View()
	if !strings.Contains(view, "provider=mock") || !strings.Contains(view, "session=s1") {
		t.Fatalf("view missing metadata:\n%s", view)
	}
}

func TestHelpCommandUpdatesView(t *testing.T) {
	model := New(Config{})
	model.input.SetValue("/help")
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	view := updated.(Model).View()
	if !strings.Contains(view, "commands: /help") {
		t.Fatalf("help command not rendered:\n%s", view)
	}
}

func TestHandlerCommandReturnsResponse(t *testing.T) {
	model := New(Config{
		Handler: func(input string) (string, error) {
			return "handled " + input, nil
		},
	})
	model.input.SetValue("/status")
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command")
	}
	msg := cmd()
	updated, _ = updated.(Model).Update(msg)
	view := updated.(Model).View()
	if !strings.Contains(view, "handled /status") {
		t.Fatalf("handler response not rendered:\n%s", view)
	}
}
