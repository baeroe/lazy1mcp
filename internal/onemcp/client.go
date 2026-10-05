// Package onemcp talks to the running 1MCP service: its /health endpoint and `1mcp inspect`.
package onemcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// Client knows where the 1mcp binary and the service are.
type Client struct {
	Bin string // 1mcp binary
	URL string // service base URL, e.g. http://127.0.0.1:3050
}

// Health is the subset of GET /health lazy1mcp shows.
type Health struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Servers struct {
		Total     int `json:"total"`
		Healthy   int `json:"healthy"`
		Unhealthy int `json:"unhealthy"`
	} `json:"servers"`
}

// Status is one server as the running service sees it.
type Status struct {
	Server          string `json:"server"`
	Type            string `json:"type"`
	Status          string `json:"status"`
	Available       bool   `json:"available"`
	ToolCount       int    `json:"toolCount"`
	HasInstructions bool   `json:"hasInstructions"`
}

// Tool is one tool of a server.
type Tool struct {
	Tool         string `json:"tool"`
	Description  string `json:"description"`
	RequiredArgs int    `json:"requiredArgs"`
	OptionalArgs int    `json:"optionalArgs"`
}

// Health fetches GET <URL>/health.
func (c Client) Health(ctx context.Context) (*Health, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.URL, "/")+"/health", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var h Health
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		return nil, fmt.Errorf("health: %w", err)
	}
	return &h, nil
}

// Servers lists the servers loaded by the running service (`1mcp inspect`).
func (c Client) Servers(ctx context.Context) ([]Status, error) {
	var out struct {
		Servers []Status `json:"servers"`
	}
	if err := c.inspect(ctx, &out); err != nil {
		return nil, err
	}
	return out.Servers, nil
}

// Tools lists the tools of one server (`1mcp inspect <name> --all`).
func (c Client) Tools(ctx context.Context, name string) ([]Tool, error) {
	var out struct {
		Tools []Tool `json:"tools"`
	}
	if err := c.inspect(ctx, &out, name, "--all"); err != nil {
		return nil, err
	}
	return out.Tools, nil
}

func (c Client) inspect(ctx context.Context, v any, args ...string) error {
	args = append([]string{"inspect"}, args...)
	args = append(args, "--format", "json", "--url", c.URL)
	b, err := c.run(ctx, 20*time.Second, args...)
	if err != nil {
		return err
	}
	// the CLI may print log lines before the JSON document
	if i := bytes.IndexByte(b, '{'); i > 0 {
		b = b[i:]
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("1mcp inspect: %w", err)
	}
	return nil
}

func (c Client) run(ctx context.Context, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, c.Bin, args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if i := strings.LastIndexByte(msg, '\n'); i >= 0 {
			msg = msg[i+1:]
		}
		if msg != "" {
			return nil, fmt.Errorf("1mcp %s: %s", args[0], msg)
		}
		return nil, fmt.Errorf("1mcp %s: %w", args[0], err)
	}
	// some 1mcp commands report failures with exit code 0
	if out := stdout.String(); strings.HasPrefix(strings.TrimSpace(out), "❌") {
		return nil, fmt.Errorf("1mcp %s: %s", args[0], strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out), "❌")))
	}
	return stdout.Bytes(), nil
}
