package tui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/baeroe/lazy1mcp/internal/config"
	"github.com/baeroe/lazy1mcp/internal/onemcp"
)

// jsonEditor is the built-in editor for one server's JSON ({"name": {...}}) or, with name == "", the
// whole mcp.json. Content is validated live and again on save.
type jsonEditor struct {
	name string
	ta   textarea.Model
	orig string
	err  string // last save error
}

func (m *Model) openJSON(name string) tea.Cmd {
	var (
		b   []byte
		err error
	)
	if name == "" {
		b, err = m.cfg.Bytes()
	} else {
		b, err = m.cfg.RawServer(name)
	}
	if err != nil {
		m.setFlash(err.Error(), true)
		return nil
	}
	ta := textarea.New()
	ta.Prompt = " "
	ta.ShowLineNumbers = true
	ta.CharLimit = 0
	ta.MaxHeight = 999 // also sets the line-number width
	ta.SetValue(string(b))
	m.json = &jsonEditor{name: name, ta: ta, orig: string(b)}
	m.mode = modeJSON
	m.sizeJSON()
	cmd := m.json.ta.Focus()
	m.json.ta, _ = m.json.ta.Update(tea.KeyMsg{Type: tea.KeyCtrlHome})
	return cmd
}

func (m *Model) sizeJSON() {
	if m.json == nil {
		return
	}
	m.json.ta.SetWidth(max(10, m.w-4))
	m.json.ta.SetHeight(max(3, m.h-5))
}

func (m *Model) jsonKey(k tea.KeyMsg) tea.Cmd {
	j := m.json
	switch k.String() {
	case "esc":
		if j.ta.Value() == j.orig {
			m.json, m.mode = nil, modeNormal
			return nil
		}
		m.confirm = &confirm{
			prompt: "Discard your changes?",
			yes:    func(m *Model) tea.Cmd { m.json = nil; return nil },
			noMode: modeJSON,
		}
		m.mode = modeConfirm
		return nil
	case "ctrl+s":
		return m.saveJSON()
	case "ctrl+f":
		var out bytes.Buffer
		if err := json.Indent(&out, []byte(strings.TrimSpace(j.ta.Value())), "", "  "); err == nil {
			j.ta.SetValue(out.String() + "\n")
		}
		return nil
	case "ctrl+o":
		if editorCommand("") == nil {
			m.setFlash("set $VISUAL or $EDITOR to use an external editor", true)
			return nil
		}
		return m.openExternal(j.ta.Value())
	case "tab":
		j.ta.InsertString("  ")
		return nil
	case "enter":
		// keep the indentation of the current line, one level deeper after { or [
		lines := strings.Split(j.ta.Value(), "\n")
		cur := ""
		if r := j.ta.Line(); r < len(lines) {
			cur = lines[r]
			if c := j.ta.LineInfo().CharOffset + j.ta.LineInfo().StartColumn; c < len([]rune(cur)) {
				cur = string([]rune(cur)[:c])
			}
		}
		indent := cur[:len(cur)-len(strings.TrimLeft(cur, " \t"))]
		if t := strings.TrimSpace(cur); strings.HasSuffix(t, "{") || strings.HasSuffix(t, "[") {
			indent += "  "
		}
		j.ta.InsertString("\n" + indent)
		return nil
	}
	var cmd tea.Cmd
	j.ta, cmd = j.ta.Update(k)
	return cmd
}

func (m *Model) saveJSON() tea.Cmd {
	j := m.json
	b := []byte(j.ta.Value())
	if j.ta.Value() == j.orig {
		m.json, m.mode = nil, modeNormal
		m.setFlash("no changes", false)
		return nil
	}
	if j.name == "" {
		f, err := config.Parse(m.cfg.Path, b)
		if err != nil {
			j.err = err.Error()
			return nil
		}
		m.cfg = f
		m.json, m.mode = nil, modeNormal
		m.tools, m.toolsErr = map[string][]onemcp.Tool{}, map[string]error{}
		return m.commit("", "config saved")
	}
	newName, err := m.cfg.ReplaceRaw(j.name, b)
	if err != nil {
		j.err = err.Error()
		return nil
	}
	m.json, m.mode = nil, modeNormal
	m.forgetTools(j.name)
	m.forgetTools(newName)
	return m.commit(newName, newName+" saved")
}

// jsonStatus describes whether the content parses, with line/column of the first syntax error.
func jsonStatus(s string) (string, bool) {
	var v any
	err := json.Unmarshal([]byte(s), &v)
	if err == nil {
		return "valid JSON", true
	}
	var se *json.SyntaxError
	if errors.As(err, &se) {
		before := s[:min(int(se.Offset), len(s))]
		line := strings.Count(before, "\n") + 1
		col := len([]rune(before[strings.LastIndexByte(before, '\n')+1:]))
		return fmt.Sprintf("line %d, col %d: %s", line, col, se.Error()), false
	}
	return err.Error(), false
}

func (j *jsonEditor) view(w, h int) []string {
	title := "Edit " + j.name
	if j.name == "" {
		title = "Edit mcp.json"
	}
	if j.ta.Value() != j.orig {
		title += dimStyle.Render(" (modified)")
	}
	lines := strings.Split(j.ta.View(), "\n")
	status, ok := jsonStatus(j.ta.Value())
	switch {
	case j.err != "":
		lines = append(lines, badStyle.Render("✗ "+san(j.err)))
	case ok:
		lines = append(lines, goodStyle.Render("✓ "+status))
	default:
		lines = append(lines, badStyle.Render("✗ "+san(status)))
	}
	k := func(key, what string) string { return keyStyle.Render(key) + " " + dimStyle.Render(what) }
	hints := []string{k("ctrl+s", "save"), k("esc", "cancel"), k("ctrl+f", "format")}
	if editorCommand("") != nil {
		hints = append(hints, k("ctrl+o", "open in $EDITOR"))
	}
	lines = append(lines, strings.Join(hints, "  "))
	return box(boldStyle.Render(title), lines, w, h, true)
}

// --- external editor (optional, only when $VISUAL/$EDITOR is set) -----------------------

type editorMsg struct {
	path string
	err  error
}

// editorCommand opens path in $VISUAL/$EDITOR; nil when neither is set.
func editorCommand(path string) *exec.Cmd {
	ed := os.Getenv("VISUAL")
	if ed == "" {
		ed = os.Getenv("EDITOR")
	}
	if ed == "" {
		return nil
	}
	return exec.Command("sh", "-c", ed+` "$1"`, "lazy1mcp", path)
}

func (m *Model) openExternal(content string) tea.Cmd {
	f, err := os.CreateTemp("", "lazy1mcp-*.json")
	if err != nil {
		m.setFlash(err.Error(), true)
		return nil
	}
	path := f.Name()
	_, err = f.WriteString(content)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		m.setFlash(err.Error(), true)
		return nil
	}
	return tea.ExecProcess(editorCommand(path), func(err error) tea.Msg { return editorMsg{path: path, err: err} })
}

// externalDone copies the edited temp file back into the built-in editor.
func (m *Model) externalDone(msg editorMsg) {
	b, rerr := os.ReadFile(msg.path)
	os.Remove(msg.path)
	switch {
	case msg.err != nil:
		m.setFlash("editor: "+msg.err.Error(), true)
	case rerr != nil:
		m.setFlash(rerr.Error(), true)
	case m.json != nil:
		m.json.ta.SetValue(string(b))
		m.json.err = ""
	}
}
