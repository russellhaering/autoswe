package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/russellhaering/autoswe/pkg/agent"
	"github.com/russellhaering/autoswe/pkg/llm"
	"github.com/russellhaering/autoswe/pkg/session"
)

const (
	textareaHeight = 3
	headerHeight   = 1
	statusHeight   = 1
	gutter         = 1
)

var (
	userLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	asstLabelStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	toolUseStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	toolOKStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	toolErrStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	dimStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	headerStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	errStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
	sysStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Italic(true)
)

type eventMsg struct{ Event agent.Event }
type streamDoneMsg struct{}

type model struct {
	ctx context.Context
	cfg Config

	width, height int

	history    []llm.Message
	transcript strings.Builder

	viewport viewport.Model
	textarea textarea.Model
	spinner  spinner.Model

	running     bool
	events      <-chan agent.Event
	resultPtr   *agent.Result
	cancel      context.CancelFunc
	curTextOpen bool

	turnUsage  llm.Usage
	totalUsage llm.Usage
	statusErr  string
}

func newModel(ctx context.Context, cfg Config) *model {
	ta := textarea.New()
	ta.Placeholder = "Message autoswe (Ctrl+J for newline)…"
	ta.Focus()
	ta.Prompt = "│ "
	ta.CharLimit = 0
	ta.SetHeight(textareaHeight)
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline.SetKeys("ctrl+j")

	vp := viewport.New(80, 20)

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))

	m := &model{
		ctx:      ctx,
		cfg:      cfg,
		viewport: vp,
		textarea: ta,
		spinner:  sp,
		history:  append([]llm.Message{}, cfg.AgentOptions.InitialMessages...),
	}
	m.appendSystem(welcomeMessage(cfg))
	return m
}

func welcomeMessage(cfg Config) string {
	prov := cfg.AgentOptions.Provider
	provName := "unknown"
	if prov != nil {
		provName = prov.Name()
	}
	return fmt.Sprintf("autoswe TUI · %s/%s · session %s\nType a message and press Enter. /help for commands, Ctrl+C to quit.",
		provName, cfg.AgentOptions.Model, cfg.SessionID)
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.spinner.Tick)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layout()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case eventMsg:
		m.handleEvent(msg.Event)
		return m, nextEvent(m.events)

	case streamDoneMsg:
		return m, m.finishStream()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	var cmds []tea.Cmd
	if !m.running {
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		cmds = append(cmds, cmd)
	}
	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	cmds = append(cmds, vpCmd)
	return m, tea.Batch(cmds...)
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		if m.running && m.cancel != nil {
			m.cancel()
			return m, nil
		}
		return m, tea.Quit
	case "ctrl+d":
		if !m.running && m.textarea.Value() == "" {
			return m, tea.Quit
		}
	case "esc":
		if m.running && m.cancel != nil {
			m.cancel()
			return m, nil
		}
	case "enter":
		if m.running {
			return m, nil
		}
		input := strings.TrimSpace(m.textarea.Value())
		if input == "" {
			return m, nil
		}
		return m, m.handleInput(input)
	case "pgup":
		m.viewport.ScrollUp(m.viewport.Height / 2)
		return m, nil
	case "pgdown":
		m.viewport.ScrollDown(m.viewport.Height / 2)
		return m, nil
	}

	if m.running {
		return m, nil
	}
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

func (m *model) handleInput(input string) tea.Cmd {
	m.textarea.Reset()
	if strings.HasPrefix(input, "/") {
		return m.handleCommand(input)
	}
	m.appendUser(input)

	opts := m.cfg.AgentOptions
	opts.InitialMessages = m.history
	a, err := agent.New(opts)
	if err != nil {
		m.appendError(err)
		return nil
	}

	runCtx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel

	events, result := a.RunStream(runCtx, input)
	m.events = events
	m.resultPtr = result
	m.running = true
	m.curTextOpen = false
	m.turnUsage = llm.Usage{}
	m.statusErr = ""

	return tea.Batch(nextEvent(events), m.spinner.Tick)
}

func (m *model) handleCommand(cmd string) tea.Cmd {
	switch cmd {
	case "/exit", "/quit", "/q":
		return tea.Quit
	case "/clear":
		m.history = nil
		m.transcript.Reset()
		m.totalUsage = llm.Usage{}
		m.appendSystem("conversation cleared")
		m.viewport.SetContent(m.transcript.String())
		m.viewport.GotoBottom()
		return nil
	case "/help", "/?":
		m.appendSystem(helpText)
		return nil
	default:
		m.appendError(fmt.Errorf("unknown command: %s (try /help)", cmd))
		return nil
	}
}

const helpText = `Commands:
  /help, /?         this help
  /clear            forget conversation history (does not delete saved session)
  /exit, /quit, /q  quit the TUI
Keys:
  Enter             send
  Ctrl+J            insert newline
  Esc               cancel current run
  Ctrl+C            cancel run / quit when idle
  PgUp / PgDn       scroll transcript`

func nextEvent(ch <-chan agent.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return streamDoneMsg{}
		}
		return eventMsg{Event: ev}
	}
}

func (m *model) handleEvent(ev agent.Event) {
	switch e := ev.(type) {
	case agent.TextDelta:
		if !m.curTextOpen {
			m.appendLine(asstLabelStyle.Render("autoswe"))
			m.curTextOpen = true
		}
		m.appendRaw(e.Text)
	case agent.ToolUse:
		if m.curTextOpen {
			m.appendRaw("\n")
			m.curTextOpen = false
		}
		m.appendLine(toolUseStyle.Render("→ "+e.Name) + " " + dimStyle.Render(summarizeArgs(e.Input)))
	case agent.ToolResult:
		if e.IsError {
			m.appendLine(toolErrStyle.Render("← " + e.Name + " error: " + oneLine(e.Content)))
		} else {
			m.appendLine(toolOKStyle.Render(fmt.Sprintf("← %s (%s)", e.Name, humanBytes(len(e.Content)))))
		}
	case agent.Stop:
		m.turnUsage = e.Usage
	case agent.Error:
		m.statusErr = e.Err.Error()
		m.appendError(e.Err)
	}
}

func (m *model) finishStream() tea.Cmd {
	m.running = false
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	if m.curTextOpen {
		m.appendRaw("\n")
		m.curTextOpen = false
	}
	if m.resultPtr != nil && len(m.resultPtr.Messages) > 0 {
		m.history = m.resultPtr.Messages
		if err := session.Save(m.cfg.SessionDir, m.cfg.SessionID, m.history); err != nil {
			m.appendError(fmt.Errorf("save session: %w", err))
		}
	}
	m.totalUsage.InputTokens += m.turnUsage.InputTokens
	m.totalUsage.OutputTokens += m.turnUsage.OutputTokens
	m.appendRaw("\n")
	return nil
}

// View

func (m *model) View() string {
	if m.height == 0 {
		return "initializing…"
	}
	header := headerStyle.Render(m.renderHeader())
	status := m.renderStatus()
	return strings.Join([]string{
		header,
		m.viewport.View(),
		m.textarea.View(),
		status,
	}, "\n")
}

func (m *model) renderHeader() string {
	provName := "?"
	if p := m.cfg.AgentOptions.Provider; p != nil {
		provName = p.Name()
	}
	return fmt.Sprintf("autoswe · %s/%s · %s", provName, m.cfg.AgentOptions.Model, m.cfg.SessionID)
}

func (m *model) renderStatus() string {
	var parts []string
	if m.running {
		parts = append(parts, m.spinner.View()+"running")
		parts = append(parts, "esc to cancel")
	} else {
		parts = append(parts, "enter to send")
		parts = append(parts, "ctrl+j newline")
		parts = append(parts, "/help")
	}
	parts = append(parts, fmt.Sprintf("turn in:%d out:%d", m.turnUsage.InputTokens, m.turnUsage.OutputTokens))
	parts = append(parts, fmt.Sprintf("total in:%d out:%d", m.totalUsage.InputTokens, m.totalUsage.OutputTokens))
	line := strings.Join(parts, " · ")
	if m.statusErr != "" {
		line += " · " + errStyle.Render("err: "+oneLine(m.statusErr))
	}
	return dimStyle.Render(line)
}

func (m *model) layout() {
	taH := m.textarea.Height()
	vpH := m.height - headerHeight - statusHeight - taH - gutter
	if vpH < 3 {
		vpH = 3
	}
	m.viewport.Width = m.width
	m.viewport.Height = vpH
	m.textarea.SetWidth(m.width)
	m.viewport.SetContent(m.transcript.String())
	m.viewport.GotoBottom()
}

// Transcript helpers

func (m *model) appendRaw(s string) {
	m.transcript.WriteString(s)
	m.viewport.SetContent(m.transcript.String())
	m.viewport.GotoBottom()
}

func (m *model) appendLine(s string) {
	m.appendRaw(s + "\n")
}

func (m *model) appendUser(text string) {
	m.appendLine(userLabelStyle.Render("you"))
	m.appendRaw(text + "\n\n")
}

func (m *model) appendSystem(text string) {
	m.appendLine(sysStyle.Render(text))
}

func (m *model) appendError(err error) {
	m.appendLine(errStyle.Render("error: ") + err.Error())
}

// Utilities

func summarizeArgs(b json.RawMessage) string {
	const maxLen = 80
	s := string(b)
	if s == "" || s == "null" {
		return ""
	}
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > maxLen {
		s = s[:maxLen] + "…"
	}
	return s
}

func oneLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + "…"
	}
	return s
}

func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
