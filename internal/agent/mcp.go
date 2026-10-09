package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/amitpatole/cgroupguard/internal/version"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

const mcpMaxLine = 1 << 20

// ServeMCP speaks the Model Context Protocol over newline-delimited JSON-RPC (stdio).
// It exposes the same read-only, redacted tools as the built-in agent. Mutating tools
// are listed but always refused, exactly as for the built-in agent.
func ServeMCP(ctx context.Context, in io.Reader, out io.Writer, env *Env, pol Policy, audit *Audit, maxToolBytes int) error {
	env.Prepare()
	reg := NewRegistry(pol)
	if maxToolBytes <= 0 {
		maxToolBytes = 8000
	}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64*1024), mcpMaxLine)
	enc := json.NewEncoder(out)
	reply := func(id json.RawMessage, result any, rerr *rpcError) error {
		if id == nil {
			return nil // notifications get no response
		}
		return enc.Encode(rpcResponse{JSONRPC: "2.0", ID: id, Result: result, Error: rerr})
	}
	_ = audit.Log("mcp_start", map[string]any{"readOnly": !pol.AllowMutating})
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var req rpcRequest
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil || req.JSONRPC != "2.0" {
			if e := enc.Encode(rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error"}}); e != nil {
				return e
			}
			continue
		}
		var err error
		switch req.Method {
		case "initialize":
			err = reply(req.ID, map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "cgroupguard", "version": version.Version}}, nil)
		case "notifications/initialized", "notifications/cancelled":
		case "ping":
			err = reply(req.ID, map[string]any{}, nil)
		case "tools/list":
			var tools []map[string]any
			for _, s := range reg.Specs() {
				tools = append(tools, map[string]any{"name": s.Name, "description": s.Description, "inputSchema": s.Schema})
			}
			err = reply(req.ID, map[string]any{"tools": tools}, nil)
		case "tools/call":
			var p struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if e := json.Unmarshal(req.Params, &p); e != nil || p.Name == "" {
				err = reply(req.ID, nil, &rpcError{-32602, "invalid params"})
				break
			}
			_ = audit.Log("tool_call", map[string]any{"via": "mcp", "tool": p.Name, "args": env.R.Scrub(string(p.Arguments))})
			text, cerr := reg.Call(ctx, env, p.Name, p.Arguments)
			isErr := cerr != nil
			if isErr {
				text = cerr.Error()
				_ = audit.Log("denied_or_error", map[string]any{"via": "mcp", "tool": p.Name})
			}
			text = env.R.Scrub(text)
			if len(text) > maxToolBytes {
				text = text[:maxToolBytes] + "\n[truncated]"
			}
			_ = audit.Log("tool_result", map[string]any{"via": "mcp", "tool": p.Name, "bytes": len(text), "sha256": sha(text)})
			err = reply(req.ID, map[string]any{"content": []map[string]string{{"type": "text", "text": wrap("tool:"+p.Name, text)}}, "isError": isErr}, nil)
		default:
			err = reply(req.ID, nil, &rpcError{-32601, fmt.Sprintf("method %q not found", req.Method)})
		}
		if err != nil {
			return err
		}
	}
	return sc.Err()
}
