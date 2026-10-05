package config

import (
	"encoding/json"
	"strings"
)

// KV is one env variable or header, in file order.
type KV struct{ Key, Value string }

// Server is the parsed, read-only view of one server entry.
type Server struct {
	Name           string
	Transport      string
	Command        string
	Args           []string
	URL            string
	Cwd            string
	Env            []KV
	Headers        []KV
	Tags           []string
	Disabled       bool
	DisabledTools  []string
	RequestTimeout int      // ms
	Other          []string // keys lazy1mcp does not show explicitly
	Err            error    // set when the entry could not be parsed
}

// Kind is stdio, http, sse, … (derived when "transport" is not set).
func (s *Server) Kind() string {
	switch {
	case s.Transport != "":
		return s.Transport
	case s.URL != "":
		return "http"
	default:
		return "stdio"
	}
}

// Remote reports whether the server is reached over the network.
func (s *Server) Remote() bool { return s.Kind() != "stdio" }

var shownKeys = map[string]bool{
	"transport": true, "type": true, "command": true, "args": true, "url": true, "cwd": true, "env": true,
	"headers": true, "tags": true, "disabled": true, "disabledTools": true, "requestTimeout": true,
}

func parseServer(name string, o *Object) *Server {
	s := &Server{Name: name}
	o.Decode("transport", &s.Transport)
	if s.Transport == "" {
		o.Decode("type", &s.Transport)
	}
	o.Decode("command", &s.Command)
	o.Decode("args", &s.Args)
	o.Decode("url", &s.URL)
	o.Decode("cwd", &s.Cwd)
	o.Decode("tags", &s.Tags)
	o.Decode("disabled", &s.Disabled)
	o.Decode("disabledTools", &s.DisabledTools)
	var timeout json.Number
	if ok, _ := o.Decode("requestTimeout", &timeout); ok {
		if n, err := timeout.Int64(); err == nil {
			s.RequestTimeout = int(n)
		}
	}
	s.Env = parseKVs(o, "env")
	s.Headers = parseKVs(o, "headers")
	for _, k := range o.Keys() {
		if !shownKeys[k] {
			s.Other = append(s.Other, k)
		}
	}
	return s
}

// parseKVs reads an object of strings, or (env only) an array of "KEY=VALUE" strings.
func parseKVs(o *Object, key string) []KV {
	raw, ok := o.Raw(key)
	if !ok {
		return nil
	}
	var out []KV
	m := NewObject()
	if err := json.Unmarshal(raw, m); err == nil {
		for _, k := range m.Keys() {
			var v string
			if r, _ := m.Raw(k); json.Unmarshal(r, &v) != nil {
				v = string(r)
			}
			out = append(out, KV{k, v})
		}
		return out
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		for _, e := range list {
			k, v, _ := strings.Cut(e, "=")
			out = append(out, KV{k, v})
		}
	}
	return out
}
