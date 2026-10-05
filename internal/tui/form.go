package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/baeroe/lazy1mcp/internal/config"
)

const (
	fName = iota
	fTarget
	fArgs
	fTags
	fTimeout
	fCount
)

var formLabels = [fCount]string{"Name", "Command / URL", "Args", "Tags", "Timeout"}

var formHints = [fCount]string{
	"letters, digits, _ . -",
	"executable path or command line, or http(s)://… for remote servers",
	"shell-style, quotes allowed (stdio only)",
	"comma separated",
	"request timeout, e.g. 300s or 5m (empty = 1MCP default)",
}

// kvRow is one env variable (stdio) or header (http) in the form.
type kvRow struct{ key, val textinput.Model }

// form is the add/edit dialog. Focus walks a flat list of slots: the basic inputs, then key and value
// of every env/header row, then the "+ add" button.
type form struct {
	oldName string
	inputs  [fCount]textinput.Model
	rows    []kvRow
	focus   int
	reveal  bool // show env/header values unmasked
	err     string
}

func newInput(placeholder string) textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 4096
	ti.Placeholder = placeholder
	ti.EchoCharacter = '•'
	return ti
}

func (m *Model) openForm(s *config.Server) {
	f := &form{reveal: m.showSecrets}
	for i := range f.inputs {
		f.inputs[i] = newInput("")
	}
	f.inputs[fTarget].Placeholder = "/path/to/server-mcp  or  https://example.com/mcp"
	f.inputs[fTags].Placeholder = "docs, web"
	if s != nil {
		f.oldName = s.Name
		f.inputs[fName].SetValue(s.Name)
		kvs := s.Env
		if s.URL != "" {
			f.inputs[fTarget].SetValue(s.URL)
			kvs = s.Headers
		} else {
			f.inputs[fTarget].SetValue(s.Command)
		}
		f.inputs[fArgs].SetValue(config.JoinArgs(s.Args))
		f.inputs[fTags].SetValue(strings.Join(s.Tags, ", "))
		f.inputs[fTimeout].SetValue(formatTimeout(s.RequestTimeout))
		for _, kv := range kvs {
			f.addRow(kv.Key, kv.Value)
		}
	}
	f.inputs[0].Focus()
	m.form, m.mode = f, modeForm
}

func (f *form) addRow(k, v string) {
	f.rows = append(f.rows, kvRow{newInput("NAME"), newInput("value")})
	f.rows[len(f.rows)-1].key.SetValue(k)
	f.rows[len(f.rows)-1].val.SetValue(v)
}

func (f *form) slots() int   { return fCount + 2*len(f.rows) + 1 }
func (f *form) addSlot() int { return f.slots() - 1 }

// row returns the env/header row of slot i, or -1.
func (f *form) row(i int) int {
	if i < fCount || i >= f.addSlot() {
		return -1
	}
	return (i - fCount) / 2
}

// input returns the text input of slot i (nil for the add button).
func (f *form) input(i int) *textinput.Model {
	if i < fCount {
		return &f.inputs[i]
	}
	r := f.row(i)
	if r < 0 {
		return nil
	}
	if (i-fCount)%2 == 0 {
		return &f.rows[r].key
	}
	return &f.rows[r].val
}

// remote reports whether the target is a URL (rows are headers then, otherwise env).
func (f *form) remote() bool {
	t := strings.TrimSpace(f.inputs[fTarget].Value())
	return strings.HasPrefix(t, "http://") || strings.HasPrefix(t, "https://")
}

func (m *Model) formKey(k tea.KeyMsg) tea.Cmd {
	f := m.form
	switch k.String() {
	case "esc":
		m.form, m.mode = nil, modeNormal
		return nil
	case "tab", "down":
		return f.setFocus((f.focus + 1) % f.slots())
	case "shift+tab", "up":
		return f.setFocus((f.focus + f.slots() - 1) % f.slots())
	case "enter":
		if f.focus == f.addSlot() {
			f.addRow("", "")
			return f.setFocus(f.addSlot() - 2)
		}
		// KEY=VALUE typed into a name: split it and skip the (now filled) value
		if r := f.row(f.focus); r >= 0 && (f.focus-fCount)%2 == 0 && f.splitRow(r) {
			return f.setFocus(f.focus + 2)
		}
		return f.setFocus(f.focus + 1)
	case "ctrl+d":
		if r := f.row(f.focus); r >= 0 {
			f.input(f.focus).Blur()
			f.rows = append(f.rows[:r], f.rows[r+1:]...)
			return f.setFocus(min(fCount+2*r, f.addSlot()))
		}
		return nil
	case "ctrl+r":
		f.reveal = !f.reveal
		return nil
	case "ctrl+s":
		return m.submitForm()
	}
	return f.update(k)
}

// splitRow turns a name "KEY=VALUE" with an empty value into KEY / VALUE.
func (f *form) splitRow(r int) bool {
	row := &f.rows[r]
	k, v, ok := strings.Cut(row.key.Value(), "=")
	if !ok || row.val.Value() != "" {
		return false
	}
	row.key.SetValue(strings.TrimSpace(k))
	row.val.SetValue(v)
	return true
}

func (f *form) setFocus(i int) tea.Cmd {
	if r := f.row(f.focus); r >= 0 && (f.focus-fCount)%2 == 0 {
		f.splitRow(r)
	}
	if in := f.input(f.focus); in != nil {
		in.Blur()
	}
	f.focus = i
	if in := f.input(i); in != nil {
		return in.Focus()
	}
	return nil
}

func (f *form) update(msg tea.Msg) tea.Cmd {
	in := f.input(f.focus)
	if in == nil {
		// typing on "+ add" starts a new row
		k, ok := msg.(tea.KeyMsg)
		if !ok || k.Type != tea.KeyRunes {
			return nil
		}
		f.addRow("", "")
		cmd := f.setFocus(f.addSlot() - 2)
		return tea.Batch(cmd, f.update(msg))
	}
	var cmd tea.Cmd
	*in, cmd = in.Update(msg)
	return cmd
}

func (f *form) basics() (config.Basics, error) {
	b := config.Basics{Name: strings.TrimSpace(f.inputs[fName].Value())}
	target := strings.TrimSpace(f.inputs[fTarget].Value())
	args, err := config.SplitArgs(f.inputs[fArgs].Value())
	if err != nil {
		return b, fmt.Errorf("args: %w", err)
	}
	var kvs []config.KV
	for _, r := range f.rows {
		k, v := strings.TrimSpace(r.key.Value()), r.val.Value()
		if v == "" {
			if kk, vv, ok := strings.Cut(k, "="); ok {
				k, v = strings.TrimSpace(kk), vv
			}
		}
		if k == "" && v == "" {
			continue
		}
		kvs = append(kvs, config.KV{Key: k, Value: v})
	}
	switch {
	case f.remote():
		b.URL, b.Headers = target, kvs
	case target != "":
		b.Command, b.Env = target, kvs
		// a pasted command line ("pnpm dlx foo --bar") becomes command + args, unless it is an
		// existing path that happens to contain spaces
		if _, statErr := os.Stat(target); statErr != nil && strings.ContainsAny(target, " \t") {
			words, err := config.SplitArgs(target)
			if err != nil {
				return b, fmt.Errorf("command: %w", err)
			}
			b.Command, args = words[0], append(words[1:], args...)
		}
		b.Args = args
	}
	for _, t := range strings.FieldsFunc(f.inputs[fTags].Value(), func(r rune) bool { return r == ',' || r == ' ' }) {
		b.Tags = append(b.Tags, t)
	}
	if b.RequestTimeout, err = parseTimeout(f.inputs[fTimeout].Value()); err != nil {
		return b, err
	}
	return b, nil
}

func (m *Model) submitForm() tea.Cmd {
	f := m.form
	b, err := f.basics()
	if err == nil {
		err = m.cfg.Apply(f.oldName, b)
	}
	if err != nil {
		f.err = err.Error()
		return nil
	}
	m.form, m.mode = nil, modeNormal
	m.forgetTools(f.oldName)
	m.forgetTools(b.Name)
	verb := "updated"
	if f.oldName == "" {
		verb = "added"
	}
	return m.commit(b.Name, b.Name+" "+verb)
}

// parseTimeout accepts "", plain seconds ("300") or a Go duration ("5m", "1500ms") and returns ms.
func parseTimeout(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 0 {
		return n * 1000, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("timeout: %q is not a duration (e.g. 300s, 5m)", s)
	}
	return int(d / time.Millisecond), nil
}

func formatTimeout(ms int) string {
	switch {
	case ms <= 0:
		return ""
	case ms%60000 == 0:
		return fmt.Sprintf("%dm", ms/60000)
	case ms%1000 == 0:
		return fmt.Sprintf("%ds", ms/1000)
	default:
		return fmt.Sprintf("%dms", ms)
	}
}

// view renders the dialog; it scrolls so the focused field stays visible within maxH lines.
func (f *form) view(w, maxH int) []string {
	inner := min(w-8, 76)
	title := "Add server"
	if f.oldName != "" {
		title = "Edit " + f.oldName
	}
	var lines []string
	focusLine := 0
	label := func(s string, focused bool) string {
		if focused {
			return keyStyle.Render(s)
		}
		return labelStyle.Render(s)
	}
	for i := range f.inputs {
		in := f.inputs[i]
		in.Width = inner - 4 // the cursor takes one extra cell
		if i == f.focus {
			focusLine = len(lines)
		}
		lines = append(lines, label(formLabels[i], i == f.focus), "  "+in.View())
		if i == f.focus {
			lines = append(lines, dimStyle.Render("  "+formHints[i]))
		} else {
			lines = append(lines, "")
		}
	}

	what, unit := "Env", "variable"
	if f.remote() {
		what, unit = "Headers", "header"
	}
	lines = append(lines, label(what, f.row(f.focus) >= 0 || f.focus == f.addSlot()))
	kw := (inner - 5) * 2 / 5
	vw := inner - 5 - kw
	for r, row := range f.rows {
		k, v := row.key, row.val
		k.Width, v.Width = kw-1, vw-1
		if !f.reveal && f.focus != fCount+2*r+1 {
			v.EchoMode = textinput.EchoPassword
		}
		if f.row(f.focus) == r {
			focusLine = len(lines)
		}
		lines = append(lines, "  "+fit(k.View(), kw)+dimStyle.Render(" = ")+fit(v.View(), vw))
	}
	add := "+ add " + unit
	if f.focus == f.addSlot() {
		focusLine = len(lines)
		lines = append(lines, "  "+keyStyle.Render(add)+dimStyle.Render("  enter"))
	} else {
		lines = append(lines, "  "+dimStyle.Render(add))
	}
	if f.row(f.focus) >= 0 {
		lines = append(lines, dimStyle.Render("  ctrl+d remove row · ctrl+r show values · KEY=VALUE in the name works too"))
	}

	lines = append(lines, "")
	if f.err != "" {
		lines = append(lines, badStyle.Render("✗ "+san(f.err)))
	} else {
		lines = append(lines, dimStyle.Render("everything else: E edits the server's JSON"))
	}
	lines = append(lines, "", keyStyle.Render("tab")+" next  "+keyStyle.Render("ctrl+s")+" save  "+
		keyStyle.Render("esc")+" cancel")

	// keep the focused field (plus its hint line) in view; the footer stays visible
	const footer = 3
	if room := maxH - 2; room > footer+3 && len(lines) > room {
		body, foot := lines[:len(lines)-footer], lines[len(lines)-footer:]
		bh := room - footer
		start := max(0, min(focusLine+3-bh, len(body)-bh))
		lines = append(append([]string(nil), body[start:start+bh]...), foot...)
	}
	return box(boldStyle.Render(title), lines, inner+4, len(lines)+2, true)
}
