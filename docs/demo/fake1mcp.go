//go:build ignore

// fake1mcp stands in for the 1mcp CLI and service in the README screenshots (see docs/screenshots.sh), so
// no real MCP servers, accounts or tokens are involved. It implements exactly what lazy1mcp uses:
//
//	fake1mcp serve --port N                 GET /health
//	fake1mcp inspect [server] --format json server list or one server's tools
//
// Like 1MCP it reads the config ($ONE_MCP_CONFIG): disabled servers are not loaded and tools listed in
// disabledTools are hidden. Status and tools come from the tables below.
//
//	go build -o 1mcp docs/demo/fake1mcp.go
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
)

// status per server name; servers missing here are "connected"
var status = map[string]string{
	"linear": "awaiting_oauth",
}

type tool struct {
	Tool         string `json:"tool"`
	Description  string `json:"description"`
	RequiredArgs int    `json:"requiredArgs"`
	OptionalArgs int    `json:"optionalArgs"`
}

var tools = map[string][]tool{
	"filesystem": {
		{"read_text_file", "Read the complete contents of a file as text.", 1, 2},
		{"write_file", "Create a new file or completely overwrite an existing file.", 2, 0},
		{"edit_file", "Make line-based edits to a text file and return a git-style diff.", 2, 1},
		{"list_directory", "Get a detailed listing of all files and directories in a path.", 1, 0},
		{"directory_tree", "Get a recursive tree view of files and directories as JSON.", 1, 1},
		{"move_file", "Move or rename files and directories.", 2, 0},
		{"search_files", "Recursively search for files and directories matching a pattern.", 2, 1},
		{"get_file_info", "Retrieve detailed metadata about a file or directory.", 1, 0},
		{"list_allowed_directories", "Returns the list of directories this server is allowed to access.", 0, 0},
	},
	"github": {
		{"get_me", "Get details of the authenticated GitHub user.", 0, 0},
		{"search_repositories", "Search for GitHub repositories.", 1, 2},
		{"get_file_contents", "Get the contents of a file or directory from a repository.", 3, 1},
		{"list_issues", "List issues in a repository with filters.", 2, 5},
		{"create_issue", "Create a new issue in a repository.", 3, 3},
		{"list_pull_requests", "List pull requests in a repository.", 2, 4},
		{"create_pull_request", "Create a new pull request.", 5, 2},
		{"merge_pull_request", "Merge a pull request.", 3, 3},
		{"get_workflow_run", "Get details of a GitHub Actions workflow run.", 3, 0},
		{"delete_file", "Delete a file from a repository.", 5, 0},
	},
	"context7": {
		{"resolve-library-id", "Resolves a package name to a Context7-compatible library ID.", 1, 0},
		{"query-docs", "Retrieves up-to-date documentation and code examples for a library.", 2, 1},
	},
	"playwright": {
		{"browser_navigate", "Navigate to a URL.", 1, 0},
		{"browser_snapshot", "Capture an accessibility snapshot of the current page.", 0, 0},
		{"browser_click", "Perform a click on a web page.", 2, 3},
		{"browser_type", "Type text into an editable element.", 3, 2},
		{"browser_fill_form", "Fill multiple form fields.", 1, 0},
		{"browser_take_screenshot", "Take a screenshot of the current page.", 0, 4},
		{"browser_console_messages", "Returns all console messages.", 0, 1},
		{"browser_network_requests", "Returns all network requests since loading the page.", 0, 1},
		{"browser_evaluate", "Evaluate a JavaScript expression on the page or an element.", 1, 2},
		{"browser_tabs", "List, create, close, or select a browser tab.", 1, 1},
		{"browser_close", "Close the page.", 0, 0},
	},
	"postgres": {
		{"query", "Run a read-only SQL query.", 1, 0},
		{"list_tables", "List the tables of a schema.", 0, 1},
		{"describe_table", "Show the columns, indexes and constraints of a table.", 1, 1},
		{"explain_query", "Show the execution plan of a query.", 1, 1},
	},
	"memory": {
		{"create_entities", "Create multiple new entities in the knowledge graph.", 1, 0},
		{"create_relations", "Create relations between entities.", 1, 0},
		{"add_observations", "Add new observations to existing entities.", 1, 0},
		{"search_nodes", "Search for nodes in the knowledge graph.", 1, 0},
		{"open_nodes", "Open specific nodes by name.", 1, 0},
		{"read_graph", "Read the entire knowledge graph.", 0, 0},
		{"delete_entities", "Delete entities and their relations.", 1, 0},
	},
}

type server struct {
	Disabled      bool     `json:"disabled"`
	DisabledTools []string `json:"disabledTools"`
}

func config() (names []string, servers map[string]server) {
	b, err := os.ReadFile(os.Getenv("ONE_MCP_CONFIG"))
	if err != nil {
		fail(err)
	}
	var c struct {
		MCPServers map[string]server `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &c); err != nil {
		fail(err)
	}
	for n := range c.MCPServers {
		names = append(names, n)
	}
	slices.Sort(names)
	return names, c.MCPServers
}

func visibleTools(name string, s server) []tool {
	var out []tool
	for _, t := range tools[name] {
		if !slices.Contains(s.DisabledTools, t.Tool) {
			out = append(out, t)
		}
	}
	return out
}

func stateOf(name string) string {
	if s, ok := status[name]; ok {
		return s
	}
	return "connected"
}

func inspect(target string) any {
	names, servers := config()
	if target == "" {
		type row struct {
			Server          string `json:"server"`
			Type            string `json:"type"`
			Status          string `json:"status"`
			Available       bool   `json:"available"`
			ToolCount       int    `json:"toolCount"`
			HasInstructions bool   `json:"hasInstructions"`
		}
		var rows []row
		for _, n := range names {
			s := servers[n]
			if s.Disabled {
				continue
			}
			st := stateOf(n)
			rows = append(rows, row{n, "external", st, st == "connected", len(visibleTools(n, s)), false})
		}
		return map[string]any{"kind": "servers", "servers": rows}
	}
	s, ok := servers[target]
	if !ok || s.Disabled || stateOf(target) != "connected" {
		fmt.Fprintf(os.Stderr, "server %s is not available\n", target)
		os.Exit(1)
	}
	return map[string]any{"kind": "server", "server": target, "status": "connected", "tools": visibleTools(target, s)}
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fail(fmt.Errorf("usage: fake1mcp serve --port N | inspect [server] --format json"))
	}
	switch args[0] {
	case "serve":
		port := "3050"
		for i, a := range args {
			if a == "--port" && i+1 < len(args) {
				port = args[i+1]
			}
		}
		http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"status": "healthy", "version": "0.38.2"})
		})
		fail(http.ListenAndServe("127.0.0.1:"+port, nil))
	case "inspect":
		target := ""
		for i := 1; i < len(args); i++ {
			switch a := args[i]; {
			case a == "--format" || a == "--url" || a == "-u":
				i++
			case len(a) > 1 && a[0] == '-':
			case target == "":
				target = a
			}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(inspect(target))
	default:
		fail(fmt.Errorf("fake1mcp: unsupported command %q", args[0]))
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "fake1mcp:", err)
	os.Exit(1)
}
