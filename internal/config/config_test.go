package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const sample = `{
  "configReload": {"enabled": true},
  "mcpServers": {
    "docs": {
      "command": "/usr/local/bin/docs-mcp",
      "tags": ["docs"],
      "x-custom": {"keep": "me <&>"}
    },
    "remote": {
      "transport": "http",
      "url": "https://example.com/mcp",
      "headers": {"auth-token": "s3cret"},
      "tags": ["web"]
    },
    "shell": {
      "command": "/opt/bin/shell-mcp",
      "args": ["mcp", "--flag value"],
      "env": {"API_KEY": "abc", "MODE": "dev"},
      "requestTimeout": 300000
    }
  }
}
`

func load(t *testing.T, content string) *File {
	t.Helper()
	p := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// normalized is sample in the canonical two-space layout Save writes.
func normalized(t *testing.T) string {
	t.Helper()
	b, err := indent([]byte(strings.TrimSpace(sample)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

func TestRoundTripKeepsEverything(t *testing.T) {
	f := load(t, sample)
	b, err := f.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != normalized(t) {
		t.Fatalf("round trip changed the file:\n%s", b)
	}
}

func TestParse(t *testing.T) {
	f := load(t, sample)
	if got := f.Names(); !reflect.DeepEqual(got, []string{"docs", "remote", "shell"}) {
		t.Fatalf("names = %v", got)
	}
	s, _ := f.Server("shell")
	if s.Kind() != "stdio" || s.RequestTimeout != 300000 || len(s.Env) != 2 || s.Env[0] != (KV{"API_KEY", "abc"}) {
		t.Fatalf("shell = %+v", s)
	}
	r, _ := f.Server("remote")
	if r.Kind() != "http" || !r.Remote() || r.Headers[0].Value != "s3cret" {
		t.Fatalf("remote = %+v", r)
	}
	d, _ := f.Server("docs")
	if !reflect.DeepEqual(d.Other, []string{"x-custom"}) {
		t.Fatalf("other = %v", d.Other)
	}
}

func TestDisableAndTools(t *testing.T) {
	f := load(t, sample)
	if err := f.SetDisabled("docs", true); err != nil {
		t.Fatal(err)
	}
	if on, _ := f.ToggleTool("docs", "a"); !on {
		t.Fatal("tool a should be disabled")
	}
	f.ToggleTool("docs", "b")
	f.ToggleTool("docs", "a")
	s, _ := f.Server("docs")
	if !s.Disabled || !reflect.DeepEqual(s.DisabledTools, []string{"b"}) {
		t.Fatalf("docs = %+v", s)
	}
	f.ToggleTool("docs", "b")
	f.SetDisabled("docs", false)
	b, _ := f.Bytes()
	if string(b) != normalized(t) {
		t.Fatalf("undo did not restore the file:\n%s", b)
	}
}

func TestApply(t *testing.T) {
	f := load(t, sample)
	// rename + switch the stdio server to http: env/args go, unknown keys stay
	err := f.Apply("shell", Basics{Name: "shell2", URL: "https://x.test/mcp", Tags: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	if f.Has("shell") || !f.Has("shell2") {
		t.Fatal("rename failed")
	}
	if got := f.Names(); got[2] != "shell2" {
		t.Fatalf("rename moved the entry: %v", got)
	}
	s, _ := f.Server("shell2")
	if s.Kind() != "http" || s.Command != "" || len(s.Env) != 0 || s.RequestTimeout != 0 || s.Tags[0] != "a" {
		t.Fatalf("shell2 = %+v", s)
	}
	// env/headers lists: order kept, empty list removes the key
	if err := f.Apply("remote", Basics{Name: "remote", URL: "https://example.com/mcp", Headers: []KV{{"b", "2"}, {"a", "1"}}}); err != nil {
		t.Fatal(err)
	}
	if r, _ := f.Server("remote"); !reflect.DeepEqual(r.Headers, []KV{{"b", "2"}, {"a", "1"}}) {
		t.Fatalf("headers = %v", r.Headers)
	}
	if err := f.Apply("remote", Basics{Name: "remote", URL: "https://example.com/mcp"}); err != nil {
		t.Fatal(err)
	}
	if raw, _ := f.RawServer("remote"); strings.Contains(string(raw), "headers") {
		t.Fatalf("empty headers kept: %s", raw)
	}
	if err := f.Apply("remote", Basics{Name: "remote", URL: "u", Headers: []KV{{"a", "1"}, {"a", "2"}}}); err == nil {
		t.Fatal("duplicate header accepted")
	}
	if err := f.Apply("remote", Basics{Name: "remote", Command: "c", Env: []KV{{"", "x"}}}); err == nil {
		t.Fatal("empty env name accepted")
	}
	// new server
	if err := f.Apply("", Basics{Name: "new", Command: "/bin/new", Args: []string{"x"}, Env: []KV{{"K", "v"}}}); err != nil {
		t.Fatal(err)
	}
	n, _ := f.Server("new")
	if n.Command != "/bin/new" || n.Args[0] != "x" || n.Env[0] != (KV{"K", "v"}) {
		t.Fatalf("new = %+v", n)
	}
	if err := f.Apply("", Basics{Name: "docs", Command: "x"}); err == nil {
		t.Fatal("duplicate name accepted")
	}
	if err := f.Apply("", Basics{Name: "bad name", Command: "x"}); err == nil {
		t.Fatal("bad name accepted")
	}
	if err := f.Apply("", Basics{Name: "both", Command: "x", URL: "y"}); err == nil {
		t.Fatal("command+url accepted")
	}
}

func TestEditRaw(t *testing.T) {
	f := load(t, sample)
	raw, err := f.RawServer("docs")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), "{\n  \"docs\": {") {
		t.Fatalf("raw = %s", raw)
	}
	edited := strings.Replace(string(raw), `"docs":`, `"docs2":`, 1)
	name, err := f.ReplaceRaw("docs", []byte(edited))
	if err != nil || name != "docs2" || f.Names()[0] != "docs2" {
		t.Fatalf("name=%q err=%v names=%v", name, err, f.Names())
	}
	if _, err := f.ReplaceRaw("docs2", []byte(`{"a":{},"b":{}}`)); err == nil {
		t.Fatal("two servers accepted")
	}
	if _, err := f.ReplaceRaw("docs2", []byte(`{"remote":{}}`)); err == nil {
		t.Fatal("rename onto existing accepted")
	}
}

func TestSaveBackupAndMode(t *testing.T) {
	f := load(t, sample)
	f.Remove("docs")
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(f.Path)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", st.Mode())
	}
	bak, _ := os.ReadFile(f.Path + ".bak")
	if string(bak) != sample {
		t.Fatal("backup missing")
	}
	g, err := Load(f.Path)
	if err != nil || g.Has("docs") {
		t.Fatalf("reload: %v %v", err, g.Names())
	}
}

func TestMissingFile(t *testing.T) {
	f, err := Load(filepath.Join(t.TempDir(), "sub", "mcp.json"))
	if err != nil || len(f.Names()) != 0 {
		t.Fatal(err)
	}
	if err := f.Apply("", Basics{Name: "a", Command: "/bin/a"}); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
}

func TestSplitJoinArgs(t *testing.T) {
	for _, c := range []struct {
		in   string
		want []string
	}{
		{`a b  c`, []string{"a", "b", "c"}},
		{`--x 'hello world' "a \"q\""`, []string{"--x", "hello world", `a "q"`}},
		{`a\ b ''`, []string{"a b", ""}},
	} {
		got, err := SplitArgs(c.in)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("SplitArgs(%q) = %q, %v", c.in, got, err)
		}
		back, _ := SplitArgs(JoinArgs(got))
		if !reflect.DeepEqual(back, got) {
			t.Errorf("JoinArgs round trip of %q = %q", got, back)
		}
	}
	if _, err := SplitArgs(`'open`); err == nil {
		t.Error("unterminated quote accepted")
	}
}

func TestParseRejectsNonObjectServer(t *testing.T) {
	if _, err := Parse("x", []byte(`{"mcpServers": {"a": []}}`)); err == nil {
		t.Fatal("array server accepted")
	}
	if _, err := Parse("x", []byte(`{"mcpServers": {"a": {}}}`)); err != nil {
		t.Fatal(err)
	}
}
