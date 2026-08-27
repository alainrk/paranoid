package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
	"github.com/alainrk/paranoid/internal/mcp"
)

// runMCPRequests drives the real mcp.Server, wired to this package's
// own verifyMCPTool, over an in-process io.Pipe: requests go in one
// end, get processed exactly as they would over a real stdio
// transport, and every response line comes back decoded.
func runMCPRequests(t *testing.T, requests []string) []map[string]any {
	t.Helper()
	server := &mcp.Server{Name: AppName, Version: version, Verify: verifyMCPTool}
	reqR, reqW := io.Pipe()
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(context.Background(), reqR, &out)
	}()
	for _, line := range requests {
		_, err := reqW.Write([]byte(line + "\n"))
		require.NoError(t, err)
	}
	require.NoError(t, reqW.Close())
	require.NoError(t, <-done)

	var responses []map[string]any
	dec := json.NewDecoder(&out)
	for dec.More() {
		var msg map[string]any
		require.NoError(t, dec.Decode(&msg))
		responses = append(responses, msg)
	}
	return responses
}

// TestMCPToolsCallOnFixtureRepo is the MCP server's own end-to-end
// test: initialize, tools/list, then tools/call against a small,
// real git repository (the same shape TestVerifyOnRepo builds),
// checked over an in-process pipe. It proves the tool result carries
// exactly the JSON report `paranoid verify` would have printed for the
// same range.
func TestMCPToolsCallOnFixtureRepo(t *testing.T) {
	dir := initFixtureRepo(t)
	writeRepoFile(t, dir, "calc.go", "package calc\n\nfunc Add(a, b int) int { return a + b }\n")
	writeRepoFile(t, dir, "calc_test.go", testFileSource)
	base := commitRepoAll(t, dir, "base")
	removeRepoFile(t, dir, "calc_test.go")
	head := commitRepoAll(t, dir, "head")

	callParams, err := json.Marshal(map[string]any{
		"name": "verify",
		"arguments": map[string]any{
			"path":    dir,
			"base":    base,
			"head":    head,
			"sandbox": "none",
		},
	})
	require.NoError(t, err)

	responses := runMCPRequests(t, []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":` + string(callParams) + `}`,
	})
	require.Len(t, responses, 3, "the notification must produce no response line")

	require.Nil(t, responses[0]["error"])
	require.Nil(t, responses[1]["error"])

	toolsList := responses[1]["result"].(map[string]any)
	tools := toolsList["tools"].([]any)
	require.Len(t, tools, 1)
	require.Equal(t, "verify", tools[0].(map[string]any)["name"])

	require.Nil(t, responses[2]["error"], "tools/call itself must succeed even though the audited repo fails")
	callResult := responses[2]["result"].(map[string]any)
	require.Equal(t, false, callResult["isError"])
	content := callResult["content"].([]any)
	require.Len(t, content, 1)
	text := content[0].(map[string]any)["text"].(string)

	var res finding.Result
	require.NoError(t, json.Unmarshal([]byte(text), &res))
	require.Equal(t, finding.SchemaVersion, res.SchemaVersion)
	require.Equal(t, finding.VerdictWarn, res.Verdict)
	require.Len(t, res.Findings, 1)
	require.Equal(t, "TI001", res.Findings[0].RuleID)
	require.Equal(t, "calc_test.go", res.Findings[0].File)
}

// TestMCPToolsCallBadRefIsToolFailureNotProtocolError proves a verify
// execution error (an unresolvable ref) comes back as a normal
// tools/call result with isError true, not a JSON-RPC error: the
// protocol call itself succeeded.
func TestMCPToolsCallBadRefIsToolFailureNotProtocolError(t *testing.T) {
	dir := initFixtureRepo(t)
	writeRepoFile(t, dir, "a.txt", "hello\n")
	commitRepoAll(t, dir, "base")

	callParams, err := json.Marshal(map[string]any{
		"name":      "verify",
		"arguments": map[string]any{"path": dir, "base": "no-such-ref"},
	})
	require.NoError(t, err)

	responses := runMCPRequests(t, []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":` + string(callParams) + `}`,
	})
	require.Len(t, responses, 1)
	require.Nil(t, responses[0]["error"])
	callResult := responses[0]["result"].(map[string]any)
	require.Equal(t, true, callResult["isError"])
}

// TestMCPUnknownSandboxModeIsToolFailure proves an invalid sandbox
// argument is reported as a tool failure with a clear message, the same
// validation the verify command applies to --sandbox.
func TestMCPUnknownSandboxModeIsToolFailure(t *testing.T) {
	dir := initFixtureRepo(t)
	writeRepoFile(t, dir, "a.txt", "hello\n")
	commitRepoAll(t, dir, "base")

	callParams, err := json.Marshal(map[string]any{
		"name":      "verify",
		"arguments": map[string]any{"path": dir, "sandbox": "vagrant"},
	})
	require.NoError(t, err)

	responses := runMCPRequests(t, []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":` + string(callParams) + `}`,
	})
	require.Len(t, responses, 1)
	callResult := responses[0]["result"].(map[string]any)
	require.Equal(t, true, callResult["isError"])
	content := callResult["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	require.Contains(t, text, "unknown sandbox mode")
}

func TestMCPCommandRegistered(t *testing.T) {
	root := newRootCmd()
	cmd, _, err := root.Find([]string{"mcp"})
	require.NoError(t, err)
	require.Equal(t, "mcp", cmd.Name())
}
