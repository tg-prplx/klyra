package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
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

func isClearScreenCmd(cmd tea.Cmd) bool {
	msg := executeCmd(cmd)
	return strings.Contains(fmt.Sprintf("%T", msg), "clearScreenMsg")
}

func TestViewIncludesMetadata(t *testing.T) {
	model := New(Config{SessionID: "s1", Provider: "mock", Model: "mock-agent"})
	view := model.View()
	if !strings.Contains(view, "mock") || !strings.Contains(view, "session s1") {
		t.Fatalf("view missing metadata:\n%s", view)
	}
}

func TestSidebarRendersFilesDiffAndContext(t *testing.T) {
	model := New(Config{
		SidebarFiles:    []string{"README.md", "pkg/tui/model.go"},
		SidebarDiff:     "diff --git a/a b/a\n+added\n-removed",
		CartCount:       2,
		ContextCockpit:  true,
		ContextRecipes:  true,
		NegativeContext: true,
	})
	model.width = 120
	model.height = 30
	model.syncViewport(true)

	view := stripANSI(model.View())
	if !strings.Contains(view, "Klyra Sidebar") || !strings.Contains(view, "README.md") {
		t.Fatalf("files sidebar missing:\n%s", view)
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyF7})
	m := updated.(Model)
	view = stripANSI(m.View())
	if !strings.Contains(view, "tracked diff") || !strings.Contains(view, "+added") {
		t.Fatalf("diff sidebar missing:\n%s", view)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyF7})
	m = updated.(Model)
	view = stripANSI(m.View())
	if !strings.Contains(view, "cart files: 2") || !strings.Contains(view, "cockpit: on") {
		t.Fatalf("context sidebar missing:\n%s", view)
	}
}

func TestSidebarCanBeHidden(t *testing.T) {
	model := New(Config{SidebarFiles: []string{"README.md"}})
	model.width = 120
	model.height = 30
	model.syncViewport(true)
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyF7, Alt: true})
	m := updated.(Model)
	view := stripANSI(m.View())
	if strings.Contains(view, "Klyra Sidebar") {
		t.Fatalf("sidebar should be hidden:\n%s", view)
	}
}

func TestThinkingBarKeepsStableWidth(t *testing.T) {
	model := New(Config{})
	model.spinnerFrame = 0
	first := stripANSI(model.renderThinkingBar())
	model.spinnerFrame = 7
	second := stripANSI(model.renderThinkingBar())
	if len([]rune(first)) != len([]rune(second)) {
		t.Fatalf("thinking bar width changed: %q (%d) vs %q (%d)", first, len([]rune(first)), second, len([]rune(second)))
	}
	if !strings.Contains(first, "thinking") || !strings.Contains(second, "thinking") {
		t.Fatalf("thinking label missing: %q / %q", first, second)
	}
}

func TestInitialLinesHydrateSavedSession(t *testing.T) {
	model := New(Config{
		SessionID:    "saved",
		InitialLines: []string{"you: hello", "agent: hi there"},
	})
	view := model.View()
	plain := stripANSI(view)
	if !strings.Contains(plain, "hello") || !strings.Contains(plain, "hi there") {
		t.Fatalf("saved session lines not rendered:\n%s", view)
	}
}

func TestSessionLoadedReplacesVisibleHistory(t *testing.T) {
	model := New(Config{
		SessionID:    "old",
		InitialLines: []string{"you: old chat"},
	})
	updated, _ := model.Update(SessionLoadedMsg{
		SessionID: "new",
		Lines:     []string{"you: new chat", "agent: loaded"},
	})
	m := updated.(Model)
	if m.sessionID != "new" {
		t.Fatalf("expected session id to update, got %q", m.sessionID)
	}
	view := m.View()
	plain := stripANSI(view)
	if strings.Contains(plain, "old chat") || !strings.Contains(plain, "new chat") || !strings.Contains(plain, "loaded") {
		t.Fatalf("session history was not replaced:\n%s", view)
	}
}

func TestHydratedAssistantMarkdownRenders(t *testing.T) {
	model := New(Config{
		InitialLines: []string{"agent: ## Loaded\n\n- item"},
	})
	view := model.View()
	if strings.Contains(view, "## Loaded") || strings.Contains(view, "- item") {
		t.Fatalf("hydrated assistant markdown was not rendered:\n%s", view)
	}
	if !strings.Contains(view, "Loaded") || !strings.Contains(view, "item") {
		t.Fatalf("rendered markdown content missing:\n%s", view)
	}
}

func TestToolOutputWrapsDecodedJSON(t *testing.T) {
	model := New(Config{
		InitialLines: []string{`tool: {"tool":"list_files","output":"one\ntwo\nthree"}`},
	})
	view := stripANSI(model.View())
	if strings.Contains(view, `{"tool"`) || strings.Contains(view, `\n`) {
		t.Fatalf("tool JSON should be decoded before rendering:\n%s", view)
	}
	if !strings.Contains(view, "list_files") || !strings.Contains(view, "one") || !strings.Contains(view, "two") {
		t.Fatalf("decoded tool output missing:\n%s", view)
	}
}

func TestToolOutputCollapsedByDefaultAndExpands(t *testing.T) {
	model := New(Config{
		InitialLines: []string{`tool: {"tool":"read_file","output":"line one\nline two\nline three"}`},
	})
	view := stripANSI(model.View())
	if !strings.Contains(view, "read_file") || !strings.Contains(view, "finished") {
		t.Fatalf("collapsed tool summary missing:\n%s", view)
	}
	if strings.Contains(view, "│ line one") || strings.Contains(view, "│ line two") {
		t.Fatalf("tool body should be collapsed by default:\n%s", view)
	}
	if !model.toggleLatestToolDetails() {
		t.Fatal("expected tool details to toggle")
	}
	model.syncViewport(true)
	view = stripANSI(model.View())
	if !strings.Contains(view, "│ line one") || !strings.Contains(view, "│ line two") {
		t.Fatalf("expanded tool body missing:\n%s", view)
	}
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

func stripANSI(value string) string {
	return ansiPattern.ReplaceAllString(value, "")
}

func TestCommandOutputLineHasAccent(t *testing.T) {
	line := stripANSI(renderCommandOutputLine("setting saved: reasoning = xhigh"))
	if !strings.Contains(line, "│") || !strings.Contains(line, "setting saved") {
		t.Fatalf("command output accent missing: %q", line)
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
	// Provider section is collapsed by default: expand, move to field, then change it.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	updated, _ = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyDown})
	updated, _ = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyRight})
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

func TestSettingsModalSectionsCollapsedByDefault(t *testing.T) {
	model := New(Config{Provider: "mock", Model: "mock-agent"})
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyF2})
	m := updated.(Model)
	if m.settingsModal == nil {
		t.Fatal("expected settings modal")
	}
	view := stripANSI(m.settingsModal.View(100, 40))
	if !strings.Contains(view, "▸ PROVIDER") || !strings.Contains(view, "▸ CONTEXT") {
		t.Fatalf("expected collapsed section rows:\n%s", view)
	}
	if strings.Contains(view, "Provider:") || strings.Contains(view, "Context Tokens:") {
		t.Fatalf("fields should be hidden while sections are collapsed:\n%s", view)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)
	view = stripANSI(m.settingsModal.View(100, 40))
	if !strings.Contains(view, "▾ PROVIDER") || !strings.Contains(view, "Provider:") {
		t.Fatalf("provider section should expand:\n%s", view)
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

func TestSessionsPickerLoadsAndSelectsSession(t *testing.T) {
	var seen string
	model := New(Config{
		SessionID: "current",
		Handler: func(input string) (string, error) {
			seen = input
			return "switched", nil
		},
		PickerProvider: func(field string) (PickerModal, error) {
			if field != "session" {
				t.Fatalf("unexpected picker field: %s", field)
			}
			return SessionPicker("current", []PickerOption{
				{Value: "current", Label: "current", Description: "active"},
				{Value: "next", Label: "next", Description: "2 messages"},
			}), nil
		},
	})
	model.input.SetValue("/sessions")
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected picker provider command")
	}
	msg := executeCmd(cmd)
	updated, _ = updated.(Model).Update(msg)
	m := updated.(Model)
	if m.activeModal != modalPicker || m.pickerModal == nil {
		t.Fatal("expected session picker modal")
	}
	if m.pickerModal.Field != "session" {
		t.Fatalf("expected session field, got %q", m.pickerModal.Field)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected handler command after selecting session")
	}
	_ = executeCmd(cmd)
	m = updated.(Model)
	if m.sessionID != "next" {
		t.Fatalf("expected optimistic session id update, got %q", m.sessionID)
	}
	if seen != "/session next" {
		t.Fatalf("expected /session command, got %q", seen)
	}
}

func TestCheckpointPickerRestoreLoadsCheckpointOptions(t *testing.T) {
	var seenField string
	model := New(Config{
		PickerProvider: func(field string) (PickerModal, error) {
			seenField = field
			return CheckpointRestorePicker([]PickerOption{{Value: "cp1", Label: "cp1"}}), nil
		},
	})
	model.input.SetValue("/checkpoint")
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m := updated.(Model)
	if m.activeModal != modalPicker || m.pickerModal == nil || m.pickerModal.Field != "checkpoint" {
		t.Fatal("expected checkpoint action picker")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected checkpoint restore picker command")
	}
	msg := executeCmd(cmd)
	updated, _ = updated.(Model).Update(msg)
	m = updated.(Model)
	if seenField != "checkpoint_restore" {
		t.Fatalf("expected checkpoint_restore picker, got %q", seenField)
	}
	if m.activeModal != modalPicker || m.pickerModal == nil || m.pickerModal.Field != "checkpoint_restore" {
		t.Fatal("expected checkpoint restore picker")
	}
}

func TestConfigPickerSendsCommand(t *testing.T) {
	var seen string
	model := New(Config{
		Handler: func(input string) (string, error) {
			seen = input
			return "ok", nil
		},
	})
	model.input.SetValue("/config")
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m := updated.(Model)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected config command")
	}
	_ = executeCmd(cmd)
	if seen != "/config show" {
		t.Fatalf("expected /config show, got %q", seen)
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
	model.busy = true

	// 1. Send ReasoningMsg and check compact rendering
	updated, _ := model.Update(ReasoningMsg("## Plan\n\n- thinking\n- about coding\n"))
	m := updated.(Model)
	if m.reasoningText != "## Plan\n\n- thinking\n- about coding\n" {
		t.Fatalf("expected reasoning text with markdown/newlines, got %q", m.reasoningText)
	}
	if m.reasonExpanded {
		t.Fatal("expected reasoning to be collapsed by default")
	}
	view := m.View()
	if !strings.Contains(view, "▸ Thinking") || !strings.Contains(view, "Plan") {
		t.Fatalf("compact thoughts not rendered in view:\n%s", view)
	}

	// 2. Right arrow expands.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)
	if !m.reasonExpanded {
		t.Fatal("expected reasoning to be expanded after right arrow")
	}
	view = m.View()
	if !strings.Contains(view, "▾ Thinking") || !strings.Contains(view, "Plan") || !strings.Contains(view, "thinking") {
		t.Fatalf("expanded thoughts not rendered in view:\n%s", view)
	}

	// 3. Left arrow collapses.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = updated.(Model)
	if m.reasonExpanded {
		t.Fatal("expected reasoning to be collapsed after left arrow")
	}

	// 4. Starting a new input should clear transient reasoning.
	m.busy = false
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

func TestReasoningWhitespaceDeltasArePreserved(t *testing.T) {
	model := New(Config{})
	model.busy = true

	updated, _ := model.Update(ReasoningMsg("first"))
	m := updated.(Model)
	updated, _ = m.Update(ReasoningMsg("\n\n"))
	m = updated.(Model)
	updated, _ = m.Update(ReasoningMsg("- second"))
	m = updated.(Model)

	if m.reasoningText != "first\n\n- second" {
		t.Fatalf("reasoning newlines were not preserved: %q", m.reasoningText)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(Model)
	view := stripANSI(m.View())
	if !strings.Contains(view, "first") || !strings.Contains(view, "second") {
		t.Fatalf("expanded reasoning missing content:\n%s", view)
	}
	if strings.Contains(view, "first - second") {
		t.Fatalf("reasoning newlines were collapsed in markdown rendering:\n%s", view)
	}
}

func TestReasoningPersistsAboveStreamedResponse(t *testing.T) {
	model := New(Config{})
	model.busy = true
	updated, _ := model.Update(ReasoningMsg("## Reason\n\n- private local reasoning\n- wraps near the answer\n"))
	m := updated.(Model)
	updated, _ = m.Update(StreamMsg("visible answer"))
	m = updated.(Model)
	updated, _ = m.Update(responseMsg{input: "hello", output: "", agentRun: true})
	m = updated.(Model)
	if !modelLinesContain(m.lines, "thoughts:0:", "## Reason\n\n- private local reasoning\n- wraps near the answer\n") {
		t.Fatalf("expected persisted thoughts block above response: %#v", m.lines)
	}
	if !modelLinesContain(m.lines, "agent:", "visible", "answer") {
		t.Fatalf("expected persisted agent answer: %#v", m.lines)
	}
}

func TestMouseClickTogglesThoughts(t *testing.T) {
	model := New(Config{
		InitialLines: []string{"thoughts:0:## Plan\n\n- inspect files", "agent: done"},
	})
	lines := model.currentViewportLines()
	clickY := -1
	for i, line := range lines {
		if strings.Contains(stripANSICodes(line), "Thoughts") {
			clickY = i - model.viewport.YOffset
			break
		}
	}
	if clickY < 0 {
		t.Fatalf("thoughts header not found in viewport:\n%s", model.View())
	}
	updated, cmd := model.Update(tea.MouseMsg{Type: tea.MouseLeft, Y: clickY})
	m := updated.(Model)
	if !isClearScreenCmd(cmd) {
		t.Fatal("expected mouse thoughts toggle to force a clean repaint")
	}
	if !modelLinesContain(m.lines, "thoughts:1:", "## Plan") {
		t.Fatalf("expected mouse click to expand thoughts: %#v", m.lines)
	}
}

func TestThoughtsKeyboardToggleForcesCleanRepaint(t *testing.T) {
	model := New(Config{InitialLines: []string{"thoughts:0:thinking", "agent: done"}})
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyF4})
	m := updated.(Model)
	if !isClearScreenCmd(cmd) {
		t.Fatal("expected F4 thoughts toggle to force a clean repaint")
	}
	if !modelLinesContain(m.lines, "thoughts:1:", "thinking") {
		t.Fatalf("expected thoughts to expand: %#v", m.lines)
	}
}

func TestStreamedResponsePersistsAfterCompletion(t *testing.T) {
	model := New(Config{})
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")})
	m := updated.(Model)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected handler command")
	}
	m = updated.(Model)
	updated, _ = m.Update(StreamMsg("streamed answer"))
	m = updated.(Model)
	if m.streamBuf != "streamed answer" {
		t.Fatalf("expected stream buffer while busy, got %q", m.streamBuf)
	}
	updated, _ = m.Update(responseMsg{input: "hello", output: "", agentRun: true})
	m = updated.(Model)
	if m.streamBuf != "" {
		t.Fatalf("expected stream buffer to be cleared, got %q", m.streamBuf)
	}
	if !modelLinesContain(m.lines, "streamed", "answer") {
		t.Fatalf("streamed answer should persist after completion: %#v", m.lines)
	}
}

func TestSlashCommandRunsWhileAgentIsBusy(t *testing.T) {
	var seen string
	model := New(Config{
		Handler: func(input string) (string, error) {
			seen = input
			return "ok", nil
		},
	})
	model.busy = true
	model.streamBuf = "partial answer"
	model.reasoningText = "still thinking"
	model.input.SetValue("/status")

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected slash command to run while busy")
	}
	msg := executeCmd(cmd)
	if _, ok := msg.(responseMsg); !ok {
		t.Fatalf("expected response message, got %#v", msg)
	}
	m := updated.(Model)
	if seen != "/status" {
		t.Fatalf("handler saw %q", seen)
	}
	if !m.busy || m.streamBuf != "partial answer" || m.reasoningText != "still thinking" {
		t.Fatalf("busy agent state was disturbed: busy=%v stream=%q reasoning=%q", m.busy, m.streamBuf, m.reasoningText)
	}
}

func TestToolProgressRendersLive(t *testing.T) {
	model := New(Config{})
	updated, _ := model.Update(ToolProgressMsg{
		Phase: "running",
		Tool:  "read_file",
		Args:  map[string]any{"path": "main.go"},
	})
	m := updated.(Model)
	view := stripANSI(m.View())
	if !strings.Contains(view, "read_file") || !strings.Contains(view, "running") || !strings.Contains(view, "details collapsed") {
		t.Fatalf("tool progress missing from view:\n%s", view)
	}
}

func TestToolStreamDeltasAggregateIntoOneCollapsedLine(t *testing.T) {
	model := New(Config{})
	updated, _ := model.Update(ToolStreamMsg{Index: 0, ID: "call-1", Name: "read_file", Arguments: "{\"path\""})
	m := updated.(Model)
	updated, _ = m.Update(ToolStreamMsg{Index: 0, Arguments: ":\"README.md\"}"})
	m = updated.(Model)

	count := 0
	for _, line := range m.lines {
		if strings.HasPrefix(line, "toolstream:") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected one aggregated toolstream line, got %d: %#v", count, m.lines)
	}
	view := stripANSI(m.View())
	if strings.Count(view, "model is preparing tool call") != 1 {
		t.Fatalf("tool stream should render as one collapsed block:\n%s", view)
	}
	if strings.Contains(view, "│") {
		t.Fatalf("tool stream details should be collapsed by default:\n%s", view)
	}
	if !strings.Contains(view, "README.md") {
		t.Fatalf("tool stream summary should include compact args:\n%s", view)
	}
}

func TestToolStreamFlushesCurrentAssistantSegment(t *testing.T) {
	model := New(Config{})
	model.busy = true

	updated, _ := model.Update(ReasoningMsg("think before tool"))
	m := updated.(Model)
	updated, _ = m.Update(StreamMsg("I will inspect README."))
	m = updated.(Model)
	updated, _ = m.Update(ToolStreamMsg{Index: 0, ID: "call-1", Name: "read_file", Arguments: `{"path":"README.md"}`})
	m = updated.(Model)

	if m.reasoningText != "" || m.streamBuf != "" {
		t.Fatalf("live assistant segment should be flushed before tool: reasoning=%q stream=%q", m.reasoningText, m.streamBuf)
	}
	thoughtIdx := lineIndex(m.lines, "thoughts:", "think before tool")
	agentIdx := lineIndex(m.lines, "agent:", "I will inspect README.")
	toolIdx := lineIndex(m.lines, "toolstream:", "README.md")
	if thoughtIdx < 0 || agentIdx < 0 || toolIdx < 0 {
		t.Fatalf("expected thoughts, answer, and toolstream lines: %#v", m.lines)
	}
	if !(thoughtIdx < agentIdx && agentIdx < toolIdx) {
		t.Fatalf("expected thoughts -> answer -> tool order, got thoughts=%d agent=%d tool=%d lines=%#v", thoughtIdx, agentIdx, toolIdx, m.lines)
	}

	updated, _ = m.Update(ReasoningMsg("think after tool"))
	m = updated.(Model)
	updated, _ = m.Update(StreamMsg("Now I can explain."))
	m = updated.(Model)
	updated, _ = m.Update(responseMsg{input: "hello", output: "", agentRun: true})
	m = updated.(Model)

	secondThoughtIdx := lineIndexAfter(m.lines, toolIdx, "thoughts:", "think after tool")
	secondAgentIdx := lineIndexAfter(m.lines, toolIdx, "agent:", "Now I can explain.")
	if secondThoughtIdx < 0 || secondAgentIdx < 0 {
		t.Fatalf("expected second assistant segment after tool: %#v", m.lines)
	}
	if !(toolIdx < secondThoughtIdx && secondThoughtIdx < secondAgentIdx) {
		t.Fatalf("expected tool -> thoughts -> answer order, got tool=%d thoughts=%d agent=%d lines=%#v", toolIdx, secondThoughtIdx, secondAgentIdx, m.lines)
	}
}

func TestMouseClickTogglesSpecificToolDuringRun(t *testing.T) {
	model := New(Config{
		InitialLines: []string{
			`toolprogress:0:{"Phase":"done","Tool":"older_tool","Output":"old details"}`,
			`toolprogress:0:{"Phase":"done","Tool":"newer_tool","Output":"new details"}`,
		},
	})
	model.busy = true
	model.streamBuf = "still streaming"
	model.syncViewport(true)

	clickY := -1
	for i, line := range model.currentViewportLines() {
		if strings.Contains(stripANSICodes(line), "older_tool") {
			clickY = i - model.viewport.YOffset
			break
		}
	}
	if clickY < 0 {
		t.Fatalf("older tool line not found:\n%s", model.View())
	}

	updated, cmd := model.Update(tea.MouseMsg{Type: tea.MouseLeft, Y: clickY})
	m := updated.(Model)
	if !isClearScreenCmd(cmd) {
		t.Fatal("expected tool toggle to force a clean repaint")
	}
	if !strings.HasPrefix(m.lines[0], "toolprogress:1:") {
		t.Fatalf("clicked tool should expand, lines=%#v", m.lines)
	}
	if !strings.HasPrefix(m.lines[1], "toolprogress:0:") {
		t.Fatalf("unclicked latest tool should stay collapsed, lines=%#v", m.lines)
	}
}

func modelLinesContain(lines []string, parts ...string) bool {
	for _, line := range lines {
		matched := true
		for _, part := range parts {
			if !strings.Contains(line, part) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func lineIndex(lines []string, parts ...string) int {
	return lineIndexAfter(lines, -1, parts...)
}

func lineIndexAfter(lines []string, start int, parts ...string) int {
	for i := start + 1; i < len(lines); i++ {
		matched := true
		for _, part := range parts {
			if !strings.Contains(lines[i], part) {
				matched = false
				break
			}
		}
		if matched {
			return i
		}
	}
	return -1
}

func TestModelHistoryNavigation(t *testing.T) {
	model := New(Config{
		Handler: func(input string) (string, error) {
			return "ok", nil
		},
	})

	// 1. Submit two commands to populate history
	model.input.SetValue("first command")
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := executeCmd(cmd)
	updated, _ = updated.(Model).Update(msg)
	m := updated.(Model)

	m.input.SetValue("second command")
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg = executeCmd(cmd)
	updated, _ = updated.(Model).Update(msg)
	m = updated.(Model)

	// 2. Regular arrows navigate command history.
	m.input.SetValue("current typing")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)
	if m.input.Value() != "second command" {
		t.Fatalf("expected Up arrow to retrieve 'second command', got %q", m.input.Value())
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)
	if m.input.Value() != "first command" {
		t.Fatalf("expected second Up arrow to retrieve 'first command', got %q", m.input.Value())
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.input.Value() != "second command" {
		t.Fatalf("expected Down arrow to retrieve 'second command', got %q", m.input.Value())
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.input.Value() != "current typing" {
		t.Fatalf("expected second Down arrow to restore temp input 'current typing', got %q", m.input.Value())
	}

	// 3. Ctrl+P/Ctrl+N remain aliases for keyboard-centric terminals.
	m.input.SetValue("current typing")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = updated.(Model)
	if m.input.Value() != "second command" {
		t.Fatalf("expected Ctrl+P to retrieve 'second command', got %q", m.input.Value())
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	m = updated.(Model)
	if m.input.Value() != "current typing" {
		t.Fatalf("expected Ctrl+N to restore temp input 'current typing', got %q", m.input.Value())
	}
}

func TestMouseWheelScrollsChat(t *testing.T) {
	model := New(Config{})
	for i := 0; i < 80; i++ {
		model.lines = append(model.lines, fmt.Sprintf("system: line %02d", i))
	}
	model.syncViewport(true)
	start := model.viewport.YOffset

	updated, _ := model.Update(tea.MouseMsg{Type: tea.MouseWheelUp})
	m := updated.(Model)
	if m.viewport.YOffset >= start {
		t.Fatalf("expected mouse wheel up to scroll chat up from %d, got %d", start, m.viewport.YOffset)
	}

	updated, _ = m.Update(tea.MouseMsg{Type: tea.MouseWheelDown})
	m = updated.(Model)
	if m.viewport.YOffset <= start-3 {
		t.Fatalf("expected mouse wheel down to scroll chat down, got %d", m.viewport.YOffset)
	}
}

func TestRawMouseEscapeDoesNotReachInput(t *testing.T) {
	model := New(Config{})
	updated, _ := model.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune("[<65;14;34M[<65;14;34M"),
	})
	m := updated.(Model)
	if m.input.Value() != "" {
		t.Fatalf("raw mouse escape leaked into input: %q", m.input.Value())
	}
}

func TestRawMouseEscapeFragmentsDoNotReachInput(t *testing.T) {
	model := New(Config{})
	updated, _ := model.Update(tea.MouseMsg{Type: tea.MouseWheelDown})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune("[[[[[["),
	})
	m := updated.(Model)
	if m.input.Value() != "" {
		t.Fatalf("raw mouse escape fragment leaked into input: %q", m.input.Value())
	}
}

func TestSplitMouseEscapeFragmentsDoNotFlickerIntoInput(t *testing.T) {
	model := New(Config{})
	updated, _ := model.Update(tea.MouseMsg{Type: tea.MouseWheelDown})
	model = updated.(Model)
	for i := 0; i < 6; i++ {
		updated, _ = model.Update(tea.KeyMsg{
			Type:  tea.KeyRunes,
			Runes: []rune("["),
		})
		model = updated.(Model)
		if model.input.Value() != "" {
			t.Fatalf("split raw mouse escape fragment leaked into input at step %d: %q", i, model.input.Value())
		}
	}
}

func TestBracketInputWorksNormally(t *testing.T) {
	model := New(Config{})
	updated, _ := model.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune("["),
	})
	m := updated.(Model)
	if m.input.Value() != "[" {
		t.Fatalf("expected normal bracket input to work, got %q", m.input.Value())
	}
}

func TestCtrlCInterruptsBusyAgentWithoutQuitting(t *testing.T) {
	called := false
	model := New(Config{
		Interrupt: func() bool {
			called = true
			return true
		},
	})
	model.busy = true
	model.streamBuf = "partial"

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m := updated.(Model)
	if !called {
		t.Fatal("expected interrupt callback")
	}
	if m.busy {
		t.Fatal("expected busy state to stop after interrupt")
	}
	if !m.interrupted {
		t.Fatal("expected interrupted state")
	}
	if !isClearScreenCmd(cmd) {
		t.Fatal("expected interrupt to force a clean repaint")
	}
	if !modelLinesContain(m.lines, "interrupted", "current", "run") {
		t.Fatalf("expected interrupt status line: %#v", m.lines)
	}
}

func TestInterruptedResponseKeepsPartialStreamAndSuppressesCancelError(t *testing.T) {
	model := New(Config{})
	model.interrupted = true
	model.streamBuf = "partial answer"
	model.reasoningText = "thinking"

	updated, _ := model.Update(responseMsg{
		input:    "hello",
		err:      context.Canceled,
		agentRun: true,
	})
	m := updated.(Model)
	if m.err != nil {
		t.Fatalf("cancel error should be suppressed after interrupt: %v", m.err)
	}
	if m.interrupted {
		t.Fatal("interrupted state should be cleared after canceled response")
	}
	if !modelLinesContain(m.lines, "agent:", "partial answer") {
		t.Fatalf("partial stream should persist after interrupt: %#v", m.lines)
	}
	if modelLinesContain(m.lines, "error:", "context canceled") {
		t.Fatalf("context canceled should not be rendered as an error: %#v", m.lines)
	}
}

func TestMouseEscapeSanitizerKeepsNormalText(t *testing.T) {
	cleaned := sanitizeMouseEscapes("hello[<65;14;34M world")
	if cleaned != "hello world" {
		t.Fatalf("unexpected sanitized value: %q", cleaned)
	}
}

func TestMouseEscapeSanitizerDropsBracketBursts(t *testing.T) {
	cleaned := sanitizeMouseEscapes("hello[[[[ world")
	if cleaned != "hello world" {
		t.Fatalf("unexpected sanitized value: %q", cleaned)
	}
}

func TestF6TogglesCopyModeMouseCapture(t *testing.T) {
	model := New(Config{})
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyF6})
	m := updated.(Model)
	if !m.copyMode {
		t.Fatal("expected copy mode after F6")
	}
	if cmd == nil {
		t.Fatal("expected DisableMouse command")
	}

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyF6})
	m = updated.(Model)
	if m.copyMode {
		t.Fatal("expected scroll mode after second F6")
	}
	if cmd == nil {
		t.Fatal("expected EnableMouseCellMotion command")
	}
}

func TestStreamDoesNotStealScrollWhenReadingBacklog(t *testing.T) {
	model := New(Config{})
	model.height = 18
	model.busy = true
	for i := 0; i < 80; i++ {
		model.lines = append(model.lines, fmt.Sprintf("system: line %02d", i))
	}
	model.syncViewport(true)
	model.viewport.LineUp(10)
	before := model.viewport.YOffset

	updated, _ := model.Update(StreamMsg("new streamed token"))
	m := updated.(Model)
	if m.viewport.YOffset != before {
		t.Fatalf("stream should not force-scroll while reading backlog: before=%d after=%d", before, m.viewport.YOffset)
	}
}

func TestStreamFollowsWhenAlreadyAtBottom(t *testing.T) {
	model := New(Config{})
	model.height = 18
	model.busy = true
	for i := 0; i < 80; i++ {
		model.lines = append(model.lines, fmt.Sprintf("system: line %02d", i))
	}
	model.syncViewport(true)
	if !model.viewport.AtBottom() {
		t.Fatal("expected viewport to start at bottom")
	}

	updated, _ := model.Update(StreamMsg(strings.Repeat("stream line\n", 20)))
	m := updated.(Model)
	if !m.viewport.AtBottom() {
		t.Fatalf("stream should keep following when already at bottom, offset=%d", m.viewport.YOffset)
	}
}

func TestModelAPIKeyPersistence(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "agentcli-env-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Clean up environment variables to avoid leaks
	originalKey := os.Getenv("OPENAI_API_KEY")
	os.Unsetenv("OPENAI_API_KEY")
	defer func() {
		if originalKey != "" {
			os.Setenv("OPENAI_API_KEY", originalKey)
		} else {
			os.Unsetenv("OPENAI_API_KEY")
		}
	}()

	model := New(Config{
		CWD: tempDir,
		Handler: func(input string) (string, error) {
			return "saved", nil
		},
	})

	// 1. Open settings modal
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyF2})
	m := updated.(Model)
	if m.activeModal != modalSettings {
		t.Fatal("expected settings modal to open")
	}

	// 2. Modify openai_key
	found := false
	for idx, f := range m.settingsModal.Fields {
		if f.Name == "openai_key" {
			m.settingsModal.Fields[idx].Value = "test-secret-key"
			found = true
			break
		}
	}
	if !found {
		t.Fatal("openai_key field not found in settings modal")
	}

	// 3. Save settings
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	_ = executeCmd(cmd)

	// 4. Verify environment variable is set
	if os.Getenv("OPENAI_API_KEY") != "test-secret-key" {
		t.Fatalf("expected OPENAI_API_KEY env var to be 'test-secret-key', got %q", os.Getenv("OPENAI_API_KEY"))
	}

	// 5. Verify .env file is saved
	envPath := filepath.Join(tempDir, ".env")
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("failed to read .env file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "OPENAI_API_KEY=\"test-secret-key\"") {
		t.Fatalf("expected .env file to contain key assignment, got:\n%s", content)
	}
}
