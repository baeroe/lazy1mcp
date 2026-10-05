// Package config reads and writes the 1MCP config file (mcp.json). Only the fields lazy1mcp edits are
// touched; everything else (unknown keys, key order) survives a save unchanged.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"
)

const serversKey = "mcpServers"

// DefaultPath is $ONE_MCP_CONFIG or ~/.config/1mcp/mcp.json.
func DefaultPath() string {
	if p := os.Getenv("ONE_MCP_CONFIG"); p != "" {
		return p
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".config", "1mcp", "mcp.json")
}

// File is a loaded mcp.json.
type File struct {
	Path    string
	ModTime time.Time

	root    *Object
	servers *Object
}

// Load reads path. A missing file yields an empty config that Save creates.
func Load(path string) (*File, error) {
	f := &File{Path: path, root: NewObject(), servers: NewObject()}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return nil, err
	}
	if f, err = Parse(path, b); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if st, err := os.Stat(path); err == nil {
		f.ModTime = st.ModTime()
	}
	return f, nil
}

// Parse reads config content that will be saved to path. Every server entry must be a JSON object.
func Parse(path string, b []byte) (*File, error) {
	f := &File{Path: path, root: NewObject(), servers: NewObject()}
	if err := json.Unmarshal(b, f.root); err != nil {
		return nil, err
	}
	if raw, ok := f.root.Raw(serversKey); ok {
		if err := json.Unmarshal(raw, f.servers); err != nil {
			return nil, fmt.Errorf("%s: %w", serversKey, err)
		}
	}
	for _, n := range f.Names() {
		if _, err := f.obj(n); err != nil {
			return nil, err
		}
	}
	return f, nil
}

// Names returns the server names in file order.
func (f *File) Names() []string { return f.servers.Keys() }

// Has reports whether a server exists.
func (f *File) Has(name string) bool { return f.servers.Has(name) }

func (f *File) obj(name string) (*Object, error) {
	raw, ok := f.servers.Raw(name)
	if !ok {
		return nil, fmt.Errorf("no server %q", name)
	}
	o := NewObject()
	if err := json.Unmarshal(raw, o); err != nil {
		return nil, fmt.Errorf("server %q: %w", name, err)
	}
	return o, nil
}

func (f *File) put(name string, o *Object) error {
	b, err := o.MarshalJSON()
	if err != nil {
		return err
	}
	f.servers.SetRaw(name, b)
	return nil
}

// Server parses one server entry.
func (f *File) Server(name string) (*Server, error) {
	o, err := f.obj(name)
	if err != nil {
		return nil, err
	}
	return parseServer(name, o), nil
}

// Servers parses all servers; broken entries come back with Err set.
func (f *File) Servers() []*Server {
	var out []*Server
	for _, n := range f.Names() {
		s, err := f.Server(n)
		if err != nil {
			s = &Server{Name: n, Err: err}
		}
		out = append(out, s)
	}
	return out
}

// SetDisabled enables or disables a server ("disabled": true; removed when enabled).
func (f *File) SetDisabled(name string, disabled bool) error {
	o, err := f.obj(name)
	if err != nil {
		return err
	}
	if disabled {
		if err := o.Set("disabled", true); err != nil {
			return err
		}
	} else {
		o.Delete("disabled")
	}
	return f.put(name, o)
}

// ToggleTool adds or removes tool in the server's disabledTools list and returns whether it is now disabled.
func (f *File) ToggleTool(name, tool string) (bool, error) {
	o, err := f.obj(name)
	if err != nil {
		return false, err
	}
	var list []string
	if _, err := o.Decode("disabledTools", &list); err != nil {
		return false, fmt.Errorf("disabledTools: %w", err)
	}
	disabled := !slices.Contains(list, tool)
	if disabled {
		list = append(list, tool)
	} else {
		list = slices.DeleteFunc(list, func(t string) bool { return t == tool })
	}
	if len(list) == 0 {
		o.Delete("disabledTools")
	} else if err := o.Set("disabledTools", list); err != nil {
		return false, err
	}
	return disabled, f.put(name, o)
}

// Basics are the fields of the add/edit form.
type Basics struct {
	Name           string
	Command        string // stdio servers
	Args           []string
	URL            string // http/sse servers
	Tags           []string
	RequestTimeout int  // ms, 0 = unset
	Env            []KV // stdio servers
	Headers        []KV // http/sse servers
}

// kvObject validates a key/value list (no empty or duplicate keys) and turns it into a JSON object.
func kvObject(what string, list []KV) (*Object, error) {
	o := NewObject()
	for _, kv := range list {
		if kv.Key == "" {
			return nil, fmt.Errorf("%s: empty name", what)
		}
		if o.Has(kv.Key) {
			return nil, fmt.Errorf("%s: %q is set twice", what, kv.Key)
		}
		if err := o.Set(kv.Key, kv.Value); err != nil {
			return nil, err
		}
	}
	return o, nil
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// ValidName reports whether name is usable as a server name.
func ValidName(name string) bool { return nameRe.MatchString(name) }

// Apply creates a server (oldName == "") or updates/renames one from the form fields. Fields the form
// does not cover (env, headers, cwd, …) are kept.
func (f *File) Apply(oldName string, b Basics) error {
	if !ValidName(b.Name) {
		return fmt.Errorf("invalid name %q (letters, digits, _ . -)", b.Name)
	}
	if (b.Command == "") == (b.URL == "") {
		return errors.New("set either a command or a URL")
	}
	if b.Name != oldName && f.Has(b.Name) {
		return fmt.Errorf("server %q already exists", b.Name)
	}
	env, err := kvObject("env", b.Env)
	if err != nil {
		return err
	}
	headers, err := kvObject("headers", b.Headers)
	if err != nil {
		return err
	}
	o := NewObject()
	if oldName != "" {
		var err error
		if o, err = f.obj(oldName); err != nil {
			return err
		}
	}
	set := func(k string, v any, empty bool) error {
		if empty {
			o.Delete(k)
			return nil
		}
		return o.Set(k, v)
	}
	if b.URL != "" {
		for _, k := range []string{"command", "args", "cwd", "env", "inheritParentEnv", "envFilter"} {
			o.Delete(k)
		}
		if err := o.Set("url", b.URL); err != nil {
			return err
		}
		var tr string
		o.Decode("transport", &tr)
		if tr == "" || tr == "stdio" {
			o.Set("transport", "http")
		}
		if err := setObj(o, "headers", headers); err != nil {
			return err
		}
	} else {
		for _, k := range []string{"url", "headers", "oauth"} {
			o.Delete(k)
		}
		var tr string
		if o.Decode("transport", &tr); tr != "" && tr != "stdio" {
			o.Delete("transport")
		}
		if err := o.Set("command", b.Command); err != nil {
			return err
		}
		if err := set("args", b.Args, len(b.Args) == 0); err != nil {
			return err
		}
		if err := setObj(o, "env", env); err != nil {
			return err
		}
	}
	if err := set("requestTimeout", b.RequestTimeout, b.RequestTimeout <= 0); err != nil {
		return err
	}
	if err := set("tags", b.Tags, len(b.Tags) == 0); err != nil {
		return err
	}
	if oldName != "" && oldName != b.Name {
		f.servers.Rename(oldName, b.Name)
	}
	return f.put(b.Name, o)
}

// setObj stores v under k, or removes k when v is empty.
func setObj(o *Object, k string, v *Object) error {
	if len(v.Keys()) == 0 {
		o.Delete(k)
		return nil
	}
	b, err := v.MarshalJSON()
	if err != nil {
		return err
	}
	o.SetRaw(k, b)
	return nil
}

// Remove deletes a server.
func (f *File) Remove(name string) { f.servers.Delete(name) }

// RawServer returns {"name": {...}} pretty-printed, for editing in $EDITOR.
func (f *File) RawServer(name string) ([]byte, error) {
	raw, ok := f.servers.Raw(name)
	if !ok {
		return nil, fmt.Errorf("no server %q", name)
	}
	w := NewObject()
	w.SetRaw(name, raw)
	b, err := w.MarshalJSON()
	if err != nil {
		return nil, err
	}
	b, err = indent(b)
	return append(b, '\n'), err
}

// ReplaceRaw replaces server oldName with the edited {"name": {...}} JSON (a rename is allowed). It
// returns the (possibly new) name.
func (f *File) ReplaceRaw(oldName string, b []byte) (string, error) {
	w := NewObject()
	if err := json.Unmarshal(b, w); err != nil {
		return "", err
	}
	keys := w.Keys()
	if len(keys) != 1 {
		return "", fmt.Errorf(`expected exactly one server {"name": {...}}, got %d keys`, len(keys))
	}
	name := keys[0]
	if !ValidName(name) {
		return "", fmt.Errorf("invalid name %q", name)
	}
	if name != oldName && f.Has(name) {
		return "", fmt.Errorf("server %q already exists", name)
	}
	raw, _ := w.Raw(name)
	o := NewObject()
	if err := json.Unmarshal(raw, o); err != nil {
		return "", fmt.Errorf("server %q: %w", name, err)
	}
	if oldName != "" && oldName != name {
		f.servers.Rename(oldName, name)
	}
	return name, f.put(name, o)
}

// Bytes renders the whole file.
func (f *File) Bytes() ([]byte, error) {
	sb, err := f.servers.MarshalJSON()
	if err != nil {
		return nil, err
	}
	f.root.SetRaw(serversKey, sb)
	b, err := f.root.MarshalJSON()
	if err != nil {
		return nil, err
	}
	b, err = indent(b)
	return append(b, '\n'), err
}

// Save writes the file atomically and keeps the previous version as <path>.bak.
func (f *File) Save() error { return f.save(true) }

// SaveNoBackup writes the file without touching <path>.bak (for transient changes like a restart cycle).
func (f *File) SaveNoBackup() error { return f.save(false) }

func (f *File) save(backup bool) error {
	b, err := f.Bytes()
	if err != nil {
		return err
	}
	mode := fs.FileMode(0o600)
	if st, err := os.Stat(f.Path); err == nil {
		mode = st.Mode().Perm()
		if old, err := os.ReadFile(f.Path); err == nil && backup {
			_ = os.WriteFile(f.Path+".bak", old, 0o600)
		}
	}
	dir := filepath.Dir(f.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".mcp-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), f.Path); err != nil {
		return err
	}
	if st, err := os.Stat(f.Path); err == nil {
		f.ModTime = st.ModTime()
	}
	return nil
}
