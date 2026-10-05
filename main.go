// lazy1mcp is a terminal UI to see and manage the MCP servers of a 1MCP (https://github.com/1mcp-app/agent)
// instance: status, tools, enable/disable, add/edit/remove.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/baeroe/lazy1mcp/internal/config"
	"github.com/baeroe/lazy1mcp/internal/onemcp"
	"github.com/baeroe/lazy1mcp/internal/tui"
)

var version = "dev"

func main() {
	cfgPath := flag.String("config", config.DefaultPath(), "1MCP config file (env ONE_MCP_CONFIG)")
	url := flag.String("url", envOr("LAZY1MCP_URL", "http://127.0.0.1:3050"), "URL of the running 1MCP service (env LAZY1MCP_URL)")
	bin := flag.String("bin", envOr("LAZY1MCP_BIN", "1mcp"), "1mcp binary (env LAZY1MCP_BIN)")
	showVersion := flag.Bool("version", false, "print the version")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: lazy1mcp [flags]\n       lazy1mcp version\n\nflags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion || flag.Arg(0) == "version" {
		fmt.Println("lazy1mcp", version)
		return
	}
	if flag.NArg() > 0 {
		flag.Usage()
		os.Exit(2)
	}
	*bin = findBin(*bin)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lazy1mcp:", err)
		os.Exit(1)
	}
	m := tui.New(cfg, onemcp.Client{Bin: *bin, URL: *url})
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "lazy1mcp:", err)
		os.Exit(1)
	}
}

// findBin resolves the 1mcp binary: PATH first, then the usual global install locations of pnpm/npm,
// which are often missing from PATH when lazy1mcp is started by a multiplexer or launcher.
func findBin(bin string) string {
	if p, err := exec.LookPath(bin); err == nil {
		return p
	}
	if bin != "1mcp" {
		return bin
	}
	home, _ := os.UserHomeDir()
	for _, p := range []string{
		filepath.Join(home, "Library", "pnpm", "1mcp"),
		filepath.Join(home, "Library", "pnpm", "bin", "1mcp"),
		filepath.Join(home, ".local", "share", "pnpm", "1mcp"),
		filepath.Join(home, ".local", "share", "pnpm", "bin", "1mcp"),
		"/opt/homebrew/bin/1mcp",
		"/usr/local/bin/1mcp",
	} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return bin
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
