package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/baeroe/lazy1mcp/internal/config"
	"github.com/baeroe/lazy1mcp/internal/onemcp"
)

const demo = `{
  "mcpServers": {
    "docs": {"command": "/bin/docs-mcp", "tags": ["docs"]},
    "web": {"transport": "http", "url": "https://example.com/mcp", "headers": {"token": "s3cret"}}
  }
}`

func newModel(t *testing.T) *Model {
	t.Helper()
	p := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(p, []byte(demo), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	m := New(cfg, onemcp.Client{Bin: "false", URL: "http://127.0.0.1:1"})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	return m
}

func press(m *Model, keys ...string) {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "ctrl+s":
			msg = tea.KeyMsg{Type: tea.KeyCtrlS}
		case "ctrl+d":
			msg = tea.KeyMsg{Type: tea.KeyCtrlD}
		case "ctrl+f":
			msg = tea.KeyMsg{Type: tea.KeyCtrlF}
		case " ":
			msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		m.Update(msg)
	}
}

func reload(t *testing.T, m *Model) *config.File {
	t.Helper()
	f, err := config.Load(m.cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestToggleServerPersists(t *testing.T) {
	m := newModel(t)
	press(m, " ")
	if s, _ := reload(t, m).Server("docs"); !s.Disabled {
		t.Fatal("docs not disabled on disk")
	}
	if !strings.Contains(m.View(), "disabled") {
		t.Fatal("view does not show disabled")
	}
	press(m, " ")
	if s, _ := reload(t, m).Server("docs"); s.Disabled {
		t.Fatal("docs not re-enabled")
	}
}

func TestSecretsMasked(t *testing.T) {
	m := newModel(t)
	press(m, "j")
	if v := m.View(); strings.Contains(v, "s3cret") || !strings.Contains(v, secretMask) {
		t.Fatal("header value not masked")
	}
	press(m, "s")
	if !strings.Contains(m.View(), "s3cret") {
		t.Fatal("s does not reveal secrets")
	}
}

func TestAddViaForm(t *testing.T) {
	m := newModel(t)
	press(m, "a")
	for _, r := range "new" {
		press(m, string(r))
	}
	press(m, "tab")
	for _, r := range "pnpm dlx some-mcp --x 'a b'" {
		press(m, string(r))
	}
	press(m, "ctrl+s")
	if m.mode != modeNormal {
		t.Fatalf("form still open: %v", m.form.err)
	}
	s, err := reload(t, m).Server("new")
	if err != nil || s.Command != "pnpm" || config.JoinArgs(s.Args) != "dlx some-mcp --x 'a b'" {
		t.Fatalf("new = %+v, %v", s, err)
	}
	if sel := m.selected(); sel == nil || sel.Name != "new" {
		t.Fatal("new server not selected")
	}
}

func TestFormRejectsDuplicate(t *testing.T) {
	m := newModel(t)
	press(m, "a", "w", "e", "b", "tab", "x", "ctrl+s")
	if m.mode != modeForm || !strings.Contains(m.form.err, "exists") {
		t.Fatalf("duplicate accepted, err=%q", m.form.err)
	}
	press(m, "esc")
	if m.mode != modeNormal {
		t.Fatal("esc did not close the form")
	}
}

func TestDeleteConfirm(t *testing.T) {
	m := newModel(t)
	press(m, "d", "n")
	if !reload(t, m).Has("docs") {
		t.Fatal("deleted without confirmation")
	}
	press(m, "d", "y")
	if reload(t, m).Has("docs") {
		t.Fatal("not deleted")
	}
}

func TestFilter(t *testing.T) {
	m := newModel(t)
	press(m, "/", "w", "e", "enter")
	if len(m.visible) != 1 || m.selected().Name != "web" {
		t.Fatalf("visible = %v", m.visible)
	}
	press(m, "esc")
	if len(m.visible) != 2 {
		t.Fatal("esc did not clear the filter")
	}
}

func TestToolToggleIncludesHiddenDisabledTools(t *testing.T) {
	m := newModel(t)
	m.Update(statusMsg{health: &onemcp.Health{Status: "healthy"}, list: []onemcp.Status{{Server: "docs", Status: "connected", Available: true, ToolCount: 2}}})
	m.Update(toolsMsg{name: "docs", tools: []onemcp.Tool{{Tool: "b"}, {Tool: "a"}}})
	press(m, "tab", " ") // disables "a" (sorted first)
	s, _ := reload(t, m).Server("docs")
	if len(s.DisabledTools) != 1 || s.DisabledTools[0] != "a" {
		t.Fatalf("disabledTools = %v", s.DisabledTools)
	}
	// 1MCP no longer lists "a"; it must still show up (as disabled) so it can be re-enabled
	m.Update(toolsMsg{name: "docs", tools: []onemcp.Tool{{Tool: "b"}}})
	rows := m.toolRows(m.selected())
	if len(rows) != 2 || rows[0].name != "a" || !rows[0].disabled {
		t.Fatalf("rows = %+v", rows)
	}
	press(m, " ")
	if s, _ := reload(t, m).Server("docs"); len(s.DisabledTools) != 0 {
		t.Fatalf("not re-enabled: %v", s.DisabledTools)
	}
}

func TestViewFitsSmallTerminal(t *testing.T) {
	m := newModel(t)
	for _, w := range []int{40, 60, 200} {
		m.Update(tea.WindowSizeMsg{Width: w, Height: 12})
		for i, l := range strings.Split(m.View(), "\n") {
			if width(l) > w {
				t.Fatalf("w=%d line %d is %d cells", w, i, width(l))
			}
		}
	}
}

func TestRestartCyclesWithoutTouchingBackup(t *testing.T) {
	m := newModel(t)
	if err := os.WriteFile(m.cfg.Path+".bak", []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	press(m, "R")
	if s, _ := reload(t, m).Server("docs"); !s.Disabled {
		t.Fatal("restart did not disable first")
	}
	m.Update(restartMsg{"docs"})
	if s, _ := reload(t, m).Server("docs"); s.Disabled {
		t.Fatal("restart did not re-enable")
	}
	if b, _ := os.ReadFile(m.cfg.Path + ".bak"); string(b) != "old" {
		t.Fatal("restart overwrote the backup")
	}
}

func typeText(m *Model, s string) {
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
}

func TestFormEnvAndHeaders(t *testing.T) {
	m := newModel(t)
	press(m, "j", "e") // web: one header row prefilled
	if len(m.form.rows) != 1 || m.form.rows[0].val.Value() != "s3cret" {
		t.Fatalf("rows = %+v", m.form.rows)
	}
	if v := m.View(); strings.Contains(v, "s3cret") || !strings.Contains(v, "Headers") {
		t.Fatal("header value not masked in the form")
	}
	// jump to "+ add header", add a row and paste KEY=VALUE into its name
	for m.form.focus != m.form.addSlot() {
		press(m, "tab")
	}
	press(m, "enter")
	typeText(m, "X-Team=core")
	press(m, "ctrl+s")
	if m.mode != modeNormal {
		t.Fatalf("form error: %s", m.form.err)
	}
	s, _ := reload(t, m).Server("web")
	want := []config.KV{{Key: "token", Value: "s3cret"}, {Key: "X-Team", Value: "core"}}
	if len(s.Headers) != 2 || s.Headers[0] != want[0] || s.Headers[1] != want[1] {
		t.Fatalf("headers = %v", s.Headers)
	}
	// remove the first row
	press(m, "e")
	for m.form.row(m.form.focus) != 0 {
		press(m, "tab")
	}
	press(m, "ctrl+d", "ctrl+s")
	if s, _ := reload(t, m).Server("web"); len(s.Headers) != 1 || s.Headers[0].Key != "X-Team" {
		t.Fatalf("headers after delete = %v", s.Headers)
	}
}

func TestFormEnvForStdio(t *testing.T) {
	m := newModel(t)
	press(m, "e")
	for m.form.focus != m.form.addSlot() {
		press(m, "tab")
	}
	press(m, "enter")
	typeText(m, "API_KEY")
	press(m, "tab")
	typeText(m, "abc")
	press(m, "ctrl+s")
	if s, _ := reload(t, m).Server("docs"); len(s.Env) != 1 || s.Env[0] != (config.KV{Key: "API_KEY", Value: "abc"}) {
		t.Fatalf("env = %v", s.Env)
	}
}

func TestFormScrollsWhenTall(t *testing.T) {
	m := newModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	press(m, "e")
	for i := 0; i < 15; i++ {
		m.form.addRow("K", "v")
	}
	for m.form.focus != m.form.addSlot() {
		press(m, "tab")
	}
	v := m.View()
	if len(strings.Split(v, "\n")) > 20 || !strings.Contains(v, "+ add variable") {
		t.Fatal("form does not scroll to the focused field")
	}
}

func TestJSONEditorSave(t *testing.T) {
	m := newModel(t)
	press(m, "E")
	if m.mode != modeJSON || !strings.Contains(m.json.ta.Value(), `"docs"`) {
		t.Fatal("json editor not open")
	}
	m.json.ta.SetValue(`{"docs": {"command": "/bin/other", "cwd": "/tmp"}}`)
	press(m, "ctrl+s")
	if m.mode != modeNormal {
		t.Fatalf("save failed: %s", m.json.err)
	}
	if s, _ := reload(t, m).Server("docs"); s.Command != "/bin/other" || s.Cwd != "/tmp" {
		t.Fatalf("docs = %+v", s)
	}
}

func TestJSONEditorInvalidStaysOpen(t *testing.T) {
	m := newModel(t)
	press(m, "E")
	m.json.ta.SetValue("{\n  \"docs\": {,}\n}")
	if !strings.Contains(m.View(), "line 2") {
		t.Fatal("no live syntax error with line number")
	}
	press(m, "ctrl+s")
	if m.mode != modeJSON || m.json.err == "" {
		t.Fatal("invalid JSON saved or editor closed")
	}
	// esc on modified content asks; "n" goes back to the editor
	press(m, "esc", "n")
	if m.mode != modeJSON {
		t.Fatal("n did not return to the editor")
	}
	press(m, "esc", "y")
	if m.mode != modeNormal || m.json != nil {
		t.Fatal("discard did not close the editor")
	}
	if s, _ := reload(t, m).Server("docs"); s.Command != "/bin/docs-mcp" {
		t.Fatal("file changed")
	}
}

func TestJSONEditorWholeFile(t *testing.T) {
	m := newModel(t)
	press(m, "O")
	m.json.ta.SetValue(`{"mcpServers": {"only": {"command": "/bin/only"}}}`)
	press(m, "ctrl+f")
	if !strings.Contains(m.json.ta.Value(), "\n  \"mcpServers\"") {
		t.Fatal("ctrl+f did not format")
	}
	press(m, "ctrl+s")
	if f := reload(t, m); len(f.Names()) != 1 || !f.Has("only") || m.selected().Name != "only" {
		t.Fatalf("names = %v", f.Names())
	}
}

func TestJSONEditorAutoIndent(t *testing.T) {
	m := newModel(t)
	press(m, "E")
	m.json.ta.SetValue("{\n  \"a\": {")
	press(m, "enter")
	if got := m.json.ta.Value(); got != "{\n  \"a\": {\n    " {
		t.Fatalf("value = %q", got)
	}
}

func TestFormTypingFlow(t *testing.T) {
	m := newModel(t)
	press(m, "e")
	for m.form.focus != m.form.addSlot() {
		press(m, "tab")
	}
	// type on "+ add" → new row; KEY=VALUE + enter → split, focus back on "+ add"
	typeText(m, "DEBUG=pw:*")
	press(m, "enter")
	if m.form.focus != m.form.addSlot() {
		t.Fatalf("focus = %d, want add slot", m.form.focus)
	}
	typeText(m, "TOKEN")
	press(m, "enter")
	typeText(m, "abc")
	press(m, "ctrl+s")
	s, _ := reload(t, m).Server("docs")
	want := []config.KV{{Key: "DEBUG", Value: "pw:*"}, {Key: "TOKEN", Value: "abc"}}
	if len(s.Env) != 2 || s.Env[0] != want[0] || s.Env[1] != want[1] {
		t.Fatalf("env = %v", s.Env)
	}
}
