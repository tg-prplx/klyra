package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Color palette
// ---------------------------------------------------------------------------

var (
	colorBrand      = lipgloss.Color("#A78BFA") // violet
	colorBrandDim   = lipgloss.Color("#7C3AED") // deeper violet
	colorBlue       = lipgloss.Color("#60A5FA") // soft blue
	colorText       = lipgloss.Color("#E5E7EB") // off-white
	colorDim        = lipgloss.Color("#6B7280") // warm gray
	colorMuted      = lipgloss.Color("#4B5563") // muted
	colorSeparator  = lipgloss.Color("#374151") // charcoal
	colorSurface    = lipgloss.Color("#1F2937") // dark surface
	colorEmerald    = lipgloss.Color("#34D399") // green
	colorAmber      = lipgloss.Color("#FBBF24") // amber
	colorRed        = lipgloss.Color("#F87171") // soft red
	colorBadgeBg    = lipgloss.Color("#312E81") // indigo dark bg
	colorBadgeBg2   = lipgloss.Color("#1E3A5F") // blue dark bg
	colorBadgeBg3   = lipgloss.Color("#3B1F5E") // purple dark bg
	colorBadgeBg4   = lipgloss.Color("#1A3636") // teal dark bg
	colorWhite      = lipgloss.Color("#F9FAFB") // near-white
	colorInputBg    = lipgloss.Color("#111827") // very dark bg
)

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

// Pulse sizes: bar width oscillates for a breathing effect.
var pulseSizes = []int{3, 4, 5, 7, 9, 11, 12, 12, 11, 9, 7, 5, 4, 3}

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

type StreamMsg string

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
	Title           string
	SessionID       string
	Provider        string
	Model           string
	BaseURL         string
	Reasoning       string
	Sandbox         string
	Approval        string
	Mode            string
	CartCount       int
	MaxContext      int
	MaxOutput       int
	MaxSteps        int
	MaxMessages     int
	MaxInstructions int
	FastModel       string
	EditModel       string
	DeepModel       string
	Handler         Handler
	Commands        []CommandDef
}

type modalKind int

const (
	modalNone modalKind = iota
	modalPicker
	modalSettings
	modalHelp
)

type Model struct {
	title           string
	sessionID       string
	provider        string
	model           string
	baseURL         string
	reasoning       string
	sandbox         string
	approval        string
	mode            string
	cartCount       int
	maxContext      int
	maxOutput       int
	maxSteps        int
	maxMessages     int
	maxInstructions int
	fastModel       string
	editModel       string
	deepModel       string
	handler         Handler
	input           textinput.Model
	lines           []string
	width           int
	height          int
	busy            bool
	err             error
	commands        []CommandDef
	filteredCmds    []CommandDef
	selectedCmdIdx  int
	streamBuf       string
	renderer        *glamour.TermRenderer
	approvalReq     *ApprovalRequestMsg
	spinnerFrame    int

	// Modal state
	activeModal   modalKind
	pickerModal   *PickerModal
	helpModal     *HelpModal
	settingsModal *SettingsModal
}

type responseMsg struct {
	input  string
	output string
	err    error
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
		glamour.WithWordWrap(80),
	)

	return Model{
		title:           title,
		sessionID:       cfg.SessionID,
		provider:        cfg.Provider,
		model:           cfg.Model,
		baseURL:         cfg.BaseURL,
		reasoning:       cfg.Reasoning,
		sandbox:         cfg.Sandbox,
		approval:        cfg.Approval,
		mode:            cfg.Mode,
		cartCount:       cfg.CartCount,
		maxContext:      cfg.MaxContext,
		maxOutput:       cfg.MaxOutput,
		maxSteps:        cfg.MaxSteps,
		maxMessages:     cfg.MaxMessages,
		maxInstructions: cfg.MaxInstructions,
		fastModel:       cfg.FastModel,
		editModel:       cfg.EditModel,
		deepModel:       cfg.DeepModel,
		handler:         handler,
		input:           input,
		commands:        cfg.Commands,
		filteredCmds:    nil,
		selectedCmdIdx:  0,
		renderer:        renderer,
		lines:           []string{},
	}
}

func (m Model) Init() tea.Cmd {
	return tickSpinner()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = max(20, msg.Width-6)
		m.renderer, _ = glamour.NewTermRenderer(
			glamour.WithStandardStyle("dark"),
			glamour.WithWordWrap(max(40, m.width-8)),
		)
		return m, nil
	case spinnerTickMsg:
		if m.busy {
			m.spinnerFrame = (m.spinnerFrame + 1) % animTotalFrames
		}
		return m, tickSpinner()
	case tea.KeyMsg:
		// Approval prompt takes highest priority
		if m.approvalReq != nil {
			switch msg.String() {
			case "y", "Y", "enter":
				m.approvalReq.Reply <- true
				m.lines = append(m.lines, "system: approved "+m.approvalReq.Tool)
				m.approvalReq = nil
				return m, nil
			case "n", "N", "esc":
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
			return m, tea.Quit
		case "f2", "ctrl+s":
			m.openSettingsModal()
			return m, nil
		case "up", "shift+tab":
			if len(m.filteredCmds) > 0 {
				m.selectedCmdIdx--
				if m.selectedCmdIdx < 0 {
					m.selectedCmdIdx = len(m.filteredCmds) - 1
				}
				return m, nil
			}
		case "down", "tab":
			if len(m.filteredCmds) > 0 {
				m.selectedCmdIdx++
				if m.selectedCmdIdx >= len(m.filteredCmds) {
					m.selectedCmdIdx = 0
				}
				return m, nil
			}
		case "enter":
			if len(m.filteredCmds) > 0 {
				m.input.SetValue(m.filteredCmds[m.selectedCmdIdx].Name + " ")
				m.input.SetCursor(len(m.input.Value()))
				m.filteredCmds = nil
				return m, nil
			}

			value := strings.TrimSpace(m.input.Value())
			if value == "" || m.busy {
				return m, nil
			}
			m.input.SetValue("")
			m.filteredCmds = nil
			if value == "/exit" || value == "/quit" {
				return m, tea.Quit
			}
			if handled, cmd := m.handleLocalCommand(value); handled {
				return m, cmd
			}
			m.busy = true
			m.streamBuf = ""
			m.spinnerFrame = 0
			if len(m.lines) > 0 {
				m.lines = append(m.lines, "")
			}
			m.lines = append(m.lines, "you: "+value)
			return m, runHandler(m.handler, value)
		}
	case StreamMsg:
		m.streamBuf += string(msg)
		return m, nil
	case ApprovalRequestMsg:
		m.approvalReq = &msg
		return m, nil
	case responseMsg:
		m.busy = false
		m.streamBuf = ""
		m.err = msg.err
		if msg.err != nil {
			m.lines = append(m.lines, "")
			m.lines = append(m.lines, "error: "+msg.err.Error())
		}

		outText := strings.TrimSpace(msg.output)
		if outText != "" {
			m.lines = append(m.lines, "")

			isAgentStream := strings.Contains(outText, "assistant: ") || strings.Contains(outText, "tool: ")
			if !isAgentStream {
				text := outText
				if m.renderer != nil && strings.HasPrefix(text, "#") {
					if rendered, errRender := m.renderer.Render(text); errRender == nil {
						text = strings.TrimRight(rendered, " \n\r\t")
					}
				}
				for _, line := range strings.Split(text, "\n") {
					m.lines = append(m.lines, line)
				}
			} else {
				var assistantBlock []string
				flushAssistant := func() {
					if len(assistantBlock) > 0 {
						text := strings.Join(assistantBlock, "\n")
						if m.renderer != nil {
							if rendered, errRender := m.renderer.Render(text); errRender == nil {
								text = strings.TrimRight(rendered, " \n\r\t")
							}
						}
						m.lines = append(m.lines, "agent: "+text+"\x1b[0m")
						assistantBlock = nil
					}
				}

				for _, line := range strings.Split(outText, "\n") {
					if strings.HasPrefix(line, "assistant: ") {
						assistantBlock = append(assistantBlock, strings.TrimPrefix(line, "assistant: "))
					} else if strings.HasPrefix(line, "tool: ") || strings.HasPrefix(line, "tool rejected:") || strings.HasPrefix(line, "usage:") {
						flushAssistant()
						m.lines = append(m.lines, line)
					} else if strings.TrimSpace(line) == "" {
						if len(assistantBlock) > 0 {
							assistantBlock = append(assistantBlock, "")
						} else {
							m.lines = append(m.lines, "")
						}
					} else {
						if len(assistantBlock) > 0 {
							assistantBlock = append(assistantBlock, line)
						} else {
							m.lines = append(m.lines, line)
						}
					}
				}
				flushAssistant()
			}
		}
		return m, nil
	}

	var cmd tea.Cmd
	prevVal := m.input.Value()
	m.input, cmd = m.input.Update(msg)

	if m.input.Value() != prevVal {
		m.updateCompletions()
	}

	return m, cmd
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

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

func (m Model) View() string {
	headerLines := strings.Split(m.renderHeader(), "\n")

	// Chat history
	var formattedLines []string
	formattedLines = append(formattedLines, headerLines...)
	formattedLines = append(formattedLines, "") // breathing room after header

	for _, line := range m.lines {
		if strings.HasPrefix(line, "you: ") {
			formattedLines = append(formattedLines, userMsgStyle.Render("  "+userPrefix+" "+line[5:]))
		} else if strings.HasPrefix(line, "agent: ") {
			agentLines := strings.Split(line[7:], "\n")
			for _, al := range agentLines {
				formattedLines = append(formattedLines, agentBarStyle.Render(agentBar)+" "+agentMsgStyle.Render(al))
			}
		} else if strings.HasPrefix(line, "error: ") {
			formattedLines = append(formattedLines, errorMsgStyle.Render("  "+errorPrefix+" "+line[7:]))
		} else if strings.HasPrefix(line, "system: ") {
			formattedLines = append(formattedLines, systemMsgStyle.Render("  "+systemPrefix+" "+line[8:]))
		} else if line == "" {
			formattedLines = append(formattedLines, "")
		} else {
			formattedLines = append(formattedLines, systemMsgStyle.Render("  "+systemPrefix+" "+line))
		}
	}

	// Streaming content with spinner
	if m.busy && m.streamBuf != "" {
		formattedLines = append(formattedLines, "")

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
			formattedLines = append(formattedLines, agentBarStyle.Render(agentBar)+" "+agentMsgStyle.Render(al)+"\x1b[0m")
		}
	} else if m.busy {
		// Pulsing gradient bar animation
		formattedLines = append(formattedLines, "")
		formattedLines = append(formattedLines, m.renderThinkingBar())
	}

	// Autocomplete
	var autocomplete string
	autocompleteHeight := 0
	if len(m.filteredCmds) > 0 {
		autocomplete, autocompleteHeight = m.renderAutocomplete()
	}

	// Footer
	footer := m.renderFooter()
	footerHeight := lipgloss.Height(footer)

	// Input with separator
	inputSep := lipgloss.NewStyle().Foreground(colorSeparator).Render(strings.Repeat("─", max(10, m.width)))
	inputView := inputSep + "\n" + m.input.View()
	inputHeight := 2

	// Calculate layout
	bodyHeight := m.height - footerHeight - inputHeight
	if autocompleteHeight > 0 {
		bodyHeight -= autocompleteHeight
	}

	if bodyHeight < 5 {
		bodyHeight = 5
	}

	bodyLines := visibleTail(formattedLines, bodyHeight)

	// Push content to the bottom
	padding := bodyHeight - len(bodyLines)
	if padding > 0 {
		for i := 0; i < padding; i++ {
			bodyLines = append([]string{""}, bodyLines...)
		}
	}

	body := strings.Join(bodyLines, "\n")

	// Overlays: approval prompt
	if m.approvalReq != nil {
		body = lipgloss.JoinVertical(lipgloss.Left, body, m.renderApprovalModal())
	}

	// Modal overlays
	switch m.activeModal {
	case modalPicker:
		if m.pickerModal != nil {
			body = lipgloss.JoinVertical(lipgloss.Left, body, m.pickerModal.View(m.width))
		}
	case modalHelp:
		if m.helpModal != nil {
			body = lipgloss.JoinVertical(lipgloss.Left, body, m.helpModal.View(m.width, m.height))
		}
	case modalSettings:
		if m.settingsModal != nil {
			body = lipgloss.JoinVertical(lipgloss.Left, body, m.settingsModal.View(m.width, m.height))
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
		m.maxContext, m.maxOutput, m.maxSteps, m.maxMessages, m.maxInstructions,
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
		}

		// Send to handler
		cmdText := "/" + field + " " + value
		m.lines = append(m.lines, "system: "+field+" → "+valueOr(value, "default"))
		return m, runHandler(m.handler, cmdText)
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
	case "enter", "ctrl+s":
		// Save all settings
		m.provider = sm.GetValue("provider")
		m.model = sm.GetValue("model")
		m.baseURL = sm.GetValue("endpoint")
		m.reasoning = sm.GetValue("reasoning")
		m.approval = sm.GetValue("approval")
		m.sandbox = sm.GetValue("sandbox")
		m.mode = sm.GetValue("mode")
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
			fmt.Sprintf("context=%d", m.maxContext),
			fmt.Sprintf("output=%d", m.maxOutput),
		)
		cmdText := strings.Join(parts, " ")

		// Handle API keys — set env vars at runtime
		for _, envField := range []struct{ name, envVar string }{
			{"openai_key", "OPENAI_API_KEY"},
			{"anthropic_key", "ANTHROPIC_API_KEY"},
			{"gemini_key", "GEMINI_API_KEY"},
		} {
			if val := sm.GetValue(envField.name); val != "" {
				_ = setEnvIfChanged(envField.envVar, val)
			}
		}

		m.closeModal()
		m.lines = append(m.lines, "system: settings saved")
		return m, runHandler(m.handler, cmdText)
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

	// Title line
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

	// Safety line
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
		budgetParts = append(budgetParts, lipgloss.NewStyle().Foreground(colorMuted).Render("endpoint ")+lipgloss.NewStyle().Foreground(colorDim).Render(shorten(m.baseURL, 34)))
	}
	budgets := strings.Join(budgetParts, lipgloss.NewStyle().Foreground(colorSeparator).Render(" · "))

	// Separator
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

	settingsHint := lipgloss.NewStyle().Foreground(colorMuted).Render("F2 settings")
	rightFooter := modelStyle.Render(valueOr(m.model, "routed")) + "  " + settingsHint + " "

	separator := sepStyle.Render(strings.Repeat("─", max(10, m.width)))

	return lipgloss.JoinVertical(lipgloss.Left, separator, leftFooter+strings.Repeat(" ", max(0, m.width-lipgloss.Width(leftFooter)-lipgloss.Width(rightFooter)))+rightFooter)
}

// ---------------------------------------------------------------------------
// Thinking animation
// ---------------------------------------------------------------------------

func (m Model) renderThinkingBar() string {
	barLen := pulseSizes[m.spinnerFrame%len(pulseSizes)]
	colorOffset := m.spinnerFrame * 2 // flow speed

	var bar strings.Builder
	for i := 0; i < barLen; i++ {
		// Pick gradient color (flows over time)
		cIdx := (i + colorOffset) % len(gradientPalette)
		col := gradientPalette[cIdx]

		// Pick block density char (fade at edges)
		var ch rune
		distFromEdge := i
		if barLen-1-i < distFromEdge {
			distFromEdge = barLen - 1 - i
		}
		if distFromEdge >= len(densityChars) {
			ch = densityChars[len(densityChars)-1] // full block
		} else {
			ch = densityChars[distFromEdge]
		}

		bar.WriteString(lipgloss.NewStyle().Foreground(col).Render(string(ch)))
	}

	label := lipgloss.NewStyle().Foreground(colorMuted).Italic(true).Render(" thinking...")
	return "  " + bar.String() + label
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
		m.busy = true
		m.streamBuf = ""
		m.spinnerFrame = 0
		if len(m.lines) > 0 {
			m.lines = append(m.lines, "")
		}
		m.lines = append(m.lines, "you: "+value)
		return true, runHandler(m.handler, value)
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
			case "context":
				if parsed := parsePositiveInt(value); parsed > 0 {
					m.maxContext = parsed
				}
			case "output":
				if parsed := parsePositiveInt(value); parsed > 0 {
					m.maxOutput = parsed
				}
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
		return "~"
	case "error":
		return "x"
	default:
		return "*"
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

func runHandler(handler Handler, input string) tea.Cmd {
	return func() tea.Msg {
		output, err := handler(input)
		return responseMsg{input: input, output: output, err: err}
	}
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
