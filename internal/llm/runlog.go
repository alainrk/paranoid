package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// RunLogDir is where request logs go, relative to the repo root.
const RunLogDir = ".paranoid/formal/runs"

// LogEntry is one line of the request log. It holds what was sent and
// what came back. It has no field for a key or a header, on purpose.
type LogEntry struct {
	Seq          int       `json:"seq"`
	Time         string    `json:"time"`
	Provider     string    `json:"provider"`
	Model        string    `json:"model"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	StopReason   string    `json:"stop_reason,omitempty"`
	System       string    `json:"system,omitempty"`
	Messages     []Message `json:"messages"`
	MaxTokens    int       `json:"max_tokens"`
	Response     string    `json:"response"`
	Error        string    `json:"error,omitempty"`
}

// RunLog writes one JSON line per provider call. It is safe for
// concurrent use.
type RunLog struct {
	mu      sync.Mutex
	path    string
	id      string
	f       *os.File
	seq     int
	now     func() time.Time
	secrets []string
}

// OpenRunLog creates a new log file under root/.paranoid/formal/runs.
// The run id is the UTC time from now, like 20260102T030405Z. If that
// file exists, a counter is added (-2, -3, ...), so the id is
// predictable and needs no random numbers. secrets are strings (the
// API key) that are replaced by [redacted] if they ever show up in the
// text being logged.
func OpenRunLog(root string, now func() time.Time, secrets ...string) (*RunLog, error) {
	if now == nil {
		now = time.Now
	}
	dir := filepath.Join(root, filepath.FromSlash(RunLogDir))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("creating the run log directory: %w", err)
	}
	base := now().UTC().Format("20060102T150405Z")
	for n := 1; n < 1000; n++ {
		id := base
		if n > 1 {
			id = fmt.Sprintf("%s-%d", base, n)
		}
		path := filepath.Join(dir, id+".jsonl")
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- fixed directory, generated name
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("creating the run log: %w", err)
		}
		var keep []string
		for _, s := range secrets {
			if len(s) >= 4 {
				keep = append(keep, s)
			}
		}
		return &RunLog{path: path, id: id, f: f, now: now, secrets: keep}, nil
	}
	return nil, errors.New("creating the run log: too many logs with the same timestamp")
}

// ID is the run id.
func (l *RunLog) ID() string { return l.id }

// Path is the log file path.
func (l *RunLog) Path() string { return l.path }

// Append writes one entry. The sequence number and time are set here.
func (l *RunLog) Append(e LogEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	e.Seq = l.seq
	e.Time = l.now().UTC().Format(time.RFC3339)
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("encoding a run log entry: %w", err)
	}
	text := string(line)
	for _, s := range l.secrets {
		// The key could be echoed in a prompt or a reply. Look for the
		// JSON-escaped form too, which is what sits in the line.
		esc, _ := json.Marshal(s)
		text = strings.ReplaceAll(text, strings.Trim(string(esc), `"`), "[redacted]")
		text = strings.ReplaceAll(text, s, "[redacted]")
	}
	if _, err := l.f.WriteString(text + "\n"); err != nil {
		return fmt.Errorf("writing the run log: %w", err)
	}
	return nil
}

// Close closes the log file.
func (l *RunLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.f.Close(); err != nil {
		return fmt.Errorf("closing the run log: %w", err)
	}
	return nil
}
