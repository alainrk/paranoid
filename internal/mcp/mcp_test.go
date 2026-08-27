package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
)

// rpcMessage is a loosely-typed JSON-RPC message used to build request
// lines and read back response fields in tests, without depending on
// this package's own unexported request/response types.
type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  any             `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// serve runs the server over an in-process pipe: requests is a slice of
// already-encoded JSON-RPC request lines. serve writes them all, closes
// the input side so Serve sees EOF, waits for Serve to return, and
// returns the response lines it wrote (one decoded rpcMessage per
// line), in order. Using io.Pipe rather than a plain buffer means the
// server reads exactly the way it would over a real stdio pipe: one
// line at a time, blocked on input until it arrives or the writer
// closes.
func serve(t *testing.T, srv *Server, requests []string) []rpcMessage {
	t.Helper()
	reqR, reqW := io.Pipe()
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- srv.Serve(context.Background(), reqR, &out)
	}()
	for _, line := range requests {
		_, err := reqW.Write([]byte(line + "\n"))
		require.NoError(t, err)
	}
	require.NoError(t, reqW.Close())
	require.NoError(t, <-done, "Serve must return nil on a clean EOF shutdown")

	var responses []rpcMessage
	dec := json.NewDecoder(&out)
	for dec.More() {
		var msg rpcMessage
		require.NoError(t, dec.Decode(&msg))
		responses = append(responses, msg)
	}
	return responses
}

func stubVerify(res finding.Result, err error) VerifyFunc {
	return func(context.Context, VerifyArgs) (finding.Result, error) {
		return res, err
	}
}

func TestServeInitializeToolsListToolsCall(t *testing.T) {
	res := finding.Result{
		SchemaVersion: finding.SchemaVersion,
		Score:         42,
		Verdict:       finding.VerdictWarn,
		Findings: []finding.Finding{
			{RuleID: "TI001", Severity: finding.Critical, Category: finding.TestIntegrity, File: "calc_test.go"},
		},
	}
	srv := &Server{Name: "paranoid", Version: "v0.0.0-test", Verify: stubVerify(res, nil)}

	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"verify","arguments":{"path":".","base":"HEAD~1"}}}`,
	}
	responses := serve(t, srv, requests)
	require.Len(t, responses, 3, "the notification must not get a response")

	// initialize
	require.Nil(t, responses[0].Error)
	var initResult struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	require.NoError(t, json.Unmarshal(responses[0].Result, &initResult))
	require.Equal(t, "2024-11-05", initResult.ProtocolVersion)
	require.Equal(t, "paranoid", initResult.ServerInfo.Name)
	require.Equal(t, "v0.0.0-test", initResult.ServerInfo.Version)

	// tools/list
	require.Nil(t, responses[1].Error)
	var listResult struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(responses[1].Result, &listResult))
	require.Len(t, listResult.Tools, 1)
	require.Equal(t, "verify", listResult.Tools[0].Name)

	// tools/call
	require.Nil(t, responses[2].Error)
	var callResult struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	require.NoError(t, json.Unmarshal(responses[2].Result, &callResult))
	require.False(t, callResult.IsError)
	require.Len(t, callResult.Content, 1)
	require.Equal(t, "text", callResult.Content[0].Type)

	var gotReport finding.Result
	require.NoError(t, json.Unmarshal([]byte(callResult.Content[0].Text), &gotReport))
	require.Equal(t, res.Score, gotReport.Score)
	require.Equal(t, res.Verdict, gotReport.Verdict)
	require.Len(t, gotReport.Findings, 1)
	require.Equal(t, "TI001", gotReport.Findings[0].RuleID)
}

func TestServeToolsCallReturnsVerifyErrorAsToolFailure(t *testing.T) {
	srv := &Server{Name: "paranoid", Version: "test", Verify: stubVerify(finding.Result{}, io.ErrUnexpectedEOF)}
	responses := serve(t, srv, []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"verify","arguments":{}}}`,
	})
	require.Len(t, responses, 1)
	require.Nil(t, responses[0].Error, "a verify failure is a tool result, not a JSON-RPC error")
	var callResult struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	require.NoError(t, json.Unmarshal(responses[0].Result, &callResult))
	require.True(t, callResult.IsError)
	require.Contains(t, callResult.Content[0].Text, io.ErrUnexpectedEOF.Error())
}

// TestServeProtocolErrors table-drives the JSON-RPC error paths: a
// malformed line, an invalid envelope, an unknown method, and an
// unknown tool name.
func TestServeProtocolErrors(t *testing.T) {
	cases := []struct {
		name       string
		request    string
		wantCode   int
		wantErrLen bool // require an error object at all
	}{
		{
			name:       "not JSON at all",
			request:    `{not json`,
			wantCode:   codeParseError,
			wantErrLen: true,
		},
		{
			name:       "wrong jsonrpc version",
			request:    `{"jsonrpc":"1.0","id":1,"method":"initialize"}`,
			wantCode:   codeInvalidRequest,
			wantErrLen: true,
		},
		{
			name:       "missing method",
			request:    `{"jsonrpc":"2.0","id":1}`,
			wantCode:   codeInvalidRequest,
			wantErrLen: true,
		},
		{
			name:       "unknown method",
			request:    `{"jsonrpc":"2.0","id":1,"method":"tools/frobnicate"}`,
			wantCode:   codeMethodNotFound,
			wantErrLen: true,
		},
		{
			name:       "unknown tool name",
			request:    `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nope"}}`,
			wantCode:   codeInvalidParams,
			wantErrLen: true,
		},
		{
			name:       "tools/call with unparseable arguments",
			request:    `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"verify","arguments":"not an object"}}`,
			wantCode:   codeInvalidParams,
			wantErrLen: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := &Server{Name: "paranoid", Version: "test", Verify: stubVerify(finding.Result{}, nil)}
			responses := serve(t, srv, []string{tc.request})
			require.Len(t, responses, 1)
			if tc.wantErrLen {
				require.NotNil(t, responses[0].Error)
				require.Equal(t, tc.wantCode, responses[0].Error.Code)
			}
		})
	}
}

// TestServeNotificationGetsNoResponse proves a request with no id, even
// one naming a perfectly valid method, never produces a response line:
// JSON-RPC 2.0 notifications are one-way by definition.
func TestServeNotificationGetsNoResponse(t *testing.T) {
	srv := &Server{Name: "paranoid", Version: "test", Verify: stubVerify(finding.Result{}, nil)}
	responses := serve(t, srv, []string{
		`{"jsonrpc":"2.0","method":"tools/list"}`,
	})
	require.Empty(t, responses)
}

// TestServeBlankLinesAreIgnored proves a blank line between messages
// (allowed by the newline-delimited framing) produces no response and
// does not break parsing of the messages around it.
func TestServeBlankLinesAreIgnored(t *testing.T) {
	srv := &Server{Name: "paranoid", Version: "test", Verify: stubVerify(finding.Result{}, nil)}
	responses := serve(t, srv, []string{
		"",
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		"",
	})
	require.Len(t, responses, 1)
	require.Nil(t, responses[0].Error)
}

// TestServeShutsDownCleanlyOnEmptyInput proves Serve returns nil (a
// clean shutdown) even when stdin closes without a single message.
func TestServeShutsDownCleanlyOnEmptyInput(t *testing.T) {
	srv := &Server{Name: "paranoid", Version: "test", Verify: stubVerify(finding.Result{}, nil)}
	responses := serve(t, srv, nil)
	require.Empty(t, responses)
}
