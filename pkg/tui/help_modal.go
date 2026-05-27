package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Help Modal — categorized command reference
// ---------------------------------------------------------------------------

type helpCategory struct {
	Name     string
	Commands []CommandDef
}

// HelpModal renders a scrollable, categorized help overlay.
type HelpModal struct {
	Categories []helpCategory
	Scroll     int
	MaxVisible int
}

func NewHelpModal(commands []CommandDef) HelpModal {
	catMap := map[string][]CommandDef{
		"Session":     {},
		"Settings":    {},
		"Safety":      {},
		"Context":     {},
		"Tools":       {},
		"Workspace":   {},
		"Other":       {},
	}
	catOrder := []string{"Session", "Settings", "Safety", "Context", "Tools", "Workspace", "Other"}

	for _, cmd := range commands {
		switch {
		case cmd.Name == "/help" || cmd.Name == "/exit" || cmd.Name == "/quit" || cmd.Name == "/clear" || cmd.Name == "/save" || cmd.Name == "/compact":
			catMap["Session"] = append(catMap["Session"], cmd)
		case cmd.Name == "/provider" || cmd.Name == "/model" || cmd.Name == "/endpoint" || cmd.Name == "/reasoning" || cmd.Name == "/limits" || cmd.Name == "/settings":
			catMap["Settings"] = append(catMap["Settings"], cmd)
		case cmd.Name == "/approval" || cmd.Name == "/sandbox" || cmd.Name == "/mode":
			catMap["Safety"] = append(catMap["Safety"], cmd)
		case cmd.Name == "/cart" || cmd.Name == "/attach" || cmd.Name == "/attachments" || cmd.Name == "/instructions":
			catMap["Context"] = append(catMap["Context"], cmd)
		case cmd.Name == "/doctor" || cmd.Name == "/tools" || cmd.Name == "/status" || cmd.Name == "/sessions":
			catMap["Tools"] = append(catMap["Tools"], cmd)
		case strings.HasPrefix(cmd.Name, "/checkpoint") || strings.HasPrefix(cmd.Name, "/diff") || strings.HasPrefix(cmd.Name, "/policy") || strings.HasPrefix(cmd.Name, "/config"):
			catMap["Workspace"] = append(catMap["Workspace"], cmd)
		default:
			catMap["Other"] = append(catMap["Other"], cmd)
		}
	}

	var categories []helpCategory
	for _, name := range catOrder {
		if cmds, ok := catMap[name]; ok && len(cmds) > 0 {
			categories = append(categories, helpCategory{Name: name, Commands: cmds})
		}
	}

	return HelpModal{
		Categories: categories,
		Scroll:     0,
		MaxVisible: 20,
	}
}

func (h *HelpModal) ScrollUp() {
	h.Scroll--
	if h.Scroll < 0 {
		h.Scroll = 0
	}
}

func (h *HelpModal) ScrollDown(totalLines int) {
	if h.Scroll < totalLines-h.MaxVisible {
		h.Scroll++
	}
}

func (h HelpModal) View(termWidth, termHeight int) string {
	catTitleStyle := lipgloss.NewStyle().
		Foreground(colorBrand).
		Bold(true).
		MarginTop(1)

	firstCatTitleStyle := lipgloss.NewStyle().
		Foreground(colorBrand).
		Bold(true)

	cmdNameStyle := lipgloss.NewStyle().
		Foreground(colorBlue).
		Bold(true)

	cmdDescStyle := lipgloss.NewStyle().
		Foreground(colorDim)

	headerStyle := lipgloss.NewStyle().
		Foreground(colorWhite).
		Bold(true)

	hintKeyStyle := lipgloss.NewStyle().Foreground(colorMuted)
	hintTextStyle := lipgloss.NewStyle().Foreground(colorDim)

	kbdStyle := lipgloss.NewStyle().
		Foreground(colorAmber).
		Bold(true)

	var allLines []string
	allLines = append(allLines, headerStyle.Render("Klyra — Command Reference"))
	allLines = append(allLines, "")

	// Keyboard shortcuts section
	allLines = append(allLines, firstCatTitleStyle.Render("⌨  Keyboard Shortcuts"))
	allLines = append(allLines, fmt.Sprintf("  %s  %s", kbdStyle.Render("F2 / Ctrl+S"), cmdDescStyle.Render("Open settings panel")))
	allLines = append(allLines, fmt.Sprintf("  %s      %s", kbdStyle.Render("Ctrl+C"), cmdDescStyle.Render("Exit")))
	allLines = append(allLines, fmt.Sprintf("  %s      %s", kbdStyle.Render("↑ / ↓ "), cmdDescStyle.Render("Navigate autocomplete / modals")))
	allLines = append(allLines, fmt.Sprintf("  %s      %s", kbdStyle.Render("Tab   "), cmdDescStyle.Render("Next field in settings")))
	allLines = append(allLines, fmt.Sprintf("  %s      %s", kbdStyle.Render("F3    "), cmdDescStyle.Render("Toggle Context Debugger")))
	allLines = append(allLines, fmt.Sprintf("  %s      %s", kbdStyle.Render("F4    "), cmdDescStyle.Render("Toggle Model Reasoning (Thoughts)")))

	for i, cat := range h.Categories {
		if i == 0 {
			allLines = append(allLines, catTitleStyle.Render("◆  "+cat.Name))
		} else {
			allLines = append(allLines, catTitleStyle.Render("◆  "+cat.Name))
		}
		for _, cmd := range cat.Commands {
			allLines = append(allLines,
				fmt.Sprintf("  %s  %s",
					cmdNameStyle.Render(fmt.Sprintf("%-22s", cmd.Name)),
					cmdDescStyle.Render(cmd.Description)))
		}
	}

	allLines = append(allLines, "")
	allLines = append(allLines,
		hintKeyStyle.Render("↑/↓")+hintTextStyle.Render(" scroll  ")+
			hintKeyStyle.Render("Esc")+hintTextStyle.Render(" close"))

	// Apply scrolling
	visibleMax := h.MaxVisible
	if termHeight > 0 {
		visibleMax = termHeight - 8
		if visibleMax < 10 {
			visibleMax = 10
		}
	}
	h.MaxVisible = visibleMax

	visibleLines := allLines
	if len(allLines) > visibleMax {
		end := h.Scroll + visibleMax
		if end > len(allLines) {
			end = len(allLines)
		}
		start := h.Scroll
		if start >= len(allLines) {
			start = len(allLines) - 1
		}
		visibleLines = allLines[start:end]

		// Scroll indicators
		if h.Scroll > 0 {
			visibleLines = append([]string{hintTextStyle.Render("  ▲ more above")}, visibleLines...)
		}
		if end < len(allLines) {
			visibleLines = append(visibleLines, hintTextStyle.Render("  ▼ more below"))
		}
	}

	content := strings.Join(visibleLines, "\n")

	boxWidth := 64
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
