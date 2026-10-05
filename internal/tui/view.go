package tui

import (
	"fmt"
	"strings"

	"github.com/baeroe/lazy1mcp/internal/config"
)

const secretMask = "••••••"

// View renders the screen.
func (m *Model) View() string {
	if m.w < 40 || m.h < 10 {
		return "lazy1mcp: terminal too small"
	}
	bodyH := m.h - 3
	listW := min(max(28, m.w/3), 44)
	left := box(m.listTitle(), m.listLines(listW-4, bodyH-2), listW, bodyH, m.focus == focusServers)
	right := box(m.detailTitle(), m.detailLines(m.w-listW-4, bodyH-2), m.w-listW, bodyH, m.focus == focusTools)

	screen := []string{m.header()}
	for i := range bodyH {
		screen = append(screen, left[i]+right[i])
	}
	screen = append(screen, m.flashLine(), m.hints())

	switch m.mode {
	case modeForm:
		screen = overlay(screen, m.form.view(m.w, m.h), m.w)
	case modeJSON:
		screen = m.json.view(m.w, m.h)
	case modeConfirm:
		screen = overlay(screen, m.confirmView(), m.w)
	case modeHelp:
		screen = overlay(screen, helpView(), m.w)
	}
	return strings.Join(screen, "\n")
}

func (m *Model) header() string {
	l := " " + appName.Render("lazy1mcp") + "  " + dimStyle.Render(tildify(m.cfg.Path))
	var r string
	switch {
	case m.statusAt.IsZero():
		r = dimStyle.Render("connecting…")
	case m.health == nil:
		r = badStyle.Render("● 1mcp offline")
	default:
		st := goodStyle
		if m.health.Status != "healthy" {
			st = warnStyle
		}
		r = st.Render("● 1mcp "+m.health.Status) + dimStyle.Render(fmt.Sprintf(" v%s · %s", m.health.Version, m.client.URL))
		if m.statusErr != nil {
			r = badStyle.Render("✗ "+san(m.statusErr.Error())) + "  " + r
		}
	}
	if m.refreshing {
		r = dimStyle.Render("↻ ") + r
	}
	r = "  " + r + " "
	return fit(l, m.w-width(r)) + r
}

func (m *Model) listTitle() string {
	t := boldStyle.Render("Servers") + dimStyle.Render(fmt.Sprintf(" %d", len(m.servers)))
	if q := m.filter.Value(); q != "" && m.mode != modeFilter {
		t += accentStyle.Render(" /" + q)
	}
	return t
}

// state returns the status glyph, label and its rendering for a server.
func (m *Model) state(s *config.Server) (glyph, label string, render func(...string) string) {
	switch {
	case s.Err != nil:
		return "✗", "invalid entry", badStyle.Render
	case s.Disabled:
		return "○", "disabled", dimStyle.Render
	case m.statusAt.IsZero():
		return "·", "…", dimStyle.Render
	case m.statusErr != nil:
		return "?", "unknown", warnStyle.Render
	}
	st, ok := m.status[s.Name]
	switch {
	case !ok:
		return "◌", "not loaded", warnStyle.Render
	case st.Status == "connected" && st.Available:
		return "●", "connected", goodStyle.Render
	case strings.Contains(strings.ToLower(st.Status), "oauth"):
		return "◐", "awaiting OAuth", warnStyle.Render
	case strings.Contains(st.Status, "error") || strings.Contains(st.Status, "fail") || st.Status == "disconnected":
		return "●", st.Status, badStyle.Render
	default:
		return "◐", st.Status, warnStyle.Render
	}
}

func (m *Model) listLines(w, h int) []string {
	var lines []string
	if m.mode == modeFilter {
		m.filter.Width = w - 2
		lines = append(lines, m.filter.View(), "")
		h -= 2
	}
	if len(m.visible) == 0 {
		msg := "no servers — press a to add one"
		if m.filter.Value() != "" {
			msg = "no match"
		}
		return append(lines, dimStyle.Render(msg))
	}
	start := 0
	if m.cursor >= h {
		start = m.cursor - h + 1
	}
	for i := start; i < len(m.visible) && i < start+h; i++ {
		s := m.servers[m.visible[i]]
		glyph, _, render := m.state(s)
		right := dimStyle.Render(s.Kind())
		if st, ok := m.status[s.Name]; ok && !s.Disabled {
			right = dimStyle.Render(fmt.Sprintf("%s %3d", s.Kind(), st.ToolCount))
		}
		name := s.Name
		if s.Disabled {
			name = dimStyle.Render(name)
		}
		row := fit(" "+render(glyph)+" "+name, w-width(right)-1) + right + " "
		if i == m.cursor {
			if m.focus == focusServers {
				row = highlight(fit(row, w))
			} else {
				row = accentStyle.Render("▌") + fit(row, w)[1:]
			}
		}
		lines = append(lines, row)
	}
	return lines
}

func (m *Model) detailTitle() string {
	s := m.selected()
	if s == nil {
		return ""
	}
	glyph, label, render := m.state(s)
	return boldStyle.Render(s.Name) + " " + render(glyph+" "+label)
}

func (m *Model) detailLines(w, h int) []string {
	s := m.selected()
	if s == nil {
		return nil
	}
	if s.Err != nil {
		return []string{badStyle.Render(san(s.Err.Error())), "", dimStyle.Render("press E to fix the JSON")}
	}
	const lw = 10
	var lines []string
	field := func(label, value string) {
		lines = append(lines, labelStyle.Render(fit(label, lw))+value)
	}
	field("Type", s.Kind())
	if s.Command != "" {
		field("Command", san(tildify(s.Command)))
	}
	if len(s.Args) > 0 {
		field("Args", san(config.JoinArgs(s.Args)))
	}
	if s.URL != "" {
		field("URL", san(s.URL))
	}
	if s.Cwd != "" {
		field("Cwd", san(tildify(s.Cwd)))
	}
	kvs := func(label string, list []config.KV, sep string) {
		for i, kv := range list {
			v := secretMask
			if m.showSecrets {
				v = san(kv.Value)
			}
			if i > 0 {
				label = ""
			}
			field(label, san(kv.Key)+sep+dimStyle.Render(v))
		}
	}
	kvs("Env", s.Env, "=")
	kvs("Headers", s.Headers, ": ")
	if len(s.Tags) > 0 {
		var tags []string
		for _, t := range s.Tags {
			tags = append(tags, tagStyle.Render("#"+san(t)))
		}
		field("Tags", strings.Join(tags, " "))
	}
	if s.RequestTimeout > 0 {
		field("Timeout", formatTimeout(s.RequestTimeout))
	}
	if len(s.Other) > 0 {
		field("Other", dimStyle.Render(strings.Join(s.Other, ", ")+"  (E to edit)"))
	}
	lines = append(lines, "")
	return append(lines, m.toolLines(s, w, h-len(lines))...)
}

func (m *Model) toolLines(s *config.Server, w, h int) []string {
	if h < 2 {
		return nil
	}
	rows := m.toolRows(s)
	disabled := 0
	for _, r := range rows {
		if r.disabled {
			disabled++
		}
	}
	head := boldStyle.Render(fmt.Sprintf("Tools %d", len(rows)))
	if disabled > 0 {
		head += warnStyle.Render(fmt.Sprintf(" · %d disabled", disabled))
	}
	switch {
	case s.Disabled:
		return []string{head, dimStyle.Render("server is disabled — space to enable")}
	case m.toolsLoading[s.Name] && len(rows) == 0:
		return []string{head, dimStyle.Render("loading…")}
	case m.toolsErr[s.Name] != nil:
		return []string{head, badStyle.Render(san(m.toolsErr[s.Name].Error()))}
	case len(rows) == 0:
		st, ok := m.status[s.Name]
		if !ok {
			return []string{head, dimStyle.Render("not loaded by 1mcp")}
		}
		if _, cached := m.tools[s.Name]; !cached && st.Available {
			return []string{head, dimStyle.Render("loading…")}
		}
		return []string{head, dimStyle.Render("no tools")}
	}
	if m.focus == focusServers {
		head += dimStyle.Render("  (tab to manage)")
	}
	lines := []string{head}
	h--
	m.toolCursor = min(m.toolCursor, len(rows)-1)
	start := 0
	if m.toolCursor >= h {
		start = m.toolCursor - h + 1
	}
	nameW := 0
	for _, r := range rows {
		nameW = max(nameW, width(r.name))
	}
	nameW = min(nameW, w/2)
	for i := start; i < len(rows) && i < start+h; i++ {
		r := rows[i]
		mark, name, desc := goodStyle.Render("✓"), fit(san(r.name), nameW), dimStyle.Render(san(r.desc))
		if r.disabled {
			mark, name, desc = badStyle.Render("✗"), dimStyle.Render(fit(san(r.name), nameW)), dimStyle.Render("disabled")
		}
		row := fit(mark+" "+name+"  "+desc, w)
		if m.focus == focusTools && i == m.toolCursor {
			row = highlight(row)
		}
		lines = append(lines, row)
	}
	return lines
}

func (m *Model) flashLine() string {
	if m.flash == "" {
		return ""
	}
	if m.flashBad {
		return fit(" "+badStyle.Render("✗ "+san(m.flash)), m.w)
	}
	return fit(" "+goodStyle.Render("✓ ")+san(m.flash), m.w)
}

func (m *Model) hints() string {
	k := func(key, what string) string { return keyStyle.Render(key) + " " + dimStyle.Render(what) }
	var parts []string
	switch {
	case m.mode == modeFilter:
		parts = []string{k("enter", "apply"), k("esc", "clear")}
	case m.focus == focusTools:
		parts = []string{k("space", "toggle tool"), k("j/k", "move"), k("esc", "back"), k("?", "help")}
	default:
		parts = []string{k("space", "on/off"), k("a", "add"), k("e", "edit"), k("E", "json"), k("d", "delete"),
			k("R", "restart"), k("tab", "tools"), k("/", "filter"), k("?", "help"), k("q", "quit")}
	}
	return fit(" "+strings.Join(parts, "  "), m.w)
}

func (m *Model) confirmView() []string {
	lines := []string{san(m.confirm.prompt), "", keyStyle.Render("y") + " yes   " + keyStyle.Render("n") + " no"}
	w := min(max(width(lines[0])+4, 30), m.w-4)
	return box(warnStyle.Render("Confirm"), lines, w, len(lines)+2, true)
}

func helpView() []string {
	rows := [][2]string{
		{"j/k ↑/↓", "move"},
		{"g/G", "first / last"},
		{"tab / enter", "switch to the tool list"},
		{"esc", "back / clear filter"},
		{"space", "enable/disable server or tool"},
		{"a", "add server"},
		{"e", "edit server (command/URL, args, env/headers, tags, …)"},
		{"E", "edit the server's JSON"},
		{"O", "edit the whole mcp.json"},
		{"d", "remove server (backup in mcp.json.bak)"},
		{"R", "restart server (off/on cycle via hot-reload)"},
		{"r", "refresh status and tools"},
		{"s", "show/hide secrets"},
		{"/", "filter by name or tag"},
		{"q", "quit"},
	}
	var lines []string
	for _, r := range rows {
		lines = append(lines, keyStyle.Render(fit(r[0], 13))+r[1])
	}
	lines = append(lines, "", dimStyle.Render("1MCP hot-reloads mcp.json; changes apply within a second."), dimStyle.Render("any key closes this"))
	return box(boldStyle.Render("Keys"), lines, 70, len(lines)+2, true)
}
