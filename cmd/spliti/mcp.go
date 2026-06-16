package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// cmdMCP runs a minimal Model Context Protocol server over stdio (newline-
// delimited JSON-RPC 2.0), exposing the spliti agent loop as tools so an
// MCP-capable agent (e.g. Claude Code) can scaffold, inspect, and verify games
// natively. It is dependency-free: the MCP stdio transport is one JSON message
// per line, which is all this needs.
//
// stdout is the protocol channel — all diagnostics go to stderr.
func cmdMCP() error {
	// stdout is the JSON-RPC channel; keep all subprocess chatter off it.
	childStdout = os.Stderr

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	for in.Scan() {
		line := in.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcReq
		if err := json.Unmarshal(line, &req); err != nil {
			writeRPC(out, rpcResp{JSONRPC: "2.0", Error: &rpcErr{Code: -32700, Message: "parse error"}})
			continue
		}
		resp, isNotification := dispatchMCP(req)
		if isNotification {
			continue // notifications get no response
		}
		writeRPC(out, resp)
	}
	return in.Err()
}

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func writeRPC(out *bufio.Writer, r rpcResp) {
	r.JSONRPC = "2.0"
	b, err := json.Marshal(r)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mcp: marshal response:", err)
		return
	}
	out.Write(b)
	out.WriteByte('\n')
	out.Flush()
}

// dispatchMCP routes one request; the bool reports whether it was a
// notification (no response expected).
func dispatchMCP(req rpcReq) (rpcResp, bool) {
	resp := rpcResp{ID: req.ID}
	notification := len(req.ID) == 0
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "spliti", "version": "0.1.0"},
		}
	case "notifications/initialized":
		return resp, true
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": mcpTools()}
	case "tools/call":
		resp.Result = callTool(req.Params)
	default:
		if notification {
			return resp, true
		}
		resp.Error = &rpcErr{Code: -32601, Message: "method not found: " + req.Method}
	}
	return resp, false
}

// mcpTools is the advertised tool set: the agent loop — scaffold, read the
// surface, verify.
func mcpTools() []map[string]any {
	dirProp := map[string]any{
		"dir": map[string]any{"type": "string", "description": "project directory (default \".\")"},
	}
	return []map[string]any{
		{
			"name":        "spliti_new",
			"description": "Scaffold a new spliti game project. Always use this instead of hand-rolling the layout.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":   map[string]any{"type": "string", "description": "project name (creates <parent>/<name>/)"},
					"parent": map[string]any{"type": "string", "description": "parent directory (default \".\")"},
				},
				"required": []string{"name"},
			},
		},
		{
			"name":        "spliti_manifest",
			"description": "Print a project's scanned surface (scenes, components, prefabs, input) plus the engine quick-reference. Load this before working on a game.",
			"inputSchema": map[string]any{"type": "object", "properties": dirProp},
		},
		{
			"name":        "spliti_check",
			"description": "Run the game headlessly for N deterministic frames and return the resulting world state as JSON. The agent's verify step: edit code, run this, assert on the state.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"dir":   map[string]any{"type": "string", "description": "project directory (default \".\")"},
					"ticks": map[string]any{"type": "integer", "description": "frames to run (default 120)"},
					"seed":  map[string]any{"type": "integer", "description": "RNG seed (default 1)"},
				},
			},
		},
	}
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// callTool runs one tool and returns an MCP tool result. Tool failures are
// reported as results with isError=true (not JSON-RPC errors), per MCP.
func callTool(raw json.RawMessage) map[string]any {
	var p toolCallParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return toolError("bad tool params: " + err.Error())
	}
	args := map[string]any{}
	if len(p.Arguments) > 0 {
		_ = json.Unmarshal(p.Arguments, &args)
	}
	dir := strArg(args, "dir", ".")

	switch p.Name {
	case "spliti_new":
		name := strArg(args, "name", "")
		if name == "" {
			return toolError("spliti_new: name is required")
		}
		parent := strArg(args, "parent", ".")
		target := filepath.Join(parent, name)
		if err := scaffold(target, engineReplacePath("")); err != nil {
			return toolError(err.Error())
		}
		return toolText(fmt.Sprintf("created %s/\nNext: spliti_manifest dir=%s, then edit and spliti_check.", target, target))

	case "spliti_manifest":
		s, err := manifestText(dir)
		if err != nil {
			return toolError(err.Error())
		}
		return toolText(s)

	case "spliti_check":
		ticks := intArg(args, "ticks", 120)
		seed := intArg(args, "seed", 1)
		worldPath := filepath.Join(dir, "world.json")
		cargs := []string{"-ticks", strconv.Itoa(ticks), "-seed", strconv.Itoa(seed), "-out", "world.json"}
		if err := checkProject(dir, cargs); err != nil {
			return toolError("check failed: " + err.Error())
		}
		b, err := os.ReadFile(worldPath)
		if err != nil {
			return toolError("check ran but world.json unreadable: " + err.Error())
		}
		return toolText(fmt.Sprintf("ran %d frames (seed %d). world.json:\n%s", ticks, seed, b))

	default:
		return toolError("unknown tool: " + p.Name)
	}
}

func toolText(s string) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": s}}}
}

func toolError(s string) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": s}}, "isError": true}
}

func strArg(m map[string]any, key, def string) string {
	if v, ok := m[key].(string); ok && v != "" {
		return v
	}
	return def
}

func intArg(m map[string]any, key string, def int) int {
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return def
}
