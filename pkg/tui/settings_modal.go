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
	secContext
	secRouting
)

var sectionNames = []string{"PROVIDER", "API KEYS", "SAFETY", "LIMITS", "CONTEXT", "ROUTING"}

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
	Fields     []settingsField
	Cursor     int    // visible row cursor, including section rows
	EditBuffer string // for text fields being edited
	Editing    bool   // true when actively typing into a text field
	Width      int
	Scroll     int
	MaxVisible int
	Expanded   map[settingsSection]bool
}

// NewSettingsModal creates a settings modal pre-populated with current values.
func NewSettingsModal(
	provider, model, endpoint, reasoning, approval, sandbox, mode string,
	storeResponses bool,
	maxContext, maxOutput, maxSteps, maxMessages, maxInstructions int,
	contextCockpit, contextCockpitInject bool,
	contextCockpitTokens, contextCockpitMaxFiles int,
	contextCockpitDiff bool,
	contextRecipes, negativeContext bool,
	fastModel, editModel, deepModel string,
) SettingsModal {
	fields := []settingsField{
		// Provider section
		{Section: secProvider, Name: "provider", DisplayName: "Provider", Value: provider, Choices: []string{"mock", "openai", "local", "ollama", "anthropic", "gemini"}},
		{Section: secProvider, Name: "model", DisplayName: "Model", Value: model},
		{Section: secProvider, Name: "endpoint", DisplayName: "Endpoint", Value: endpoint},
		{Section: secProvider, Name: "reasoning", DisplayName: "Reasoning", Value: reasoning, Choices: []string{"", "minimal", "low", "medium", "high", "xhigh"}},

		// API Keys section
		{Section: secAPIKeys, Name: "openai_key", DisplayName: "OpenAI Key", Value: "", EnvVar: "OPENAI_API_KEY", Masked: true},
		{Section: secAPIKeys, Name: "anthropic_key", DisplayName: "Anthropic Key", Value: "", EnvVar: "ANTHROPIC_API_KEY", Masked: true},
		{Section: secAPIKeys, Name: "gemini_key", DisplayName: "Gemini Key", Value: "", EnvVar: "GEMINI_API_KEY", Masked: true},

		// Safety section
		{Section: secSafety, Name: "approval", DisplayName: "Approval", Value: approval, Choices: []string{"auto", "ask", "never"}},
		{Section: secSafety, Name: "sandbox", DisplayName: "Sandbox", Value: sandbox, Choices: []string{"read-only", "workspace-write", "danger-full-access"}},
		{Section: secSafety, Name: "mode", DisplayName: "Mode", Value: mode, Choices: []string{"inspect", "edit", "repair", "refactor"}},
		{Section: secSafety, Name: "store", DisplayName: "Provider Store", Value: onOff(storeResponses), Choices: []string{"on", "off"}},

		// Limits section
		{Section: secLimits, Name: "context", DisplayName: "Context Tokens", Value: fmt.Sprintf("%d", maxContext)},
		{Section: secLimits, Name: "output", DisplayName: "Output Tokens", Value: fmt.Sprintf("%d", maxOutput)},
		{Section: secLimits, Name: "steps", DisplayName: "Max Steps", Value: fmt.Sprintf("%d", maxSteps)},
		{Section: secLimits, Name: "messages", DisplayName: "Max Messages", Value: fmt.Sprintf("%d", maxMessages)},
		{Section: secLimits, Name: "instructions", DisplayName: "Instruction Bytes", Value: fmt.Sprintf("%d", maxInstructions)},

		// Context section
		{Section: secContext, Name: "context_cockpit", DisplayName: "Cockpit", Value: onOff(contextCockpit), Choices: []string{"on", "off"}},
		{Section: secContext, Name: "context_cockpit_inject", DisplayName: "Inject Cards", Value: onOff(contextCockpitInject), Choices: []string{"on", "off"}},
		{Section: secContext, Name: "context_cockpit_tokens", DisplayName: "Cockpit Tokens", Value: fmt.Sprintf("%d", contextCockpitTokens)},
		{Section: secContext, Name: "context_cockpit_files", DisplayName: "Repo Map Files", Value: fmt.Sprintf("%d", contextCockpitMaxFiles)},
		{Section: secContext, Name: "context_cockpit_diff", DisplayName: "Include Diff", Value: onOff(contextCockpitDiff), Choices: []string{"on", "off"}},
		{Section: secContext, Name: "context_recipes", DisplayName: "Scoped Recipes", Value: onOff(contextRecipes), Choices: []string{"on", "off"}},
		{Section: secContext, Name: "negative_context", DisplayName: "Negative Context", Value: onOff(negativeContext), Choices: []string{"on", "off"}},

		// Routing section
		{Section: secRouting, Name: "fast_model", DisplayName: "Fast Model", Value: fastModel},
		{Section: secRouting, Name: "edit_model", DisplayName: "Edit Model", Value: editModel},
		{Section: secRouting, Name: "deep_model", DisplayName: "Deep Model", Value: deepModel},
	}

	return SettingsModal{
		Fields:     fields,
		Cursor:     0,
		Width:      72,
		MaxVisible: 18,
		Expanded:   map[settingsSection]bool{},
	}
}

func onOff(value bool) string {
	if value {
		return "on"
	}
	return "off"
}

func (s *SettingsModal) MoveUp() {
	if s.Editing {
		return
	}
	s.Cursor--
	if s.Cursor < 0 {
		s.Cursor = len(s.visibleRows()) - 1
	}
	s.adjustScroll()
}

func (s *SettingsModal) MoveDown() {
	if s.Editing {
		return
	}
	s.Cursor++
	if s.Cursor >= len(s.visibleRows()) {
		s.Cursor = 0
	}
	s.adjustScroll()
}

func (s *SettingsModal) CycleLeft() {
	row, ok := s.currentRow()
	if !ok {
		return
	}
	if row.IsSection {
		s.setExpanded(row.Section, false)
		s.adjustScroll()
		return
	}
	f := &s.Fields[row.FieldIndex]
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
	row, ok := s.currentRow()
	if !ok {
		return
	}
	if row.IsSection {
		s.setExpanded(row.Section, true)
		s.adjustScroll()
		return
	}
	f := &s.Fields[row.FieldIndex]
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
	f := s.currentField()
	if f == nil {
		return
	}
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
		f := s.currentField()
		if f == nil {
			return
		}
		if len(f.Choices) > 0 || f.ReadOnly {
			return
		}
		s.Editing = true
		s.EditBuffer = f.Value
	}
	s.EditBuffer += ch
	if f := s.currentField(); f != nil {
		f.Value = s.EditBuffer
	}
}

// Backspace removes the last character from the edit buffer.
func (s *SettingsModal) Backspace() {
	if !s.Editing {
		f := s.currentField()
		if f == nil {
			return
		}
		if len(f.Choices) > 0 || f.ReadOnly {
			return
		}
		s.Editing = true
		s.EditBuffer = f.Value
	}
	if len(s.EditBuffer) > 0 {
		s.EditBuffer = s.EditBuffer[:len(s.EditBuffer)-1]
	}
	if f := s.currentField(); f != nil {
		f.Value = s.EditBuffer
	}
}

// CommitEdit finishes editing and commits the value.
func (s *SettingsModal) CommitEdit() {
	if s.Editing {
		if f := s.currentField(); f != nil {
			f.Value = s.EditBuffer
		}
		s.Editing = false
	}
}

func (s *SettingsModal) adjustScroll() {
	if s.Cursor < 0 {
		s.Cursor = 0
	}
	rows := s.visibleRows()
	if len(rows) == 0 {
		s.Scroll = 0
		return
	}
	if s.Cursor >= len(rows) {
		s.Cursor = len(rows) - 1
	}
	if s.Cursor < s.Scroll {
		s.Scroll = s.Cursor
	}
	if s.Cursor >= s.Scroll+s.MaxVisible {
		s.Scroll = s.Cursor - s.MaxVisible + 1
	}
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

func (s *SettingsModal) ToggleCurrentSection() bool {
	row, ok := s.currentRow()
	if !ok || !row.IsSection {
		return false
	}
	s.setExpanded(row.Section, !s.isExpanded(row.Section))
	s.adjustScroll()
	return true
}

type settingsRow struct {
	IsSection  bool
	Section    settingsSection
	FieldIndex int
}

func (s SettingsModal) visibleRows() []settingsRow {
	seen := map[settingsSection]bool{}
	var rows []settingsRow
	for i, field := range s.Fields {
		if !seen[field.Section] {
			rows = append(rows, settingsRow{IsSection: true, Section: field.Section})
			seen[field.Section] = true
		}
		if s.isExpanded(field.Section) {
			rows = append(rows, settingsRow{Section: field.Section, FieldIndex: i})
		}
	}
	return rows
}

func (s SettingsModal) currentRow() (settingsRow, bool) {
	rows := s.visibleRows()
	if s.Cursor < 0 || s.Cursor >= len(rows) {
		return settingsRow{}, false
	}
	return rows[s.Cursor], true
}

func (s *SettingsModal) currentField() *settingsField {
	row, ok := s.currentRow()
	if !ok || row.IsSection || row.FieldIndex < 0 || row.FieldIndex >= len(s.Fields) {
		return nil
	}
	return &s.Fields[row.FieldIndex]
}

func (s SettingsModal) isExpanded(section settingsSection) bool {
	if s.Expanded == nil {
		return false
	}
	return s.Expanded[section]
}

func (s *SettingsModal) setExpanded(section settingsSection, expanded bool) {
	if s.Expanded == nil {
		s.Expanded = map[settingsSection]bool{}
	}
	s.Expanded[section] = expanded
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

	for rowIdx, row := range s.visibleRows() {
		if row.IsSection {
			if rowIdx > 0 {
				allLines = append(allLines, "")
			}
			secIdx := int(row.Section)
			name := "SECTION"
			if secIdx < len(sectionNames) {
				name = sectionNames[secIdx]
			}
			icon := "▸"
			if s.isExpanded(row.Section) {
				icon = "▾"
			}
			style := sectionStyle
			if rowIdx == s.Cursor {
				style = activeLabel
			}
			allLines = append(allLines, style.Render(fmt.Sprintf("  %s %s", icon, name))+" "+sectionSummary(s, row.Section))
			continue
		}
		f := s.Fields[row.FieldIndex]

		isActive := rowIdx == s.Cursor
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

		allLines = append(allLines, "  "+label+" "+val)
	}

	allLines = append(allLines, "")

	if s.Editing {
		allLines = append(allLines,
			hintKeyStyle.Render("Enter")+hintTextStyle.Render(" confirm field  ")+
				hintKeyStyle.Render("Esc")+hintTextStyle.Render(" cancel edit"))
	} else {
		allLines = append(allLines,
			hintKeyStyle.Render("↑/↓")+hintTextStyle.Render(" navigate  ")+
				hintKeyStyle.Render("Enter/→")+hintTextStyle.Render(" expand  ")+
				hintKeyStyle.Render("←")+hintTextStyle.Render(" collapse  "))
		allLines = append(allLines,
			hintKeyStyle.Render("◀/▶")+hintTextStyle.Render(" choose  ")+
				hintTextStyle.Render("type to edit  "))
		allLines = append(allLines,
			hintKeyStyle.Render("Enter")+hintTextStyle.Render(" save field form  ")+
				hintKeyStyle.Render("Ctrl+S")+hintTextStyle.Render(" save all  ")+
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
		boxWidth = 68
	}
	if termWidth > 0 && boxWidth > termWidth-8 {
		boxWidth = termWidth - 8
	}
	if boxWidth < 36 {
		boxWidth = max(24, termWidth-4)
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

func sectionSummary(s SettingsModal, section settingsSection) string {
	style := lipgloss.NewStyle().Foreground(colorDim)
	count := 0
	preview := []string{}
	for _, field := range s.Fields {
		if field.Section != section {
			continue
		}
		count++
		if len(preview) >= 2 {
			continue
		}
		value := field.Value
		if field.Masked {
			if value != "" {
				value = "set"
			} else if field.EnvVar != "" && os.Getenv(field.EnvVar) != "" {
				value = "env"
			} else {
				value = "unset"
			}
		}
		if value == "" {
			value = "default"
		}
		preview = append(preview, field.DisplayName+"="+shorten(value, 18))
	}
	if len(preview) == 0 {
		return style.Render(fmt.Sprintf("(%d fields)", count))
	}
	return style.Render(fmt.Sprintf("(%d fields · %s)", count, strings.Join(preview, " · ")))
}
