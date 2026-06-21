package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// toolFlag represents a single toggleable tool in the tools modal.
type toolFlag struct {
	Name        string
	DisplayName string
	Description string
	Enabled     bool
}

// ToolsModal is a focused overlay for toggling agent tools on/off.
type ToolsModal struct {
	Tools      []toolFlag
	Cursor     int
	Width      int
	Scroll     int
	MaxVisible int
}

// NewToolsModal creates a tools modal pre-populated with current tool states.
func NewToolsModal(allTools []string, disabledTools []string) ToolsModal {
	disabledMap := make(map[string]bool)
	for _, dt := range disabledTools {
		disabledMap[dt] = true
	}

	tools := make([]toolFlag, 0, len(allTools))
	for _, name := range allTools {
		tools = append(tools, toolFlag{
			Name:        name,
			DisplayName: name,
			Description: getToolDescription(name),
			Enabled:     !disabledMap[name],
		})
	}

	return ToolsModal{
		Tools:      tools,
		Cursor:     0,
		Width:      80,
		MaxVisible: 20,
	}
}

func getToolDescription(name string) string {
	switch name {
	case "guide":
		return "Project layout guidelines and instructions"
	case "project_map":
		return "Get high-level summary of workspace"
	case "git_status":
		return "Show status of files in git repository"
	case "git_diff":
		return "Show diff of modified files"
	case "workspace_checkpoint":
		return "Create checkpoint of active files"
	case "workspace_checkpoint_list":
		return "List created workspace checkpoints"
	case "workspace_restore":
		return "Restore files from a checkpoint"
	case "policy_check":
		return "Check shell command risk level"
	case "bash":
		return "Run shell commands in workspace"
	case "list_files":
		return "Recursively list directory files"
	case "read_file":
		return "Read contents of a file"
	case "file_outline":
		return "Extract symbols and classes from file"
	case "read_symbol":
		return "Read definition of a code symbol"
	case "read_go_symbol":
		return "Read Go AST structure and details"
	case "web_search":
		return "Search the web for info"
	case "fetch_url":
		return "Fetch and parse webpage content"
	case "write_file":
		return "Create/write new files (safety-guarded)"
	case "create_file":
		return "Create a new file in workspace"
	case "edit_file":
		return "Edit an existing file by exact text"
	case "replace_lines":
		return "Replace specific lines in file"
	case "insert_lines":
		return "Insert lines in file"
	case "replace_symbol":
		return "Modify specific code symbol definition"
	case "search":
		return "Search text patterns in workspace"
	case "diff_preview":
		return "Preview applying a patch file"
	case "diff_patch":
		return "Apply patch to files in workspace"
	default:
		if strings.HasPrefix(name, "mcp_") {
			return "MCP external tool"
		}
		return "Agent tool capability"
	}
}

func (t *ToolsModal) MoveUp() {
	t.Cursor--
	if t.Cursor < 0 {
		t.Cursor = len(t.Tools) - 1
	}
	t.adjustScroll()
}

func (t *ToolsModal) MoveDown() {
	t.Cursor++
	if t.Cursor >= len(t.Tools) {
		t.Cursor = 0
	}
	t.adjustScroll()
}

// Toggle flips the current tool on/off.
func (t *ToolsModal) Toggle() {
	if t.Cursor >= 0 && t.Cursor < len(t.Tools) {
		t.Tools[t.Cursor].Enabled = !t.Tools[t.Cursor].Enabled
	}
}

// EnableAll turns on all tools.
func (t *ToolsModal) EnableAll() {
	for i := range t.Tools {
		t.Tools[i].Enabled = true
	}
}

// DisableAll turns off all tools.
func (t *ToolsModal) DisableAll() {
	for i := range t.Tools {
		t.Tools[i].Enabled = false
	}
}

func (t *ToolsModal) adjustScroll() {
	if t.Cursor < 0 {
		t.Cursor = 0
	}
	if t.Cursor >= len(t.Tools) {
		t.Cursor = len(t.Tools) - 1
	}
	if t.Cursor < t.Scroll {
		t.Scroll = t.Cursor
	}
	if t.Cursor >= t.Scroll+t.MaxVisible {
		t.Scroll = t.Cursor - t.MaxVisible + 1
	}
}

// View renders the tools modal.
func (t *ToolsModal) View(termWidth, termHeight int) string {
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
	for _, flag := range t.Tools {
		if flag.Enabled {
			enabledCount++
		}
	}
	totalCount := len(t.Tools)

	counterStyle := lipgloss.NewStyle().Foreground(colorDim)

	var allLines []string
	allLines = append(allLines, headerStyle.Render("🛠️  Agent Tools")+" "+counterStyle.Render(fmt.Sprintf("(%d/%d enabled)", enabledCount, totalCount)))
	allLines = append(allLines, "")

	for i, flag := range t.Tools {
		isActive := i == t.Cursor
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

		label := lblStyle.Render(fmt.Sprintf("%s%-24s", marker, flag.DisplayName))
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

	paddingY := modalPaddingY(termHeight)

	visibleMax := t.MaxVisible
	if termHeight > 0 {
		overhead := 2 + paddingY*2 + 6 // borders + paddingY*2 + scroll indicators (2) + layout overhead (4)
		visibleMax = termHeight - overhead
		if visibleMax < 4 {
			visibleMax = 4
		}
	}
	t.MaxVisible = visibleMax

	visibleLines := allLines
	if len(allLines) > visibleMax {
		start := t.Scroll
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

	return renderModalFrame(termWidth, termHeight, t.Width, 80, 48, 90, colorBrand, strings.Join(visibleLines, "\n"))
}
