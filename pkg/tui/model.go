package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Handler func(string) (string, error)

type Config struct {
	Title     string
	SessionID string
	Provider  string
	Model     string
	Handler   Handler
}

type Model struct {
	title     string
	sessionID string
	provider  string
	model     string
	handler   Handler
	input     textinput.Model
	lines     []string
	width     int
	height    int
	busy      bool
	err       error
}

type responseMsg struct {
	input  string
	output string
	err    error
}

func New(cfg Config) Model {
	input := textinput.New()
	input.Placeholder = "Ask for a coding task or type /help"
	input.Prompt = "> "
	input.Focus()
	input.CharLimit = 8000

	title := cfg.Title
	if strings.TrimSpace(title) == "" {
		title = "Agent CLI"
	}
	handler := cfg.Handler
	if handler == nil {
		handler = func(string) (string, error) { return "", nil }
	}

	return Model{
		title:     title,
		sessionID: cfg.SessionID,
		provider:  cfg.Provider,
		model:     cfg.Model,
		handler:   handler,
		input:     input,
		lines: []string{
			"Type /help for commands. Press Ctrl+C or type /exit to quit.",
		},
	}
}

func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = max(20, msg.Width-4)
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "enter":
			value := strings.TrimSpace(m.input.Value())
			if value == "" || m.busy {
				return m, nil
			}
			m.input.SetValue("")
			if value == "/exit" || value == "/quit" {
				return m, tea.Quit
			}
			if handled, cmd := m.handleLocalCommand(value); handled {
				return m, cmd
			}
			m.busy = true
			m.lines = append(m.lines, "you: "+value)
			return m, runHandler(m.handler, value)
		}
	case responseMsg:
		m.busy = false
		m.err = msg.err
		if msg.err != nil {
			m.lines = append(m.lines, "error: "+msg.err.Error())
		}
		if strings.TrimSpace(msg.output) != "" {
			m.lines = append(m.lines, "agent: "+strings.TrimSpace(msg.output))
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	header := headerStyle.Render(m.title)
	meta := fmt.Sprintf("provider=%s model=%s session=%s", valueOr(m.provider, "default"), valueOr(m.model, "routed"), valueOr(m.sessionID, "ephemeral"))
	bodyHeight := m.height - 6
	if bodyHeight < 6 {
		bodyHeight = 6
	}
	body := visibleTail(m.lines, bodyHeight)
	status := "ready"
	if m.busy {
		status = "running"
	}
	if m.err != nil {
		status = "error: " + m.err.Error()
	}
	return strings.Join([]string{
		header,
		metaStyle.Render(meta),
		bodyStyle.Width(max(40, m.width-2)).Height(bodyHeight).Render(strings.Join(body, "\n")),
		statusStyle.Render(status),
		m.input.View(),
	}, "\n")
}

func (m *Model) handleLocalCommand(value string) (bool, tea.Cmd) {
	switch value {
	case "/help":
		m.lines = append(m.lines,
			"commands: /help, /status, /compact, /clear, /exit",
			"/status and /compact are handled by the agent command bridge when available.",
		)
		return true, nil
	case "/clear":
		m.lines = nil
		return true, nil
	case "/status", "/compact":
		m.busy = true
		m.lines = append(m.lines, "you: "+value)
		return true, runHandler(m.handler, value)
	default:
		if strings.HasPrefix(value, "/") {
			m.lines = append(m.lines, "unknown command: "+value)
			return true, nil
		}
		return false, nil
	}
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

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	metaStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	bodyStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
)
