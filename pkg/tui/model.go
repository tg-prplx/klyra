package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Color palette
// ---------------------------------------------------------------------------

var (
	colorBrand     = lipgloss.Color("#A78BFA") // violet
	colorBrandDim  = lipgloss.Color("#7C3AED") // deeper violet
	colorBlue      = lipgloss.Color("#60A5FA") // soft blue
	colorText      = lipgloss.Color("#E5E7EB") // off-white
	colorDim       = lipgloss.Color("#6B7280") // warm gray
	colorMuted     = lipgloss.Color("#4B5563") // muted
	colorSeparator = lipgloss.Color("#374151") // charcoal
	colorSurface   = lipgloss.Color("#1F2937") // dark surface
	colorEmerald   = lipgloss.Color("#34D399") // green
	colorAmber     = lipgloss.Color("#FBBF24") // amber
	colorRed       = lipgloss.Color("#F87171") // soft red
	colorBadgeBg   = lipgloss.Color("#312E81") // indigo dark bg
	colorBadgeBg2  = lipgloss.Color("#1E3A5F") // blue dark bg
	colorBadgeBg3  = lipgloss.Color("#3B1F5E") // purple dark bg
	colorBadgeBg4  = lipgloss.Color("#1A3636") // teal dark bg
	colorWhite     = lipgloss.Color("#F9FAFB") // near-white
	colorInputBg   = lipgloss.Color("#111827") // very dark bg
)

var mouseEscapePattern = regexp.MustCompile(`(?:\x1b\[|\[)?<\d+;\d+;\d+[mM]`)
var mouseEscapeFragmentPattern = regexp.MustCompile(`\[{3,}`)

// ---------------------------------------------------------------------------
// Spinner
// ---------------------------------------------------------------------------

// Saturated gradient animation for the thinking indicator.
var gradientPalette = []lipgloss.Color{
	"#A855F7", // purple
	"#8B5CF6", // violet
	"#6366F1", // indigo
	"#3B82F6", // blue
	"#0EA5E9", // sky
	"#06B6D4", // cyan
}

// Block density chars for soft-edge rendering.
var densityChars = []rune{'░', '▒', '▓', '█'}

const animTotalFrames = 56 // LCM-ish of palette and pulse lengths

type spinnerTickMsg time.Time

func tickSpinner() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return spinnerTickMsg(t)
	})
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type Handler func(string) (string, error)

type PickerProvider func(field string) (PickerModal, error)

type InterruptFunc func() bool

type StreamMsg string

type ReasoningMsg string

type ToolStreamMsg struct {
	Index     int
	ID        string
	Name      string
	Arguments string
}

type ToolProgressMsg struct {
	Phase  string
	Tool   string
	ID     string
	Args   map[string]any
	Output string
	Error  string
}

type ApprovalRequestMsg struct {
	Tool   string
	Risk   string
	Reason string
	Args   map[string]any
	Reply  chan bool
}

type CommandDef struct {
	Name        string
	Description string
}

type Config struct {
	CWD                    string
	Title                  string
	SessionID              string
	Provider               string
	Model                  string
	BaseURL                string
	Reasoning              string
	Sandbox                string
	Approval               string
	Mode                   string
	StoreResponses         bool
	CartCount              int
	MaxContext             int
	MaxOutput              int
	MaxSteps               int
	MaxMessages            int
	MaxInstructions        int
	ContextCockpit         bool
	ContextCockpitInject   bool
	ContextCockpitTokens   int
	ContextCockpitMaxFiles int
	ContextCockpitDiff     bool
	ContextRecipes         bool
	NegativeContext        bool
	FastModel              string
	EditModel              string
	DeepModel              string
	Handler                Handler
	Interrupt              InterruptFunc
	PickerProvider         PickerProvider
	Commands               []CommandDef
	InitialLines           []string
	SidebarFiles           []string
	SidebarDiff            string
	SidebarPosition        int // 0=left, 1=right
}

type modalKind int

const (
	modalNone modalKind = iota
	modalPicker
	modalSettings
	modalHelp
)

type Model struct {
	cwd                    string
	title                  string
	sessionID              string
	provider               string
	model                  string
	baseURL                string
	reasoning              string
	sandbox                string
	approval               string
	mode                   string
	storeResponses         bool
	cartCount              int
	maxContext             int
	maxOutput              int
	maxSteps               int
	maxMessages            int
	maxInstructions        int
	contextCockpit         bool
	contextCockpitInject   bool
	contextCockpitTokens   int
	contextCockpitMaxFiles int
	contextCockpitDiff     bool
	contextRecipes         bool
	negativeContext        bool
	fastModel              string
	editModel              string
	deepModel              string
	handler                Handler
	interrupt              InterruptFunc
	pickerProvider         PickerProvider
	input                  textinput.Model
	lines                  []string
	width                  int
	height                 int
	busy                   bool
	err                    error
	commands               []CommandDef
	filteredCmds           []CommandDef
	selectedCmdIdx         int
	streamBuf              string
	renderer               *glamour.TermRenderer
	approvalReq            *ApprovalRequestMsg
	spinnerFrame           int
	viewport               viewport.Model
	contextDebug           string
	debugExpanded          bool
	history                []string
	historyIdx             int
	tempInput              string
	reasoningText          string
	reasonExpanded         bool
	copyMode               bool
	interrupted            bool
	mouseFragmentTTL       int
	sidebarVisible         bool
	sidebarMode            int
	sidebarFiles           []string
	sidebarDiff            string
	sidebarPosition        int // 0=left, 1=right
	sidebarScroll          int // scroll offset for sidebar content
	sidebarCursor          int // selected item in sidebar (-1 = none)

	// Modal state
	activeModal   modalKind
	pickerModal   *PickerModal
	helpModal     *HelpModal
	settingsModal *SettingsModal
}

type responseMsg struct {
	input    string
	output   string
	err      error
	agentRun bool
}

type pickerLoadedMsg struct {
	picker PickerModal
	err    error
}

type SessionLoadedMsg struct {
	SessionID string
	Lines     []string
}

func New(cfg Config) Model {
	input := textinput.New()
	input.Placeholder = "Ask anything or type / for commands..."
	input.Prompt = "  > "
	input.PromptStyle = lipgloss.NewStyle().Foreground(colorBrand).Bold(true)
	input.TextStyle = lipgloss.NewStyle().Foreground(colorText)
	input.Cursor.Style = lipgloss.NewStyle().Foreground(colorBrand)
	input.Cursor.Blink = false
	input.PlaceholderStyle = lipgloss.NewStyle().Foreground(colorMuted).Italic(true)
	input.Focus()
	input.CharLimit = 8000

	title := cfg.Title
	if strings.TrimSpace(title) == "" {
		title = "Klyra"
	}
	handler := cfg.Handler
	if handler == nil {
		handler = func(string) (string, error) { return "", nil }
	}

	renderer, _ := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithPreservedNewLines(),
		glamour.WithWordWrap(80),
	)

	m := Model{
		cwd:                    cfg.CWD,
		title:                  title,
		sessionID:              cfg.SessionID,
		provider:               cfg.Provider,
		model:                  cfg.Model,
		baseURL:                cfg.BaseURL,
		reasoning:              cfg.Reasoning,
		sandbox:                cfg.Sandbox,
		approval:               cfg.Approval,
		mode:                   cfg.Mode,
		storeResponses:         cfg.StoreResponses,
		cartCount:              cfg.CartCount,
		maxContext:             cfg.MaxContext,
		maxOutput:              cfg.MaxOutput,
		maxSteps:               cfg.MaxSteps,
		maxMessages:            cfg.MaxMessages,
		maxInstructions:        cfg.MaxInstructions,
		contextCockpit:         cfg.ContextCockpit,
		contextCockpitInject:   cfg.ContextCockpitInject,
		contextCockpitTokens:   cfg.ContextCockpitTokens,
		contextCockpitMaxFiles: cfg.ContextCockpitMaxFiles,
		contextCockpitDiff:     cfg.ContextCockpitDiff,
		contextRecipes:         cfg.ContextRecipes,
		negativeContext:        cfg.NegativeContext,
		fastModel:              cfg.FastModel,
		editModel:              cfg.EditModel,
		deepModel:              cfg.DeepModel,
		handler:                handler,
		interrupt:              cfg.Interrupt,
		pickerProvider:         cfg.PickerProvider,
		input:                  input,
		commands:               cfg.Commands,
		filteredCmds:           nil,
		selectedCmdIdx:         0,
		renderer:               renderer,
		lines:                  append([]string(nil), cfg.InitialLines...),
		viewport:               viewport.New(80, 20),
		history:                []string{},
		historyIdx:             0,
		reasoningText:          "",
		reasonExpanded:         false,
		sidebarVisible:         true,
		sidebarFiles:           append([]string(nil), cfg.SidebarFiles...),
		sidebarDiff:            cfg.SidebarDiff,
		sidebarPosition:        cfg.SidebarPosition,
		sidebarScroll:          0,
		sidebarCursor:          -1,
	}
	m.width = 80
	m.height = 24
	m.syncViewport(true)
	return m
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = max(20, msg.Width-6)
		m.renderer, _ = glamour.NewTermRenderer(
			glamour.WithStandardStyle("dark"),
			glamour.WithPreservedNewLines(),
			glamour.WithWordWrap(max(40, m.chatWidth()-8)),
		)
		m.syncViewport(true)
		return m, nil
	case spinnerTickMsg:
		if m.busy {
			m.spinnerFrame = (m.spinnerFrame + 1) % animTotalFrames
			m.syncViewport(false)
			return m, tickSpinner()
		}
		return m, nil
	case tea.MouseMsg:
		// If mouse event is in the sidebar area, consume it entirely so that
		// the viewport below never sees it (prevents chat from scrolling when
		// the user scrolls inside the sidebar).
		if m.shouldRenderSidebar() && m.isInSidebarArea(msg.X) {
			switch msg.Type {
			case tea.MouseLeft:
				if m.handleSidebarClick(msg.X, msg.Y) {
					m.syncViewport(false)
					return m, tea.ClearScreen
				}
			case tea.MouseWheelUp:
				m.sidebarScrollUp(3)
			case tea.MouseWheelDown:
				m.sidebarScrollDown(3)
			}
			// Always consume — never let sidebar mouse events reach the viewport
			return m, nil
		}
		switch msg.Type {
		case tea.MouseLeft:
			if m.handleViewportClick(msg.Y) {
				m.syncViewport(false)
				return m, tea.ClearScreen
			}
		case tea.MouseWheelUp:
			m.viewport.LineUp(3)
			m.mouseFragmentTTL = 8
			return m, nil
		case tea.MouseWheelDown:
			m.viewport.LineDown(3)
			m.mouseFragmentTTL = 8
			return m, nil
		}
	case tea.KeyMsg:
		if m.isMouseEscapeKeyMsg(msg) {
			return m, nil
		}
		if m.mouseFragmentTTL > 0 {
			m.mouseFragmentTTL--
		}

		// Approval prompt takes highest priority
		if m.approvalReq != nil {
			switch msg.String() {
			case "y", "Y", "enter":
				m.approvalReq.Reply <- true
				m.lines = append(m.lines, "system: approved "+m.approvalReq.Tool)
				m.approvalReq = nil
				return m, nil
			case "n", "N", "esc", "ctrl+c":
				m.approvalReq.Reply <- false
				m.lines = append(m.lines, "system: rejected "+m.approvalReq.Tool)
				m.approvalReq = nil
				return m, nil
			}
			return m, nil
		}

		// Modal routing
		if m.activeModal != modalNone {
			return m.updateModal(msg)
		}

		switch msg.String() {
		case "ctrl+c":
			if m.busy && m.interrupt != nil && m.interrupt() {
				m.busy = false
				m.interrupted = true
				m.lines = append(m.lines, "", "system: interrupted current run")
				m.syncViewport(true)
				return m, tea.ClearScreen
			}
			return m, tea.Quit
		case "f2", "ctrl+s":
			m.openSettingsModal()
			return m, nil
		case "f3":
			m.debugExpanded = !m.debugExpanded
			m.syncViewport(m.debugExpanded)
			return m, nil
		case "f4":
			m.toggleLatestThoughts()
			m.syncViewport(true)
			return m, tea.ClearScreen
		case "f6":
			m.copyMode = !m.copyMode
			if m.copyMode {
				return m, tea.Batch(tea.DisableMouse, tea.ExitAltScreen)
			}
			return m, tea.Batch(tea.EnterAltScreen, tea.EnableMouseCellMotion)
		case "f7":
			m.sidebarMode = (m.sidebarMode + 1) % 3
			m.sidebarVisible = true
			m.sidebarScroll = 0
			m.sidebarCursor = -1
			m.rebuildRenderer()
			m.syncViewport(false)
			return m, nil
		case "alt+f7":
			m.sidebarVisible = !m.sidebarVisible
			m.rebuildRenderer()
			m.syncViewport(false)
			return m, nil
		case "f8":
			m.sidebarPosition = (m.sidebarPosition + 1) % 2
			m.syncViewport(false)
			return m, nil
		case "pgup":
			m.viewport.PageUp()
			return m, nil
		case "pgdn":
			m.viewport.PageDown()
			return m, nil
		case "right", "l":
			if m.toggleLatestThoughtsExpand(true) {
				m.syncViewport(true)
				return m, tea.ClearScreen
			}
		case "left", "h":
			if m.toggleLatestThoughtsExpand(false) {
				m.syncViewport(true)
				return m, tea.ClearScreen
			}
		case "shift+up":
			m.viewport.LineUp(1)
			return m, nil
		case "shift+down":
			m.viewport.LineDown(1)
			return m, nil
		case "up":
			if len(m.filteredCmds) > 0 {
				m.selectedCmdIdx--
				if m.selectedCmdIdx < 0 {
					m.selectedCmdIdx = len(m.filteredCmds) - 1
				}
				return m, nil
			}
			return m.historyPrevious()
		case "shift+tab":
			if len(m.filteredCmds) > 0 {
				m.selectedCmdIdx--
				if m.selectedCmdIdx < 0 {
					m.selectedCmdIdx = len(m.filteredCmds) - 1
				}
				return m, nil
			}
		case "down":
			if len(m.filteredCmds) > 0 {
				m.selectedCmdIdx++
				if m.selectedCmdIdx >= len(m.filteredCmds) {
					m.selectedCmdIdx = 0
				}
				return m, nil
			}
			return m.historyNext()
		case "tab":
			if len(m.filteredCmds) > 0 {
				m.selectedCmdIdx++
				if m.selectedCmdIdx >= len(m.filteredCmds) {
					m.selectedCmdIdx = 0
				}
				return m, nil
			}
		case "ctrl+up", "ctrl+p":
			return m.historyPrevious()
		case "ctrl+down", "ctrl+n":
			return m.historyNext()
		case "enter":
			if len(m.filteredCmds) > 0 {
				m.input.SetValue(m.filteredCmds[m.selectedCmdIdx].Name + " ")
				m.input.SetCursor(len(m.input.Value()))
				m.filteredCmds = nil
				return m, nil
			}

			value := strings.TrimSpace(m.input.Value())
			if value == "" {
				if m.toggleLatestThoughts() {
					m.syncViewport(false)
					return m, tea.ClearScreen
				}
				return m, nil
			}
			if len(m.history) == 0 || m.history[len(m.history)-1] != value {
				m.history = append(m.history, value)
			}
			m.historyIdx = len(m.history)
			m.tempInput = ""

			m.input.SetValue("")
			m.filteredCmds = nil
			if value == "/exit" || value == "/quit" {
				return m, tea.Quit
			}
			if handled, cmd := m.handleLocalCommand(value); handled {
				return m, cmd
			}
			if m.busy {
				m.lines = append(m.lines, "", "system: agent is still running; slash commands remain available")
				m.syncViewport(true)
				return m, nil
			}
			m.busy = true
			m.streamBuf = ""
			m.reasoningText = ""
			m.reasonExpanded = false
			m.spinnerFrame = 0
			if len(m.lines) > 0 {
				m.lines = append(m.lines, "")
			}
			m.lines = append(m.lines, "you: "+value)
			m.syncViewport(true)
			return m, tea.Batch(runHandler(m.handler, value, true), tickSpinner())
		}
	case StreamMsg:
		wasAtBottom := m.viewport.AtBottom()
		m.streamBuf += string(msg)
		m.syncViewport(wasAtBottom)
		return m, nil
	case ReasoningMsg:
		wasAtBottom := m.viewport.AtBottom()
		m.reasoningText += string(msg)
		m.syncViewport(wasAtBottom)
		return m, nil
	case ToolStreamMsg:
		wasAtBottom := m.viewport.AtBottom()
		m.flushLiveAssistantSegment()
		m.appendToolStream(msg)
		m.syncViewport(wasAtBottom)
		return m, nil
	case ToolProgressMsg:
		wasAtBottom := m.viewport.AtBottom()
		m.flushLiveAssistantSegment()
		m.appendToolProgress(msg)
		m.syncViewport(wasAtBottom)
		return m, nil
	case ApprovalRequestMsg:
		wasAtBottom := m.viewport.AtBottom()
		m.approvalReq = &msg
		m.syncViewport(wasAtBottom)
		return m, nil
	case pickerLoadedMsg:
		if msg.err != nil {
			m.lines = append(m.lines, "", "error: "+msg.err.Error())
			m.syncViewport(true)
			return m, nil
		}
		m.openPickerModal(msg.picker)
		return m, nil
	case SessionLoadedMsg:
		m.sessionID = msg.SessionID
		m.lines = append([]string(nil), msg.Lines...)
		m.contextDebug = ""
		m.streamBuf = ""
		m.reasoningText = ""
		m.reasonExpanded = false
		m.syncViewport(true)
		return m, nil
	case responseMsg:
		wasAtBottom := m.viewport.AtBottom()
		interrupted := msg.agentRun && m.interrupted && isCancelError(msg.err)
		if msg.agentRun {
			m.busy = false
		}
		m.err = msg.err
		if interrupted {
			m.err = nil
			msg.err = nil
			m.interrupted = false
		} else if msg.agentRun {
			m.interrupted = false
		}
		if msg.agentRun {
			m.flushLiveAssistantSegment()
		}
		if msg.err != nil {
			m.lines = append(m.lines, "")
			m.lines = append(m.lines, "error: "+msg.err.Error())
		}

		outText := strings.TrimSpace(msg.output)
		var debugText string
		if idx := strings.Index(outText, "Context Debugger"); idx >= 0 {
			debugText = strings.TrimSpace(outText[idx:])
			outText = strings.TrimSpace(outText[:idx])
		}
		m.contextDebug = debugText

		if outText != "" {
			m.lines = append(m.lines, "")

			isAgentStream := strings.Contains(outText, "assistant: ") || strings.Contains(outText, "tool: ")
			if !isAgentStream {
				text := outText
				if m.renderer != nil && (strings.HasPrefix(text, "#") || strings.HasPrefix(text, "-") || strings.HasPrefix(text, "*") || strings.Contains(text, "`")) {
					if rendered, errRender := m.renderer.Render(text); errRender == nil {
						text = strings.TrimRight(rendered, " \n\r\t")
					}
				}
				for _, line := range strings.Split(text, "\n") {
					m.lines = append(m.lines, "md: "+line)
				}
			} else {
				var assistantBlock []string
				var mdBlock []string

				flushAssistant := func() {
					if len(assistantBlock) > 0 {
						m.appendThoughtsOutput(m.reasoningText, false)
						m.reasoningText = ""
						m.reasonExpanded = false
						m.appendAgentOutput(strings.Join(assistantBlock, "\n"))
						assistantBlock = nil
					}
				}

				flushMd := func() {
					if len(mdBlock) > 0 {
						text := strings.Join(mdBlock, "\n")
						if m.renderer != nil && (strings.HasPrefix(text, "#") || strings.HasPrefix(text, "-") || strings.HasPrefix(text, "*") || strings.Contains(text, "`")) {
							if rendered, errRender := m.renderer.Render(text); errRender == nil {
								text = strings.TrimRight(rendered, " \n\r\t")
							}
						}
						for _, line := range strings.Split(text, "\n") {
							m.lines = append(m.lines, "md: "+line)
						}
						mdBlock = nil
					}
				}

				inAssistant := false

				for _, line := range strings.Split(outText, "\n") {
					if strings.HasPrefix(line, "assistant: ") {
						flushMd()
						inAssistant = true
						assistantBlock = append(assistantBlock, strings.TrimPrefix(line, "assistant: "))
					} else if strings.HasPrefix(line, "tool: ") || strings.HasPrefix(line, "tool rejected:") || strings.HasPrefix(line, "tool error:") || strings.HasPrefix(line, "usage:") || strings.HasPrefix(line, "policy:") {
						flushAssistant()
						flushMd()
						inAssistant = false
						m.lines = append(m.lines, line)
					} else if strings.TrimSpace(line) == "" {
						if inAssistant {
							if len(assistantBlock) > 0 {
								assistantBlock = append(assistantBlock, "")
							} else {
								m.lines = append(m.lines, "")
							}
						} else {
							if len(mdBlock) > 0 {
								mdBlock = append(mdBlock, "")
							} else {
								m.lines = append(m.lines, "")
							}
						}
					} else {
						if inAssistant {
							assistantBlock = append(assistantBlock, line)
						} else {
							mdBlock = append(mdBlock, line)
						}
					}
				}
				flushAssistant()
				flushMd()
			}
		}
		m.syncViewport(wasAtBottom)
		return m, nil
	}

	var cmd tea.Cmd
	prevVal := m.input.Value()
	m.input, cmd = m.input.Update(msg)
	if cleaned := sanitizeMouseEscapes(m.input.Value()); cleaned != m.input.Value() {
		m.input.SetValue(cleaned)
		m.input.SetCursor(len(cleaned))
	}

	if m.input.Value() != prevVal {
		m.updateCompletions()
	}

	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	m.syncViewport(false)

	return m, tea.Batch(cmd, vpCmd)
}

func (m *Model) updateCompletions() {
	val := m.input.Value()
	m.filteredCmds = nil
	m.selectedCmdIdx = 0
	if strings.HasPrefix(val, "/") {
		for _, c := range m.commands {
			if strings.HasPrefix(c.Name, val) {
				m.filteredCmds = append(m.filteredCmds, c)
			}
		}
	}
}

func (m Model) isMouseEscapeKeyMsg(msg tea.KeyMsg) bool {
	if mouseEscapePattern.MatchString(msg.String()) {
		return true
	}
	if len(msg.Runes) == 0 {
		return false
	}
	text := string(msg.Runes)
	return mouseEscapePattern.MatchString(text) || (m.mouseFragmentTTL > 0 && isMouseEscapeFragment(text))
}

func sanitizeMouseEscapes(value string) string {
	value = mouseEscapePattern.ReplaceAllString(value, "")
	return mouseEscapeFragmentPattern.ReplaceAllString(value, "")
}

func isMouseEscapeFragment(text string) bool {
	if mouseEscapeFragmentPattern.MatchString(text) {
		return true
	}
	return strings.Trim(text, "[") == ""
}

func isCancelError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "context canceled") ||
		strings.Contains(text, "context cancelled") ||
		strings.Contains(text, "operation was canceled") ||
		strings.Contains(text, "operation was cancelled")
}

func (m Model) calculateBodyHeight() int {
	footerHeight := 2 // separator + footer text
	inputHeight := 2  // separator + input text
	autocompleteHeight := 0
	if len(m.filteredCmds) > 0 {
		maxItems := 5
		itemsCount := len(m.filteredCmds)
		if itemsCount > maxItems {
			itemsCount = maxItems
		}
		autocompleteHeight = itemsCount + 3
	}
	bodyHeight := m.height - footerHeight - inputHeight - autocompleteHeight
	if bodyHeight < 5 {
		bodyHeight = 5
	}
	return bodyHeight
}

type formattedLineItem struct {
	text   string
	source int
}

const (
	lineSourceNone         = -1
	lineSourceLiveThoughts = -2
)

func (m Model) buildFormattedLines() []string {
	items := m.buildFormattedLineItems()
	lines := make([]string, 0, len(items))
	for _, item := range items {
		lines = append(lines, item.text)
	}
	return lines
}

func (m Model) buildFormattedLineItems() []formattedLineItem {
	headerLines := strings.Split(m.renderHeader(), "\n")

	var items []formattedLineItem
	add := func(source int, lines ...string) {
		for _, line := range lines {
			items = append(items, formattedLineItem{text: line, source: source})
		}
	}
	add(lineSourceNone, headerLines...)
	add(lineSourceNone, "") // breathing room after header

	for idx, line := range m.lines {
		if strings.HasPrefix(line, "you: ") {
			add(idx, userMsgStyle.Render("  "+userPrefix+" "+line[5:]))
		} else if strings.HasPrefix(line, "agent: ") {
			text := line[7:]
			if m.renderer != nil {
				if rendered, err := m.renderer.Render(text); err == nil {
					text = strings.TrimRight(rendered, " \n\r\t")
				}
			}
			agentLines := strings.Split(text, "\n")
			for _, al := range agentLines {
				add(idx, agentBarStyle.Render(agentBar)+" "+agentMsgStyle.Render(al)+"\x1b[0m")
			}
		} else if strings.HasPrefix(line, "thoughts:") {
			add(idx, m.renderThoughtBlock(line)...)
		} else if strings.HasPrefix(line, "error: ") {
			add(idx, errorMsgStyle.Render("  "+errorPrefix+" "+line[7:]))
		} else if strings.HasPrefix(line, "system: ") {
			add(idx, systemMsgStyle.Render("  "+systemPrefix+" "+line[8:]))
		} else if strings.HasPrefix(line, "toolstream:") {
			add(idx, m.renderToolStreamLine(line)...)
		} else if strings.HasPrefix(line, "toolprogress:") {
			add(idx, m.renderToolProgressLine(line)...)
		} else if strings.HasPrefix(line, "tool:") {
			add(idx, m.renderToolLine(line)...)
		} else if strings.HasPrefix(line, "tool rejected: ") {
			add(idx, lipgloss.NewStyle().Foreground(colorRed).Bold(true).Render("  tool rejected: "+line[15:]))
		} else if strings.HasPrefix(line, "tool error: ") {
			add(idx, lipgloss.NewStyle().Foreground(colorRed).Bold(true).Render("  tool error: "+line[12:]))
		} else if strings.HasPrefix(line, "usage: ") {
			add(idx, lipgloss.NewStyle().Foreground(colorDim).Render("  usage: "+line[7:]))
		} else if strings.HasPrefix(line, "policy: ") {
			add(idx, lipgloss.NewStyle().Foreground(colorAmber).Render("  policy: "+line[8:]))
		} else if strings.HasPrefix(line, "md: ") {
			add(idx, renderCommandOutputLine(line[4:]))
		} else if line == "" {
			add(idx, "")
		} else {
			add(idx, systemMsgStyle.Render("  "+systemPrefix+" "+line))
		}
	}

	// Streaming content with spinner
	if m.busy && m.streamBuf != "" {
		add(lineSourceNone, "")
		if strings.TrimSpace(m.reasoningText) != "" {
			add(lineSourceLiveThoughts, m.renderLiveThoughtBlock()...)
			add(lineSourceNone, "")
		}

		var rendered string
		var err error
		if m.renderer != nil {
			rendered, err = m.renderer.Render(m.streamBuf)
		} else {
			err = fmt.Errorf("no renderer")
		}

		var agentLines []string
		if err == nil {
			agentLines = strings.Split(strings.TrimRight(rendered, "\n"), "\n")
		} else {
			agentLines = strings.Split(m.streamBuf, "\n")
		}

		for _, al := range agentLines {
			add(lineSourceNone, agentBarStyle.Render(agentBar)+" "+agentMsgStyle.Render(al)+"\x1b[0m")
		}
	} else if m.busy {
		add(lineSourceNone, "")
		if strings.TrimSpace(m.reasoningText) != "" {
			add(lineSourceLiveThoughts, m.renderLiveThoughtBlock()...)
			add(lineSourceNone, "")
		}
		add(lineSourceNone, m.renderThinkingBar())
	}

	if m.contextDebug != "" {
		if m.debugExpanded {
			add(lineSourceNone, "")

			// Render the debugger text via Glamour
			text := m.contextDebug
			if m.renderer != nil {
				if rendered, errRender := m.renderer.Render(text); errRender == nil {
					text = strings.TrimRight(rendered, " \n\r\t")
				}
			}
			for _, line := range strings.Split(text, "\n") {
				add(lineSourceNone, "  "+line)
			}
		}
	}

	return items
}

func (m *Model) syncViewport(scrollToBottom bool) {
	m.viewport.Width = m.chatWidth()
	m.viewport.Height = m.calculateBodyHeight()

	lines := m.buildFormattedLines()
	padding := m.viewport.Height - len(lines)
	if padding > 0 {
		paddedLines := make([]string, padding)
		for i := range paddedLines {
			paddedLines[i] = ""
		}
		lines = append(paddedLines, lines...)
	}

	m.viewport.SetContent(strings.Join(lines, "\n"))
	if scrollToBottom {
		m.viewport.GotoBottom()
	}
}

func (m Model) shouldRenderSidebar() bool {
	return m.sidebarVisible && m.width >= 100
}

func (m *Model) rebuildRenderer() {
	m.renderer, _ = glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithPreservedNewLines(),
		glamour.WithWordWrap(max(40, m.chatWidth()-8)),
	)
}

func (m Model) sidebarWidth() int {
	if !m.shouldRenderSidebar() {
		return 0
	}
	return min(34, max(26, m.width/4))
}

func (m Model) chatWidth() int {
	width := m.width - m.sidebarWidth()
	if width < 48 {
		return max(20, m.width)
	}
	return width
}

func (m Model) renderSidebar(height int) string {
	width := m.sidebarWidth()
	if width <= 0 {
		return ""
	}
	titleStyle := lipgloss.NewStyle().Foreground(colorBrand).Bold(true)
	tabActive := lipgloss.NewStyle().Foreground(colorWhite).Background(colorBadgeBg).Padding(0, 1)
	tabIdle := lipgloss.NewStyle().Foreground(colorMuted).Padding(0, 1)
	labelStyle := lipgloss.NewStyle().Foreground(colorMuted)
	scrollIndicator := lipgloss.NewStyle().Foreground(colorBrandDim)

	tabs := []string{"files", "diff", "context"}
	var tabLine []string
	for i, tab := range tabs {
		if i == m.sidebarMode {
			tabLine = append(tabLine, tabActive.Render(tab))
		} else {
			tabLine = append(tabLine, tabIdle.Render(tab))
		}
	}

	posLabel := "◀"
	if m.sidebarPosition == 1 {
		posLabel = "▶"
	}

	headerLines := []string{
		titleStyle.Render(" Klyra Sidebar") + " " + lipgloss.NewStyle().Foreground(colorMuted).Render(posLabel),
		strings.Join(tabLine, ""),
		"",
	}

	var contentLines []string
	switch m.sidebarMode {
	case 1:
		contentLines = m.sidebarDiffLines(width - 4)
	case 2:
		contentLines = m.sidebarContextLines(width - 4)
	default:
		contentLines = m.sidebarFileLines(width - 4)
	}

	footerLines := []string{
		"",
		labelStyle.Render("F7 next · F8 " + []string{"→right", "→left"}[m.sidebarPosition]),
	}

	// Calculate scrollable area height
	scrollAreaHeight := max(1, height-len(headerLines)-len(footerLines)-2)

	// Apply scroll offset
	scroll := m.sidebarScroll
	if scroll > len(contentLines)-scrollAreaHeight {
		scroll = max(0, len(contentLines)-scrollAreaHeight)
	}
	if scroll < 0 {
		scroll = 0
	}

	// Build visible content with scroll
	var visibleContent []string
	if scroll > 0 {
		visibleContent = append(visibleContent, scrollIndicator.Render("  ▲ "+fmt.Sprintf("%d more", scroll)))
		scrollAreaHeight--
	}
	endIdx := scroll + scrollAreaHeight
	if endIdx > len(contentLines) {
		endIdx = len(contentLines)
	}
	for i := scroll; i < endIdx; i++ {
		visibleContent = append(visibleContent, contentLines[i])
	}
	remaining := len(contentLines) - endIdx
	if remaining > 0 {
		visibleContent = append(visibleContent, scrollIndicator.Render("  ▼ "+fmt.Sprintf("%d more", remaining)))
	}

	var lines []string
	lines = append(lines, headerLines...)
	lines = append(lines, visibleContent...)
	lines = append(lines, footerLines...)

	content := strings.Join(lines, "\n")

	// Position-aware border: left sidebar has right border, right sidebar has left border
	borderRight := m.sidebarPosition == 0
	borderLeft := m.sidebarPosition == 1
	return lipgloss.NewStyle().
		Width(width-1).
		Height(height).
		Border(lipgloss.NormalBorder(), false, borderRight, false, borderLeft).
		BorderForeground(colorSeparator).
		Padding(0, 1).
		Render(content)
}

func (m Model) sidebarFileLines(width int) []string {
	if len(m.sidebarFiles) == 0 {
		return []string{lipgloss.NewStyle().Foreground(colorDim).Render("no file snapshot")}
	}
	headerStyle := lipgloss.NewStyle().Foreground(colorMuted)
	fileStyle := lipgloss.NewStyle().Foreground(colorText)
	selectedStyle := lipgloss.NewStyle().Foreground(colorWhite).Background(colorBadgeBg)
	countStyle := lipgloss.NewStyle().Foreground(colorDim)
	lines := []string{headerStyle.Render("▪ workspace files") + " " + countStyle.Render(fmt.Sprintf("(%d)", len(m.sidebarFiles)))}
	for idx, file := range m.sidebarFiles {
		file = strings.TrimSpace(file)
		if file == "" {
			continue
		}
		icon := fileIcon(file)
		entry := icon + " " + shorten(file, max(8, width-4))
		if idx == m.sidebarCursor {
			lines = append(lines, selectedStyle.Render(" ▸ "+entry+" "))
		} else {
			lines = append(lines, fileStyle.Render("  "+entry))
		}
	}
	return lines
}

func (m Model) sidebarDiffLines(width int) []string {
	diff := strings.TrimSpace(m.sidebarDiff)
	if diff == "" || diff == "no tracked diff" {
		return []string{lipgloss.NewStyle().Foreground(colorDim).Render("no tracked diff")}
	}
	lines := []string{lipgloss.NewStyle().Foreground(colorMuted).Render("tracked diff")}
	for _, line := range strings.Split(diff, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		style := lipgloss.NewStyle().Foreground(colorDim)
		if strings.HasPrefix(line, "+") {
			style = lipgloss.NewStyle().Foreground(colorEmerald)
		} else if strings.HasPrefix(line, "-") {
			style = lipgloss.NewStyle().Foreground(colorRed)
		} else if strings.HasPrefix(line, "diff ") || strings.HasPrefix(line, "@@") {
			style = lipgloss.NewStyle().Foreground(colorAmber)
		}
		lines = append(lines, style.Render(shorten(line, max(8, width))))
	}
	return lines
}

func (m Model) sidebarContextLines(width int) []string {
	lines := []string{
		lipgloss.NewStyle().Foreground(colorMuted).Render("context"),
		"  cart files: " + fmt.Sprintf("%d", m.cartCount),
		"  cockpit: " + onOff(m.contextCockpit),
		"  recipes: " + onOff(m.contextRecipes),
		"  negative: " + onOff(m.negativeContext),
		"  budget: " + formatNumber(m.contextCockpitTokens) + " / " + fmt.Sprintf("%d files", m.contextCockpitMaxFiles),
		"",
		lipgloss.NewStyle().Foreground(colorMuted).Render("runtime"),
		"  mode: " + valueOr(m.mode, "edit"),
		"  sandbox: " + valueOr(m.sandbox, "workspace-write"),
		"  approval: " + valueOr(m.approval, "auto"),
	}
	if strings.TrimSpace(m.contextDebug) != "" {
		lines = append(lines, "", lipgloss.NewStyle().Foreground(colorMuted).Render("debug"), "  available via F3")
	}
	for i, line := range lines {
		lines[i] = shorten(line, max(8, width))
	}
	return lines
}
// ---------------------------------------------------------------------------
// Sidebar helpers: icons, mouse, scroll
// ---------------------------------------------------------------------------

// fileIcon returns an appropriate icon for a file path based on its extension or name.
func fileIcon(name string) string {
	// Check if it looks like a directory (ends with / or has no extension)
	if strings.HasSuffix(name, "/") {
		return "▸"
	}

	ext := strings.ToLower(filepath.Ext(name))
	base := strings.ToLower(filepath.Base(name))

	// Special filenames
	switch base {
	case "dockerfile", "containerfile":
		return "◈"
	case "makefile", "cmakelists.txt":
		return "⚙"
	case ".gitignore", ".gitmodules", ".gitattributes":
		return "±"
	case "license", "licence", "license.md", "licence.md":
		return "§"
	case "readme.md", "readme", "readme.txt":
		return "¶"
	case "go.mod", "go.sum":
		return "⊕"
	case "package.json", "package-lock.json":
		return "◫"
	case "cargo.toml", "cargo.lock":
		return "◫"
	case ".env", ".env.local", ".env.example":
		return "⊘"
	}

	// Extension-based icons
	switch ext {
	case ".go":
		return "◆"
	case ".py":
		return "◇"
	case ".js", ".jsx", ".mjs":
		return "●"
	case ".ts", ".tsx":
		return "◉"
	case ".rs":
		return "⬥"
	case ".rb":
		return "◈"
	case ".java", ".kt", ".kts":
		return "○"
	case ".c", ".h":
		return "■"
	case ".cpp", ".cc", ".cxx", ".hpp":
		return "■"
	case ".cs":
		return "□"
	case ".swift":
		return "▪"
	case ".html", ".htm":
		return "◌"
	case ".css", ".scss", ".sass", ".less":
		return "◍"
	case ".json":
		return "⊞"
	case ".yaml", ".yml":
		return "≡"
	case ".toml":
		return "≡"
	case ".xml":
		return "⊟"
	case ".md", ".markdown":
		return "¶"
	case ".txt":
		return "─"
	case ".sh", ".bash", ".zsh", ".fish":
		return "$"
	case ".sql":
		return "⊡"
	case ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".ico":
		return "◩"
	case ".mp3", ".wav", ".ogg", ".flac":
		return "♫"
	case ".mp4", ".avi", ".mov", ".mkv", ".webm":
		return "▶"
	case ".zip", ".tar", ".gz", ".bz2", ".xz", ".rar", ".7z":
		return "◫"
	case ".pdf":
		return "▥"
	case ".doc", ".docx":
		return "▤"
	case ".xls", ".xlsx":
		return "▦"
	case ".log":
		return "≋"
	case ".lock":
		return "⊗"
	case ".mod", ".sum":
		return "⊕"
	case ".proto":
		return "⚡"
	case ".graphql", ".gql":
		return "⬡"
	case ".test.go", ".spec.js", ".spec.ts", ".test.js", ".test.ts":
		return "✓"
	default:
		if ext == "" {
			return "▸" // likely a directory
		}
		return "○"
	}
}

// isInSidebarArea checks if the given X coordinate falls within the sidebar region.
func (m Model) isInSidebarArea(x int) bool {
	sw := m.sidebarWidth()
	if sw <= 0 {
		return false
	}
	if m.sidebarPosition == 0 {
		// Sidebar on the left
		return x < sw
	}
	// Sidebar on the right
	return x >= m.width-sw
}

// handleSidebarClick processes a mouse click within the sidebar area.
// It checks whether the click is on a tab header or on a file item.
func (m *Model) handleSidebarClick(x, y int) bool {
	// y==0 is the title line, y==1 is the tab bar
	if y == 1 {
		// Click on tab bar — cycle sidebar mode
		m.sidebarMode = (m.sidebarMode + 1) % 3
		m.sidebarScroll = 0
		m.sidebarCursor = -1
		return true
	}

	// Only handle file clicks in files mode
	if m.sidebarMode != 0 || len(m.sidebarFiles) == 0 {
		return false
	}

	// Header lines in renderSidebar: title(0), tabs(1), blank(2), then "workspace files" header(3)
	// File items start at y==4 relative to sidebar top
	contentStartY := 4
	if m.sidebarScroll > 0 {
		// There's a "▲ N more" line, so content shifts down by 1
		contentStartY++
	}

	fileIdx := (y - contentStartY) + m.sidebarScroll
	if fileIdx >= 0 && fileIdx < len(m.sidebarFiles) {
		if m.sidebarCursor == fileIdx {
			m.sidebarCursor = -1 // deselect on second click
		} else {
			m.sidebarCursor = fileIdx
		}
		return true
	}
	return false
}

// sidebarScrollUp scrolls the sidebar content up by n lines.
func (m *Model) sidebarScrollUp(n int) {
	m.sidebarScroll -= n
	if m.sidebarScroll < 0 {
		m.sidebarScroll = 0
	}
}

// sidebarScrollDown scrolls the sidebar content down by n lines.
func (m *Model) sidebarScrollDown(n int) {
	m.sidebarScroll += n
	maxScroll := m.sidebarContentLineCount() - 5
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.sidebarScroll > maxScroll {
		m.sidebarScroll = maxScroll
	}
}

// sidebarContentLineCount returns the total number of content lines for the current sidebar mode.
func (m Model) sidebarContentLineCount() int {
	width := m.sidebarWidth() - 4
	switch m.sidebarMode {
	case 1:
		return len(m.sidebarDiffLines(width))
	case 2:
		return len(m.sidebarContextLines(width))
	default:
		return len(m.sidebarFileLines(width))
	}
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

func (m Model) View() string {
	// Autocomplete
	var autocomplete string
	if len(m.filteredCmds) > 0 {
		autocomplete, _ = m.renderAutocomplete()
	}

	// Footer
	footer := m.renderFooter()

	// Input with separator
	inputSep := lipgloss.NewStyle().Foreground(colorSeparator).Render(strings.Repeat("─", max(10, m.width)))
	inputView := inputSep + "\n" + m.input.View()

	// Body from viewport
	body := m.viewport.View()
	if m.shouldRenderSidebar() && m.activeModal == modalNone && m.approvalReq == nil {
		sidebar := m.renderSidebar(m.viewport.Height)
		if m.sidebarPosition == 1 {
			body = lipgloss.JoinHorizontal(lipgloss.Top, body, sidebar)
		} else {
			body = lipgloss.JoinHorizontal(lipgloss.Top, sidebar, body)
		}
	}

	// Overlays: approval prompt
	if m.approvalReq != nil {
		body = centerOverlay(m.width, m.viewport.Height, m.renderApprovalModal())
	}

	// Modal overlays
	switch m.activeModal {
	case modalPicker:
		if m.pickerModal != nil {
			body = centerOverlay(m.width, m.viewport.Height, m.pickerModal.View(m.width))
		}
	case modalHelp:
		if m.helpModal != nil {
			body = centerOverlay(m.width, m.viewport.Height, m.helpModal.View(m.width, m.height))
		}
	case modalSettings:
		if m.settingsModal != nil {
			body = centerOverlay(m.width, m.viewport.Height, m.settingsModal.View(m.width, m.height))
		}
	}

	if autocomplete != "" {
		return lipgloss.JoinVertical(lipgloss.Left,
			body,
			autocomplete,
			inputView,
			footer,
		)
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		body,
		inputView,
		footer,
	)
}

// ---------------------------------------------------------------------------
// Autocomplete
// ---------------------------------------------------------------------------

func (m Model) renderAutocomplete() (string, int) {
	var lines []string
	maxItems := 5
	startIdx := m.selectedCmdIdx - maxItems/2
	if startIdx < 0 {
		startIdx = 0
	}
	if startIdx+maxItems > len(m.filteredCmds) {
		startIdx = len(m.filteredCmds) - maxItems
		if startIdx < 0 {
			startIdx = 0
		}
	}

	for i := startIdx; i < startIdx+maxItems && i < len(m.filteredCmds); i++ {
		c := m.filteredCmds[i]
		if i == m.selectedCmdIdx {
			line := acSelectedStyle.Render(fmt.Sprintf(" %s %-18s %s ", acPointer, c.Name, c.Description))
			lines = append(lines, line)
		} else {
			line := acItemStyle.Render(fmt.Sprintf("   %-18s %s ", c.Name, c.Description))
			lines = append(lines, line)
		}
	}

	// Hint line
	hint := acHintStyle.Render("  up/down navigate  enter select  esc dismiss")
	lines = append(lines, hint)

	content := strings.Join(lines, "\n")

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorMuted).
		Render(content)

	return box, lipgloss.Height(box)
}

// ---------------------------------------------------------------------------
// Approval modal
// ---------------------------------------------------------------------------

func (m Model) renderApprovalModal() string {
	req := m.approvalReq
	if req == nil {
		return ""
	}

	titleStyle := lipgloss.NewStyle().Foreground(colorAmber).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(colorDim)
	valueStyle := lipgloss.NewStyle().Foreground(colorText).Bold(true)
	keyStyle := lipgloss.NewStyle().Foreground(colorEmerald).Bold(true)
	keyRejectStyle := lipgloss.NewStyle().Foreground(colorRed).Bold(true)

	lines := []string{
		titleStyle.Render("Approval required"),
		"",
		labelStyle.Render("tool:   ") + valueStyle.Render(req.Tool),
	}
	if req.Risk != "" {
		lines = append(lines, labelStyle.Render("risk:   ")+valueStyle.Render(req.Risk))
	}
	if req.Reason != "" {
		lines = append(lines, labelStyle.Render("reason: ")+valueStyle.Render(req.Reason))
	}
	lines = append(lines, "")
	lines = append(lines, keyStyle.Render("[Y] Approve")+"  "+keyRejectStyle.Render("[N] Reject"))

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorAmber).
		Foreground(colorText).
		Padding(1, 2).
		Render(strings.Join(lines, "\n"))
}

// ---------------------------------------------------------------------------
// Modal management
// ---------------------------------------------------------------------------

func (m *Model) openSettingsModal() {
	sm := NewSettingsModal(
		valueOr(m.provider, "mock"),
		m.model,
		m.baseURL,
		m.reasoning,
		valueOr(m.approval, "auto"),
		valueOr(m.sandbox, "workspace-write"),
		valueOr(m.mode, "edit"),
		m.storeResponses,
		m.maxContext, m.maxOutput, m.maxSteps, m.maxMessages, m.maxInstructions,
		m.contextCockpit, m.contextCockpitInject,
		m.contextCockpitTokens, m.contextCockpitMaxFiles, m.contextCockpitDiff,
		m.contextRecipes, m.negativeContext,
		m.fastModel, m.editModel, m.deepModel,
	)
	m.settingsModal = &sm
	m.activeModal = modalSettings
}

func (m *Model) openPickerModal(picker PickerModal) {
	m.pickerModal = &picker
	m.activeModal = modalPicker
}

func (m *Model) openHelpModal() {
	hm := NewHelpModal(m.commands)
	m.helpModal = &hm
	m.activeModal = modalHelp
}

func (m *Model) closeModal() {
	m.activeModal = modalNone
	m.pickerModal = nil
	m.helpModal = nil
	m.settingsModal = nil
}

func (m Model) updateModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.activeModal {
	case modalPicker:
		return m.updatePickerModal(msg)
	case modalHelp:
		return m.updateHelpModal(msg)
	case modalSettings:
		return m.updateSettingsModal(msg)
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Picker modal update
// ---------------------------------------------------------------------------

func (m Model) updatePickerModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.pickerModal == nil {
		m.closeModal()
		return m, nil
	}
	switch msg.String() {
	case "esc", "ctrl+c":
		m.closeModal()
		return m, nil
	case "up", "k":
		m.pickerModal.MoveUp()
		return m, nil
	case "down", "j":
		m.pickerModal.MoveDown()
		return m, nil
	case "enter":
		value := m.pickerModal.SelectedValue()
		field := m.pickerModal.Field
		m.closeModal()

		// Apply optimistically
		switch field {
		case "approval":
			m.approval = value
		case "provider":
			m.provider = value
			m.model = ""
		case "sandbox":
			m.sandbox = value
		case "mode":
			m.mode = value
		case "reasoning":
			m.reasoning = value
		case "session":
			m.sessionID = value
		}

		// Send to handler
		cmdName := field
		if field == "checkpoint" && value == "restore" && m.pickerProvider != nil {
			return m, runPickerProvider(m.pickerProvider, "checkpoint_restore")
		}
		if field == "checkpoint_restore" {
			cmdText := "/checkpoint restore " + value
			m.lines = append(m.lines, "system: checkpoint restore → "+valueOr(value, "default"))
			m.syncViewport(true)
			return m, runHandler(m.handler, cmdText, false)
		}
		if field == "session" {
			cmdName = "session"
		}
		cmdText := "/" + cmdName + " " + value
		m.lines = append(m.lines, "system: "+field+" → "+valueOr(value, "default"))
		m.syncViewport(true)
		return m, runHandler(m.handler, cmdText, false)
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Help modal update
// ---------------------------------------------------------------------------

func (m Model) updateHelpModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.helpModal == nil {
		m.closeModal()
		return m, nil
	}
	switch msg.String() {
	case "esc", "ctrl+c", "q":
		m.closeModal()
		return m, nil
	case "up", "k":
		m.helpModal.ScrollUp()
		return m, nil
	case "down", "j":
		// Calculate total lines for scroll bounds
		total := 0
		for _, cat := range m.helpModal.Categories {
			total += 2 + len(cat.Commands) // header + blank + commands
		}
		total += 8 // keyboard shortcuts section + padding
		m.helpModal.ScrollDown(total)
		return m, nil
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Settings modal update
// ---------------------------------------------------------------------------

func (m Model) updateSettingsModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.settingsModal == nil {
		m.closeModal()
		return m, nil
	}
	sm := m.settingsModal

	// If editing a text field, handle specially
	if sm.Editing {
		switch msg.String() {
		case "esc":
			sm.Editing = false
			return m, nil
		case "enter":
			sm.CommitEdit()
			return m, nil
		case "backspace":
			sm.Backspace()
			return m, nil
		default:
			if len(msg.Runes) > 0 {
				sm.TypeChar(string(msg.Runes))
			}
			return m, nil
		}
	}

	switch msg.String() {
	case "esc":
		m.closeModal()
		return m, nil
	case "tab", "down", "j":
		sm.MoveDown()
		return m, nil
	case "shift+tab", "up", "k":
		sm.MoveUp()
		return m, nil
	case "left", "h":
		sm.CycleLeft()
		return m, nil
	case "right", "l":
		sm.CycleRight()
		return m, nil
	case "backspace":
		sm.Backspace()
		return m, nil
	case "enter":
		if sm.ToggleCurrentSection() {
			return m, nil
		}
		fallthrough
	case "ctrl+s":
		// Save all settings
		m.provider = sm.GetValue("provider")
		m.model = sm.GetValue("model")
		m.baseURL = sm.GetValue("endpoint")
		m.reasoning = sm.GetValue("reasoning")
		m.approval = sm.GetValue("approval")
		m.sandbox = sm.GetValue("sandbox")
		m.mode = sm.GetValue("mode")
		m.storeResponses = sm.GetValue("store") == "on"
		if parsed := parsePositiveInt(sm.GetValue("context")); parsed > 0 {
			m.maxContext = parsed
		}
		if parsed := parsePositiveInt(sm.GetValue("output")); parsed > 0 {
			m.maxOutput = parsed
		}
		if parsed := parsePositiveInt(sm.GetValue("steps")); parsed > 0 {
			m.maxSteps = parsed
		}
		if parsed := parsePositiveInt(sm.GetValue("messages")); parsed > 0 {
			m.maxMessages = parsed
		}
		if parsed := parsePositiveInt(sm.GetValue("instructions")); parsed > 0 {
			m.maxInstructions = parsed
		}
		m.contextCockpit = sm.GetValue("context_cockpit") != "off"
		m.contextCockpitInject = sm.GetValue("context_cockpit_inject") != "off"
		if parsed := parsePositiveInt(sm.GetValue("context_cockpit_tokens")); parsed > 0 {
			m.contextCockpitTokens = parsed
		}
		if parsed := parsePositiveInt(sm.GetValue("context_cockpit_files")); parsed > 0 {
			m.contextCockpitMaxFiles = parsed
		}
		m.contextCockpitDiff = sm.GetValue("context_cockpit_diff") != "off"
		m.contextRecipes = sm.GetValue("context_recipes") != "off"
		m.negativeContext = sm.GetValue("negative_context") != "off"
		m.fastModel = sm.GetValue("fast_model")
		m.editModel = sm.GetValue("edit_model")
		m.deepModel = sm.GetValue("deep_model")

		// Build /set command for handler
		parts := []string{"/set",
			"provider=" + m.provider,
			"model=" + m.model,
			"endpoint=" + m.baseURL,
		}
		if strings.TrimSpace(m.reasoning) != "" {
			parts = append(parts, "reasoning="+m.reasoning)
		}
		parts = append(parts,
			"approval="+valueOr(m.approval, "auto"),
			"sandbox="+valueOr(m.sandbox, "workspace-write"),
			"mode="+valueOr(m.mode, "edit"),
			"store="+onOff(m.storeResponses),
			fmt.Sprintf("context=%d", m.maxContext),
			fmt.Sprintf("output=%d", m.maxOutput),
			fmt.Sprintf("steps=%d", m.maxSteps),
			fmt.Sprintf("messages=%d", m.maxMessages),
			fmt.Sprintf("instructions=%d", m.maxInstructions),
			"context_cockpit="+onOff(m.contextCockpit),
			"context_cockpit_inject="+onOff(m.contextCockpitInject),
			fmt.Sprintf("context_cockpit_tokens=%d", m.contextCockpitTokens),
			fmt.Sprintf("context_cockpit_files=%d", m.contextCockpitMaxFiles),
			"context_cockpit_diff="+onOff(m.contextCockpitDiff),
			"context_recipes="+onOff(m.contextRecipes),
			"negative_context="+onOff(m.negativeContext),
		)
		cmdText := strings.Join(parts, " ")

		// Handle API keys — set env vars at runtime
		keysToSave := make(map[string]string)
		for _, envField := range []struct{ name, envVar string }{
			{"openai_key", "OPENAI_API_KEY"},
			{"anthropic_key", "ANTHROPIC_API_KEY"},
			{"gemini_key", "GEMINI_API_KEY"},
		} {
			if val := sm.GetValue(envField.name); val != "" {
				_ = setEnvIfChanged(envField.envVar, val)
				keysToSave[envField.envVar] = val
			}
		}
		if len(keysToSave) > 0 {
			_ = saveEnvFile(m.cwd, keysToSave)
		}

		m.closeModal()
		m.lines = append(m.lines, "system: settings saved")
		m.syncViewport(true)
		return m, runHandler(m.handler, cmdText, false)
	}
	if len(msg.Runes) > 0 {
		sm.TypeChar(string(msg.Runes))
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Header
// ---------------------------------------------------------------------------

func (m Model) renderHeader() string {
	width := max(50, m.width)

	// --- Logo (>< chevrons with gradient bar) ---
	chevronStyle := lipgloss.NewStyle().Foreground(colorWhite).Bold(true)

	colors := []string{"#A855F7", "#8B5CF6", "#6366F1", "#3B82F6", "#0EA5E9", "#06B6D4"}
	var gradientBar strings.Builder
	for _, hex := range colors {
		gradientBar.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Render("█"))
	}

	logoLines := []string{
		chevronStyle.Render("     ██▄                ▄██"),
		chevronStyle.Render("       ██▄            ▄██"),
		chevronStyle.Render("         ██▄        ▄██"),
		chevronStyle.Render("         ▄██        ██▄"),
		chevronStyle.Render("       ▄██            ██▄"),
		chevronStyle.Render("     ▄██     ") + gradientBar.String() + chevronStyle.Render("     ██▄"),
	}

	titleText := m.title
	if strings.TrimSpace(titleText) == "" {
		titleText = "Klyra"
	}
	title := lipgloss.NewStyle().Foreground(colorBrand).Bold(true).Render(titleText)
	subtitle := lipgloss.NewStyle().Foreground(colorDim).Render("  agentic coding workspace")

	// Status
	status := "ready"
	if m.busy {
		status = "thinking"
	}
	if m.err != nil {
		status = "error"
	}
	statusIcon := statusGlyph(status)
	statusBadge := pillBadge(statusIcon+" "+status, statusColor(status), "")

	// Provider & model
	providerBadge := pillBadge(valueOr(m.provider, "mock"), colorBadgeBg, colorBlue)
	modelBadge := pillBadge(valueOr(m.model, "routed"), colorBadgeBg3, colorBrand)
	reasoningBadge := pillBadge("reasoning "+valueOr(m.reasoning, "default"), colorBadgeBg2, colorDim)

	// Safety
	safetyText := valueOr(m.mode, "edit") + " / " + valueOr(m.sandbox, "workspace-write") + " / " + valueOr(m.approval, "auto")
	safetyBadge := pillBadge(safetyText, colorBadgeBg4, colorEmerald)

	// Budget info
	budgetParts := []string{
		lipgloss.NewStyle().Foreground(colorMuted).Render("ctx ") + lipgloss.NewStyle().Foreground(colorDim).Render(formatNumber(m.maxContext)),
		lipgloss.NewStyle().Foreground(colorMuted).Render("out ") + lipgloss.NewStyle().Foreground(colorDim).Render(formatNumber(m.maxOutput)),
		lipgloss.NewStyle().Foreground(colorMuted).Render("cart ") + lipgloss.NewStyle().Foreground(colorDim).Render(fmt.Sprintf("%d", m.cartCount)),
		lipgloss.NewStyle().Foreground(colorMuted).Render("session ") + lipgloss.NewStyle().Foreground(colorDim).Render(valueOr(m.sessionID, "ephemeral")),
	}
	if m.baseURL != "" {
		budgetParts = append(budgetParts, lipgloss.NewStyle().Foreground(colorMuted).Render("endpoint ")+lipgloss.NewStyle().Foreground(colorDim).Render(shorten(m.baseURL, 24)))
	}
	budgets := strings.Join(budgetParts, lipgloss.NewStyle().Foreground(colorSeparator).Render(" · "))

	// Separator bar
	barWidth := max(10, min(width-2, 90))
	bar := lipgloss.NewStyle().Foreground(colorSeparator).Render(strings.Repeat("─", barWidth))

	topLine := lipgloss.JoinHorizontal(lipgloss.Top, title, subtitle)
	badgeLine := lipgloss.JoinHorizontal(lipgloss.Top, statusBadge, " ", providerBadge, " ", modelBadge, " ", reasoningBadge)

	result := []string{""}
	result = append(result, logoLines...)
	result = append(result,
		"",
		"  "+topLine,
		"  "+badgeLine,
		"  "+budgets+"  "+safetyBadge,
		"  "+bar,
	)

	return strings.Join(result, "\n")
}

// ---------------------------------------------------------------------------
// Footer
// ---------------------------------------------------------------------------

func (m Model) renderFooter() string {
	cmdHintStyle := lipgloss.NewStyle().Foreground(colorMuted)
	cmdSlashStyle := lipgloss.NewStyle().Foreground(colorBrandDim)
	modelStyle := lipgloss.NewStyle().Foreground(colorDim)
	sepStyle := lipgloss.NewStyle().Foreground(colorSeparator)

	leftParts := []string{
		cmdSlashStyle.Render("/") + cmdHintStyle.Render("help"),
		cmdSlashStyle.Render("/") + cmdHintStyle.Render("status"),
		cmdSlashStyle.Render("/") + cmdHintStyle.Render("mode"),
		cmdSlashStyle.Render("/") + cmdHintStyle.Render("attach"),
	}
	leftFooter := " " + strings.Join(leftParts, "  ")

	copyHint := "F6 copy"
	if m.copyMode {
		copyHint = "F6 scroll"
	}
	sidebarHint := "F7 sidebar"
	if m.shouldRenderSidebar() {
		sidebarHint = "F7 " + []string{"files", "diff", "context"}[m.sidebarMode]
	}
	posHint := "F8 " + []string{"→right", "→left"}[m.sidebarPosition]
	hints := []string{"F2 settings", sidebarHint, posHint, copyHint}
	if m.contextDebug != "" {
		if m.debugExpanded {
			hints = append([]string{"F3 hide context"}, hints...)
		} else {
			hints = append([]string{"F3 context"}, hints...)
		}
	}
	settingsHint := lipgloss.NewStyle().Foreground(colorMuted).Render(strings.Join(hints, "  "))
	rightFooter := modelStyle.Render(valueOr(m.model, "routed")) + "  " + settingsHint + " "

	separator := sepStyle.Render(strings.Repeat("─", max(10, m.width)))

	return lipgloss.JoinVertical(lipgloss.Left, separator, leftFooter+strings.Repeat(" ", max(0, m.width-lipgloss.Width(leftFooter)-lipgloss.Width(rightFooter)))+rightFooter)
}

// ---------------------------------------------------------------------------
// Thinking animation
// ---------------------------------------------------------------------------

func (m Model) renderThinkingBar() string {
	const barWidth = 12
	head := m.spinnerFrame % barWidth

	var bar strings.Builder
	for i := 0; i < barWidth; i++ {
		distance := circularDistance(i, head, barWidth)
		densityIdx := max(0, len(densityChars)-1-distance)
		ch := densityChars[densityIdx]
		col := gradientPalette[(i+m.spinnerFrame)%len(gradientPalette)]
		bar.WriteString(lipgloss.NewStyle().Foreground(col).Render(string(ch)))
	}

	label := lipgloss.NewStyle().Foreground(colorMuted).Italic(true).Render(" thinking...")
	return "  " + bar.String() + label
}

func circularDistance(a, b, width int) int {
	if width <= 0 {
		return 0
	}
	diff := abs(a - b)
	if width-diff < diff {
		return width - diff
	}
	return diff
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

// ---------------------------------------------------------------------------
// Local command handling
// ---------------------------------------------------------------------------

func (m *Model) handleLocalCommand(value string) (bool, tea.Cmd) {
	if value == "/clear" {
		m.lines = nil
		return true, nil
	}

	// Open modals for commands without arguments
	args := strings.Fields(value)
	if len(args) == 1 {
		switch args[0] {
		case "/approval":
			m.openPickerModal(ApprovalPicker(valueOr(m.approval, "auto")))
			return true, nil
		case "/provider":
			m.openPickerModal(ProviderPicker(valueOr(m.provider, "mock")))
			return true, nil
		case "/sandbox":
			m.openPickerModal(SandboxPicker(valueOr(m.sandbox, "workspace-write")))
			return true, nil
		case "/mode":
			m.openPickerModal(ModePicker(valueOr(m.mode, "edit")))
			return true, nil
		case "/reasoning":
			m.openPickerModal(ReasoningPicker(m.reasoning))
			return true, nil
		case "/sessions":
			if m.pickerProvider != nil {
				return true, runPickerProvider(m.pickerProvider, "session")
			}
		case "/checkpoint":
			m.openPickerModal(CheckpointPicker())
			return true, nil
		case "/config":
			m.openPickerModal(ConfigPicker())
			return true, nil
		case "/instructions":
			m.openPickerModal(InstructionsPicker())
			return true, nil
		case "/diff":
			m.openPickerModal(DiffPicker())
			return true, nil
		case "/settings":
			m.openSettingsModal()
			return true, nil
		case "/help":
			m.openHelpModal()
			return true, nil
		}
	}

	if strings.HasPrefix(value, "/") {
		m.applyOptimisticCommand(value)
		if len(m.lines) > 0 {
			m.lines = append(m.lines, "")
		}
		m.lines = append(m.lines, "you: "+value)
		m.syncViewport(true)
		return true, runHandler(m.handler, value, false)
	}

	return false, nil
}

func (m *Model) applyOptimisticCommand(value string) {
	args := strings.Fields(value)
	if len(args) < 2 {
		return
	}
	switch args[0] {
	case "/set":
		for _, arg := range args[1:] {
			key, value, ok := strings.Cut(arg, "=")
			if !ok {
				continue
			}
			switch key {
			case "provider":
				m.provider = value
			case "model":
				m.model = value
			case "endpoint":
				m.baseURL = value
			case "reasoning":
				m.reasoning = value
			case "approval":
				m.approval = value
			case "sandbox":
				m.sandbox = value
			case "mode":
				m.mode = value
			case "store":
				m.storeResponses = value == "on"
			case "context":
				if parsed := parsePositiveInt(value); parsed > 0 {
					m.maxContext = parsed
				}
			case "output":
				if parsed := parsePositiveInt(value); parsed > 0 {
					m.maxOutput = parsed
				}
			case "steps":
				if parsed := parsePositiveInt(value); parsed > 0 {
					m.maxSteps = parsed
				}
			case "messages":
				if parsed := parsePositiveInt(value); parsed > 0 {
					m.maxMessages = parsed
				}
			case "instructions":
				if parsed := parsePositiveInt(value); parsed > 0 {
					m.maxInstructions = parsed
				}
			case "context_cockpit":
				m.contextCockpit = value != "off"
			case "context_cockpit_inject":
				m.contextCockpitInject = value != "off"
			case "context_cockpit_tokens":
				if parsed := parsePositiveInt(value); parsed > 0 {
					m.contextCockpitTokens = parsed
				}
			case "context_cockpit_files":
				if parsed := parsePositiveInt(value); parsed > 0 {
					m.contextCockpitMaxFiles = parsed
				}
			case "context_cockpit_diff":
				m.contextCockpitDiff = value != "off"
			case "context_recipes":
				m.contextRecipes = value != "off"
			case "negative_context":
				m.negativeContext = value != "off"
			}
		}
	case "/provider":
		m.provider = args[1]
		m.model = ""
	case "/model":
		m.model = strings.Join(args[1:], " ")
	case "/endpoint":
		m.baseURL = strings.Join(args[1:], " ")
	case "/reasoning":
		m.reasoning = args[1]
	case "/approval":
		m.approval = args[1]
	case "/sandbox":
		m.sandbox = args[1]
	case "/mode":
		m.mode = args[1]
	case "/cart":
		if len(args) >= 3 && args[1] == "add" {
			m.cartCount += len(args) - 2
		}
	case "/limits":
		if len(args) < 3 {
			return
		}
		value := parsePositiveInt(args[2])
		if value <= 0 {
			return
		}
		switch args[1] {
		case "context", "ctx":
			m.maxContext = value
		case "output", "out":
			m.maxOutput = value
		}
	}
}

// ---------------------------------------------------------------------------
// Style helpers
// ---------------------------------------------------------------------------

func pillBadge(text string, bg, fg lipgloss.Color) string {
	style := lipgloss.NewStyle().
		Padding(0, 1)
	if bg != "" {
		style = style.Background(bg)
	}
	if fg != "" {
		style = style.Foreground(fg)
	} else {
		style = style.Foreground(colorWhite)
	}
	return style.Render(text)
}

func statusGlyph(status string) string {
	switch status {
	case "thinking":
		return "●"
	case "error":
		return "✖"
	default:
		return "✔"
	}
}

func statusColor(status string) lipgloss.Color {
	switch status {
	case "thinking":
		return colorAmber
	case "error":
		return colorRed
	default:
		return colorEmerald
	}
}

func formatNumber(value int) string {
	if value <= 0 {
		return "default"
	}
	if value >= 1000 {
		return fmt.Sprintf("%dk", value/1000)
	}
	return fmt.Sprintf("%d", value)
}

func parsePositiveInt(value string) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0
	}
	return parsed
}

func shorten(value string, maxLen int) string {
	if len(value) <= maxLen {
		return value
	}
	if maxLen <= 1 {
		return value[:maxLen]
	}
	return value[:maxLen-1] + "..."
}

func runHandler(handler Handler, input string, agentRun bool) tea.Cmd {
	return func() tea.Msg {
		output, err := handler(input)
		return responseMsg{input: input, output: output, err: err, agentRun: agentRun}
	}
}

func runPickerProvider(provider PickerProvider, field string) tea.Cmd {
	return func() tea.Msg {
		picker, err := provider(field)
		return pickerLoadedMsg{picker: picker, err: err}
	}
}

func centerOverlay(width, height int, content string) string {
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 20
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}

func (m *Model) appendAgentOutput(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	m.lines = append(m.lines, "agent: "+text)
}

func (m *Model) flushLiveAssistantSegment() {
	if strings.TrimSpace(m.reasoningText) == "" && strings.TrimSpace(m.streamBuf) == "" {
		return
	}
	if len(m.lines) > 0 && m.lines[len(m.lines)-1] != "" {
		m.lines = append(m.lines, "")
	}
	m.appendThoughtsOutput(m.reasoningText, false)
	m.appendAgentOutput(m.streamBuf)
	m.streamBuf = ""
	m.reasoningText = ""
	m.reasonExpanded = false
}

func (m *Model) appendThoughtsOutput(text string, expanded bool) {
	if strings.TrimSpace(text) == "" {
		return
	}
	state := "0"
	if expanded {
		state = "1"
	}
	m.lines = append(m.lines, "thoughts:"+state+":"+text)
}

func (m *Model) appendToolStream(msg ToolStreamMsg) {
	if strings.TrimSpace(msg.Name) == "" && strings.TrimSpace(msg.Arguments) == "" && strings.TrimSpace(msg.ID) == "" {
		return
	}
	for i := len(m.lines) - 1; i >= 0; i-- {
		if !strings.HasPrefix(m.lines[i], "toolstream:") {
			continue
		}
		expanded, raw := parseCollapsiblePayload(m.lines[i], "toolstream")
		var existing ToolStreamMsg
		if err := json.Unmarshal([]byte(raw), &existing); err != nil {
			continue
		}
		if !sameToolStream(existing, msg) {
			continue
		}
		if strings.TrimSpace(msg.ID) != "" {
			existing.ID = msg.ID
		}
		if strings.TrimSpace(msg.Name) != "" {
			existing.Name = msg.Name
		}
		existing.Arguments += msg.Arguments
		if data, err := json.Marshal(existing); err == nil {
			state := "0"
			if expanded {
				state = "1"
			}
			m.lines[i] = "toolstream:" + state + ":" + string(data)
		}
		return
	}
	if data, err := json.Marshal(msg); err == nil {
		m.lines = append(m.lines, "toolstream:0:"+string(data))
	}
}

func sameToolStream(existing, next ToolStreamMsg) bool {
	if strings.TrimSpace(existing.ID) != "" && strings.TrimSpace(next.ID) != "" {
		return existing.ID == next.ID
	}
	if existing.Index == next.Index {
		return true
	}
	return false
}

func (m *Model) appendToolProgress(msg ToolProgressMsg) {
	if strings.TrimSpace(msg.Tool) == "" {
		return
	}
	if data, err := json.Marshal(msg); err == nil {
		m.lines = append(m.lines, "toolprogress:0:"+string(data))
	}
}

func (m *Model) toggleLatestThoughts() bool {
	if strings.TrimSpace(m.reasoningText) != "" {
		m.reasonExpanded = !m.reasonExpanded
		return true
	}
	for i := len(m.lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(m.lines[i], "thoughts:0:") {
			m.lines[i] = "thoughts:1:" + strings.TrimPrefix(m.lines[i], "thoughts:0:")
			return true
		}
		if strings.HasPrefix(m.lines[i], "thoughts:1:") {
			m.lines[i] = "thoughts:0:" + strings.TrimPrefix(m.lines[i], "thoughts:1:")
			return true
		}
	}
	return false
}

func (m *Model) toggleLatestThoughtsExpand(expanded bool) bool {
	if strings.TrimSpace(m.reasoningText) != "" {
		m.reasonExpanded = expanded
		return true
	}
	for i := len(m.lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(m.lines[i], "thoughts:0:") || strings.HasPrefix(m.lines[i], "thoughts:1:") {
			text := strings.TrimPrefix(strings.TrimPrefix(m.lines[i], "thoughts:0:"), "thoughts:1:")
			state := "0"
			if expanded {
				state = "1"
			}
			m.lines[i] = "thoughts:" + state + ":" + text
			return true
		}
	}
	return false
}

func (m *Model) toggleLatestToolDetails() bool {
	for i := len(m.lines) - 1; i >= 0; i-- {
		switch {
		case strings.HasPrefix(m.lines[i], "toolstream:0:"):
			m.lines[i] = "toolstream:1:" + strings.TrimPrefix(m.lines[i], "toolstream:0:")
			return true
		case strings.HasPrefix(m.lines[i], "toolstream:1:"):
			m.lines[i] = "toolstream:0:" + strings.TrimPrefix(m.lines[i], "toolstream:1:")
			return true
		case strings.HasPrefix(m.lines[i], "toolprogress:0:"):
			m.lines[i] = "toolprogress:1:" + strings.TrimPrefix(m.lines[i], "toolprogress:0:")
			return true
		case strings.HasPrefix(m.lines[i], "toolprogress:1:"):
			m.lines[i] = "toolprogress:0:" + strings.TrimPrefix(m.lines[i], "toolprogress:1:")
			return true
		case strings.HasPrefix(m.lines[i], "tool:0:"):
			m.lines[i] = "tool:1:" + strings.TrimPrefix(m.lines[i], "tool:0:")
			return true
		case strings.HasPrefix(m.lines[i], "tool:1:"):
			m.lines[i] = "tool:0:" + strings.TrimPrefix(m.lines[i], "tool:1:")
			return true
		case strings.HasPrefix(m.lines[i], "tool: "):
			m.lines[i] = "tool:1:" + strings.TrimPrefix(m.lines[i], "tool: ")
			return true
		}
	}
	return false
}

func (m *Model) toggleLineDetails(index int) bool {
	if index == lineSourceLiveThoughts {
		m.reasonExpanded = !m.reasonExpanded
		return true
	}
	if index < 0 || index >= len(m.lines) {
		return false
	}
	switch {
	case strings.HasPrefix(m.lines[index], "thoughts:0:"):
		m.lines[index] = "thoughts:1:" + strings.TrimPrefix(m.lines[index], "thoughts:0:")
		return true
	case strings.HasPrefix(m.lines[index], "thoughts:1:"):
		m.lines[index] = "thoughts:0:" + strings.TrimPrefix(m.lines[index], "thoughts:1:")
		return true
	case strings.HasPrefix(m.lines[index], "toolstream:0:"):
		m.lines[index] = "toolstream:1:" + strings.TrimPrefix(m.lines[index], "toolstream:0:")
		return true
	case strings.HasPrefix(m.lines[index], "toolstream:1:"):
		m.lines[index] = "toolstream:0:" + strings.TrimPrefix(m.lines[index], "toolstream:1:")
		return true
	case strings.HasPrefix(m.lines[index], "toolprogress:0:"):
		m.lines[index] = "toolprogress:1:" + strings.TrimPrefix(m.lines[index], "toolprogress:0:")
		return true
	case strings.HasPrefix(m.lines[index], "toolprogress:1:"):
		m.lines[index] = "toolprogress:0:" + strings.TrimPrefix(m.lines[index], "toolprogress:1:")
		return true
	case strings.HasPrefix(m.lines[index], "tool:0:"):
		m.lines[index] = "tool:1:" + strings.TrimPrefix(m.lines[index], "tool:0:")
		return true
	case strings.HasPrefix(m.lines[index], "tool:1:"):
		m.lines[index] = "tool:0:" + strings.TrimPrefix(m.lines[index], "tool:1:")
		return true
	case strings.HasPrefix(m.lines[index], "tool: "):
		m.lines[index] = "tool:1:" + strings.TrimPrefix(m.lines[index], "tool: ")
		return true
	}
	return false
}

func (m *Model) handleViewportClick(y int) bool {
	if y < 0 || y >= m.viewport.Height {
		return false
	}
	items := m.currentViewportItems()
	index := m.viewport.YOffset + y
	if index < 0 || index >= len(items) {
		return false
	}
	item := items[index]
	plain := stripANSICodes(item.text)
	if strings.Contains(plain, "Thinking") || strings.Contains(plain, "Thoughts") {
		return m.toggleLineDetails(item.source)
	}
	if strings.Contains(plain, "▸ ") || strings.Contains(plain, "▾ ") || strings.Contains(plain, "details") {
		return m.toggleLineDetails(item.source)
	}
	return false
}

func (m Model) currentViewportLines() []string {
	items := m.currentViewportItems()
	lines := make([]string, 0, len(items))
	for _, item := range items {
		lines = append(lines, item.text)
	}
	return lines
}

func (m Model) currentViewportItems() []formattedLineItem {
	items := m.buildFormattedLineItems()
	padding := m.viewport.Height - len(items)
	if padding > 0 {
		padded := make([]formattedLineItem, padding)
		for i := range padded {
			padded[i] = formattedLineItem{source: lineSourceNone}
		}
		items = append(padded, items...)
	}
	return items
}

func stripANSICodes(value string) string {
	var out strings.Builder
	inEscape := false
	for _, r := range value {
		if inEscape {
			if r >= '@' && r <= '~' {
				inEscape = false
			}
			continue
		}
		if r == '\x1b' {
			inEscape = true
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

func (m Model) renderLiveThoughtBlock() []string {
	return m.renderThoughts(m.reasoningText, m.reasonExpanded, true)
}

func (m Model) renderThoughtBlock(line string) []string {
	expanded := strings.HasPrefix(line, "thoughts:1:")
	text := strings.TrimPrefix(strings.TrimPrefix(line, "thoughts:0:"), "thoughts:1:")
	return m.renderThoughts(text, expanded, false)
}

func (m Model) renderThoughts(text string, expanded, live bool) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	icon := "▸"
	label := "Thoughts"
	if live {
		label = "Thinking"
	}
	if expanded {
		icon = "▾"
	}
	headerStyle := lipgloss.NewStyle().Foreground(colorDim).Background(colorInputBg).Padding(0, 1)
	bodyStyle := lipgloss.NewStyle().Foreground(colorDim).Background(colorInputBg).Padding(0, 1).Width(max(24, m.chatWidth()-8))
	borderStyle := lipgloss.NewStyle().Foreground(colorMuted)

	header := headerStyle.Render(fmt.Sprintf("%s %s", icon, label))
	if !expanded {
		summary := compactThoughtSummary(text, max(20, m.chatWidth()-22))
		return []string{"  " + borderStyle.Render("┌") + header + " " + lipgloss.NewStyle().Foreground(colorMuted).Render(summary)}
	}

	var lines []string
	lines = append(lines, "  "+borderStyle.Render("┌")+" "+header)
	rendered := text
	if m.renderer != nil {
		if out, err := m.renderer.Render(text); err == nil {
			rendered = strings.TrimRight(out, " \n\r\t")
		}
	}
	wrapped := bodyStyle.Render(rendered)
	for _, line := range strings.Split(strings.TrimRight(wrapped, "\n"), "\n") {
		lines = append(lines, "  "+borderStyle.Render("│")+" "+line)
	}
	lines = append(lines, "  "+borderStyle.Render("└")+" "+lipgloss.NewStyle().Foreground(colorMuted).Render("Enter/F4 toggles"))
	return lines
}

func compactThoughtSummary(text string, maxLen int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= maxLen {
		return text
	}
	if maxLen <= 3 {
		return text[:maxLen]
	}
	return text[:maxLen-3] + "..."
}

type toolDisplay struct {
	Tool   string `json:"tool"`
	Output string `json:"output"`
	Error  string `json:"error"`
}

func (m Model) renderToolStreamLine(raw string) []string {
	expanded, raw := parseCollapsiblePayload(raw, "toolstream")
	var msg ToolStreamMsg
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		return []string{lipgloss.NewStyle().Foreground(colorEmerald).Render("  ◇ tool call " + raw)}
	}
	name := strings.TrimSpace(msg.Name)
	if name == "" {
		name = "tool"
	}
	args := strings.TrimSpace(msg.Arguments)
	headerStyle := lipgloss.NewStyle().Foreground(colorEmerald).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(colorMuted)
	bodyStyle := lipgloss.NewStyle().Foreground(colorDim).Width(max(24, m.chatWidth()-12))
	borderStyle := lipgloss.NewStyle().Foreground(colorMuted)
	icon := "▸"
	if expanded {
		icon = "▾"
	}
	summary := "model is preparing tool call"
	if args != "" {
		summary += ", " + outputSummary(args, 64)
	}
	lines := []string{"  " + headerStyle.Render(icon+" "+name) + " " + labelStyle.Render(summary)}
	if !expanded {
		return lines
	}
	if args != "" {
		for _, line := range strings.Split(bodyStyle.Render(args), "\n") {
			lines = append(lines, "  "+borderStyle.Render("│")+" "+line)
		}
	}
	return lines
}

func (m Model) renderToolProgressLine(raw string) []string {
	expanded, raw := parseCollapsiblePayload(raw, "toolprogress")
	var msg ToolProgressMsg
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		return []string{lipgloss.NewStyle().Foreground(colorEmerald).Render("  ◆ tool " + raw)}
	}
	phase := strings.TrimSpace(msg.Phase)
	if phase == "" {
		phase = "running"
	}
	name := strings.TrimSpace(msg.Tool)
	if name == "" {
		name = "tool"
	}
	headerStyle := lipgloss.NewStyle().Foreground(colorEmerald).Bold(true)
	if phase == "error" || phase == "rejected" {
		headerStyle = lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	}
	labelStyle := lipgloss.NewStyle().Foreground(colorMuted)
	bodyStyle := lipgloss.NewStyle().Foreground(colorDim).Width(max(24, m.chatWidth()-12))
	borderStyle := lipgloss.NewStyle().Foreground(colorMuted)
	errorStyle := lipgloss.NewStyle().Foreground(colorRed)

	summary := toolProgressSummary(msg)
	icon := "▸"
	if expanded {
		icon = "▾"
	}
	lines := []string{"  " + headerStyle.Render(icon+" "+name) + " " + labelStyle.Render(summary)}
	if !expanded {
		return lines
	}
	if len(msg.Args) > 0 && (phase == "queued" || phase == "running") {
		if data, err := json.Marshal(msg.Args); err == nil {
			for _, line := range strings.Split(bodyStyle.Render(string(data)), "\n") {
				lines = append(lines, "  "+borderStyle.Render("│")+" "+line)
			}
		}
	}
	output := strings.TrimRight(msg.Output, "\n")
	if output != "" {
		for _, line := range strings.Split(bodyStyle.Render(output), "\n") {
			lines = append(lines, "  "+borderStyle.Render("│")+" "+line)
		}
	}
	if strings.TrimSpace(msg.Error) != "" {
		for _, line := range strings.Split(bodyStyle.Render(msg.Error), "\n") {
			lines = append(lines, "  "+borderStyle.Render("│")+" "+errorStyle.Render(line))
		}
	}
	return lines
}

func toolProgressSummary(msg ToolProgressMsg) string {
	phase := strings.TrimSpace(msg.Phase)
	if phase == "" {
		phase = "running"
	}
	switch phase {
	case "queued":
		return "planned tool call, details collapsed"
	case "running":
		return "running tool, details collapsed"
	case "done":
		if strings.TrimSpace(msg.Output) == "" {
			return "finished with empty result"
		}
		return fmt.Sprintf("finished, %s", outputSummary(msg.Output, 70))
	case "error":
		return fmt.Sprintf("failed, %s", outputSummary(msg.Error, 70))
	case "rejected":
		return fmt.Sprintf("rejected, %s", outputSummary(msg.Error, 70))
	default:
		return phase + ", details collapsed"
	}
}

func toolPhaseLabel(phase string) string {
	switch phase {
	case "queued":
		return "queued"
	case "running":
		return "running"
	case "done":
		return "done"
	case "error":
		return "error"
	case "rejected":
		return "rejected"
	default:
		return phase
	}
}

func (m Model) renderToolLine(raw string) []string {
	expanded, raw := parseCollapsiblePayload(raw, "tool")
	raw = strings.TrimSpace(raw)
	display := toolDisplay{Tool: "tool", Output: raw}
	if strings.HasPrefix(raw, "{") {
		var parsed toolDisplay
		if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
			display = parsed
		}
	}
	if strings.TrimSpace(display.Tool) == "" {
		display.Tool = "tool"
	}

	headerStyle := lipgloss.NewStyle().Foreground(colorEmerald).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(colorMuted)
	bodyStyle := lipgloss.NewStyle().Foreground(colorDim).Width(max(24, m.chatWidth()-10))
	borderStyle := lipgloss.NewStyle().Foreground(colorMuted)
	errorStyle := lipgloss.NewStyle().Foreground(colorRed)

	icon := "▸"
	if expanded {
		icon = "▾"
	}
	summary := toolDisplaySummary(display)
	lines := []string{
		"  " + headerStyle.Render(icon+" "+display.Tool) + " " + labelStyle.Render(summary),
	}
	if !expanded {
		return lines
	}
	output := strings.TrimRight(display.Output, "\n")
	if output != "" {
		for _, line := range strings.Split(bodyStyle.Render(output), "\n") {
			lines = append(lines, "  "+borderStyle.Render("│")+" "+line)
		}
	}
	if strings.TrimSpace(display.Error) != "" {
		for _, line := range strings.Split(bodyStyle.Render(display.Error), "\n") {
			lines = append(lines, "  "+borderStyle.Render("│")+" "+errorStyle.Render(line))
		}
	}
	if output == "" && strings.TrimSpace(display.Error) == "" {
		lines = append(lines, "  "+borderStyle.Render("│")+" "+labelStyle.Render("empty result"))
	}
	return lines
}

func parseCollapsiblePayload(raw, prefix string) (bool, string) {
	zero := prefix + ":0:"
	one := prefix + ":1:"
	spaced := prefix + ": "
	switch {
	case strings.HasPrefix(raw, zero):
		return false, strings.TrimPrefix(raw, zero)
	case strings.HasPrefix(raw, one):
		return true, strings.TrimPrefix(raw, one)
	case strings.HasPrefix(raw, spaced):
		return false, strings.TrimPrefix(raw, spaced)
	}
	return false, raw
}

func toolDisplaySummary(display toolDisplay) string {
	if strings.TrimSpace(display.Error) != "" {
		return "failed, " + outputSummary(display.Error, 70)
	}
	if strings.TrimSpace(display.Output) == "" {
		return "finished with empty result"
	}
	return fmt.Sprintf("finished, %s", outputSummary(display.Output, 70))
}

func outputSummary(text string, maxLen int) string {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return "no details"
	}
	if len(text) <= maxLen {
		return text
	}
	if maxLen <= 3 {
		return text[:maxLen]
	}
	return text[:maxLen-3] + "..."
}

func renderCommandOutputLine(line string) string {
	if strings.TrimSpace(stripANSICodes(line)) == "" {
		return ""
	}
	plain := strings.TrimSpace(stripANSICodes(line))
	lower := strings.ToLower(plain)
	accent := lipgloss.NewStyle().Foreground(colorSeparator).Render("│")
	style := lipgloss.NewStyle().Foreground(colorDim)

	switch {
	case strings.HasPrefix(lower, "setting saved:"):
		accent = lipgloss.NewStyle().Foreground(colorEmerald).Render("│")
		style = lipgloss.NewStyle().Foreground(colorEmerald)
	case strings.HasPrefix(lower, "usage:") || strings.Contains(lower, " usage"):
		accent = lipgloss.NewStyle().Foreground(colorBlue).Render("│")
		style = lipgloss.NewStyle().Foreground(colorBlue)
	case strings.Contains(lower, "requires") || strings.Contains(lower, "warning"):
		accent = lipgloss.NewStyle().Foreground(colorAmber).Render("│")
		style = lipgloss.NewStyle().Foreground(colorAmber)
	case strings.HasPrefix(plain, "✓") || strings.HasPrefix(lower, "saved"):
		accent = lipgloss.NewStyle().Foreground(colorEmerald).Render("│")
		style = lipgloss.NewStyle().Foreground(colorEmerald)
	case strings.HasPrefix(plain, "✗") || strings.HasPrefix(lower, "error"):
		accent = lipgloss.NewStyle().Foreground(colorRed).Render("│")
		style = lipgloss.NewStyle().Foreground(colorRed)
	case strings.HasPrefix(plain, "- ") || strings.HasPrefix(plain, "• "):
		accent = lipgloss.NewStyle().Foreground(colorBrandDim).Render("│")
	}

	if strings.Contains(line, "\x1b[") {
		return "  " + accent + " " + line
	}
	return "  " + accent + " " + style.Render(line)
}

func (m Model) historyPrevious() (tea.Model, tea.Cmd) {
	if len(m.history) == 0 {
		return m, nil
	}
	if m.historyIdx == len(m.history) {
		m.tempInput = m.input.Value()
	}
	m.historyIdx--
	if m.historyIdx < 0 {
		m.historyIdx = 0
	}
	m.input.SetValue(m.history[m.historyIdx])
	m.input.SetCursor(len(m.input.Value()))
	return m, nil
}

func (m Model) historyNext() (tea.Model, tea.Cmd) {
	if len(m.history) == 0 {
		return m, nil
	}
	m.historyIdx++
	if m.historyIdx > len(m.history) {
		m.historyIdx = len(m.history)
	}
	if m.historyIdx == len(m.history) {
		m.input.SetValue(m.tempInput)
	} else {
		m.input.SetValue(m.history[m.historyIdx])
	}
	m.input.SetCursor(len(m.input.Value()))
	return m, nil
}

func visibleTail(lines []string, limit int) []string {
	if len(lines) <= limit {
		return lines
	}
	return lines[len(lines)-limit:]
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}

// ---------------------------------------------------------------------------
// Message formatting constants
// ---------------------------------------------------------------------------

const (
	userPrefix   = ">"
	agentBar     = "|"
	errorPrefix  = "x"
	systemPrefix = "-"
	acPointer    = ">"
)

// ---------------------------------------------------------------------------
// Styles
// ---------------------------------------------------------------------------

var (
	userMsgStyle = lipgloss.NewStyle().
			Foreground(colorBlue)

	agentMsgStyle = lipgloss.NewStyle().
			Foreground(colorText)

	agentBarStyle = lipgloss.NewStyle().
			Foreground(colorBrand)

	errorMsgStyle = lipgloss.NewStyle().
			Foreground(colorRed).
			Bold(true)

	systemMsgStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true)

	acItemStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	acSelectedStyle = lipgloss.NewStyle().
			Background(colorBadgeBg).
			Foreground(colorBrand).
			Bold(true)

	acHintStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true)
)

// setEnvIfChanged sets an environment variable only when the new value differs.
func setEnvIfChanged(envVar, value string) error {
	if os.Getenv(envVar) == value {
		return nil
	}
	return os.Setenv(envVar, value)
}

func saveEnvFile(dir string, keys map[string]string) error {
	if dir == "" {
		dir = "."
	}
	path := filepath.Join(dir, ".env")

	envMap := make(map[string]string)
	var lines []string

	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				lines = append(lines, line)
				continue
			}
			rawLine := line
			trimmed = strings.TrimPrefix(trimmed, "export ")
			key, _, ok := strings.Cut(trimmed, "=")
			if !ok {
				lines = append(lines, line)
				continue
			}
			key = strings.TrimSpace(key)
			envMap[key] = rawLine
			lines = append(lines, key)
		}
	}

	for k, v := range keys {
		quotedVal := fmt.Sprintf("%s=\"%s\"", k, v)
		envMap[k] = quotedVal

		found := false
		for _, line := range lines {
			if line == k {
				found = true
				break
			}
		}
		if !found {
			lines = append(lines, k)
		}
	}

	var outLines []string
	for _, line := range lines {
		if val, exists := envMap[line]; exists {
			outLines = append(outLines, val)
		} else {
			outLines = append(outLines, line)
		}
	}

	return os.WriteFile(path, []byte(strings.Join(outLines, "\n")), 0o600)
}
