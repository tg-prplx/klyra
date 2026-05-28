package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Features Modal — quick toggle for boolean feature flags
// ---------------------------------------------------------------------------

// featureFlag represents a single toggleable feature in the features modal.
type featureFlag struct {
	Name        string // internal key used for /set commands
	DisplayName string // human-readable label
	Description string // short help text
	Enabled     bool
}

// FeaturesModal is a focused overlay for toggling feature flags on/off.
type FeaturesModal struct {
	Flags      []featureFlag
	Cursor     int
	Width      int
	Scroll     int
	MaxVisible int
}

// NewFeaturesModal creates a features modal pre-populated with current feature states.
func NewFeaturesModal(
	stream bool,
	storeResponses bool,
	contextCockpit bool,
	contextCockpitInject bool,
	contextCockpitDiff bool,
	contextRetrieval bool,
	contextEmbeddings bool,
	contextReranker bool,
	contextRecipes bool,
	negativeContext bool,
	skills bool,
) FeaturesModal {
	flags := []featureFlag{
		{Name: "stream", DisplayName: "Streaming", Description: "Stream model responses token-by-token", Enabled: stream},
		{Name: "store", DisplayName: "Provider Store", Description: "Allow provider to store responses", Enabled: storeResponses},
		{Name: "context_cockpit", DisplayName: "Cockpit", Description: "Enable context cockpit with repo facts", Enabled: contextCockpit},
		{Name: "context_cockpit_inject", DisplayName: "Inject Cards", Description: "Auto-inject cockpit cards into prompt", Enabled: contextCockpitInject},
		{Name: "context_cockpit_diff", DisplayName: "Include Diff", Description: "Include git diff in cockpit", Enabled: contextCockpitDiff},
		{Name: "context_retrieval", DisplayName: "Retrieval Cart", Description: "Retrieve relevant code snippets", Enabled: contextRetrieval},
		{Name: "context_embeddings", DisplayName: "Embeddings", Description: "Use embeddings for retrieval", Enabled: contextEmbeddings},
		{Name: "context_reranker", DisplayName: "Reranker", Description: "Rerank retrieved snippets", Enabled: contextReranker},
		{Name: "context_recipes", DisplayName: "Scoped Recipes", Description: "Load scoped instruction recipes", Enabled: contextRecipes},
		{Name: "negative_context", DisplayName: "Negative Context", Description: "Track negative context signals", Enabled: negativeContext},
		{Name: "skills", DisplayName: "Skills", Description: "Match and inject project skills", Enabled: skills},
	}

	return FeaturesModal{
		Flags:      flags,
		Cursor:     0,
		Width:      80,
		MaxVisible: 20,
	}
}

func (f *FeaturesModal) MoveUp() {
	f.Cursor--
	if f.Cursor < 0 {
		f.Cursor = len(f.Flags) - 1
	}
	f.adjustScroll()
}

func (f *FeaturesModal) MoveDown() {
	f.Cursor++
	if f.Cursor >= len(f.Flags) {
		f.Cursor = 0
	}
	f.adjustScroll()
}

// Toggle flips the current feature on/off.
func (f *FeaturesModal) Toggle() {
	if f.Cursor >= 0 && f.Cursor < len(f.Flags) {
		f.Flags[f.Cursor].Enabled = !f.Flags[f.Cursor].Enabled
	}
}

// EnableAll turns on all features.
func (f *FeaturesModal) EnableAll() {
	for i := range f.Flags {
		f.Flags[i].Enabled = true
	}
}

// DisableAll turns off all features.
func (f *FeaturesModal) DisableAll() {
	for i := range f.Flags {
		f.Flags[i].Enabled = false
	}
}

func (f *FeaturesModal) adjustScroll() {
	if f.Cursor < 0 {
		f.Cursor = 0
	}
	if f.Cursor >= len(f.Flags) {
		f.Cursor = len(f.Flags) - 1
	}
	if f.Cursor < f.Scroll {
		f.Scroll = f.Cursor
	}
	if f.Cursor >= f.Scroll+f.MaxVisible {
		f.Scroll = f.Cursor - f.MaxVisible + 1
	}
}

// GetValue returns the on/off value for a feature by name.
func (f *FeaturesModal) GetValue(name string) string {
	for _, flag := range f.Flags {
		if flag.Name == name {
			if flag.Enabled {
				return "on"
			}
			return "off"
		}
	}
	return ""
}

// View renders the features modal.
func (f *FeaturesModal) View(termWidth, termHeight int) string {
	headerStyle := lipgloss.NewStyle().
		Foreground(colorBrand).
		Bold(true)

	activeLabel := lipgloss.NewStyle().
		Foreground(colorBrand).
		Bold(true)

	normalLabel := lipgloss.NewStyle().
		Foreground(colorText)

	descStyle := lipgloss.NewStyle().
		Foreground(colorDim).
		Italic(true)

	onStyle := lipgloss.NewStyle().
		Foreground(colorEmerald).
		Bold(true)

	offStyle := lipgloss.NewStyle().
		Foreground(colorRed).
		Bold(true)

	onBgStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#065F46")).
		Background(lipgloss.Color("#064E3B")).
		Bold(true).
		Padding(0, 1)

	offBgStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#991B1B")).
		Background(lipgloss.Color("#7F1D1D")).
		Bold(true).
		Padding(0, 1)

	hintKeyStyle := lipgloss.NewStyle().Foreground(colorMuted)
	hintTextStyle := lipgloss.NewStyle().Foreground(colorDim)

	// Count enabled/total
	enabledCount := 0
	for _, flag := range f.Flags {
		if flag.Enabled {
			enabledCount++
		}
	}
	totalCount := len(f.Flags)

	counterStyle := lipgloss.NewStyle().Foreground(colorDim)

	var allLines []string
	allLines = append(allLines, headerStyle.Render("⚡ Features")+" "+counterStyle.Render(fmt.Sprintf("(%d/%d enabled)", enabledCount, totalCount)))
	allLines = append(allLines, "")

	for i, flag := range f.Flags {
		isActive := i == f.Cursor
		marker := "  "
		lblStyle := normalLabel
		if isActive {
			marker = "▸ "
			lblStyle = activeLabel
		}

		// Toggle switch
		var toggle string
		if isActive {
			if flag.Enabled {
				toggle = onBgStyle.Render("● ON ")
			} else {
				toggle = offBgStyle.Render("○ OFF")
			}
		} else {
			if flag.Enabled {
				toggle = onStyle.Render("● ON ")
			} else {
				toggle = offStyle.Render("○ OFF")
			}
		}

		label := lblStyle.Render(fmt.Sprintf("%s%-20s", marker, flag.DisplayName))
		line := "  " + label + " " + toggle

		// Show description for active item
		if isActive && flag.Description != "" {
			line += " " + descStyle.Render(flag.Description)
		}

		allLines = append(allLines, line)
	}

	allLines = append(allLines, "")
	allLines = append(allLines,
		hintKeyStyle.Render("↑/↓")+hintTextStyle.Render(" navigate  ")+
			hintKeyStyle.Render("Space/Enter")+hintTextStyle.Render(" toggle  ")+
			hintKeyStyle.Render("a")+hintTextStyle.Render(" all on  ")+
			hintKeyStyle.Render("n")+hintTextStyle.Render(" all off"))
	allLines = append(allLines,
		hintKeyStyle.Render("Ctrl+S")+hintTextStyle.Render(" save & close  ")+
			hintKeyStyle.Render("Esc")+hintTextStyle.Render(" discard & close"))

	hintScrollStyle := lipgloss.NewStyle().Foreground(colorDim)

	paddingY := 1
	if termHeight > 0 && termHeight <= 14 {
		paddingY = 0
	}

	visibleMax := f.MaxVisible
	if termHeight > 0 {
		overhead := 2 + paddingY*2 + 6 // borders + paddingY*2 + scroll indicators (2) + layout overhead (4)
		visibleMax = termHeight - overhead
		if visibleMax < 4 {
			visibleMax = 4
		}
	}
	f.MaxVisible = visibleMax

	visibleLines := allLines
	if len(allLines) > visibleMax {
		start := f.Scroll
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
		if start > 0 {
			visibleLines = append([]string{hintScrollStyle.Render("  ▲ more above")}, visibleLines...)
		}
		if end < len(allLines) {
			visibleLines = append(visibleLines, hintScrollStyle.Render("  ▼ more below"))
		}
	}

	content := strings.Join(visibleLines, "\n")

	// Width: use 80% of terminal, clamped to [48, 90]
	boxWidth := f.Width
	if termWidth > 0 {
		adaptive := termWidth * 80 / 100
		if adaptive > 90 {
			adaptive = 90
		}
		if adaptive < 48 {
			adaptive = max(36, termWidth-4)
		}
		if adaptive > boxWidth {
			boxWidth = adaptive
		}
		if boxWidth > termWidth-4 {
			boxWidth = termWidth - 4
		}
	}
	if boxWidth < 48 {
		boxWidth = 48
	}

	// Hard-cap height to prevent any overflow past the terminal.
	// In Lipgloss, MaxHeight restricts the content area (inner size).
	// We want outer height to be at most termHeight - 2 to leave 1-line margin top/bottom.
	// Outer height = content + borders (2) + paddingY * 2.
	maxInnerHeight := termHeight - 4 - paddingY*2
	if maxInnerHeight < 2 {
		maxInnerHeight = 2
	}

	// Manually truncate visibleLines to strictly guarantee that the rendered box
	// height does not exceed maxInnerHeight lines under any circumstances.
	if len(visibleLines) > maxInnerHeight {
		visibleLines = visibleLines[:maxInnerHeight]
	}

	content = strings.Join(visibleLines, "\n")

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBrand).
		Foreground(colorText).
		Padding(paddingY, 2).
		Width(boxWidth).
		MaxHeight(maxInnerHeight).
		Render(content)

	if termWidth > 0 {
		box = lipgloss.NewStyle().
			Width(termWidth).
			Align(lipgloss.Center).
			Render(box)
	}

	return box
}
