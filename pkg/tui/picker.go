package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Picker Modal — universal option selector
// ---------------------------------------------------------------------------

// PickerOption represents a single choice in the picker.
type PickerOption struct {
	Value       string
	Label       string
	Description string
}

// PickerModal is a reusable modal for choosing one value from a list.
type PickerModal struct {
	Title    string
	Options  []PickerOption
	Cursor   int
	Current  string // currently active value (shown with ●)
	Width    int
	Field    string // setting field name for the callback, e.g. "approval"
}

// NewPicker creates a PickerModal pre-selecting the current value.
func NewPicker(title, field, current string, options []PickerOption) PickerModal {
	cursor := 0
	for i, opt := range options {
		if opt.Value == current {
			cursor = i
			break
		}
	}
	return PickerModal{
		Title:   title,
		Field:   field,
		Options: options,
		Cursor:  cursor,
		Current: current,
		Width:   48,
	}
}

func (p *PickerModal) MoveUp() {
	p.Cursor--
	if p.Cursor < 0 {
		p.Cursor = len(p.Options) - 1
	}
}

func (p *PickerModal) MoveDown() {
	p.Cursor++
	if p.Cursor >= len(p.Options) {
		p.Cursor = 0
	}
}

// SelectedValue returns the value at the current cursor position.
func (p *PickerModal) SelectedValue() string {
	if p.Cursor < 0 || p.Cursor >= len(p.Options) {
		return ""
	}
	return p.Options[p.Cursor].Value
}

// View renders the picker modal.
func (p PickerModal) View(termWidth int) string {
	titleStyle := lipgloss.NewStyle().
		Foreground(colorBrand).
		Bold(true)

	activeStyle := lipgloss.NewStyle().
		Foreground(colorWhite).
		Background(colorBrandDim).
		Bold(true).
		Padding(0, 1)

	normalStyle := lipgloss.NewStyle().
		Foreground(colorText).
		Padding(0, 1)

	currentBadge := lipgloss.NewStyle().
		Foreground(colorEmerald).
		Bold(true)

	descStyle := lipgloss.NewStyle().
		Foreground(colorDim).
		Italic(true)

	hintKeyStyle := lipgloss.NewStyle().Foreground(colorMuted)
	hintTextStyle := lipgloss.NewStyle().Foreground(colorDim)

	var lines []string
	lines = append(lines, titleStyle.Render(p.Title))
	lines = append(lines, "")

	for i, opt := range p.Options {
		label := opt.Label
		if label == "" {
			label = opt.Value
		}
		if label == "" {
			label = "(default)"
		}

		isCurrent := opt.Value == p.Current
		indicator := "  "
		if isCurrent {
			indicator = currentBadge.Render("● ")
		}

		var line string
		if i == p.Cursor {
			line = indicator + activeStyle.Render(fmt.Sprintf(" ▸ %s ", label))
		} else {
			line = indicator + normalStyle.Render(fmt.Sprintf("   %s ", label))
		}

		if opt.Description != "" {
			line += " " + descStyle.Render(opt.Description)
		}

		lines = append(lines, line)
	}

	lines = append(lines, "")
	lines = append(lines,
		hintKeyStyle.Render("↑/↓")+hintTextStyle.Render(" navigate  ")+
			hintKeyStyle.Render("Enter")+hintTextStyle.Render(" select  ")+
			hintKeyStyle.Render("Esc")+hintTextStyle.Render(" cancel"))

	content := strings.Join(lines, "\n")

	boxWidth := p.Width
	if boxWidth <= 0 {
		boxWidth = 48
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

	// Center horizontally
	if termWidth > 0 {
		box = lipgloss.NewStyle().
			Width(termWidth).
			Align(lipgloss.Center).
			Render(box)
	}

	return box
}

// ---------------------------------------------------------------------------
// Predefined picker builders
// ---------------------------------------------------------------------------

func ApprovalPicker(current string) PickerModal {
	return NewPicker("Approval Mode", "approval", current, []PickerOption{
		{Value: "auto", Label: "Auto", Description: "approve safe, block risky"},
		{Value: "ask", Label: "Ask", Description: "prompt before risky tools"},
		{Value: "never", Label: "Never", Description: "block all risky tools"},
	})
}

func ProviderPicker(current string) PickerModal {
	return NewPicker("Provider", "provider", current, []PickerOption{
		{Value: "mock", Label: "Mock", Description: "local testing, no API key"},
		{Value: "openai", Label: "OpenAI", Description: "Responses API"},
		{Value: "local", Label: "Local", Description: "OpenAI-compatible local server (LMStudio, vLLM, etc)"},
		{Value: "ollama", Label: "Ollama", Description: "local models"},
		{Value: "anthropic", Label: "Anthropic", Description: "Claude models"},
		{Value: "gemini", Label: "Gemini", Description: "Google Gemini models"},
	})
}

func SandboxPicker(current string) PickerModal {
	return NewPicker("Sandbox Profile", "sandbox", current, []PickerOption{
		{Value: "read-only", Label: "Read-Only", Description: "no writes, no shell"},
		{Value: "workspace-write", Label: "Workspace Write", Description: "write inside workspace"},
		{Value: "danger-full-access", Label: "Full Access ⚠", Description: "unrestricted access"},
	})
}

func ModePicker(current string) PickerModal {
	return NewPicker("Agent Mode", "mode", current, []PickerOption{
		{Value: "inspect", Label: "Inspect", Description: "read-only exploration"},
		{Value: "edit", Label: "Edit", Description: "modify files in cart"},
		{Value: "repair", Label: "Repair", Description: "fix failing output"},
		{Value: "refactor", Label: "Refactor", Description: "broad structural changes"},
	})
}

func ReasoningPicker(current string) PickerModal {
	return NewPicker("Reasoning Effort", "reasoning", current, []PickerOption{
		{Value: "", Label: "Default", Description: "provider decides"},
		{Value: "minimal", Label: "Minimal", Description: "fastest, least thinking"},
		{Value: "low", Label: "Low", Description: "quick reasoning"},
		{Value: "medium", Label: "Medium", Description: "balanced"},
		{Value: "high", Label: "High", Description: "deepest thinking"},
	})
}
