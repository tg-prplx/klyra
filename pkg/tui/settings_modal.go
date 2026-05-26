package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Settings Modal — full-featured settings editor
// ---------------------------------------------------------------------------

type settingsSection int

const (
	secProvider settingsSection = iota
	secAPIKeys
	secSafety
	secLimits
	secRouting
)

var sectionNames = []string{"PROVIDER", "API KEYS", "SAFETY", "LIMITS", "ROUTING"}

// settingsField represents one editable field in the settings modal.
type settingsField struct {
	Section     settingsSection
	Name        string
	DisplayName string
	Value       string
	Choices     []string // if non-empty, cycle with ←/→
	Masked      bool     // for API keys
	EnvVar      string   // show env var fallback status
	ReadOnly    bool     // e.g. env-sourced values
}

// SettingsModal is the full settings editor overlay.
type SettingsModal struct {
	Fields      []settingsField
	Cursor      int
	EditBuffer  string // for text fields being edited
	Editing     bool   // true when actively typing into a text field
	Width       int
	Scroll      int
	MaxVisible  int
}

// NewSettingsModal creates a settings modal pre-populated with current values.
func NewSettingsModal(
	provider, model, endpoint, reasoning, approval, sandbox, mode string,
	maxContext, maxOutput, maxSteps, maxMessages, maxInstructions int,
	fastModel, editModel, deepModel string,
) SettingsModal {
	fields := []settingsField{
		// Provider section
		{Section: secProvider, Name: "provider", DisplayName: "Provider", Value: provider, Choices: []string{"mock", "openai", "local", "ollama", "anthropic", "gemini"}},
		{Section: secProvider, Name: "model", DisplayName: "Model", Value: model},
		{Section: secProvider, Name: "endpoint", DisplayName: "Endpoint", Value: endpoint},
		{Section: secProvider, Name: "reasoning", DisplayName: "Reasoning", Value: reasoning, Choices: []string{"", "minimal", "low", "medium", "high"}},

		// API Keys section
		{Section: secAPIKeys, Name: "openai_key", DisplayName: "OpenAI Key", Value: "", EnvVar: "OPENAI_API_KEY", Masked: true},
		{Section: secAPIKeys, Name: "anthropic_key", DisplayName: "Anthropic Key", Value: "", EnvVar: "ANTHROPIC_API_KEY", Masked: true},
		{Section: secAPIKeys, Name: "gemini_key", DisplayName: "Gemini Key", Value: "", EnvVar: "GEMINI_API_KEY", Masked: true},

		// Safety section
		{Section: secSafety, Name: "approval", DisplayName: "Approval", Value: approval, Choices: []string{"auto", "ask", "never"}},
		{Section: secSafety, Name: "sandbox", DisplayName: "Sandbox", Value: sandbox, Choices: []string{"read-only", "workspace-write", "danger-full-access"}},
		{Section: secSafety, Name: "mode", DisplayName: "Mode", Value: mode, Choices: []string{"inspect", "edit", "repair", "refactor"}},

		// Limits section
		{Section: secLimits, Name: "context", DisplayName: "Context Tokens", Value: fmt.Sprintf("%d", maxContext)},
		{Section: secLimits, Name: "output", DisplayName: "Output Tokens", Value: fmt.Sprintf("%d", maxOutput)},
		{Section: secLimits, Name: "steps", DisplayName: "Max Steps", Value: fmt.Sprintf("%d", maxSteps)},
		{Section: secLimits, Name: "messages", DisplayName: "Max Messages", Value: fmt.Sprintf("%d", maxMessages)},
		{Section: secLimits, Name: "instructions", DisplayName: "Instruction Bytes", Value: fmt.Sprintf("%d", maxInstructions)},

		// Routing section
		{Section: secRouting, Name: "fast_model", DisplayName: "Fast Model", Value: fastModel},
		{Section: secRouting, Name: "edit_model", DisplayName: "Edit Model", Value: editModel},
		{Section: secRouting, Name: "deep_model", DisplayName: "Deep Model", Value: deepModel},
	}

	return SettingsModal{
		Fields:     fields,
		Cursor:     0,
		Width:      60,
		MaxVisible: 24,
	}
}

func (s *SettingsModal) MoveUp() {
	if s.Editing {
		return
	}
	s.Cursor--
	if s.Cursor < 0 {
		s.Cursor = len(s.Fields) - 1
	}
	s.adjustScroll()
}

func (s *SettingsModal) MoveDown() {
	if s.Editing {
		return
	}
	s.Cursor++
	if s.Cursor >= len(s.Fields) {
		s.Cursor = 0
	}
	s.adjustScroll()
}

func (s *SettingsModal) CycleLeft() {
	f := &s.Fields[s.Cursor]
	if len(f.Choices) == 0 || f.ReadOnly {
		return
	}
	idx := 0
	for i, c := range f.Choices {
		if c == f.Value {
			idx = i
			break
		}
	}
	idx = (idx - 1 + len(f.Choices)) % len(f.Choices)
	f.Value = f.Choices[idx]
}

func (s *SettingsModal) CycleRight() {
	f := &s.Fields[s.Cursor]
	if len(f.Choices) == 0 || f.ReadOnly {
		return
	}
	idx := 0
	for i, c := range f.Choices {
		if c == f.Value {
			idx = i
			break
		}
	}
	idx = (idx + 1) % len(f.Choices)
	f.Value = f.Choices[idx]
}

// StartEdit begins editing a text field.
func (s *SettingsModal) StartEdit() {
	f := &s.Fields[s.Cursor]
	if len(f.Choices) > 0 || f.ReadOnly {
		return
	}
	s.Editing = true
	s.EditBuffer = f.Value
}

// TypeChar adds a character to the edit buffer.
func (s *SettingsModal) TypeChar(ch string) {
	if !s.Editing {
		// Auto-start edit for text fields
		f := &s.Fields[s.Cursor]
		if len(f.Choices) > 0 || f.ReadOnly {
			return
		}
		s.Editing = true
		s.EditBuffer = f.Value
	}
	s.EditBuffer += ch
	s.Fields[s.Cursor].Value = s.EditBuffer
}

// Backspace removes the last character from the edit buffer.
func (s *SettingsModal) Backspace() {
	if !s.Editing {
		f := &s.Fields[s.Cursor]
		if len(f.Choices) > 0 || f.ReadOnly {
			return
		}
		s.Editing = true
		s.EditBuffer = f.Value
	}
	if len(s.EditBuffer) > 0 {
		s.EditBuffer = s.EditBuffer[:len(s.EditBuffer)-1]
	}
	s.Fields[s.Cursor].Value = s.EditBuffer
}

// CommitEdit finishes editing and commits the value.
func (s *SettingsModal) CommitEdit() {
	if s.Editing {
		s.Fields[s.Cursor].Value = s.EditBuffer
		s.Editing = false
	}
}

func (s *SettingsModal) adjustScroll() {
	// Calculate line index for current cursor (accounting for section headers)
	lineIdx := s.cursorLineIndex()
	if lineIdx < s.Scroll {
		s.Scroll = lineIdx
	}
	if lineIdx >= s.Scroll+s.MaxVisible {
		s.Scroll = lineIdx - s.MaxVisible + 1
	}
}

func (s *SettingsModal) cursorLineIndex() int {
	line := 0
	lastSection := settingsSection(-1)
	for i := 0; i <= s.Cursor && i < len(s.Fields); i++ {
		if s.Fields[i].Section != lastSection {
			line += 2 // section header + blank line
			lastSection = s.Fields[i].Section
		}
		if i == s.Cursor {
			return line
		}
		line++
	}
	return line
}

// GetValue returns the current value of a field by name.
func (s *SettingsModal) GetValue(name string) string {
	for _, f := range s.Fields {
		if f.Name == name {
			return f.Value
		}
	}
	return ""
}

// View renders the settings modal.
func (s SettingsModal) View(termWidth, termHeight int) string {
	sectionStyle := lipgloss.NewStyle().
		Foreground(colorAmber).
		Bold(true)

	activeLabel := lipgloss.NewStyle().
		Foreground(colorBrand).
		Bold(true)

	normalLabel := lipgloss.NewStyle().
		Foreground(colorText)

	valueStyle := lipgloss.NewStyle().
		Foreground(colorWhite)

	choiceActiveStyle := lipgloss.NewStyle().
		Foreground(colorWhite).
		Background(colorBrandDim).
		Bold(true).
		Padding(0, 1)

	envBadge := lipgloss.NewStyle().
		Foreground(colorEmerald).
		Italic(true)

	envMissBadge := lipgloss.NewStyle().
		Foreground(colorDim).
		Italic(true)

	maskedStyle := lipgloss.NewStyle().
		Foreground(colorDim)

	editStyle := lipgloss.NewStyle().
		Foreground(colorWhite).
		Background(colorInputBg).
		Padding(0, 1)

	headerStyle := lipgloss.NewStyle().
		Foreground(colorBrand).
		Bold(true)

	hintKeyStyle := lipgloss.NewStyle().Foreground(colorMuted)
	hintTextStyle := lipgloss.NewStyle().Foreground(colorDim)

	var allLines []string
	allLines = append(allLines, headerStyle.Render("⚙  Settings"))
	allLines = append(allLines, "")

	lastSection := settingsSection(-1)
	lineToField := map[int]int{} // track line->field mapping for scroll

	for i, f := range s.Fields {
		if f.Section != lastSection {
			if lastSection >= 0 {
				allLines = append(allLines, "")
			}
			secIdx := int(f.Section)
			if secIdx < len(sectionNames) {
				allLines = append(allLines, sectionStyle.Render("  "+sectionNames[secIdx]))
			}
			lastSection = f.Section
		}

		lineToField[len(allLines)] = i

		isActive := i == s.Cursor
		marker := "  "
		lblStyle := normalLabel
		if isActive {
			marker = "▸ "
			lblStyle = activeLabel
		}

		label := lblStyle.Render(fmt.Sprintf("%s%-18s", marker, f.DisplayName+":"))

		var val string
		if len(f.Choices) > 0 {
			// Inline chooser
			displayVal := f.Value
			if displayVal == "" {
				displayVal = "default"
			}
			if isActive {
				val = "◀ " + choiceActiveStyle.Render(displayVal) + " ▶"
			} else {
				val = valueStyle.Render(displayVal)
			}
		} else if f.Masked {
			// API key field
			if s.Editing && isActive {
				display := s.EditBuffer
				if len(display) > 20 {
					display = display[:4] + "..." + display[len(display)-4:]
				}
				val = editStyle.Render(display + "█")
			} else {
				envVal := ""
				if f.EnvVar != "" {
					envVal = os.Getenv(f.EnvVar)
				}
				if f.Value != "" {
					masked := maskKey(f.Value)
					val = maskedStyle.Render(masked) + " " + envBadge.Render("✓")
				} else if envVal != "" {
					masked := maskKey(envVal)
					val = maskedStyle.Render(masked) + " " + envBadge.Render("(env)")
				} else {
					val = envMissBadge.Render("(not set)")
				}
			}
		} else {
			// Text field
			if s.Editing && isActive {
				val = editStyle.Render(s.EditBuffer + "█")
			} else {
				displayVal := f.Value
				if displayVal == "" {
					displayVal = lipgloss.NewStyle().Foreground(colorMuted).Render("(default)")
				} else {
					displayVal = valueStyle.Render(displayVal)
				}
				val = displayVal
			}
		}

		allLines = append(allLines, label+" "+val)
	}

	allLines = append(allLines, "")

	if s.Editing {
		allLines = append(allLines,
			hintKeyStyle.Render("Enter")+hintTextStyle.Render(" confirm field  ")+
				hintKeyStyle.Render("Esc")+hintTextStyle.Render(" cancel edit"))
	} else {
		allLines = append(allLines,
			hintKeyStyle.Render("↑/↓")+hintTextStyle.Render(" navigate  ")+
				hintKeyStyle.Render("◀/▶")+hintTextStyle.Render(" choose  ")+
				hintTextStyle.Render("type to edit  "))
		allLines = append(allLines,
			hintKeyStyle.Render("Enter")+hintTextStyle.Render(" save all  ")+
				hintKeyStyle.Render("Esc")+hintTextStyle.Render(" close  ")+
				hintKeyStyle.Render("Tab")+hintTextStyle.Render(" next field"))
	}

	// Apply scrolling
	visibleMax := s.MaxVisible
	if termHeight > 0 {
		visibleMax = termHeight - 6
		if visibleMax < 12 {
			visibleMax = 12
		}
	}

	visibleLines := allLines
	if len(allLines) > visibleMax {
		end := s.Scroll + visibleMax
		if end > len(allLines) {
			end = len(allLines)
		}
		start := s.Scroll
		if start >= len(allLines) {
			start = len(allLines) - 1
		}
		if start < 0 {
			start = 0
		}
		visibleLines = allLines[start:end]
	}

	content := strings.Join(visibleLines, "\n")

	boxWidth := s.Width
	if boxWidth <= 0 {
		boxWidth = 60
	}
	if termWidth > 0 && boxWidth > termWidth-4 {
		boxWidth = termWidth - 4
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBrand).
		Foreground(colorText).
		Padding(1, 2).
		Width(boxWidth).
		Render(content)

	if termWidth > 0 {
		box = lipgloss.NewStyle().
			Width(termWidth).
			Align(lipgloss.Center).
			Render(box)
	}

	return box
}

// maskKey shows first 3 and last 3 chars of a key, rest as dots.
func maskKey(key string) string {
	if len(key) <= 8 {
		return strings.Repeat("•", len(key))
	}
	return key[:3] + strings.Repeat("•", len(key)-6) + key[len(key)-3:]
}
