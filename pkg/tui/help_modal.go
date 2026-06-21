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
		"Session":   {},
		"Settings":  {},
		"Safety":    {},
		"Context":   {},
		"Tools":     {},
		"Workspace": {},
		"Other":     {},
	}
	catOrder := []string{"Session", "Settings", "Safety", "Context", "Tools", "Workspace", "Other"}

	for _, cmd := range commands {
		switch {
		case cmd.Name == "/help" || cmd.Name == "/exit" || cmd.Name == "/quit" || cmd.Name == "/clear" || cmd.Name == "/save" || cmd.Name == "/compact":
			catMap["Session"] = append(catMap["Session"], cmd)
		case cmd.Name == "/provider" || cmd.Name == "/model" || cmd.Name == "/endpoint" || cmd.Name == "/reasoning" || cmd.Name == "/limits" || cmd.Name == "/settings" || cmd.Name == "/features":
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

func (h HelpModal) totalLines() int {
	total := 17 // 15 shortcuts + 2 trailing lines
	for _, cat := range h.Categories {
		total += 1 + len(cat.Commands)
	}
	return total
}

func (h *HelpModal) ScrollDown() {
	total := h.totalLines()
	if h.Scroll < total-h.MaxVisible {
		h.Scroll++
	}
}

func (h *HelpModal) View(termWidth, termHeight int) string {
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
	allLines = append(allLines, fmt.Sprintf("  %s      %s", kbdStyle.Render("↑ / ↓ "), cmdDescStyle.Render("Scroll chat history / Navigate modals")))
	allLines = append(allLines, fmt.Sprintf("  %s %s", kbdStyle.Render("Ctrl+↑/↓ / Ctrl+P/N"), cmdDescStyle.Render("Navigate command history")))
	allLines = append(allLines, fmt.Sprintf("  %s      %s", kbdStyle.Render("Tab   "), cmdDescStyle.Render("Next field in settings / Autocomplete")))
	allLines = append(allLines, fmt.Sprintf("  %s      %s", kbdStyle.Render("F3    "), cmdDescStyle.Render("Toggle Context Debugger")))
	allLines = append(allLines, fmt.Sprintf("  %s      %s", kbdStyle.Render("F4    "), cmdDescStyle.Render("Toggle Model Reasoning (Thoughts)")))
	allLines = append(allLines, fmt.Sprintf("  %s      %s", kbdStyle.Render("F5    "), cmdDescStyle.Render("Toggle features on/off")))
	allLines = append(allLines, fmt.Sprintf("  %s      %s", kbdStyle.Render("F6    "), cmdDescStyle.Render("Toggle copy mode")))
	allLines = append(allLines, fmt.Sprintf("  %s      %s", kbdStyle.Render("F7    "), cmdDescStyle.Render("Cycle sidebar mode (files/diff/context)")))
	allLines = append(allLines, fmt.Sprintf("  %s  %s", kbdStyle.Render("Alt+F7  "), cmdDescStyle.Render("Toggle sidebar visibility")))
	allLines = append(allLines, fmt.Sprintf("  %s      %s", kbdStyle.Render("F8    "), cmdDescStyle.Render("Toggle sidebar position (left/right)")))
	allLines = append(allLines, fmt.Sprintf("  %s      %s", kbdStyle.Render("F9    "), cmdDescStyle.Render("Toggle agent tools on/off")))

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

	// Apply scrolling — leave room for border + padding + header chrome
	paddingY := modalPaddingY(termHeight)

	visibleMax := h.MaxVisible
	if termHeight > 0 {
		overhead := 2 + paddingY*2 + 6 // borders + paddingY*2 + scroll indicators (2) + layout overhead (4)
		visibleMax = termHeight - overhead
		if visibleMax < 4 {
			visibleMax = 4
		}
	}
	h.MaxVisible = visibleMax

	visibleLines := allLines
	if len(allLines) > visibleMax {
		start := h.Scroll
		end := start + visibleMax
		if end > len(allLines) {
			end = len(allLines)
			start = end - visibleMax
			if start < 0 {
				start = 0
			}
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

	return renderModalFrame(termWidth, termHeight, 80, 80, 48, 90, colorBrand, strings.Join(visibleLines, "\n"))
}
