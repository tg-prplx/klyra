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
	Title      string
	Options    []PickerOption
	Cursor     int
	Current    string // currently active value (shown with ●)
	Width      int
	Field      string // setting field name for the callback, e.g. "approval"
	Scroll     int
	MaxVisible int
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
		Title:      title,
		Field:      field,
		Options:    options,
		Cursor:     cursor,
		Current:    current,
		Width:      48,
		Scroll:     0,
		MaxVisible: 10,
	}
}

func (p *PickerModal) adjustScroll() {
	if p.Cursor < 0 {
		p.Cursor = 0
	}
	if p.Cursor >= len(p.Options) {
		p.Cursor = len(p.Options) - 1
	}
	if p.Cursor < p.Scroll {
		p.Scroll = p.Cursor
	}
	if p.Cursor >= p.Scroll+p.MaxVisible {
		p.Scroll = p.Cursor - p.MaxVisible + 1
	}
}

func (p *PickerModal) MoveUp() {
	p.Cursor--
	if p.Cursor < 0 {
		p.Cursor = len(p.Options) - 1
	}
	p.adjustScroll()
}

func (p *PickerModal) MoveDown() {
	p.Cursor++
	if p.Cursor >= len(p.Options) {
		p.Cursor = 0
	}
	p.adjustScroll()
}

// SelectedValue returns the value at the current cursor position.
func (p *PickerModal) SelectedValue() string {
	if p.Cursor < 0 || p.Cursor >= len(p.Options) {
		return ""
	}
	return p.Options[p.Cursor].Value
}

// View renders the picker modal.
func (p *PickerModal) View(termWidth, termHeight int) string {
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

	var optionLines []string
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

		optionLines = append(optionLines, line)
	}

	// Calculate vertical budget
	paddingY := modalPaddingY(termHeight)
	showHints := termHeight > 8

	// Apply scrolling to options
	visibleMax := p.MaxVisible
	if termHeight > 0 {
		overhead := 2 + paddingY*2 + 2 // borders + paddingY*2 + title & blank
		if showHints {
			overhead += 2 // blank + hint
		}
		overhead += 2 // scroll indicators overhead
		visibleMax = termHeight - overhead
		if visibleMax < 2 {
			visibleMax = 2
		}
	}
	p.MaxVisible = visibleMax

	visibleOptions := optionLines
	if len(optionLines) > visibleMax {
		start := p.Scroll
		end := start + visibleMax
		if end > len(optionLines) {
			end = len(optionLines)
			start = end - visibleMax
			if start < 0 {
				start = 0
			}
		}
		visibleOptions = optionLines[start:end]

		// Add scroll indicators
		hintScrollStyle := lipgloss.NewStyle().Foreground(colorDim)
		if start > 0 {
			visibleOptions = append([]string{hintScrollStyle.Render("  ▲ more above")}, visibleOptions...)
		}
		if end < len(optionLines) {
			visibleOptions = append(visibleOptions, hintScrollStyle.Render("  ▼ more below"))
		}
	}

	var allLines []string
	allLines = append(allLines, titleStyle.Render(p.Title))
	allLines = append(allLines, "")
	allLines = append(allLines, visibleOptions...)

	if showHints {
		allLines = append(allLines, "")
		allLines = append(allLines,
			hintKeyStyle.Render("↑/↓")+hintTextStyle.Render(" navigate  ")+
				hintKeyStyle.Render("Enter")+hintTextStyle.Render(" select  ")+
				hintKeyStyle.Render("Esc")+hintTextStyle.Render(" cancel"))
	}

	return renderModalFrame(termWidth, termHeight, max(p.Width, 56), 60, 40, 72, colorBrand, strings.Join(allLines, "\n"))
}

// ---------------------------------------------------------------------------
// Predefined picker builders
// ---------------------------------------------------------------------------

func ApprovalPicker(current string) PickerModal {
	return NewPicker("Approval Mode", "approval", current, []PickerOption{
		{Value: "auto", Label: "Auto", Description: "approve safe, block risky"},
		{Value: "ask", Label: "Ask", Description: "prompt before risky tools"},
		{Value: "always", Label: "Always", Description: "allow tools without prompts"},
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
		{Value: "plan", Label: "Plan", Description: "read-only exploration and planning"},
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
		{Value: "xhigh", Label: "XHigh", Description: "maximum reasoning budget"},
	})
}

func SessionPicker(current string, options []PickerOption) PickerModal {
	picker := NewPicker("Saved Sessions", "session", current, options)
	picker.Width = 72
	return picker
}

func CheckpointPicker() PickerModal {
	picker := NewPicker("Workspace Checkpoints", "checkpoint", "", []PickerOption{
		{Value: "list", Label: "List", Description: "show available checkpoints"},
		{Value: "create", Label: "Create", Description: "save a restore point now"},
		{Value: "restore", Label: "Restore", Description: "choose checkpoint to restore"},
	})
	picker.Width = 64
	return picker
}

func CheckpointRestorePicker(options []PickerOption) PickerModal {
	picker := NewPicker("Restore Checkpoint", "checkpoint_restore", "", options)
	picker.Width = 72
	return picker
}

func ConfigPicker() PickerModal {
	picker := NewPicker("Config", "config", "", []PickerOption{
		{Value: "show", Label: "Show", Description: "print effective configuration"},
		{Value: "init", Label: "Init", Description: "write default config file"},
	})
	picker.Width = 64
	return picker
}

func InstructionsPicker() PickerModal {
	picker := NewPicker("Project Instructions", "instructions", "", []PickerOption{
		{Value: "", Label: "List", Description: "show loaded instruction files"},
		{Value: "--content", Label: "Content", Description: "show loaded instruction text"},
	})
	picker.Width = 68
	return picker
}

func DiffPicker() PickerModal {
	picker := NewPicker("Diff", "diff", "", []PickerOption{
		{Value: "preview", Label: "Preview", Description: "validate patch from stdin/file"},
		{Value: "apply --yes", Label: "Apply", Description: "apply patch from stdin/file"},
	})
	picker.Width = 68
	return picker
}
