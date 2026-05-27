package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func executeCmd(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if msg == nil {
		return nil
	}
	val := reflect.ValueOf(msg)
	if val.Kind() == reflect.Slice && strings.ToLower(val.Type().Name()) == "batchmsg" {
		var finalMsg tea.Msg
		for i := 0; i < val.Len(); i++ {
			subCmdVal := val.Index(i)
			subCmd, ok := subCmdVal.Interface().(tea.Cmd)
			if !ok {
				continue
			}
			if subCmd != nil {
				subMsg := subCmd()
				if subMsg != nil {
					if _, ok := subMsg.(responseMsg); ok {
						return subMsg
					}
					finalMsg = subMsg
				}
			}
		}
		return finalMsg
	}
	return msg
}

func TestViewIncludesMetadata(t *testing.T) {
	model := New(Config{SessionID: "s1", Provider: "mock", Model: "mock-agent"})
	view := model.View()
	if !strings.Contains(view, "mock") || !strings.Contains(view, "session s1") {
		t.Fatalf("view missing metadata:\n%s", view)
	}
}

func TestHelpCommandOpensModal(t *testing.T) {
	model := New(Config{
		Commands: []CommandDef{
			{Name: "/help", Description: "Show help"},
			{Name: "/status", Description: "Show status"},
		},
		Handler: func(input string) (string, error) {
			return "", nil
		},
	})
	model.input.SetValue("/help")
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("expected no command from help modal open")
	}
	m := updated.(Model)
	if m.activeModal != modalHelp {
		t.Fatalf("expected help modal to be open, got %d", m.activeModal)
	}
	view := m.View()
	if !strings.Contains(view, "Command Reference") {
		t.Fatalf("help modal not rendered:\n%s", view)
	}
	// Close with Esc
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.activeModal != modalNone {
		t.Fatal("expected modal to be closed after Esc")
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
	msg := executeCmd(cmd)
	updated, _ = updated.(Model).Update(msg)
	view := updated.(Model).View()
	if !strings.Contains(view, "handled") || !strings.Contains(view, "/status") {
		t.Fatalf("handler response not rendered:\n%s", view)
	}
}

func TestFirstEnterSendsMessageInsteadOfAutocomplete(t *testing.T) {
	var seen string
	model := New(Config{
		Commands: []CommandDef{{Name: "/help", Description: "help"}},
		Handler: func(input string) (string, error) {
			seen = input
			return "ok", nil
		},
	})
	model.input.SetValue("hello")
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected handler command")
	}
	_ = executeCmd(cmd)
	if seen != "hello" {
		t.Fatalf("expected natural message to be sent, got %q", seen)
	}
}

func TestSettingsCommandsUpdateHeaderOptimistically(t *testing.T) {
	model := New(Config{
		Provider: "mock",
		Model:    "mock-agent",
		Commands: []CommandDef{{Name: "/provider", Description: "provider"}},
		Handler: func(input string) (string, error) {
			return "ok " + input, nil
		},
	})
	model.input.SetValue("/provider ollama")
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command")
	}
	view := updated.(Model).View()
	if !strings.Contains(view, "ollama") {
		t.Fatalf("provider header did not update:\n%s", view)
	}
}

func TestSettingsModalAppliesFormWithoutSlashTyping(t *testing.T) {
	var seen string
	model := New(Config{
		Provider: "mock",
		Model:    "mock-agent",
		Handler: func(input string) (string, error) {
			seen = input
			return "saved", nil
		},
	})
	// Open settings modal with F2
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyF2})
	m := updated.(Model)
	if m.activeModal != modalSettings {
		t.Fatal("expected settings modal to be open")
	}
	// Provider is the first field, cycle right to change it
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	// Press Enter to save
	updated, cmd := updated.(Model).Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected settings apply command")
	}
	_ = executeCmd(cmd)
	m = updated.(Model)
	if !strings.Contains(seen, "/set provider=openai") {
		t.Fatalf("settings form did not submit provider update: %q", seen)
	}
	view := m.View()
	if !strings.Contains(view, "openai") {
		t.Fatalf("settings form did not update header:\n%s", view)
	}
}

func TestApprovalPromptUsesKeys(t *testing.T) {
	reply := make(chan bool, 1)
	model := New(Config{})
	updated, _ := model.Update(ApprovalRequestMsg{Tool: "write_file", Reply: reply})
	view := updated.(Model).View()
	if !strings.Contains(view, "Approval required") {
		t.Fatalf("approval prompt not rendered:\n%s", view)
	}
	updated, _ = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if !<-reply {
		t.Fatal("expected approval")
	}
}

func TestPickerModalOpensForApproval(t *testing.T) {
	model := New(Config{
		Approval: "auto",
		Handler: func(input string) (string, error) {
			return "ok", nil
		},
	})
	model.input.SetValue("/approval")
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("expected no command, picker should open")
	}
	m := updated.(Model)
	if m.activeModal != modalPicker {
		t.Fatalf("expected picker modal, got %d", m.activeModal)
	}
	if m.pickerModal == nil {
		t.Fatal("picker modal is nil")
	}
	if m.pickerModal.Title != "Approval Mode" {
		t.Fatalf("wrong picker title: %s", m.pickerModal.Title)
	}
	// Navigate down to "ask" (index 1)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	// Select with Enter
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected handler command after picker selection")
	}
	m = updated.(Model)
	if m.approval != "ask" {
		t.Fatalf("expected approval=ask, got %q", m.approval)
	}
	if m.activeModal != modalNone {
		t.Fatal("expected modal to be closed")
	}
}

func TestPickerModalCancelWithEsc(t *testing.T) {
	model := New(Config{
		Approval: "auto",
		Handler: func(input string) (string, error) {
			return "", nil
		},
	})
	model.input.SetValue("/approval")
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m := updated.(Model)
	if m.activeModal != modalPicker {
		t.Fatal("expected picker modal")
	}
	// Cancel with Esc
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.activeModal != modalNone {
		t.Fatal("expected modal to be closed")
	}
	if m.approval != "auto" {
		t.Fatalf("approval should not change after cancel, got %q", m.approval)
	}
}

func TestProviderPickerOpens(t *testing.T) {
	model := New(Config{Provider: "mock"})
	model.input.SetValue("/provider")
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m := updated.(Model)
	if m.activeModal != modalPicker {
		t.Fatal("expected picker modal for /provider")
	}
	if m.pickerModal.Title != "Provider" {
		t.Fatalf("wrong picker title: %s", m.pickerModal.Title)
	}
}

func TestCommandWithArgsBypassesPicker(t *testing.T) {
	var seen string
	model := New(Config{
		Provider: "mock",
		Handler: func(input string) (string, error) {
			seen = input
			return "ok", nil
		},
	})
	// /approval with arg should NOT open picker, should go to handler
	model.input.SetValue("/approval ask")
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command for /approval with argument")
	}
	_ = executeCmd(cmd)
	m := updated.(Model)
	if m.activeModal != modalNone {
		t.Fatal("picker should not open when arg is provided")
	}
	if m.approval != "ask" {
		t.Fatalf("expected optimistic approval=ask, got %q", m.approval)
	}
	if seen != "/approval ask" {
		t.Fatalf("handler should receive full command, got %q", seen)
	}
}

func TestModelReasoningThoughts(t *testing.T) {
	model := New(Config{
		Handler: func(input string) (string, error) {
			return "done", nil
		},
	})

	// 1. Send ReasoningMsg and check compact rendering
	updated, _ := model.Update(ReasoningMsg("thinking about coding"))
	m := updated.(Model)
	if m.reasoningText != "thinking about coding" {
		t.Fatalf("expected reasoning text 'thinking about coding', got %q", m.reasoningText)
	}
	if m.reasonExpanded {
		t.Fatal("expected reasoning to be collapsed by default")
	}
	view := m.View()
	if !strings.Contains(view, "Show Thoughts: 💭 thinking about coding") {
		t.Fatalf("compact thoughts not rendered in view:\n%s", view)
	}

	// 2. Toggle F4 (expand)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyF4})
	m = updated.(Model)
	if !m.reasonExpanded {
		t.Fatal("expected reasoning to be expanded after F4 keypress")
	}
	view = m.View()
	if !strings.Contains(view, "Hide Thoughts") || !strings.Contains(view, "│ thinking about coding") {
		t.Fatalf("expanded thoughts not rendered in view:\n%s", view)
	}

	// 3. Toggle F4 again (collapse)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyF4})
	m = updated.(Model)
	if m.reasonExpanded {
		t.Fatal("expected reasoning to be collapsed after second F4 keypress")
	}

	// 4. Submit input (Enter) should clear reasoning
	m.input.SetValue("hello")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.reasoningText != "" {
		t.Fatalf("expected reasoning text to be cleared on Enter, got %q", m.reasoningText)
	}
	if m.reasonExpanded {
		t.Fatal("expected reasoning expanded state to be reset on Enter")
	}
}
