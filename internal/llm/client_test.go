package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeProvider returns scripted responses.
type fakeProvider struct {
	mu    sync.Mutex
	resp  Response
	err   error
	calls int
}

func (f *fakeProvider) Name() string { return "fake" }
func (f *fakeProvider) Complete(context.Context, Request) (Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.resp, f.err
}

func fixedClock() func() time.Time {
	t := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	return func() time.Time { return t }
}

func readLog(t *testing.T, path string) []LogEntry {
	t.Helper()
	f, err := os.Open(path) // #nosec G304 -- test temp path
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	var out []LogEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var e LogEntry
		require.NoError(t, json.Unmarshal(sc.Bytes(), &e))
		out = append(out, e)
	}
	return out
}

func TestBudgetCaps(t *testing.T) {
	tests := []struct {
		name     string
		caps     Caps
		tokens   int // tokens recorded per call
		calls    int
		wantFlag string
		wantOK   int // calls admitted before the stop
	}{
		{"request cap", Caps{MaxRequests: 2, MaxTokens: 1000}, 1, 5, "--max-requests", 2},
		{"token cap", Caps{MaxRequests: 100, MaxTokens: 10}, 4, 10, "--max-tokens", 3},
		{"under both", Caps{MaxRequests: 5, MaxTokens: 100}, 1, 3, "", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBudget(tt.caps)
			var stop error
			ok := 0
			for range tt.calls {
				if err := b.Admit(); err != nil {
					stop = err
					break
				}
				ok++
				b.Record(tt.tokens, 0)
			}
			require.Equal(t, tt.wantOK, ok)
			if tt.wantFlag == "" {
				require.NoError(t, stop)
				return
			}
			require.ErrorIs(t, stop, ErrCapReached)
			var ce *CapError
			require.ErrorAs(t, stop, &ce)
			require.Equal(t, tt.wantFlag, ce.Flag)
			require.Contains(t, stop.Error(), tt.wantFlag)
		})
	}
}

func TestBudgetDefaults(t *testing.T) {
	b := NewBudget(Caps{})
	require.Equal(t, DefaultMaxRequests, b.caps.MaxRequests)
	require.Equal(t, DefaultMaxTotalTokens, b.caps.MaxTokens)
	b.Record(-5, 3)
	_, tokens := b.Usage()
	require.Equal(t, 3, tokens)
}

func TestClientStopsAtCapBeforeSending(t *testing.T) {
	dir := t.TempDir()
	lg, err := OpenRunLog(dir, fixedClock())
	require.NoError(t, err)
	defer func() { _ = lg.Close() }()
	fp := &fakeProvider{resp: Response{Text: "a", InputTokens: 5, OutputTokens: 5}}
	c := NewClient(fp, "m", NewBudget(Caps{MaxRequests: 100, MaxTokens: 10}), lg)

	_, err = c.Complete(t.Context(), testReq)
	require.NoError(t, err)
	_, err = c.Complete(t.Context(), testReq)
	require.ErrorIs(t, err, ErrCapReached)
	require.Contains(t, err.Error(), "--max-tokens")
	require.Equal(t, 1, fp.calls, "the provider is not called after the cap")
	require.Len(t, readLog(t, lg.Path()), 1)
}

func TestRunLogContentAndDeterministicID(t *testing.T) {
	dir := t.TempDir()
	lg, err := OpenRunLog(dir, fixedClock())
	require.NoError(t, err)
	require.Equal(t, "20260304T050607Z", lg.ID())
	require.Equal(t, filepath.Join(dir, ".paranoid", "formal", "runs", "20260304T050607Z.jsonl"), lg.Path())

	// A second log at the same second gets a counter, not randomness.
	lg2, err := OpenRunLog(dir, fixedClock())
	require.NoError(t, err)
	require.Equal(t, "20260304T050607Z-2", lg2.ID())
	require.NoError(t, lg2.Close())

	fp := &fakeProvider{resp: Response{Text: "the answer", Model: "real-model", StopReason: "end_turn", InputTokens: 7, OutputTokens: 9}}
	c := NewClient(fp, "configured", NewBudget(Caps{}), lg)
	_, err = c.Complete(t.Context(), testReq)
	require.NoError(t, err)
	fp.resp, fp.err = Response{}, errors.New("provider failed")
	_, err = c.Complete(t.Context(), testReq)
	require.Error(t, err)
	require.NoError(t, lg.Close())

	entries := readLog(t, lg.Path())
	require.Len(t, entries, 2)
	require.Equal(t, LogEntry{
		Seq: 1, Time: "2026-03-04T05:06:07Z", Provider: "fake", Model: "real-model",
		InputTokens: 7, OutputTokens: 9, StopReason: "end_turn",
		System: testReq.System, Messages: testReq.Messages, MaxTokens: 500, Response: "the answer",
	}, entries[0])
	require.Equal(t, 2, entries[1].Seq)
	require.Equal(t, "configured", entries[1].Model)
	require.Equal(t, "provider failed", entries[1].Error)

	info, err := os.Stat(lg.Path())
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// TestRunLogNeverHoldsTheKey runs real provider clients against a test
// server whose replies echo the key, and checks that neither the log
// nor any error text holds it.
func TestRunLogNeverHoldsTheKey(t *testing.T) {
	for _, pc := range providerCases() {
		t.Run(pc.name, func(t *testing.T) {
			echo := strings.NewReplacer("theorem done", "leaked "+testKey).Replace(pc.okBody)
			sc := &scripted{steps: []step{{status: 200, body: echo}}}
			srv := newServer(sc)
			defer srv.Close()

			dir := t.TempDir()
			lg, err := OpenRunLog(dir, fixedClock(), testKey)
			require.NoError(t, err)
			c := NewClient(pc.build(newOpts(srv, &sleeps{})), "test-model", NewBudget(Caps{}), lg)
			req := testReq
			req.Messages = append([]Message{{Role: "user", Text: "my key is " + testKey}}, req.Messages[1:]...)
			_, err = c.Complete(t.Context(), req)
			require.NoError(t, err)
			require.NoError(t, lg.Close())

			raw, err := os.ReadFile(lg.Path())
			require.NoError(t, err)
			require.NotContains(t, string(raw), testKey)
			require.Contains(t, string(raw), "[redacted]")
			// Auth header names and values never appear either.
			for _, bad := range []string{"x-api-key", "Authorization", "Bearer", "x-goog-api-key"} {
				require.NotContains(t, strings.ToLower(string(raw)), strings.ToLower(bad))
			}
			// The request itself never carried the key in its body.
			require.NotContains(t, sc.bodies[0], "x-api-key")
		})
	}
}

func TestOpenRunLogBadRoot(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(f, []byte("x"), 0o600))
	_, err := OpenRunLog(f, fixedClock())
	require.Error(t, err)
}
