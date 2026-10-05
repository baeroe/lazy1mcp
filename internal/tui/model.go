// Package tui is the lazy1mcp terminal UI: a server list, a detail pane with the server's tools, and
// forms/editor hand-off to change the 1MCP config.
package tui

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/baeroe/lazy1mcp/internal/config"
	"github.com/baeroe/lazy1mcp/internal/onemcp"
)

type focus int

const (
	focusServers focus = iota
	focusTools
)

type mode int

const (
	modeNormal mode = iota
	modeFilter
	modeForm
	modeConfirm
	modeHelp
	modeJSON
)

const (
	tickEvery    = 2 * time.Second
	statusEvery  = 10 * time.Second
	reloadDelay  = 1500 * time.Millisecond // 1MCP debounces config reloads by 500ms, then reconnects
	flashTimeout = 5 * time.Second
)

// Model is the bubbletea model.
type Model struct {
	cfg    *config.File
	client onemcp.Client

	servers []*config.Server
	visible []int // indices into servers after filtering
	cursor  int   // index into visible

	status     map[string]onemcp.Status
	statusErr  error
	statusAt   time.Time
	health     *onemcp.Health
	refreshing bool

	tools        map[string][]onemcp.Tool
	toolsErr     map[string]error
	toolsLoading map[string]bool
	toolCursor   int

	focus       focus
	mode        mode
	filter      textinput.Model
	form        *form
	json        *jsonEditor
	confirm     *confirm
	showSecrets bool

	flash    string
	flashBad bool
	flashAt  time.Time

	w, h int
}

type confirm struct {
	prompt string
	yes    func(m *Model) tea.Cmd
	noMode mode // where "no" returns to (default: normal)
}

// New builds the model for a loaded config.
func New(cfg *config.File, client onemcp.Client) *Model {
	fi := textinput.New()
	fi.Prompt = "/"
	fi.Placeholder = "filter by name or tag"
	m := &Model{
		cfg:          cfg,
		client:       client,
		status:       map[string]onemcp.Status{},
		tools:        map[string][]onemcp.Tool{},
		toolsErr:     map[string]error{},
		toolsLoading: map[string]bool{},
		filter:       fi,
	}
	m.reloadServers("")
	return m
}

// --- messages ---------------------------------------------------------------------------

type tickMsg time.Time

type refreshMsg struct{}

type statusMsg struct {
	health *onemcp.Health
	list   []onemcp.Status
	err    error
}

type toolsMsg struct {
	name  string
	tools []onemcp.Tool
	err   error
}

// restartMsg re-enables a server that restart() disabled.
type restartMsg struct{ name string }

func tick() tea.Cmd {
	return tea.Tick(tickEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func refreshLater() tea.Cmd {
	return tea.Tick(reloadDelay, func(time.Time) tea.Msg { return refreshMsg{} })
}

// Init starts polling and the first status fetch.
func (m *Model) Init() tea.Cmd { return tea.Batch(m.refresh(), tick()) }

func (m *Model) refresh() tea.Cmd {
	if m.refreshing {
		return nil
	}
	m.refreshing = true
	c := m.client
	return func() tea.Msg {
		ctx := context.Background()
		h, err := c.Health(ctx)
		if err != nil {
			return statusMsg{err: fmt.Errorf("1mcp not reachable at %s", c.URL)}
		}
		list, err := c.Servers(ctx)
		return statusMsg{health: h, list: list, err: err}
	}
}

func (m *Model) loadTools(name string) tea.Cmd {
	if m.toolsLoading[name] {
		return nil
	}
	m.toolsLoading[name] = true
	c := m.client
	return func() tea.Msg {
		t, err := c.Tools(context.Background(), name)
		return toolsMsg{name: name, tools: t, err: err}
	}
}

// ensureTools loads the tools of the selected server when they are not cached yet.
func (m *Model) ensureTools() tea.Cmd {
	s := m.selected()
	if s == nil || s.Disabled || s.Err != nil {
		return nil
	}
	if _, ok := m.tools[s.Name]; ok {
		return nil
	}
	if _, ok := m.toolsErr[s.Name]; ok {
		return nil
	}
	if st, ok := m.status[s.Name]; !ok || !st.Available {
		return nil
	}
	return m.loadTools(s.Name)
}

func (m *Model) forgetTools(name string) {
	delete(m.tools, name)
	delete(m.toolsErr, name)
}

// --- servers / selection ----------------------------------------------------------------

func (m *Model) selected() *config.Server {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return nil
	}
	return m.servers[m.visible[m.cursor]]
}

// reloadServers re-reads the parsed servers from cfg and keeps the selection on keep (or the current one).
func (m *Model) reloadServers(keep string) {
	if keep == "" {
		if s := m.selected(); s != nil {
			keep = s.Name
		}
	}
	m.servers = m.cfg.Servers()
	m.applyFilter()
	for i, idx := range m.visible {
		if m.servers[idx].Name == keep {
			m.cursor = i
		}
	}
	m.clampCursor()
}

func (m *Model) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	m.visible = m.visible[:0]
	for i, s := range m.servers {
		if q == "" || strings.Contains(strings.ToLower(s.Name), q) ||
			slices.ContainsFunc(s.Tags, func(t string) bool { return strings.Contains(strings.ToLower(t), q) }) {
			m.visible = append(m.visible, i)
		}
	}
	m.clampCursor()
}

func (m *Model) clampCursor() {
	m.cursor = min(m.cursor, len(m.visible)-1)
	m.cursor = max(m.cursor, 0)
}

// toolRows merges the live tool list with the tools disabled in the config (which 1MCP hides).
type toolRow struct {
	name, desc string
	disabled   bool
}

func (m *Model) toolRows(s *config.Server) []toolRow {
	var rows []toolRow
	seen := map[string]bool{}
	for _, t := range m.tools[s.Name] {
		seen[t.Tool] = true
		rows = append(rows, toolRow{t.Tool, t.Description, slices.Contains(s.DisabledTools, t.Tool)})
	}
	for _, t := range s.DisabledTools {
		if !seen[t] {
			rows = append(rows, toolRow{name: t, disabled: true})
		}
	}
	slices.SortFunc(rows, func(a, b toolRow) int { return strings.Compare(a.name, b.name) })
	return rows
}

// --- mutations --------------------------------------------------------------------------

func (m *Model) setFlash(msg string, bad bool) {
	m.flash, m.flashBad, m.flashAt = msg, bad, time.Now()
}

// commit saves the config after a mutation; on failure the file is re-read so the UI shows what is on disk.
func (m *Model) commit(keep, msg string) tea.Cmd {
	if err := m.cfg.Save(); err != nil {
		m.setFlash("save failed: "+err.Error(), true)
		if f, lerr := config.Load(m.cfg.Path); lerr == nil {
			m.cfg = f
		}
		m.reloadServers("")
		return nil
	}
	m.reloadServers(keep)
	m.setFlash(msg, false)
	return refreshLater()
}

func (m *Model) toggleServer() tea.Cmd {
	s := m.selected()
	if s == nil || s.Err != nil {
		return nil
	}
	if err := m.cfg.SetDisabled(s.Name, !s.Disabled); err != nil {
		m.setFlash(err.Error(), true)
		return nil
	}
	m.forgetTools(s.Name)
	verb := "disabled"
	if s.Disabled {
		verb = "enabled"
	}
	return m.commit(s.Name, s.Name+" "+verb)
}

func (m *Model) toggleTool() tea.Cmd {
	s := m.selected()
	if s == nil {
		return nil
	}
	rows := m.toolRows(s)
	if m.toolCursor >= len(rows) {
		return nil
	}
	t := rows[m.toolCursor].name
	off, err := m.cfg.ToggleTool(s.Name, t)
	if err != nil {
		m.setFlash(err.Error(), true)
		return nil
	}
	verb := "enabled"
	if off {
		verb = "disabled"
	}
	return m.commit(s.Name, fmt.Sprintf("%s/%s %s", s.Name, t, verb))
}

func (m *Model) askDelete() {
	s := m.selected()
	if s == nil {
		return
	}
	name := s.Name
	m.confirm = &confirm{
		prompt: fmt.Sprintf("Remove server %q from the config?", name),
		yes: func(m *Model) tea.Cmd {
			m.cfg.Remove(name)
			m.forgetTools(name)
			return m.commit("", name+" removed (previous config in mcp.json.bak)")
		},
	}
	m.mode = modeConfirm
}

// restart cycles a server through disabled → enabled; 1MCP's config hot-reload then reconnects it.
// (`1mcp mcp restart` only works for runtime-supervised backends.) The .bak backup is left alone.
func (m *Model) restart() tea.Cmd {
	s := m.selected()
	if s == nil || s.Disabled || s.Err != nil {
		return nil
	}
	err := m.cfg.SetDisabled(s.Name, true)
	if err == nil {
		err = m.cfg.SaveNoBackup()
	}
	if err != nil {
		m.setFlash("restart: "+err.Error(), true)
		return nil
	}
	m.reloadServers(s.Name)
	m.forgetTools(s.Name)
	m.setFlash("restarting "+s.Name+"…", false)
	name := s.Name
	return tea.Tick(reloadDelay, func(time.Time) tea.Msg { return restartMsg{name} })
}

func (m *Model) finishRestart(name string) tea.Cmd {
	err := m.cfg.SetDisabled(name, false)
	if err == nil {
		err = m.cfg.SaveNoBackup()
	}
	m.reloadServers("")
	if err != nil {
		m.setFlash("restart: "+err.Error()+" — server left disabled", true)
		return nil
	}
	m.setFlash(name+" restarted", false)
	return refreshLater()
}

// --- update -----------------------------------------------------------------------------

// Update handles messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.sizeJSON()
		return m, nil

	case tickMsg:
		cmds := []tea.Cmd{tick()}
		if m.flash != "" && time.Since(m.flashAt) > flashTimeout {
			m.flash = ""
		}
		// pick up edits made outside lazy1mcp
		if m.mode == modeNormal || m.mode == modeFilter {
			if st, err := os.Stat(m.cfg.Path); err == nil && !st.ModTime().Equal(m.cfg.ModTime) {
				if f, err := config.Load(m.cfg.Path); err == nil {
					m.cfg = f
					m.reloadServers("")
					m.setFlash("config changed on disk — reloaded", false)
				} else {
					m.cfg.ModTime = st.ModTime()
					m.setFlash("config on disk is invalid: "+err.Error(), true)
				}
			}
		}
		if time.Since(m.statusAt) > statusEvery {
			cmds = append(cmds, m.refresh())
		}
		return m, tea.Batch(cmds...)

	case refreshMsg:
		return m, m.refresh()

	case statusMsg:
		m.refreshing = false
		m.statusAt = time.Now()
		m.health, m.statusErr = msg.health, msg.err
		if msg.err == nil {
			old := m.status
			m.status = map[string]onemcp.Status{}
			for _, s := range msg.list {
				m.status[s.Server] = s
				// a reconnect can change the tool list
				if o, ok := old[s.Server]; !ok || o.Status != s.Status || o.ToolCount != s.ToolCount {
					m.forgetTools(s.Server)
				}
			}
		}
		return m, m.ensureTools()

	case toolsMsg:
		delete(m.toolsLoading, msg.name)
		if msg.err != nil {
			m.toolsErr[msg.name] = msg.err
		} else {
			m.tools[msg.name] = msg.tools
			delete(m.toolsErr, msg.name)
		}
		return m, nil

	case restartMsg:
		return m, m.finishRestart(msg.name)

	case editorMsg:
		m.externalDone(msg)
		return m, nil

	case tea.KeyMsg:
		return m, m.key(msg)
	}

	if m.mode == modeForm && m.form != nil {
		return m, m.form.update(msg)
	}
	return m, nil
}

func (m *Model) key(k tea.KeyMsg) tea.Cmd {
	if k.String() == "ctrl+c" {
		return tea.Quit
	}
	switch m.mode {
	case modeHelp:
		m.mode = modeNormal
		return nil
	case modeConfirm:
		switch k.String() {
		case "y", "Y", "enter":
			c := m.confirm
			m.confirm, m.mode = nil, modeNormal
			return c.yes(m)
		case "n", "N", "esc", "q":
			m.mode = m.confirm.noMode
			m.confirm = nil
		}
		return nil
	case modeForm:
		return m.formKey(k)
	case modeJSON:
		return m.jsonKey(k)
	case modeFilter:
		switch k.String() {
		case "esc":
			m.filter.SetValue("")
			m.filter.Blur()
			m.mode = modeNormal
			m.applyFilter()
			return m.ensureTools()
		case "enter":
			m.filter.Blur()
			m.mode = modeNormal
			return m.ensureTools()
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(k)
		m.applyFilter()
		return cmd
	}

	s := m.selected()
	switch k.String() {
	case "q":
		return tea.Quit
	case "?":
		m.mode = modeHelp
	case "/":
		m.mode = modeFilter
		m.focus = focusServers
		return m.filter.Focus()
	case "esc":
		if m.focus == focusTools {
			m.focus = focusServers
		} else if m.filter.Value() != "" {
			m.filter.SetValue("")
			m.applyFilter()
		}
	case "tab", "enter", "l", "right":
		if m.focus == focusServers && s != nil && !s.Disabled {
			m.focus, m.toolCursor = focusTools, 0
			return m.ensureTools()
		}
		if k.String() == "tab" {
			m.focus = focusServers
		}
	case "shift+tab", "h", "left":
		m.focus = focusServers
	case "j", "down":
		m.move(1)
		return m.ensureTools()
	case "k", "up":
		m.move(-1)
		return m.ensureTools()
	case "g", "home":
		m.move(-1 << 20)
		return m.ensureTools()
	case "G", "end":
		m.move(1 << 20)
		return m.ensureTools()
	case " ":
		if m.focus == focusTools {
			return m.toggleTool()
		}
		return m.toggleServer()
	case "a":
		m.openForm(nil)
	case "e":
		if s != nil && s.Err == nil {
			m.openForm(s)
		}
	case "E":
		if s != nil {
			return m.openJSON(s.Name)
		}
	case "O":
		return m.openJSON("")
	case "d", "x", "delete":
		m.askDelete()
	case "R":
		return m.restart()
	case "r", "ctrl+r":
		m.tools, m.toolsErr = map[string][]onemcp.Tool{}, map[string]error{}
		m.setFlash("refreshing…", false)
		return m.refresh()
	case "s":
		m.showSecrets = !m.showSecrets
	}
	return nil
}

func (m *Model) move(d int) {
	if m.focus == focusTools {
		if s := m.selected(); s != nil {
			n := len(m.toolRows(s))
			m.toolCursor = max(0, min(n-1, m.toolCursor+d))
		}
		return
	}
	m.cursor += d
	m.clampCursor()
	m.toolCursor = 0
}
