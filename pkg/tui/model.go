package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	termansi "github.com/charmbracelet/x/ansi"
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
	"#C084FC", // light purple
	"#A855F7", // purple
	"#8B5CF6", // violet
	"#7C3AED", // deep violet
	"#6366F1", // indigo
	"#3B82F6", // blue
	"#0EA5E9", // sky
	"#06B6D4", // cyan
	"#14B8A6", // teal
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

type copyPoint struct {
	index int
	col   int
}

type copyResultMsg struct {
	err   error
	chars int
}

var writeClipboard = clipboard.WriteAll

type Config struct {
	CWD                    string
	Title                  string
	SessionID              string
	Provider               string
	Model                  string
	BaseURL                string
	BaseURLs               map[string]string
	Reasoning              string
	Stream                 bool
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
	ContextCockpitMaxCards int
	ContextCockpitDiff     bool
	ContextRetrieval       bool
	ContextRetrievalTokens int
	ContextRetrievalChunks int
	ContextEmbeddings      bool
	ContextReranker        bool
	ContextRecipes         bool
	NegativeContext        bool
	Skills                 bool
	FastModel              string
	EditModel              string
	DeepModel              string
	AllTools               []string
	DisabledTools          []string
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
	modalFeatures
	modalTools
)

type Model struct {
	cwd                    string
	title                  string
	sessionID              string
	provider               string
	model                  string
	baseURL                string
	baseURLs               map[string]string
	reasoning              string
	stream                 bool
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
	contextCockpitMaxCards int
	contextCockpitDiff     bool
	contextRetrieval       bool
	contextRetrievalTokens int
	contextRetrievalChunks int
	contextEmbeddings      bool
	contextReranker        bool
	contextRecipes         bool
	negativeContext        bool
	skills                 bool
	fastModel              string
	editModel              string
	deepModel              string
	allTools               []string
	disabledTools          []string
	handler                Handler
	interrupt              InterruptFunc
	pickerProvider         PickerProvider
	input                  textarea.Model
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
	copyDragActive         bool
	copySelectionActive    bool
	copySelectionStart     copyPoint
	copySelectionEnd       copyPoint
	copyNotice             string
	interrupted            bool
	mouseFragmentTTL       int
	sidebarVisible         bool
	sidebarMode            int
	sidebarFiles           []string
	sidebarDiff            string
	sidebarPosition        int // 0=left, 1=right
	sidebarScroll          int // scroll offset for sidebar content
	sidebarCursor          int // selected item in sidebar (-1 = none)
	requestStartTime       time.Time

	// Modal state
	activeModal   modalKind
	pickerModal   *PickerModal
	helpModal     *HelpModal
	settingsModal *SettingsModal
	featuresModal *FeaturesModal
	toolsModal    *ToolsModal
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
	input := textarea.New()
	input.Placeholder = "Ask anything or type / for commands..."
	input.Prompt = "  ◆ "
	input.ShowLineNumbers = false
	input.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(colorBrand).Bold(true)
	input.BlurredStyle.Prompt = lipgloss.NewStyle().Foreground(colorBrand).Bold(true)
	input.FocusedStyle.Text = lipgloss.NewStyle().Foreground(colorWhite)
	input.BlurredStyle.Text = lipgloss.NewStyle().Foreground(colorWhite)
	input.Cursor.Style = lipgloss.NewStyle().Foreground(colorBrand)
	input.Cursor.Blink = false
	input.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(colorMuted).Italic(true)
	input.BlurredStyle.Placeholder = lipgloss.NewStyle().Foreground(colorMuted).Italic(true)
	input.FocusedStyle.CursorLine = lipgloss.NewStyle()
	input.BlurredStyle.CursorLine = lipgloss.NewStyle()
	input.FocusedStyle.CursorLineNumber = lipgloss.NewStyle()
	input.BlurredStyle.CursorLineNumber = lipgloss.NewStyle()
	input.SetHeight(1)
	input.SetWidth(74)
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
		baseURLs:               cloneStringMap(cfg.BaseURLs),
		reasoning:              cfg.Reasoning,
		stream:                 cfg.Stream,
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
		contextCockpitMaxCards: cfg.ContextCockpitMaxCards,
		contextCockpitDiff:     cfg.ContextCockpitDiff,
		contextRetrieval:       cfg.ContextRetrieval,
		contextRetrievalTokens: cfg.ContextRetrievalTokens,
		contextRetrievalChunks: cfg.ContextRetrievalChunks,
		contextEmbeddings:      cfg.ContextEmbeddings,
		contextReranker:        cfg.ContextReranker,
		contextRecipes:         cfg.ContextRecipes,
		negativeContext:        cfg.NegativeContext,
		skills:                 cfg.Skills,
		fastModel:              cfg.FastModel,
		editModel:              cfg.EditModel,
		deepModel:              cfg.DeepModel,
		allTools:               append([]string(nil), cfg.AllTools...),
		disabledTools:          append([]string(nil), cfg.DisabledTools...),
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
	m.syncInputSize()
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
		m.syncInputSize()
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
	case copyResultMsg:
		if msg.err != nil {
			m.err = msg.err
			m.copyNotice = "copy failed"
		} else if msg.chars > 0 {
			m.copyNotice = fmt.Sprintf("copied %d chars", msg.chars)
		} else {
			m.copyNotice = "copied"
		}
		return m, nil
	case tea.MouseMsg:
		if m.copyMode {
			if handled, cmd := m.handleCopyModeMouse(msg); handled {
				return m, cmd
			}
		}
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
		case "f5":
			m.openFeaturesModal()
			return m, nil
		case "f9":
			m.openToolsModal()
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
				m.clearCopySelection()
				m.copyNotice = ""
				m.syncViewport(false)
				return m, tea.EnableMouseAllMotion
			}
			m.clearCopySelection()
			m.copyNotice = ""
			m.syncViewport(false)
			return m, tea.EnableMouseCellMotion
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
		case "right":
			if m.toggleLatestThoughtsExpand(true) {
				m.syncViewport(true)
				return m, tea.ClearScreen
			}
		case "left":
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
			if m.shouldRouteUpToHistory() {
				return m.historyPrevious()
			}
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
			if m.shouldRouteDownToHistory() {
				return m.historyNext()
			}
		case "tab":
			if len(m.filteredCmds) > 0 {
				m.selectedCmdIdx++
				if m.selectedCmdIdx >= len(m.filteredCmds) {
					m.selectedCmdIdx = 0
				}
				return m, nil
			}
		case "shift+enter":
			m.input.InsertRune('\n')
			m.syncInputSize()
			m.updateCompletions()
			return m, nil
		case "ctrl+up", "ctrl+p":
			return m.historyPrevious()
		case "ctrl+down", "ctrl+n":
			return m.historyNext()
		case "enter":
			if len(m.filteredCmds) > 0 {
				m.input.SetValue(m.filteredCmds[m.selectedCmdIdx].Name + " ")
				m.input.SetCursor(len(m.input.Value()))
				m.syncInputSize()
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
			m.syncInputSize()
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
			m.requestStartTime = time.Now()
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
			if strings.Contains(msg.err.Error(), "stopped after") {
				m.lines = append(m.lines, "system: "+msg.err.Error())
			} else {
				m.lines = append(m.lines, "error: "+msg.err.Error())
			}
		}

		outText := strings.TrimSpace(msg.output)
		var usageLine string
		var cleanLines []string
		for _, line := range strings.Split(outText, "\n") {
			if strings.HasPrefix(line, "[TokenUsage] ") {
				usageLine = line
			} else {
				cleanLines = append(cleanLines, line)
			}
		}
		outText = strings.TrimSpace(strings.Join(cleanLines, "\n"))

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

		if msg.agentRun {
			var durationStr string = "0.0s"
			if !m.requestStartTime.IsZero() {
				elapsed := time.Since(m.requestStartTime)
				durationStr = fmt.Sprintf("%.1fs", elapsed.Seconds())
			}
			var inputT, cachedT, outputT, reasoningT, totalT int
			hasUsage := false
			if usageLine != "" {
				_, sscanfErr := fmt.Sscanf(usageLine, "[TokenUsage] input=%d cached=%d output=%d reasoning=%d total=%d", &inputT, &cachedT, &outputT, &reasoningT, &totalT)
				if sscanfErr == nil {
					hasUsage = true
				}
			}
			statsLine := fmt.Sprintf("stats: duration=%s", durationStr)
			if hasUsage {
				statsLine += fmt.Sprintf(" input=%d cached=%d output=%d reasoning=%d total=%d", inputT, cachedT, outputT, reasoningT, totalT)
			}
			m.lines = append(m.lines, statsLine)
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
		m.syncInputSize()
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

func (m *Model) syncInputSize() {
	m.input.SetWidth(max(20, m.width-6))
	lines := m.inputVisualLineCount()
	m.input.SetHeight(lines)
}

func (m Model) inputVisualLineCount() int {
	width := m.input.Width() - lipgloss.Width(m.input.Prompt)
	if width < 1 {
		width = 1
	}
	lines := 0
	for _, hardLine := range strings.Split(m.input.Value(), "\n") {
		lineWidth := termansi.StringWidth(hardLine)
		wrapped := max(1, (lineWidth+width-1)/width)
		lines += wrapped
	}
	if lines < 1 {
		lines = 1
	}
	if lines > 4 {
		lines = 4
	}
	return lines
}

func (m Model) shouldRouteUpToHistory() bool {
	if m.input.LineCount() <= 1 {
		return true
	}
	return m.input.Line() == 0
}

func (m Model) shouldRouteDownToHistory() bool {
	if m.input.LineCount() <= 1 {
		return true
	}
	return m.input.Line() >= m.input.LineCount()-1
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
	inputHeight := 1 + m.inputVisualLineCount()
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
			contentWidth := max(20, m.chatWidth()-4)
			wrapped := lipgloss.NewStyle().Width(contentWidth).Render(line[5:])
			wrappedLines := strings.Split(wrapped, "\n")
			for i, wl := range wrappedLines {
				if i == 0 {
					add(idx, userMsgStyle.Render("  "+userPrefix+" "+wl))
				} else {
					add(idx, userMsgStyle.Render("    "+wl))
				}
			}
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
			contentWidth := max(20, m.chatWidth()-4)
			wrapped := lipgloss.NewStyle().Width(contentWidth).Render(line[7:])
			wrappedLines := strings.Split(wrapped, "\n")
			for i, wl := range wrappedLines {
				if i == 0 {
					add(idx, errorMsgStyle.Render("  "+errorPrefix+" "+wl))
				} else {
					add(idx, errorMsgStyle.Render("    "+wl))
				}
			}
		} else if strings.HasPrefix(line, "system: ") {
			contentWidth := max(20, m.chatWidth()-4)
			wrapped := lipgloss.NewStyle().Width(contentWidth).Render(line[8:])
			wrappedLines := strings.Split(wrapped, "\n")
			for i, wl := range wrappedLines {
				if i == 0 {
					add(idx, systemMsgStyle.Render("  "+systemPrefix+" "+wl))
				} else {
					add(idx, systemMsgStyle.Render("    "+wl))
				}
			}
		} else if strings.HasPrefix(line, "toolstream:") {
			add(idx, m.renderToolStreamLine(line)...)
		} else if strings.HasPrefix(line, "toolprogress:") {
			add(idx, m.renderToolProgressLine(line)...)
		} else if strings.HasPrefix(line, "tool:") {
			add(idx, m.renderToolLine(line)...)
		} else if strings.HasPrefix(line, "tool rejected: ") {
			contentWidth := max(20, m.chatWidth()-17)
			wrapped := lipgloss.NewStyle().Width(contentWidth).Render(line[15:])
			wrappedLines := strings.Split(wrapped, "\n")
			style := lipgloss.NewStyle().Foreground(colorRed).Bold(true)
			for i, wl := range wrappedLines {
				if i == 0 {
					add(idx, style.Render("  tool rejected: "+wl))
				} else {
					add(idx, style.Render("                 "+wl))
				}
			}
		} else if strings.HasPrefix(line, "tool error: ") {
			contentWidth := max(20, m.chatWidth()-14)
			wrapped := lipgloss.NewStyle().Width(contentWidth).Render(line[12:])
			wrappedLines := strings.Split(wrapped, "\n")
			style := lipgloss.NewStyle().Foreground(colorRed).Bold(true)
			for i, wl := range wrappedLines {
				if i == 0 {
					add(idx, style.Render("  tool error: "+wl))
				} else {
					add(idx, style.Render("              "+wl))
				}
			}
		} else if strings.HasPrefix(line, "stats: ") {
			add(idx, m.renderStatsLine(line)...)
		} else if strings.HasPrefix(line, "usage: ") {
			contentWidth := max(20, m.chatWidth()-9)
			wrapped := lipgloss.NewStyle().Width(contentWidth).Render(line[7:])
			wrappedLines := strings.Split(wrapped, "\n")
			style := lipgloss.NewStyle().Foreground(colorDim)
			for i, wl := range wrappedLines {
				if i == 0 {
					add(idx, style.Render("  usage: "+wl))
				} else {
					add(idx, style.Render("         "+wl))
				}
			}
		} else if strings.HasPrefix(line, "policy: ") {
			contentWidth := max(20, m.chatWidth()-10)
			wrapped := lipgloss.NewStyle().Width(contentWidth).Render(line[8:])
			wrappedLines := strings.Split(wrapped, "\n")
			style := lipgloss.NewStyle().Foreground(colorAmber)
			for i, wl := range wrappedLines {
				if i == 0 {
					add(idx, style.Render("  policy: "+wl))
				} else {
					add(idx, style.Render("          "+wl))
				}
			}
		} else if strings.HasPrefix(line, "md: ") {
			add(idx, renderCommandOutputLine(line[4:]))
		} else if line == "" {
			add(idx, "")
		} else {
			contentWidth := max(20, m.chatWidth()-4)
			wrapped := lipgloss.NewStyle().Width(contentWidth).Render(line)
			wrappedLines := strings.Split(wrapped, "\n")
			for i, wl := range wrappedLines {
				if i == 0 {
					add(idx, systemMsgStyle.Render("  "+systemPrefix+" "+wl))
				} else {
					add(idx, systemMsgStyle.Render("    "+wl))
				}
			}
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

	items := m.buildFormattedLineItems()
	padding := m.viewport.Height - len(items)
	if padding > 0 {
		paddedItems := make([]formattedLineItem, padding)
		for i := range paddedItems {
			paddedItems[i] = formattedLineItem{source: lineSourceNone}
		}
		items = append(paddedItems, items...)
	}

	lines := m.renderViewportLines(items)
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
		"  skills: " + onOff(m.skills),
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
			body = centerOverlay(m.width, m.viewport.Height, m.pickerModal.View(m.width, m.viewport.Height))
		}
	case modalHelp:
		if m.helpModal != nil {
			body = centerOverlay(m.width, m.viewport.Height, m.helpModal.View(m.width, m.viewport.Height))
		}
	case modalSettings:
		if m.settingsModal != nil {
			body = centerOverlay(m.width, m.viewport.Height, m.settingsModal.View(m.width, m.viewport.Height))
		}
	case modalFeatures:
		if m.featuresModal != nil {
			body = centerOverlay(m.width, m.viewport.Height, m.featuresModal.View(m.width, m.viewport.Height))
		}
	case modalTools:
		if m.toolsModal != nil {
			body = centerOverlay(m.width, m.viewport.Height, m.toolsModal.View(m.width, m.viewport.Height))
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

	// Indent the entire block by 2 spaces
	boxLines := strings.Split(box, "\n")
	for i, line := range boxLines {
		boxLines[i] = "  " + line
	}
	indentedBox := strings.Join(boxLines, "\n")

	return indentedBox, lipgloss.Height(indentedBox)
}

// ---------------------------------------------------------------------------
// Approval modal
// ---------------------------------------------------------------------------

func (m Model) renderApprovalModal() string {
	req := m.approvalReq
	if req == nil {
		return ""
	}

	termHeight := m.viewport.Height
	paddingY := 1
	if termHeight <= 14 {
		paddingY = 0
	}

	// Calculate strict space budgets
	maxInnerHeight := termHeight - 4 - paddingY*2
	if maxInnerHeight < 4 {
		maxInnerHeight = 4
	}

	titleStyle := lipgloss.NewStyle().Foreground(colorAmber).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(colorDim)
	valueStyle := lipgloss.NewStyle().Foreground(colorText).Bold(true)
	keyStyle := lipgloss.NewStyle().Foreground(colorEmerald).Bold(true)
	keyRejectStyle := lipgloss.NewStyle().Foreground(colorRed).Bold(true)

	// Calculate maximum lines for arguments preview to avoid overflow
	overhead := 6 // title + blank + tool + blank + buttons (5) + blank (1)
	if req.Risk != "" {
		overhead++
	}
	if req.Reason != "" {
		overhead++
	}

	maxArgLines := maxInnerHeight - overhead
	if maxArgLines < 3 {
		maxArgLines = 3
	}

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
	if preview := formatApprovalArgs(req.Args, m.width-12, maxArgLines); preview != "" {
		lines = append(lines, "")
		lines = append(lines, labelStyle.Render("arguments:"))
		for _, line := range strings.Split(preview, "\n") {
			lines = append(lines, "  "+line)
		}
	}
	lines = append(lines, "")
	lines = append(lines, keyStyle.Render("[Y] Approve")+"  "+keyRejectStyle.Render("[N] Reject"))

	if len(lines) > maxInnerHeight {
		lines = lines[:maxInnerHeight]
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorAmber).
		Foreground(colorText).
		Padding(paddingY, 2).
		MaxHeight(maxInnerHeight).
		Render(strings.Join(lines, "\n"))
}

func formatApprovalArgs(args map[string]any, width, maxLines int) string {
	if len(args) == 0 {
		return ""
	}
	if width <= 0 {
		width = 96
	}
	if width > 140 {
		width = 140
	}
	var lines []string
	for _, section := range buildToolPreviewSections("", args, true) {
		lines = append(lines, section.Label+":")
		valueLines := strings.Split(strings.TrimRight(section.Value, "\n"), "\n")
		if section.Code {
			if lang := strings.TrimSpace(section.Language); lang != "" {
				lines = append(lines, "```"+lang)
			} else {
				lines = append(lines, "```")
			}
			lines = append(lines, valueLines...)
			lines = append(lines, "```")
		} else {
			for _, line := range valueLines {
				lines = append(lines, "  "+line)
			}
		}
	}
	if maxLines <= 0 {
		maxLines = 18
	}
	if len(lines) > maxLines {
		lines = append(lines[:maxLines], fmt.Sprintf("... %d more line(s)", len(lines)-maxLines))
	}
	for i, line := range lines {
		if len(line) > width {
			lines[i] = line[:max(0, width-1)] + "…"
		}
	}
	return strings.Join(lines, "\n")
}

func sanitizeApprovalValue(key string, value any) any {
	lowerKey := strings.ToLower(key)
	if strings.Contains(lowerKey, "key") ||
		strings.Contains(lowerKey, "token") ||
		strings.Contains(lowerKey, "secret") ||
		strings.Contains(lowerKey, "password") ||
		strings.Contains(lowerKey, "authorization") {
		return "<redacted>"
	}
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for k, v := range typed {
			out[k] = sanitizeApprovalValue(k, v)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, v := range typed {
			out[i] = sanitizeApprovalValue("", v)
		}
		return out
	case string:
		if len(typed) > 1200 {
			return typed[:1200] + fmt.Sprintf("\n... truncated %d byte(s)", len(typed)-1200)
		}
		return typed
	default:
		return value
	}
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
		m.stream,
		m.baseURLs,
		m.maxContext, m.maxOutput, m.maxSteps, m.maxMessages, m.maxInstructions,
		m.contextCockpit, m.contextCockpitInject,
		m.contextCockpitTokens, m.contextCockpitMaxFiles, m.contextCockpitMaxCards, m.contextCockpitDiff,
		m.contextRetrieval, m.contextRetrievalTokens, m.contextRetrievalChunks, m.contextEmbeddings, m.contextReranker,
		m.contextRecipes, m.negativeContext, m.skills,
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
	m.featuresModal = nil
	m.toolsModal = nil
}

func (m Model) updateModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.activeModal {
	case modalPicker:
		return m.updatePickerModal(msg)
	case modalHelp:
		return m.updateHelpModal(msg)
	case modalSettings:
		return m.updateSettingsModal(msg)
	case modalFeatures:
		return m.updateFeaturesModal(msg)
	case modalTools:
		return m.updateToolsModal(msg)
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
		m.helpModal.ScrollDown()
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
		m.stream = sm.GetValue("stream") != "off"
		if m.baseURLs == nil {
			m.baseURLs = map[string]string{}
		}
		for _, provider := range []string{"openai", "local", "ollama", "anthropic", "gemini"} {
			key := "endpoint_" + provider
			value := strings.TrimSpace(sm.GetValue(key))
			if value == "" {
				delete(m.baseURLs, provider)
			} else {
				m.baseURLs[provider] = value
			}
		}
		if endpoint := strings.TrimSpace(m.baseURL); endpoint != "" {
			m.baseURLs[strings.ToLower(valueOr(m.provider, "openai"))] = endpoint
		}
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
		if parsed := parsePositiveInt(sm.GetValue("context_cockpit_cards")); parsed > 0 {
			m.contextCockpitMaxCards = parsed
		}
		m.contextCockpitDiff = sm.GetValue("context_cockpit_diff") != "off"
		m.contextRetrieval = sm.GetValue("context_retrieval") != "off"
		if parsed := parsePositiveInt(sm.GetValue("context_retrieval_tokens")); parsed > 0 {
			m.contextRetrievalTokens = parsed
		}
		if parsed := parsePositiveInt(sm.GetValue("context_retrieval_chunks")); parsed > 0 {
			m.contextRetrievalChunks = parsed
		}
		m.contextEmbeddings = sm.GetValue("context_embeddings") != "off"
		m.contextReranker = sm.GetValue("context_reranker") != "off"
		m.contextRecipes = sm.GetValue("context_recipes") != "off"
		m.negativeContext = sm.GetValue("negative_context") != "off"
		m.skills = sm.GetValue("skills") != "off"
		m.fastModel = sm.GetValue("fast_model")
		m.editModel = sm.GetValue("edit_model")
		m.deepModel = sm.GetValue("deep_model")

		// Build /set command for handler
		parts := []string{"/set",
			"provider=" + m.provider,
			"model=" + m.model,
			"endpoint=" + m.baseURL,
			"stream=" + onOff(m.stream),
		}
		for _, provider := range []string{"openai", "local", "ollama", "anthropic", "gemini"} {
			parts = append(parts, "endpoint_"+provider+"="+m.baseURLs[provider])
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
			fmt.Sprintf("context_cockpit_cards=%d", m.contextCockpitMaxCards),
			"context_cockpit_diff="+onOff(m.contextCockpitDiff),
			"context_retrieval="+onOff(m.contextRetrieval),
			fmt.Sprintf("context_retrieval_tokens=%d", m.contextRetrievalTokens),
			fmt.Sprintf("context_retrieval_chunks=%d", m.contextRetrievalChunks),
			"context_embeddings="+onOff(m.contextEmbeddings),
			"context_reranker="+onOff(m.contextReranker),
			"context_recipes="+onOff(m.contextRecipes),
			"negative_context="+onOff(m.negativeContext),
			"skills="+onOff(m.skills),
			"fast_model="+m.fastModel,
			"edit_model="+m.editModel,
			"deep_model="+m.deepModel,
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
// Features modal
// ---------------------------------------------------------------------------

func (m *Model) openFeaturesModal() {
	fm := NewFeaturesModal(
		m.stream,
		m.storeResponses,
		m.contextCockpit,
		m.contextCockpitInject,
		m.contextCockpitDiff,
		m.contextRetrieval,
		m.contextEmbeddings,
		m.contextReranker,
		m.contextRecipes,
		m.negativeContext,
		m.skills,
	)
	m.featuresModal = &fm
	m.activeModal = modalFeatures
}

func (m Model) updateFeaturesModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.featuresModal == nil {
		m.closeModal()
		return m, nil
	}
	fm := m.featuresModal

	switch msg.String() {
	case "esc":
		m.closeModal()
		return m, nil
	case "tab", "down", "j":
		fm.MoveDown()
		return m, nil
	case "shift+tab", "up", "k":
		fm.MoveUp()
		return m, nil
	case " ", "enter":
		fm.Toggle()
		return m, nil
	case "a":
		fm.EnableAll()
		return m, nil
	case "n":
		fm.DisableAll()
		return m, nil
	case "ctrl+s":
		// Apply changes to model state
		m.stream = fm.GetValue("stream") != "off"
		m.storeResponses = fm.GetValue("store") != "off"
		m.contextCockpit = fm.GetValue("context_cockpit") != "off"
		m.contextCockpitInject = fm.GetValue("context_cockpit_inject") != "off"
		m.contextCockpitDiff = fm.GetValue("context_cockpit_diff") != "off"
		m.contextRetrieval = fm.GetValue("context_retrieval") != "off"
		m.contextEmbeddings = fm.GetValue("context_embeddings") != "off"
		m.contextReranker = fm.GetValue("context_reranker") != "off"
		m.contextRecipes = fm.GetValue("context_recipes") != "off"
		m.negativeContext = fm.GetValue("negative_context") != "off"
		m.skills = fm.GetValue("skills") != "off"

		// Build /set command
		parts := []string{"/set",
			"stream=" + onOff(m.stream),
			"store=" + onOff(m.storeResponses),
			"context_cockpit=" + onOff(m.contextCockpit),
			"context_cockpit_inject=" + onOff(m.contextCockpitInject),
			"context_cockpit_diff=" + onOff(m.contextCockpitDiff),
			"context_retrieval=" + onOff(m.contextRetrieval),
			"context_embeddings=" + onOff(m.contextEmbeddings),
			"context_reranker=" + onOff(m.contextReranker),
			"context_recipes=" + onOff(m.contextRecipes),
			"negative_context=" + onOff(m.negativeContext),
			"skills=" + onOff(m.skills),
		}
		cmdText := strings.Join(parts, " ")

		m.closeModal()
		m.lines = append(m.lines, "system: features saved")
		m.syncViewport(true)
		return m, runHandler(m.handler, cmdText, false)
	}
	return m, nil
}

// Tools modal

func (m *Model) openToolsModal() {
	tm := NewToolsModal(m.allTools, m.disabledTools)
	m.toolsModal = &tm
	m.activeModal = modalTools
}

func (m Model) updateToolsModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.toolsModal == nil {
		m.closeModal()
		return m, nil
	}
	tm := m.toolsModal

	switch msg.String() {
	case "esc":
		m.closeModal()
		return m, nil
	case "tab", "down", "j":
		tm.MoveDown()
		return m, nil
	case "shift+tab", "up", "k":
		tm.MoveUp()
		return m, nil
	case " ", "enter":
		tm.Toggle()
		return m, nil
	case "a":
		tm.EnableAll()
		return m, nil
	case "n":
		tm.DisableAll()
		return m, nil
	case "ctrl+s":
		// Collect disabled tools
		disabledList := []string{}
		for _, tool := range tm.Tools {
			if !tool.Enabled {
				disabledList = append(disabledList, tool.Name)
			}
		}
		m.disabledTools = disabledList

		// Build /set command for TUI runner
		cmdText := "/set disabled_tools=" + strings.Join(disabledList, ",")

		m.closeModal()
		m.lines = append(m.lines, "system: tools configuration saved")
		m.syncViewport(true)
		return m, runHandler(m.handler, cmdText, false)
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Header
// ---------------------------------------------------------------------------

func (m Model) renderHeader() string {
	width := max(50, m.chatWidth())

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
	subtitle := lipgloss.NewStyle().Foreground(colorDim).Render("  versatile project assistant")

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
	budgets := m.renderHeaderBudgets(width - 2)

	// Gradient separator bar
	barWidth := max(10, min(width-2, 90))
	hdrGradColors := []string{"#7C3AED", "#6366F1", "#3B82F6", "#0EA5E9", "#06B6D4", "#0EA5E9", "#3B82F6", "#6366F1", "#7C3AED"}
	var barBuilder strings.Builder
	for i := 0; i < barWidth; i++ {
		ci := i * len(hdrGradColors) / barWidth
		if ci >= len(hdrGradColors) {
			ci = len(hdrGradColors) - 1
		}
		barBuilder.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(hdrGradColors[ci])).Render("─"))
	}
	bar := barBuilder.String()

	topLine := lipgloss.JoinHorizontal(lipgloss.Top, title, subtitle)
	badgeLine := lipgloss.JoinHorizontal(lipgloss.Top, statusBadge, " ", providerBadge, " ", modelBadge, " ", reasoningBadge)

	result := []string{""}
	result = append(result, logoLines...)
	result = append(result,
		"",
		"  "+topLine,
		"  "+badgeLine,
	)
	if lipgloss.Width("  "+budgets+"  "+safetyBadge) <= width {
		result = append(result, "  "+budgets+"  "+safetyBadge)
	} else {
		result = append(result, "  "+budgets, "  "+safetyBadge)
	}
	result = append(result, "  "+bar)

	return strings.Join(result, "\n")
}

func (m Model) renderHeaderBudgets(maxWidth int) string {
	muted := lipgloss.NewStyle().Foreground(colorMuted)
	dim := lipgloss.NewStyle().Foreground(colorDim)
	sep := lipgloss.NewStyle().Foreground(colorSeparator).Render(" · ")

	render := func(includeSession, includeEndpoint bool, endpointLimit int) string {
		parts := []string{
			muted.Render("ctx ") + dim.Render(formatNumber(m.maxContext)),
			muted.Render("out ") + dim.Render(formatNumber(m.maxOutput)),
			muted.Render("cart ") + dim.Render(fmt.Sprintf("%d", m.cartCount)),
		}
		if includeSession {
			parts = append(parts, muted.Render("session ")+dim.Render(valueOr(m.sessionID, "ephemeral")))
		}
		if includeEndpoint && strings.TrimSpace(m.baseURL) != "" {
			parts = append(parts, muted.Render("endpoint ")+dim.Render(shorten(m.baseURL, endpointLimit)))
		}
		return strings.Join(parts, sep)
	}

	if strings.TrimSpace(m.baseURL) != "" {
		for limit := 24; limit >= 10; limit -= 2 {
			budgets := render(true, true, limit)
			if lipgloss.Width(budgets) <= maxWidth {
				return budgets
			}
		}
	}
	for _, includeSession := range []bool{true, false} {
		budgets := render(includeSession, false, 0)
		if lipgloss.Width(budgets) <= maxWidth {
			return budgets
		}
	}
	return render(false, false, 0)
}

// ---------------------------------------------------------------------------
// Footer
// ---------------------------------------------------------------------------

func (m Model) renderFooter() string {
	cmdHintStyle := lipgloss.NewStyle().Foreground(colorMuted)
	cmdSlashStyle := lipgloss.NewStyle().Foreground(colorBrandDim)
	modelStyle := lipgloss.NewStyle().Foreground(colorDim)

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
	hints := []string{"F2 settings", "F5 features", sidebarHint, posHint, copyHint}
	if m.contextDebug != "" {
		if m.debugExpanded {
			hints = append([]string{"F3 hide context"}, hints...)
		} else {
			hints = append([]string{"F3 context"}, hints...)
		}
	}
	if strings.TrimSpace(m.copyNotice) != "" {
		hints = append(hints, m.copyNotice)
	}
	settingsHint := lipgloss.NewStyle().Foreground(colorMuted).Render(strings.Join(hints, "  "))
	rightFooter := modelStyle.Render(valueOr(m.model, "routed")) + "  " + settingsHint + " "

	// Gradient separator
	sepWidth := max(10, m.width)
	gradColors := []string{"#7C3AED", "#6366F1", "#3B82F6", "#0EA5E9", "#06B6D4", "#0EA5E9", "#3B82F6", "#6366F1", "#7C3AED"}
	var sepBuilder strings.Builder
	for i := 0; i < sepWidth; i++ {
		colorIdx := i * len(gradColors) / sepWidth
		if colorIdx >= len(gradColors) {
			colorIdx = len(gradColors) - 1
		}
		sepBuilder.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(gradColors[colorIdx])).Render("─"))
	}
	separator := sepBuilder.String()

	return lipgloss.JoinVertical(lipgloss.Left, separator, leftFooter+strings.Repeat(" ", max(0, m.width-lipgloss.Width(leftFooter)-lipgloss.Width(rightFooter)))+rightFooter)
}

// ---------------------------------------------------------------------------
// Thinking animation
// ---------------------------------------------------------------------------

func (m Model) renderThinkingBar() string {
	const barWidth = 18
	head := m.spinnerFrame % barWidth

	var bar strings.Builder
	for i := 0; i < barWidth; i++ {
		distance := circularDistance(i, head, barWidth)
		densityIdx := max(0, len(densityChars)-1-distance)
		ch := densityChars[densityIdx]
		col := gradientPalette[(i+m.spinnerFrame)%len(gradientPalette)]
		bar.WriteString(lipgloss.NewStyle().Foreground(col).Render(string(ch)))
	}

	accent := lipgloss.NewStyle().Foreground(colorBrandDim).Render("⟩")
	label := lipgloss.NewStyle().Foreground(colorMuted).Italic(true).Render(" thinking...")
	return "  " + bar.String() + " " + accent + label
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
		case "/features":
			m.openFeaturesModal()
			return true, nil
		case "/tools":
			m.openToolsModal()
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
				if m.baseURLs == nil {
					m.baseURLs = map[string]string{}
				}
				m.baseURLs[strings.ToLower(valueOr(m.provider, "openai"))] = value
			case "endpoint_openai", "openai_endpoint":
				m.setProviderEndpoint("openai", value)
			case "endpoint_local", "local_endpoint":
				m.setProviderEndpoint("local", value)
			case "endpoint_ollama", "ollama_endpoint":
				m.setProviderEndpoint("ollama", value)
			case "endpoint_anthropic", "anthropic_endpoint":
				m.setProviderEndpoint("anthropic", value)
			case "endpoint_gemini", "gemini_endpoint":
				m.setProviderEndpoint("gemini", value)
			case "stream":
				m.stream = value != "off"
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
			case "context_cockpit_cards":
				if parsed := parsePositiveInt(value); parsed > 0 {
					m.contextCockpitMaxCards = parsed
				}
			case "context_cockpit_diff":
				m.contextCockpitDiff = value != "off"
			case "context_retrieval":
				m.contextRetrieval = value != "off"
			case "context_retrieval_tokens":
				if parsed := parsePositiveInt(value); parsed > 0 {
					m.contextRetrievalTokens = parsed
				}
			case "context_retrieval_chunks":
				if parsed := parsePositiveInt(value); parsed > 0 {
					m.contextRetrievalChunks = parsed
				}
			case "context_embeddings":
				m.contextEmbeddings = value != "off"
			case "context_reranker":
				m.contextReranker = value != "off"
			case "context_recipes":
				m.contextRecipes = value != "off"
			case "negative_context":
				m.negativeContext = value != "off"
			case "skills":
				m.skills = value != "off"
			case "disabled_tools", "disabled-tools":
				if value == "" {
					m.disabledTools = nil
				} else {
					parts := strings.Split(value, ",")
					cleaned := []string{}
					for _, p := range parts {
						cleaned = append(cleaned, strings.TrimSpace(p))
					}
					m.disabledTools = cleaned
				}
			case "fast_model":
				m.fastModel = value
			case "edit_model":
				m.editModel = value
			case "deep_model":
				m.deepModel = value
			}
		}
	case "/provider":
		m.provider = args[1]
		m.model = ""
	case "/model":
		m.model = strings.Join(args[1:], " ")
	case "/endpoint":
		m.baseURL = strings.Join(args[1:], " ")
		m.setProviderEndpoint(m.provider, m.baseURL)
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

func (m Model) renderStatsLine(line string) []string {
	parts := strings.Fields(line)
	var durationVal string
	var inputVal, cachedVal, outputVal, reasoningVal int

	for _, part := range parts {
		if strings.HasPrefix(part, "duration=") {
			durationVal = strings.TrimPrefix(part, "duration=")
		} else if strings.HasPrefix(part, "input=") {
			inputVal = parsePositiveInt(strings.TrimPrefix(part, "input="))
		} else if strings.HasPrefix(part, "cached=") {
			cachedVal = parsePositiveInt(strings.TrimPrefix(part, "cached="))
		} else if strings.HasPrefix(part, "output=") {
			outputVal = parsePositiveInt(strings.TrimPrefix(part, "output="))
		} else if strings.HasPrefix(part, "reasoning=") {
			reasoningVal = parsePositiveInt(strings.TrimPrefix(part, "reasoning="))
		}
	}

	timeStyle := lipgloss.NewStyle().Foreground(colorDim)
	sepStyle := lipgloss.NewStyle().Foreground(colorMuted)

	var timeStr string
	if durationVal != "" {
		timeStr = timeStyle.Render(durationVal)
	}
	sep := sepStyle.Render("  •  ")

	ctxText := fmt.Sprintf("%s ctx tokens", formatWithCommas(inputVal))
	if cachedVal > 0 {
		ctxText += fmt.Sprintf(" (%s cached)", formatWithCommas(cachedVal))
	}
	ctxBadge := pillBadge(ctxText, colorBadgeBg, colorBlue)

	var outBadge string
	if outputVal > 0 {
		outText := fmt.Sprintf("%s out tokens", formatWithCommas(outputVal))
		if reasoningVal > 0 {
			outText += fmt.Sprintf(" (%s reasoning)", formatWithCommas(reasoningVal))
		}
		outBadge = " " + pillBadge(outText, colorBadgeBg3, colorBrand)
	}

	var lineParts string
	if timeStr != "" {
		lineParts = timeStr + sep + ctxBadge + outBadge
	} else {
		lineParts = ctxBadge + outBadge
	}

	return []string{
		"",
		"  " + lineParts,
		"",
	}
}

func formatWithCommas(value int) string {
	if value < 0 {
		return "0"
	}
	s := strconv.Itoa(value)
	var res []string
	for len(s) > 3 {
		res = append([]string{s[len(s)-3:]}, res...)
		s = s[:len(s)-3]
	}
	if len(s) > 0 {
		res = append([]string{s}, res...)
	}
	return strings.Join(res, ",")
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
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return 0
	}
	return parsed
}

func (m *Model) setProviderEndpoint(provider, endpoint string) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		provider = "openai"
	}
	if m.baseURLs == nil {
		m.baseURLs = map[string]string{}
	}
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		delete(m.baseURLs, provider)
	} else {
		m.baseURLs[provider] = endpoint
	}
	if strings.EqualFold(valueOr(m.provider, "openai"), provider) {
		m.baseURL = endpoint
	}
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
	if strings.TrimSpace(m.streamBuf) == "" && strings.TrimSpace(m.reasoningText) != "" {
		m.appendAgentOutput(m.reasoningText)
		m.streamBuf = ""
		m.reasoningText = ""
		m.reasonExpanded = false
		return
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
	return m.renderViewportLines(items)
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

func (m Model) renderViewportLines(items []formattedLineItem) []string {
	lines := make([]string, 0, len(items))
	for idx, item := range items {
		if m.copySelectionActive {
			lines = append(lines, m.renderCopySelectionLine(item.text, idx))
			continue
		}
		lines = append(lines, item.text)
	}
	return lines
}

func (m *Model) handleCopyModeMouse(msg tea.MouseMsg) (bool, tea.Cmd) {
	switch {
	case msg.Button == tea.MouseButtonWheelUp && msg.Action == tea.MouseActionPress:
		m.viewport.LineUp(3)
		m.syncViewport(false)
		return true, nil
	case msg.Button == tea.MouseButtonWheelDown && msg.Action == tea.MouseActionPress:
		m.viewport.LineDown(3)
		m.syncViewport(false)
		return true, nil
	case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress:
		point, ok := m.copyPointFromMouse(msg.X, msg.Y)
		if !ok {
			return false, nil
		}
		m.copyDragActive = true
		m.copySelectionActive = true
		m.copySelectionStart = point
		m.copySelectionEnd = point
		m.syncViewport(false)
		return true, nil
	case msg.Action == tea.MouseActionMotion && m.copyDragActive:
		point, ok := m.copyPointFromMouse(msg.X, msg.Y)
		if !ok {
			return true, nil
		}
		m.copySelectionEnd = point
		m.copySelectionActive = true
		m.syncViewport(false)
		return true, nil
	case msg.Action == tea.MouseActionRelease && m.copyDragActive:
		point, ok := m.copyPointFromMouse(msg.X, msg.Y)
		if ok {
			m.copySelectionEnd = point
		}
		m.copyDragActive = false
		m.copySelectionActive = !m.copySelectionCollapsed()
		m.syncViewport(false)
		if !m.copySelectionActive {
			return true, nil
		}
		text := m.copySelectedText()
		if text == "" {
			return true, nil
		}
		return true, copyToClipboardCmd(text)
	}
	return false, nil
}

func (m *Model) clearCopySelection() {
	m.copyDragActive = false
	m.copySelectionActive = false
	m.copySelectionStart = copyPoint{}
	m.copySelectionEnd = copyPoint{}
}

func (m Model) copySelectionBounds() (copyPoint, copyPoint) {
	start := m.copySelectionStart
	end := m.copySelectionEnd
	if end.index < start.index || (end.index == start.index && end.col < start.col) {
		start, end = end, start
	}
	return start, end
}

func (m Model) copySelectionCollapsed() bool {
	start, end := m.copySelectionBounds()
	return start.index == end.index && start.col == end.col
}

func (m Model) copyPointFromMouse(x, y int) (copyPoint, bool) {
	if y < 0 || y >= m.viewport.Height {
		return copyPoint{}, false
	}
	localX := x
	if m.shouldRenderSidebar() && m.sidebarPosition == 0 {
		localX -= m.sidebarWidth()
	}
	if localX < 0 || localX >= m.chatWidth() {
		return copyPoint{}, false
	}
	items := m.currentViewportItems()
	index := m.viewport.YOffset + y
	if index < 0 || index >= len(items) {
		return copyPoint{}, false
	}
	col := visualColumnToRuneIndex(stripANSICodes(items[index].text), localX)
	return copyPoint{index: index, col: col}, true
}

func (m Model) copySelectedText() string {
	if !m.copySelectionActive {
		return ""
	}
	start, end := m.copySelectionBounds()
	items := m.currentViewportItems()
	if start.index < 0 || end.index >= len(items) || start.index > end.index {
		return ""
	}
	var parts []string
	for idx := start.index; idx <= end.index; idx++ {
		plain := stripANSICodes(items[idx].text)
		runes := []rune(plain)
		from := 0
		to := len(runes)
		if idx == start.index {
			from = clamp(start.col, 0, len(runes))
		}
		if idx == end.index {
			to = clamp(end.col, 0, len(runes))
		}
		if idx == start.index && idx == end.index && to < from {
			from, to = to, from
		}
		parts = append(parts, strings.TrimRight(string(runes[from:to]), " "))
	}
	return strings.Join(parts, "\n")
}

func (m Model) renderCopySelectionLine(line string, index int) string {
	if !m.copySelectionActive {
		return line
	}
	start, end := m.copySelectionBounds()
	if index < start.index || index > end.index {
		return line
	}
	plain := stripANSICodes(line)
	runes := []rune(plain)
	from := 0
	to := len(runes)
	if index == start.index {
		from = clamp(start.col, 0, len(runes))
	}
	if index == end.index {
		to = clamp(end.col, 0, len(runes))
	}
	if index == start.index && index == end.index && to < from {
		from, to = to, from
	}
	if from == to {
		return plain
	}
	style := lipgloss.NewStyle().Background(colorBlue).Foreground(colorInputBg)
	return string(runes[:from]) + style.Render(string(runes[from:to])) + string(runes[to:])
}

func copyToClipboardCmd(text string) tea.Cmd {
	return func() tea.Msg {
		return copyResultMsg{err: writeClipboard(text), chars: len([]rune(text))}
	}
}

func visualColumnToRuneIndex(text string, col int) int {
	if col <= 0 {
		return 0
	}
	runes := []rune(text)
	width := 0
	for i, r := range runes {
		nextWidth := width + termansi.StringWidth(string(r))
		if col <= nextWidth {
			return i + 1
		}
		width = nextWidth
	}
	return len(runes)
}

func stripANSICodes(value string) string {
	return termansi.Strip(value)
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

type toolPreviewSection struct {
	Label    string
	Value    string
	Code     bool
	Language string
}

func (m Model) renderStructuredToolCallPreview(toolName, rawArgs string) []string {
	if args, ok := parseToolPreviewArgs(rawArgs); ok {
		return m.renderStructuredToolArgs(toolName, args)
	}
	return m.renderToolPlainPreview(rawArgs)
}

func parseToolPreviewArgs(raw string) (map[string]any, bool) {
	var args map[string]any
	if json.Unmarshal([]byte(raw), &args) == nil && len(args) > 0 {
		return args, true
	}
	args = parsePartialJSONObject(raw)
	return args, len(args) > 0
}

func (m Model) renderStructuredToolArgs(toolName string, args map[string]any) []string {
	sections := buildToolPreviewSections(toolName, args, false)
	if len(sections) == 0 {
		return nil
	}
	var lines []string
	for _, section := range sections {
		lines = append(lines, m.renderToolPreviewSection(section)...)
	}
	return lines
}

func (m Model) renderToolPreviewSection(section toolPreviewSection) []string {
	labelStyle := lipgloss.NewStyle().Foreground(colorMuted)
	bodyStyle := lipgloss.NewStyle().Foreground(colorDim).Width(max(24, m.chatWidth()-14))
	borderStyle := lipgloss.NewStyle().Foreground(colorMuted)

	lines := []string{
		"  " + borderStyle.Render("│") + " " + labelStyle.Render(section.Label+":"),
	}
	if section.Code {
		rendered := m.renderToolCodeBlock(section.Value, section.Language)
		for _, line := range strings.Split(rendered, "\n") {
			lines = append(lines, "  "+borderStyle.Render("│")+" "+line)
		}
		return lines
	}
	for _, line := range strings.Split(bodyStyle.Render(section.Value), "\n") {
		lines = append(lines, "  "+borderStyle.Render("│")+" "+line)
	}
	return lines
}

func (m Model) renderToolPlainPreview(text string) []string {
	bodyStyle := lipgloss.NewStyle().Foreground(colorDim).Width(max(24, m.chatWidth()-12))
	borderStyle := lipgloss.NewStyle().Foreground(colorMuted)
	var lines []string
	for _, line := range strings.Split(bodyStyle.Render(text), "\n") {
		lines = append(lines, "  "+borderStyle.Render("│")+" "+line)
	}
	return lines
}

func (m Model) renderToolCodeBlock(value, language string) string {
	code := strings.TrimRight(value, "\n")
	if code == "" {
		return ""
	}
	if m.renderer == nil {
		return code
	}
	block := "```"
	if language != "" {
		block += language
	}
	block += "\n" + code + "\n```"
	rendered, err := m.renderer.Render(block)
	if err != nil {
		return code
	}
	return strings.TrimRight(rendered, "\n")
}

func buildToolPreviewSections(toolName string, args map[string]any, sanitizeSecrets bool) []toolPreviewSection {
	if len(args) == 0 {
		return nil
	}
	path, _ := args["path"].(string)
	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		ri := toolPreviewKeyRank(keys[i])
		rj := toolPreviewKeyRank(keys[j])
		if ri != rj {
			return ri < rj
		}
		return keys[i] < keys[j]
	})
	sections := make([]toolPreviewSection, 0, len(keys))
	for _, key := range keys {
		value := args[key]
		if sanitizeSecrets {
			value = sanitizeApprovalValue(key, value)
		}
		section, ok := buildToolPreviewSection(toolName, path, key, value)
		if ok {
			sections = append(sections, section)
		}
	}
	return sections
}

func toolPreviewSummary(toolName string, args map[string]any, maxLen int) string {
	sections := buildToolPreviewSections(toolName, args, false)
	if len(sections) == 0 {
		return ""
	}
	parts := make([]string, 0, min(3, len(sections)))
	for _, section := range sections {
		value := strings.TrimSpace(section.Value)
		if value == "" {
			continue
		}
		value = strings.ReplaceAll(value, "\n", " ")
		value = strings.Join(strings.Fields(value), " ")
		if section.Code {
			value = shorten(value, 28)
		} else {
			value = shorten(value, 20)
		}
		parts = append(parts, section.Label+"="+value)
		if len(parts) == 3 {
			break
		}
	}
	return outputSummary(strings.Join(parts, " "), maxLen)
}

func buildToolPreviewSection(toolName, path, key string, value any) (toolPreviewSection, bool) {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return toolPreviewSection{}, false
		}
		if toolPreviewTreatAsCode(toolName, key, typed) {
			return toolPreviewSection{
				Label:    key,
				Value:    typed,
				Code:     true,
				Language: toolPreviewLanguage(toolName, path, key),
			}, true
		}
		return toolPreviewSection{Label: key, Value: typed}, true
	default:
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return toolPreviewSection{Label: key, Value: fmt.Sprint(value)}, true
		}
		return toolPreviewSection{Label: key, Value: string(data)}, true
	}
}

func toolPreviewKeyRank(key string) int {
	switch key {
	case "path":
		return 0
	case "symbol":
		return 1
	case "start_line", "end_line", "after_line", "replace_all":
		return 2
	case "description":
		return 3
	case "content", "new", "old", "patch", "command":
		return 4
	default:
		return 5
	}
}

func toolPreviewTreatAsCode(toolName, key, value string) bool {
	switch key {
	case "content", "new", "old", "patch", "command":
		return true
	}
	if strings.Contains(value, "\n") {
		return true
	}
	return strings.EqualFold(toolName, "bash") && key == "command"
}

func toolPreviewLanguage(toolName, path, key string) string {
	switch key {
	case "patch":
		return "diff"
	case "command":
		return "bash"
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".tsx":
		return "tsx"
	case ".jsx":
		return "jsx"
	case ".json":
		return "json"
	case ".md":
		return "markdown"
	case ".html":
		return "html"
	case ".css":
		return "css"
	case ".sh":
		return "bash"
	case ".yml", ".yaml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".c":
		return "c"
	case ".cc", ".cpp", ".cxx", ".hpp", ".hh", ".h":
		return "cpp"
	default:
		return ""
	}
}

func parsePartialJSONObject(raw string) map[string]any {
	index := 0
	skipJSONWhitespace(raw, &index)
	if index < len(raw) && raw[index] == '{' {
		index++
	}
	out := map[string]any{}
	for index < len(raw) {
		skipJSONWhitespace(raw, &index)
		if index >= len(raw) || raw[index] == '}' {
			break
		}
		key, ok := readPartialJSONString(raw, &index)
		if !ok {
			break
		}
		skipJSONWhitespace(raw, &index)
		if index >= len(raw) || raw[index] != ':' {
			break
		}
		index++
		skipJSONWhitespace(raw, &index)
		value, ok := readPartialJSONValue(raw, &index)
		if !ok {
			break
		}
		out[key] = value
		skipJSONWhitespace(raw, &index)
		if index < len(raw) && raw[index] == ',' {
			index++
			continue
		}
		if index < len(raw) && raw[index] == '}' {
			break
		}
	}
	return out
}

func skipJSONWhitespace(raw string, index *int) {
	for *index < len(raw) {
		switch raw[*index] {
		case ' ', '\n', '\r', '\t':
			*index = *index + 1
		default:
			return
		}
	}
}

func readPartialJSONString(raw string, index *int) (string, bool) {
	if *index >= len(raw) || raw[*index] != '"' {
		return "", false
	}
	*index++
	var builder strings.Builder
	escaped := false
	for *index < len(raw) {
		ch := raw[*index]
		*index++
		if escaped {
			builder.WriteString(unescapePartialJSONEscape(ch, raw, index))
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '"' {
			return builder.String(), true
		}
		builder.WriteByte(ch)
	}
	if escaped {
		builder.WriteByte('\\')
	}
	return builder.String(), true
}

func unescapePartialJSONEscape(ch byte, raw string, index *int) string {
	switch ch {
	case 'n':
		return "\n"
	case 'r':
		return "\r"
	case 't':
		return "\t"
	case '\\':
		return "\\"
	case '"':
		return `"`
	case '/':
		return "/"
	case 'b':
		return "\b"
	case 'f':
		return "\f"
	case 'u':
		if *index+4 <= len(raw) {
			hex := raw[*index : *index+4]
			if parsed, err := strconv.ParseInt(hex, 16, 32); err == nil {
				*index += 4
				return string(rune(parsed))
			}
		}
		return `\u`
	default:
		return string(ch)
	}
}

func readPartialJSONValue(raw string, index *int) (any, bool) {
	if *index >= len(raw) {
		return nil, false
	}
	switch raw[*index] {
	case '"':
		return readPartialJSONString(raw, index)
	case '{', '[':
		return readPartialJSONComposite(raw, index)
	default:
		start := *index
		for *index < len(raw) {
			switch raw[*index] {
			case ',', '}', ']':
				goto done
			default:
				*index++
			}
		}
	done:
		token := strings.TrimSpace(raw[start:*index])
		if token == "" {
			return "", false
		}
		switch token {
		case "true":
			return true, true
		case "false":
			return false, true
		case "null":
			return "null", true
		}
		if i, err := strconv.ParseInt(token, 10, 64); err == nil {
			return i, true
		}
		if f, err := strconv.ParseFloat(token, 64); err == nil {
			return f, true
		}
		return token, true
	}
}

func readPartialJSONComposite(raw string, index *int) (any, bool) {
	start := *index
	open := raw[*index]
	close := byte('}')
	if open == '[' {
		close = ']'
	}
	depth := 0
	inString := false
	escaped := false
	for *index < len(raw) {
		ch := raw[*index]
		*index++
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			continue
		}
		if ch == open {
			depth++
			continue
		}
		if ch == close {
			depth--
			if depth == 0 {
				break
			}
		}
	}
	fragment := strings.TrimSpace(raw[start:*index])
	if fragment == "" {
		return "", false
	}
	var decoded any
	if json.Unmarshal([]byte(fragment), &decoded) == nil {
		return decoded, true
	}
	return fragment, true
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
	icon := "▸"
	if expanded {
		icon = "▾"
	}
	summary := "planned"
	if args != "" {
		if parsed, ok := parseToolPreviewArgs(args); ok {
			if preview := toolPreviewSummary(name, parsed, 64); preview != "" {
				summary += " " + preview
			} else {
				summary += " " + outputSummary(args, 64)
			}
		} else {
			summary += " " + outputSummary(args, 64)
		}
	}
	lines := []string{"  " + headerStyle.Render(icon+" "+name) + " " + labelStyle.Render(summary)}
	if !expanded {
		return lines
	}
	if args != "" {
		lines = append(lines, m.renderStructuredToolCallPreview(name, args)...)
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
		lines = append(lines, m.renderStructuredToolArgs(name, msg.Args)...)
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
		return "planned"
	case "running":
		return "running"
	case "done":
		if strings.TrimSpace(msg.Output) == "" {
			return "done"
		}
		return fmt.Sprintf("done %s", outputSummary(msg.Output, 70))
	case "error":
		return fmt.Sprintf("failed %s", outputSummary(msg.Error, 70))
	case "rejected":
		return fmt.Sprintf("rejected %s", outputSummary(msg.Error, 70))
	default:
		return phase
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
	borderStyle := lipgloss.NewStyle().Foreground(colorBrandDim)
	errorStyle := lipgloss.NewStyle().Foreground(colorRed)

	icon := "▸"
	if expanded {
		icon = "▾"
	}
	toolIcon := lipgloss.NewStyle().Foreground(colorAmber).Render("⚙")
	summary := toolDisplaySummary(display)
	lines := []string{
		"  " + toolIcon + " " + headerStyle.Render(icon+" "+display.Tool) + " " + labelStyle.Render(summary),
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
		return "failed " + outputSummary(display.Error, 70)
	}
	if strings.TrimSpace(display.Output) == "" {
		return "done"
	}
	return fmt.Sprintf("done %s", outputSummary(display.Output, 70))
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
	m.syncInputSize()
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
	m.syncInputSize()
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

func clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[strings.ToLower(strings.TrimSpace(key))] = value
	}
	return cloned
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
	userPrefix   = "❯"
	agentBar     = "│"
	errorPrefix  = "✖"
	systemPrefix = "›"
	acPointer    = "▸"
)

// ---------------------------------------------------------------------------
// Styles
// ---------------------------------------------------------------------------

var (
	userMsgStyle = lipgloss.NewStyle().
			Foreground(colorBlue).
			Bold(true)

	agentMsgStyle = lipgloss.NewStyle().
			Foreground(colorText)

	agentBarStyle = lipgloss.NewStyle().
			Foreground(colorBrandDim)

	errorMsgStyle = lipgloss.NewStyle().
			Foreground(colorRed).
			Bold(true)

	systemMsgStyle = lipgloss.NewStyle().
			Foreground(colorDim).
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
