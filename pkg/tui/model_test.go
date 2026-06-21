package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	appconfig "klyra/pkg/config"
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

func TestSidebarViewFitsTerminalWidth(t *testing.T) {
	model := New(Config{
		SidebarFiles: []string{"README.md", "pkg/tui/model.go"},
		SidebarDiff:  strings.Repeat("diff line with enough text to wrap if sidebar overflows\n", 8),
		Provider:     "openai",
		Model:        "gpt-5.5",
		BaseURL:      "https://api.compat.example/",
		Reasoning:    "medium",
		Sandbox:      "danger-full-access",
		Approval:     "ask",
		MaxContext:   60000,
		MaxOutput:    40000,
		SessionID:    "20260528-142542",
	})
	model.width = 120
	model.height = 30
	model.syncViewport(true)

	if got, want := lipgloss.Width(model.renderSidebar(model.viewport.Height)), model.sidebarWidth(); got != want {
		t.Fatalf("sidebar rendered width mismatch: got %d want %d", got, want)
	}

	for i, line := range model.buildFormattedLines() {
		if width := lipgloss.Width(line); width > model.chatWidth() {
			t.Fatalf("viewport content line %d overflows chat width: got %d want <= %d\n%s", i, width, model.chatWidth(), stripANSI(line))
		}
	}

	view := model.View()
	if got := lipgloss.Height(view); got != model.height {
		t.Fatalf("view height mismatch with sidebar: got %d want %d", got, model.height)
	}
	for i, line := range strings.Split(view, "\n") {
		if width := lipgloss.Width(line); width > model.width {
			t.Fatalf("line %d overflows terminal width: got %d want <= %d\n%s", i, width, model.width, stripANSI(line))
		}
	}
}

func TestFooterFitsNarrowTerminalWidth(t *testing.T) {
	model := New(Config{
		Model:        "gpt-5.5-super-long-model-name",
		SidebarFiles: []string{"README.md"},
		MaxContext:   60000,
	})
	model.width = 72
	model.height = 20
	model.copyNotice = "copied 12345 chars"
	model.syncViewport(true)

	footer := model.renderFooter()
	for i, line := range strings.Split(footer, "\n") {
		visible := strings.TrimRight(stripANSI(line), " ")
		if width := lipgloss.Width(visible); width > model.width {
			t.Fatalf("footer line %d overflows width: got %d want <= %d\n%s", i, width, model.width, visible)
		}
	}
}

func TestFooterShowsCurrentContextAndOmitsHelpHint(t *testing.T) {
	model := New(Config{
		MaxContext:   60000,
		InitialLines: []string{"stats: duration=2.0s input=1250 cached=250 output=500 reasoning=100 total=2000"},
	})
	model.width = 140
	model.height = 20
	model.syncViewport(true)
	footer := stripANSI(model.renderFooter())
	if strings.Contains(footer, "/help") {
		t.Fatalf("footer should not include /help:\n%s", footer)
	}
	if !strings.Contains(footer, "ctx 1,250/60,000") {
		t.Fatalf("footer should show current context tokens:\n%s", footer)
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
	if !strings.Contains(view, "read_file") || !strings.Contains(view, "done") {
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

func maxRenderedLineWidth(value string) int {
	maxWidth := 0
	for _, line := range strings.Split(stripANSI(value), "\n") {
		if width := lipgloss.Width(strings.TrimRight(line, " ")); width > maxWidth {
			maxWidth = width
		}
	}
	return maxWidth
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
	for idx, f := range m.settingsModal.Fields {
		if f.Name == "provider" {
			m.settingsModal.Fields[idx].Value = "openai"
			break
		}
	}
	// Save directly
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
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

func TestSettingsModalPersistsCustomProviderKey(t *testing.T) {
	tempDir := t.TempDir()
	const envVar = "KLYRA_PROVIDER_CUSTOM_OPENAI_API_KEY"
	original := os.Getenv(envVar)
	_ = os.Unsetenv(envVar)
	defer func() {
		if original == "" {
			_ = os.Unsetenv(envVar)
		} else {
			_ = os.Setenv(envVar, original)
		}
	}()

	model := New(Config{
		CWD: tempDir,
		Handler: func(input string) (string, error) {
			return "saved", nil
		},
	})
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyF2})
	m := updated.(Model)
	if m.settingsModal == nil {
		t.Fatal("expected settings modal")
	}
	for idx, f := range m.settingsModal.Fields {
		switch f.Name {
		case "provider":
			m.settingsModal.Fields[idx].Value = "custom-openai"
		case "endpoint":
			m.settingsModal.Fields[idx].Value = "https://api.example.test/v1"
		case "api_mode":
			m.settingsModal.Fields[idx].Value = "responses"
		case "provider_key":
			m.settingsModal.Fields[idx].Value = "test-secret-custom"
		}
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	_ = executeCmd(cmd)
	m = updated.(Model)

	if os.Getenv(envVar) != "test-secret-custom" {
		t.Fatalf("expected %s to be set, got %q", envVar, os.Getenv(envVar))
	}
	if custom := m.customProviders["custom-openai"]; custom.BaseURL != "https://api.example.test/v1" || custom.APIType != "responses" {
		t.Fatalf("expected custom provider to persist, got %+v", custom)
	}

	data, err := os.ReadFile(filepath.Join(tempDir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), envVar+"=\"test-secret-custom\"") {
		t.Fatalf("expected custom provider key in .env, got:\n%s", string(data))
	}
}

func TestSettingsModalAppliesRuntimeSettingsForContextFilesAndMCP(t *testing.T) {
	enabled := true
	var applied RuntimeSettings
	model := New(Config{
		ContextFiles: []string{"README.md"},
		MCPServers: map[string]appconfig.MCPServer{
			"github": {Command: "github-mcp", Enabled: &enabled},
		},
		ApplySettings: func(settings RuntimeSettings) error {
			applied = settings
			return nil
		},
	})

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyF2})
	m := updated.(Model)
	if m.settingsModal == nil {
		t.Fatal("expected settings modal")
	}
	for idx, f := range m.settingsModal.Fields {
		switch f.Name {
		case "context_files":
			m.settingsModal.Fields[idx].Value = "README.md | pkg/tui/model.go | pkg/tui/settings_modal.go"
		case "mcp_enabled_github":
			m.settingsModal.Fields[idx].Value = "off"
		}
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	_ = executeCmd(cmd)
	m = updated.(Model)

	if got, want := m.cartCount, 3; got != want {
		t.Fatalf("unexpected cart count: got %d want %d", got, want)
	}
	if !reflect.DeepEqual(m.contextFiles, []string{"README.md", "pkg/tui/model.go", "pkg/tui/settings_modal.go"}) {
		t.Fatalf("unexpected context files in model: %#v", m.contextFiles)
	}
	if !reflect.DeepEqual(applied.ContextFiles, m.contextFiles) {
		t.Fatalf("applySettings did not receive context files: %#v", applied.ContextFiles)
	}
	server, ok := applied.MCPServers["github"]
	if !ok || server.Enabled == nil || *server.Enabled {
		t.Fatalf("applySettings did not receive disabled MCP server: %#v", applied.MCPServers)
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

func TestRenderModalFrameClampsWrappedContentToViewport(t *testing.T) {
	content := strings.Repeat("https://very-long-hostname.example.com/some/really/long/path/without/breaks ", 8)
	content = strings.TrimSpace(content) + "\n" + strings.Repeat("second line ", 12)
	box := renderModalFrame(72, 12, 80, 80, 48, 90, colorBrand, content)
	if got, maxHeight := lipgloss.Height(box), 10; got > maxHeight {
		t.Fatalf("modal frame height overflowed: got %d want <= %d\n%s", got, maxHeight, stripANSI(box))
	}
	if got := maxRenderedLineWidth(box); got > 72 {
		t.Fatalf("modal frame width overflowed: got %d want <= 72\n%s", got, stripANSI(box))
	}
}

func TestSettingsModalViewDoesNotOverflowWithLongValues(t *testing.T) {
	enabled := true
	sm := NewSettingsModal(
		"local",
		"claude-haiku-qwen-35b",
		"http://10.171.251.1:1234/v1",
		"chat_completions",
		"OPENAI_API_KEY",
		"high",
		"always",
		"danger-full-access",
		"edit",
		true,
		true,
		map[string]string{
			"openai":    "https://api.deepseek.com",
			"local":     "http://10.171.251.1:1234/v1",
			"ollama":    "https://api.deepseek.com",
			"anthropic": "https://cc.freemodel.dev",
			"gemini":    "",
		},
		128000, 60000, 40, 80, 24000,
		true, true,
		1200, 60, 10, true,
		true, 1000, 10, true, true,
		true, true, true,
		"", "", "",
		[]string{"README.md", "pkg/tui/model.go", "pkg/tui/settings_modal.go", "cmd/klyra/root.go"},
		map[string]appconfig.MCPServer{
			"github": {Command: "github-mcp", Enabled: &enabled},
		},
	)
	for section := secProvider; section <= secIntegrations; section++ {
		sm.setExpanded(section, true)
	}
	view := sm.View(100, 18)
	if got, maxHeight := lipgloss.Height(view), 16; got > maxHeight {
		t.Fatalf("settings modal overflowed viewport: got %d want <= %d\n%s", got, maxHeight, stripANSI(view))
	}
	if got := maxRenderedLineWidth(view); got > 100 {
		t.Fatalf("settings modal width overflowed: got %d want <= 100\n%s", got, stripANSI(view))
	}
}

func TestApprovalPromptUsesKeys(t *testing.T) {
	reply := make(chan bool, 1)
	model := New(Config{})
	updated, _ := model.Update(ApprovalRequestMsg{
		Tool: "bash",
		Args: map[string]any{
			"command": "go test ./...",
		},
		Reply: reply,
	})
	view := updated.(Model).View()
	if !strings.Contains(view, "Approval required") || !strings.Contains(view, "go test ./...") {
		t.Fatalf("approval prompt not rendered:\n%s", view)
	}
	updated, _ = updated.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if !<-reply {
		t.Fatal("expected approval")
	}
}

func TestApprovalPromptRedactsSecrets(t *testing.T) {
	model := New(Config{})
	updated, _ := model.Update(ApprovalRequestMsg{
		Tool: "fetch_url",
		Args: map[string]any{
			"url":     "https://example.test",
			"api_key": "super-secret-value",
		},
		Reply: make(chan bool, 1),
	})
	view := updated.(Model).View()
	if !strings.Contains(view, "https://example.test") || !strings.Contains(view, "<redacted>") {
		t.Fatalf("approval prompt should show safe args and redaction:\n%s", view)
	}
	if strings.Contains(view, "super-secret-value") {
		t.Fatalf("approval prompt leaked secret:\n%s", view)
	}
}

func TestApprovalPromptRingsBell(t *testing.T) {
	model := New(Config{})
	var bell bytes.Buffer
	prev := bellWriter
	bellWriter = &bell
	defer func() { bellWriter = prev }()

	_, cmd := model.Update(ApprovalRequestMsg{
		Tool:  "bash",
		Args:  map[string]any{"command": "go test ./..."},
		Reply: make(chan bool, 1),
	})
	executeCmd(cmd)

	if bell.String() != "\a" {
		t.Fatalf("expected approval bell, got %q", bell.String())
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

func TestReasoningOnlyStreamBecomesAnswerOnCompletion(t *testing.T) {
	model := New(Config{})
	model.busy = true
	updated, _ := model.Update(ReasoningMsg("answer emitted as reasoning"))
	m := updated.(Model)
	updated, _ = m.Update(responseMsg{input: "hello", output: "", agentRun: true})
	m = updated.(Model)
	if modelLinesContain(m.lines, "thoughts:0:", "answer emitted as reasoning") {
		t.Fatalf("reasoning-only answer should not remain only in thoughts: %#v", m.lines)
	}
	if !modelLinesContain(m.lines, "agent:", "answer emitted as reasoning") {
		t.Fatalf("expected reasoning-only stream to be shown as answer: %#v", m.lines)
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
	if !strings.Contains(view, "read_file") || !strings.Contains(view, "running") {
		t.Fatalf("tool progress missing from view:\n%s", view)
	}
	if strings.Contains(view, "details collapsed") || strings.Contains(view, "running tool") {
		t.Fatalf("tool progress should be minimal:\n%s", view)
	}
}

func TestToolProgressFormatsCreateFileContentAsCodeBlock(t *testing.T) {
	model := New(Config{})
	raw, err := json.Marshal(ToolProgressMsg{
		Phase: "running",
		Tool:  "create_file",
		Args: map[string]any{
			"path":    "main.go",
			"content": "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := model.renderToolProgressLine("toolprogress:1:" + string(raw))
	view := stripANSI(strings.Join(lines, "\n"))
	if strings.Contains(view, `\n`) {
		t.Fatalf("expected formatted multiline content instead of escaped newlines:\n%s", view)
	}
	if !strings.Contains(view, "content:") || !strings.Contains(view, "package main") || !strings.Contains(view, "func main()") {
		t.Fatalf("expected code block style preview for create_file args:\n%s", view)
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
	if strings.Count(view, "planned") != 1 {
		t.Fatalf("tool stream should render as one collapsed block:\n%s", view)
	}
	if strings.Contains(view, "model is preparing tool call") || strings.Contains(view, "details collapsed") {
		t.Fatalf("tool stream should be minimal:\n%s", view)
	}
	if strings.Contains(view, "│") {
		t.Fatalf("tool stream details should be collapsed by default:\n%s", view)
	}
	if !strings.Contains(view, "README.md") {
		t.Fatalf("tool stream summary should include compact args:\n%s", view)
	}
}

func TestToolProgressCollapsesLifecycleIntoOneLine(t *testing.T) {
	model := New(Config{})
	updated, _ := model.Update(ToolStreamMsg{
		Index:     0,
		ID:        "call-1",
		Name:      "read_file",
		Arguments: `{"path":"README.md"}`,
	})
	m := updated.(Model)

	updates := []ToolProgressMsg{
		{Phase: "queued", Tool: "read_file", ID: "call-1"},
		{Phase: "running", Tool: "read_file", ID: "call-1"},
		{Phase: "done", Tool: "read_file", ID: "call-1", Output: "1 line"},
	}
	for _, progress := range updates {
		updated, _ = m.Update(progress)
		m = updated.(Model)
	}

	count := 0
	for _, line := range m.lines {
		if strings.HasPrefix(line, "toolstream:") || strings.HasPrefix(line, "toolprogress:") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected one merged tool lifecycle line, got %d: %#v", count, m.lines)
	}
	if !strings.HasPrefix(m.lines[len(m.lines)-1], "toolprogress:0:") {
		t.Fatalf("expected final merged line to be toolprogress, got %#v", m.lines)
	}

	view := stripANSI(m.View())
	if strings.Count(view, "read_file") != 1 {
		t.Fatalf("tool lifecycle should render once:\n%s", view)
	}
	if !strings.Contains(view, "done") {
		t.Fatalf("merged tool progress should keep final phase:\n%s", view)
	}

	_, raw := parseCollapsiblePayload(m.lines[len(m.lines)-1], "toolprogress")
	var merged ToolProgressMsg
	if err := json.Unmarshal([]byte(raw), &merged); err != nil {
		t.Fatalf("failed to decode merged payload: %v", err)
	}
	if merged.Args["path"] != "README.md" {
		t.Fatalf("merged tool progress should keep args, got %#v", merged.Args)
	}
}

func TestToolStreamFormatsPartialCodeWhileStreaming(t *testing.T) {
	model := New(Config{})
	raw, err := json.Marshal(ToolStreamMsg{
		Index:     0,
		ID:        "call-1",
		Name:      "create_file",
		Arguments: "{\"path\":\"main.go\",\"content\":\"package main\\n\\nfunc ma",
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := model.renderToolStreamLine("toolstream:1:" + string(raw))
	view := stripANSI(strings.Join(lines, "\n"))
	if strings.Contains(view, `\n`) {
		t.Fatalf("expected partial streamed code to be expanded, not shown as escaped json:\n%s", view)
	}
	if !strings.Contains(view, "content:") || !strings.Contains(view, "package main") || !strings.Contains(view, "func ma") {
		t.Fatalf("expected structured preview for partial streamed args:\n%s", view)
	}
}

func TestFormatApprovalArgsExpandsCodeFields(t *testing.T) {
	preview := formatApprovalArgs(map[string]any{
		"path":    "script.py",
		"content": "print('a')\nprint('b')",
	}, 80, 20)
	if strings.Contains(preview, `\n`) {
		t.Fatalf("expected approval preview to expand newlines:\n%s", preview)
	}
	if !strings.Contains(preview, "content:") || !strings.Contains(preview, "```python") || !strings.Contains(preview, "print('a')") {
		t.Fatalf("expected approval preview to include fenced code block:\n%s", preview)
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

func TestTextareaHistoryRoutingOnlyAtInputEdges(t *testing.T) {
	model := New(Config{})
	model.history = []string{"older command"}
	model.historyIdx = len(model.history)

	model.input.SetValue("first line\nsecond line\nthird line")
	model.syncInputSize()

	model.input.CursorUp()
	if model.shouldRouteUpToHistory() {
		t.Fatalf("up should stay inside textarea when cursor is not on the first line")
	}
	if model.shouldRouteDownToHistory() {
		t.Fatalf("down should stay inside textarea when cursor is not on the last line")
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyUp})
	m := updated.(Model)
	if m.input.Value() != "first line\nsecond line\nthird line" {
		t.Fatalf("textarea navigation should not replace multiline input, got %q", m.input.Value())
	}
	if m.input.Line() != 0 {
		t.Fatalf("expected cursor to move to first line, got line %d", m.input.Line())
	}
	if !m.shouldRouteUpToHistory() {
		t.Fatalf("up should route to history on the first line")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)
	if m.input.Value() != "older command" {
		t.Fatalf("expected history entry on up from first line, got %q", m.input.Value())
	}

	model = New(Config{})
	model.history = []string{"older command"}
	model.historyIdx = len(model.history)
	model.input.SetValue("first line\nsecond line\nthird line")
	model.syncInputSize()
	model.input.CursorUp()
	if model.shouldRouteDownToHistory() {
		t.Fatalf("down should stay inside textarea when cursor is not on the last line")
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.input.Value() != "first line\nsecond line\nthird line" {
		t.Fatalf("textarea navigation should preserve multiline input on down, got %q", m.input.Value())
	}
	if m.input.Line() != 2 {
		t.Fatalf("expected cursor to move to last line, got line %d", m.input.Line())
	}
	if !m.shouldRouteDownToHistory() {
		t.Fatalf("down should route to history on the last line")
	}
}

func TestTextareaSoftWrapExpandsHeightAndCapsAtFourLines(t *testing.T) {
	model := New(Config{})
	model.width = 26
	model.height = 24
	model.input.SetValue(strings.Repeat("wrap ", 20))
	model.syncInputSize()

	if model.input.LineCount() != 1 {
		t.Fatalf("expected one hard line, got %d", model.input.LineCount())
	}
	if got := model.input.Height(); got <= 1 {
		t.Fatalf("expected soft wrap to increase textarea height, got %d", got)
	}
	if got := model.input.Height(); got != 4 {
		t.Fatalf("expected textarea height cap at 4 lines, got %d", got)
	}
	if got := model.inputVisualLineCount(); got != 4 {
		t.Fatalf("expected visual line count cap at 4, got %d", got)
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
	if !m.busy {
		t.Fatal("expected run to remain busy until cancellation is observed")
	}
	if !m.interrupting {
		t.Fatal("expected interrupting state")
	}
	if !isClearScreenCmd(cmd) {
		t.Fatal("expected interrupt to force a clean repaint")
	}
	if !modelLinesContain(m.lines, "interrupting", "current", "run") {
		t.Fatalf("expected interrupt status line: %#v", m.lines)
	}
}

func TestInterruptedResponseKeepsPartialStreamAndSuppressesCancelError(t *testing.T) {
	model := New(Config{})
	model.interrupting = true
	model.busy = true
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
	if m.interrupted || m.interrupting {
		t.Fatal("interrupt state should be cleared after canceled response")
	}
	if m.busy {
		t.Fatal("busy state should clear after canceled response")
	}
	if !modelLinesContain(m.lines, "agent:", "partial answer") {
		t.Fatalf("partial stream should persist after interrupt: %#v", m.lines)
	}
	if modelLinesContain(m.lines, "error:", "context canceled") {
		t.Fatalf("context canceled should not be rendered as an error: %#v", m.lines)
	}
}

func TestInterruptingRunIgnoresLateStreamDeltas(t *testing.T) {
	model := New(Config{})
	model.busy = true
	model.interrupting = true
	model.streamBuf = "partial"
	model.reasoningText = "thinking"

	updated, _ := model.Update(StreamMsg(" should be ignored"))
	m := updated.(Model)
	if m.streamBuf != "partial" {
		t.Fatalf("stream delta should be ignored while interrupting, got %q", m.streamBuf)
	}

	updated, _ = m.Update(ReasoningMsg(" more"))
	m = updated.(Model)
	if m.reasoningText != "thinking" {
		t.Fatalf("reasoning delta should be ignored while interrupting, got %q", m.reasoningText)
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
		t.Fatal("expected mouse mode command")
	}
	if got := fmt.Sprintf("%T", executeCmd(cmd)); !strings.Contains(got, "enableMouseAllMotionMsg") {
		t.Fatalf("expected EnableMouseAllMotion command, got %s", got)
	}

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyF6})
	m = updated.(Model)
	if m.copyMode {
		t.Fatal("expected scroll mode after second F6")
	}
	if cmd == nil {
		t.Fatal("expected EnableMouseCellMotion command")
	}
	if got := fmt.Sprintf("%T", executeCmd(cmd)); !strings.Contains(got, "enableMouseCellMotionMsg") {
		t.Fatalf("expected EnableMouseCellMotion command, got %s", got)
	}
}

func TestCopyModeDragCopiesSelectionToClipboard(t *testing.T) {
	model := New(Config{})
	model.width = 80
	model.height = 24
	model.lines = []string{"system: alpha", "system: bravo", "system: charlie"}
	model.syncViewport(true)

	var copied string
	origWriteClipboard := writeClipboard
	writeClipboard = func(text string) error {
		copied = text
		return nil
	}
	defer func() { writeClipboard = origWriteClipboard }()

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyF6})
	m := updated.(Model)
	if !m.copyMode {
		t.Fatal("expected copy mode to be enabled")
	}

	updated, _ = m.Update(tea.MouseMsg{
		X:      4,
		Y:      m.viewport.Height - 3,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
		Type:   tea.MouseLeft,
	})
	m = updated.(Model)

	updated, _ = m.Update(tea.MouseMsg{
		X:      8,
		Y:      m.viewport.Height - 2,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionMotion,
		Type:   tea.MouseMotion,
	})
	m = updated.(Model)

	updated, cmd := m.Update(tea.MouseMsg{
		X:      8,
		Y:      m.viewport.Height - 2,
		Button: tea.MouseButtonNone,
		Action: tea.MouseActionRelease,
		Type:   tea.MouseRelease,
	})
	m = updated.(Model)
	if !m.copySelectionActive {
		t.Fatal("expected copy selection to remain active after drag release")
	}
	msg := executeCmd(cmd)
	copyMsg, ok := msg.(copyResultMsg)
	if !ok {
		t.Fatalf("expected copy result message, got %T", msg)
	}
	updated, _ = m.Update(copyMsg)
	m = updated.(Model)
	if copied == "" {
		t.Fatal("expected clipboard write after selection release")
	}
	if !strings.Contains(copied, "alpha") || !strings.Contains(copied, "brav") {
		t.Fatalf("expected multiline copied selection, got %q", copied)
	}
	if !strings.Contains(stripANSI(m.renderFooter()), "copied") {
		t.Fatalf("expected copy notification in footer, got %q", stripANSI(m.renderFooter()))
	}
}

func TestSidebarScrollDownUsesViewportHeightAwareLimit(t *testing.T) {
	model := New(Config{
		SidebarFiles: []string{
			"a.go", "b.go", "c.go", "d.go", "e.go", "f.go", "g.go", "h.go", "i.go", "j.go",
			"k.go", "l.go", "m.go", "n.go", "o.go", "p.go", "q.go", "r.go", "s.go", "t.go",
		},
	})
	model.width = 120
	model.height = 16
	model.syncViewport(true)

	for i := 0; i < 50; i++ {
		model.sidebarScrollDown(1)
	}

	maxScroll := model.sidebarMaxScrollForContent(model.viewport.Height, model.sidebarContentLineCount(), 3, 2)
	if model.sidebarScroll != maxScroll {
		t.Fatalf("unexpected sidebar max scroll: got %d want %d", model.sidebarScroll, maxScroll)
	}
}

func TestRenderCopySelectionLineStripsANSISequences(t *testing.T) {
	model := New(Config{})
	model.copySelectionActive = true
	model.copySelectionStart = copyPoint{index: 0, col: 0}
	model.copySelectionEnd = copyPoint{index: 0, col: 6}
	line := "\x1b[1;38;2;96;165;250m>Create\x1b[0m also"
	rendered := model.renderCopySelectionLine(line, 0)
	plain := stripANSI(rendered)
	if strings.Contains(plain, "38;2;96;165;250m") || strings.Contains(plain, "1;38;2;96;165;250m") {
		t.Fatalf("expected ANSI parameters to stay invisible, got %q", plain)
	}
	if !strings.Contains(plain, ">Create also") {
		t.Fatalf("expected visible text to remain, got %q", plain)
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

func TestResponseStatsAndTokens(t *testing.T) {
	model := New(Config{})
	model.requestStartTime = time.Now().Add(-2 * time.Second)

	// Simulate responseMsg with [TokenUsage]
	updated, _ := model.Update(responseMsg{
		input:    "hello",
		output:   "response text\n\n[TokenUsage] input=1250 cached=250 output=500 reasoning=100 total=2000",
		agentRun: true,
	})
	m := updated.(Model)

	// Verify stats line is appended
	foundStats := false
	var statsLine string
	for _, line := range m.lines {
		if strings.HasPrefix(line, "stats: ") {
			foundStats = true
			statsLine = line
			break
		}
	}

	if !foundStats {
		t.Fatal("expected stats line to be appended to m.lines")
	}
	if m.currentContextTokens != 1250 || m.currentCachedTokens != 250 {
		t.Fatalf("expected usage state to update, got input=%d cached=%d", m.currentContextTokens, m.currentCachedTokens)
	}

	// Verify the parsed values in the stats line
	if !strings.Contains(statsLine, "input=1250") || !strings.Contains(statsLine, "cached=250") || !strings.Contains(statsLine, "output=500") || !strings.Contains(statsLine, "reasoning=100") {
		t.Fatalf("unexpected stats line content: %q", statsLine)
	}

	// Verify that the formatted lines include the formatted badges
	formattedLines := m.buildFormattedLines()
	hasStatsRendered := false
	for _, fl := range formattedLines {
		if strings.Contains(fl, "1,250 ctx tokens") && strings.Contains(fl, "250 cached") && strings.Contains(fl, "500 out tokens") && strings.Contains(fl, "100 reasoning") {
			hasStatsRendered = true
			break
		}
	}

	if !hasStatsRendered {
		t.Fatalf("expected formatted stats block, got lines:\n%s", strings.Join(formattedLines, "\n"))
	}
}

func TestResponseCompletionRingsBell(t *testing.T) {
	model := New(Config{})
	var bell bytes.Buffer
	prev := bellWriter
	bellWriter = &bell
	defer func() { bellWriter = prev }()

	_, cmd := model.Update(responseMsg{
		input:    "hello",
		output:   "done",
		agentRun: true,
	})
	executeCmd(cmd)

	if bell.String() != "\a" {
		t.Fatalf("expected completion bell, got %q", bell.String())
	}
}
