// Package mcp implements an MCP (Model Context Protocol) server over
// stdio: JSON-RPC 2.0 messages, one per line, newline-delimited, per
// the MCP stdio transport. It hand-rolls the framing on the standard
// library instead of taking an SDK dependency: no MCP server SDK is
// on the project's dependency allowlist (see AGENTS.md), and the
// surface this tool needs, three methods and one tool, is small
// enough that an SDK would cost more than it saves.
//
// The server understands exactly three methods: initialize, tools/list,
// and tools/call for a single tool, verify. Anything else, including
// every "notifications/*" method a real MCP client sends after
// initialize, is either a no-op (notifications never get a response,
// by the JSON-RPC 2.0 contract, since they carry no id) or a JSON-RPC
// "method not found" error.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/alainrk/paranoid/internal/finding"
	"github.com/alainrk/paranoid/internal/report"
)

// jsonrpcVersion is the only JSON-RPC version this server accepts.
const jsonrpcVersion = "2.0"

// verifyToolName is the one tool this server exposes.
const verifyToolName = "verify"

// defaultProtocolVersion is echoed back on initialize when the caller's
// own request names none. This server does not implement version
// negotiation: it always answers the same three methods regardless of
// the protocol version a client asks for, so this constant only matters
// as a courtesy default, not as a compatibility promise.
const defaultProtocolVersion = "2024-11-05"

// maxMessageBytes bounds one line of input or output. A verify report
// on a very large diff can run to several megabytes of JSON; this
// leaves generous headroom without accepting an unbounded line.
const maxMessageBytes = 32 * 1024 * 1024

// JSON-RPC 2.0 error codes this server can return. These are the
// reserved codes from the spec itself, not paranoid-specific ones.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// VerifyArgs carries the verify tool's arguments, decoded from the
// tools/call request's arguments object. Every field is optional and
// defaults the same way the equivalent verify flag does.
type VerifyArgs struct {
	Path    string `json:"path"`
	Base    string `json:"base"`
	Head    string `json:"head"`
	Claims  string `json:"claims"`
	Session string `json:"session"`
	Offline bool   `json:"offline"`
	Sandbox string `json:"sandbox"`
}

// VerifyFunc runs the verify pipeline for one tools/call invocation.
// The cli package supplies the real implementation, wired to the same
// pipeline the verify command runs; tests supply a stub.
type VerifyFunc func(ctx context.Context, args VerifyArgs) (finding.Result, error)

// Server is a minimal MCP server over stdio. Verify, Name, and Version
// must be set before Serve is called.
type Server struct {
	Verify  VerifyFunc
	Name    string
	Version string
}

// rpcRequest is one incoming JSON-RPC 2.0 message. ID is nil for a
// notification (per the spec, a notification carries no id at all,
// not even a null one) and is echoed back verbatim on any response.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// rpcResponse is one outgoing JSON-RPC 2.0 message. Result and Error
// are mutually exclusive; exactly one is set on any real response.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is a JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Serve reads newline-delimited JSON-RPC requests from r, dispatches
// each one, and writes newline-delimited JSON-RPC responses to w. It
// returns nil when r reaches EOF, which is the server's normal, clean
// shutdown: the caller (the mcp command) exits 0 once Serve returns
// nil. A malformed line never stops the loop; it produces a JSON-RPC
// parse-error response for that one message and Serve keeps reading.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxMessageBytes)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		// scanner.Bytes() is only valid until the next Scan call, and
		// handle may hold onto the request past that point (json.Unmarshal
		// copies strings but RawMessage fields alias the input slice), so
		// copy the line before dispatching it.
		msg := append([]byte(nil), line...)
		resp := s.handle(ctx, msg)
		if resp == nil {
			// A notification (or a message this server treats as one):
			// JSON-RPC 2.0 never sends a response for those.
			continue
		}
		out, err := json.Marshal(resp)
		if err != nil {
			return fmt.Errorf("encoding response: %w", err)
		}
		out = append(out, '\n')
		if _, err := w.Write(out); err != nil {
			return fmt.Errorf("writing response: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading request: %w", err)
	}
	return nil
}

// handle dispatches one already-read line. It returns nil when no
// response should be sent: either the message was a notification (no
// id, or a "notifications/*" method), or the input could not be
// classified as a request at all in a way that still lets the
// notification rule apply.
func (s *Server) handle(ctx context.Context, raw []byte) *rpcResponse {
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		// A message that does not even parse as JSON cannot be checked
		// for an id, so this is the one response that always goes out,
		// with a null id, exactly as JSON-RPC 2.0's parse-error case
		// specifies.
		return &rpcResponse{JSONRPC: jsonrpcVersion, Error: &rpcError{Code: codeParseError, Message: "parse error: " + err.Error()}}
	}

	// Every method under "notifications/" (the shape every MCP
	// notification uses, for example notifications/initialized) is
	// one-way by convention, whether or not this particular message
	// carried an id.
	if strings.HasPrefix(req.Method, "notifications/") {
		return nil
	}

	isNotification := len(req.ID) == 0
	respond := func(result any, rerr *rpcError) *rpcResponse {
		if isNotification {
			return nil
		}
		resp := &rpcResponse{JSONRPC: jsonrpcVersion, ID: req.ID}
		if rerr != nil {
			resp.Error = rerr
		} else {
			resp.Result = result
		}
		return resp
	}

	if req.JSONRPC != jsonrpcVersion {
		return respond(nil, &rpcError{Code: codeInvalidRequest, Message: `"jsonrpc" must be "2.0"`})
	}
	if req.Method == "" {
		return respond(nil, &rpcError{Code: codeInvalidRequest, Message: "missing method"})
	}

	switch req.Method {
	case "initialize":
		return respond(s.handleInitialize(req.Params), nil)
	case "tools/list":
		return respond(s.handleToolsList(), nil)
	case "tools/call":
		result, rerr := s.handleToolsCall(ctx, req.Params)
		return respond(result, rerr)
	default:
		return respond(nil, &rpcError{Code: codeMethodNotFound, Message: fmt.Sprintf("unknown method %q", req.Method)})
	}
}

// initializeParams is the subset of the initialize request this server
// reads. Everything else the real MCP initialize request can carry
// (clientInfo, capabilities) is accepted and ignored.
type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
}

// handleInitialize answers the MCP handshake. This server does no real
// version negotiation: it echoes back whatever protocolVersion the
// caller asked for (falling back to defaultProtocolVersion when none
// was given) and always advertises the same, fixed tools capability.
func (s *Server) handleInitialize(raw json.RawMessage) any {
	var p initializeParams
	// Best effort: a missing or malformed params object still gets a
	// normal reply, just with the default protocol version.
	_ = json.Unmarshal(raw, &p)
	version := p.ProtocolVersion
	if version == "" {
		version = defaultProtocolVersion
	}
	return map[string]any{
		"protocolVersion": version,
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    s.Name,
			"version": s.Version,
		},
	}
}

// handleToolsList answers tools/list with the single verify tool this
// server exposes.
func (s *Server) handleToolsList() any {
	return map[string]any{
		"tools": []any{verifyToolDescriptor()},
	}
}

// verifyToolDescriptor describes the verify tool: its name, a short
// description, and a JSON Schema for its arguments, mirroring the
// verify command's own flags (path, base, head, claims, session,
// offline, sandbox). Flags verify has that would change how the
// pipeline itself runs beyond these seven (format, output, fail-under,
// strict, sandbox-image, sandbox-timeout, compare-base, baseline,
// no-baseline) are deliberately not exposed here; the tool always
// returns the full JSON report and lets the caller decide what to do
// with it.
func verifyToolDescriptor() map[string]any {
	return map[string]any{
		"name":        verifyToolName,
		"description": "Run paranoid verify over a git range and return the JSON trust report.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string", "description": "repository path (default: the current directory)"},
				"base":    map[string]any{"type": "string", "description": "git ref before the agent worked (default: merge-base with the default branch, else HEAD~1)"},
				"head":    map[string]any{"type": "string", "description": "git ref after the agent worked (default: the working tree)"},
				"claims":  map[string]any{"type": "string", "description": "path to a free-text file with the agent's summary of what it did, same as --claims"},
				"session": map[string]any{"type": "string", "description": "path to an agent session file; format auto-detected, same as --session"},
				"offline": map[string]any{"type": "boolean", "description": "skip all registry lookups, same as --offline"},
				"sandbox": map[string]any{"type": "string", "enum": []string{"auto", "docker", "podman", "none"}, "description": "clean-room runtime, same as --sandbox"},
			},
			"additionalProperties": false,
		},
	}
}

// toolsCallParams is the tools/call request's params object.
type toolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// handleToolsCall runs the verify tool and returns its MCP tool
// result. A bad request (an unknown tool name, arguments that do not
// decode) is a JSON-RPC protocol error. A verify execution failure
// (bad ref, git missing, and so on) is not: the JSON-RPC call itself
// succeeded, the tool it invoked did not, so it comes back as a normal
// tools/call result with isError true and the error text as the
// content, exactly like the MCP spec's own convention for tool
// failures.
func (s *Server) handleToolsCall(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var p toolsCallParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "invalid tools/call params: " + err.Error()}
	}
	if p.Name != verifyToolName {
		return nil, &rpcError{Code: codeInvalidParams, Message: fmt.Sprintf("unknown tool %q, want %q", p.Name, verifyToolName)}
	}
	var args VerifyArgs
	if len(p.Arguments) > 0 {
		if err := json.Unmarshal(p.Arguments, &args); err != nil {
			return nil, &rpcError{Code: codeInvalidParams, Message: "invalid verify arguments: " + err.Error()}
		}
	}
	if s.Verify == nil {
		return nil, &rpcError{Code: codeInternalError, Message: "server has no verify implementation wired in"}
	}

	res, err := s.Verify(ctx, args)
	if err != nil {
		return toolResult(true, err.Error()), nil
	}
	reportJSON, err := report.RenderJSON(res)
	if err != nil {
		return toolResult(true, fmt.Sprintf("rendering report: %s", err)), nil
	}
	return toolResult(false, reportJSON), nil
}

// toolResult builds an MCP tool result: one text content block, and
// isError marking whether the tool itself failed.
func toolResult(isError bool, text string) map[string]any {
	return map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": text},
		},
		"isError": isError,
	}
}
