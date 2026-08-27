package watch

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// requireRealFilesystemEvents skips unless this test is explicitly
// asked to touch a real fsnotify watcher. Unlike the clean-room e2e
// tests (internal/cli/sandbox_e2e_test.go), which skip when docker or
// podman is simply not installed, there is no equivalent "is fsnotify
// available" probe here: fsnotify links unconditionally on every
// target platform. The gate is instead an explicit opt-in env
// variable, so this test (real inotify/kqueue/ReadDirectoryChangesW
// events, real timing) never runs as part of `make check` or CI,
// only when a developer asks for it locally.
func requireRealFilesystemEvents(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping real filesystem watch integration test in short mode")
	}
	if os.Getenv("PARANOID_WATCH_E2E") == "" {
		t.Skip("set PARANOID_WATCH_E2E=1 to run the real fsnotify integration test")
	}
}

// TestWatchRealFilesystemEndToEnd drives Loop with a real
// NewFsnotifySource over a real temporary directory: creating a
// relevant file triggers a run, creating a new directory extends the
// watch to it, and a file inside that new directory also triggers a
// run. This is the one test in the package that does not use the
// fake Source; every debounce, coalescing, and shutdown behavior is
// already covered against the fake in loop_test.go.
func TestWatchRealFilesystemEndToEnd(t *testing.T) {
	requireRealFilesystemEvents(t)

	root := t.TempDir()
	source, err := NewFsnotifySource()
	require.NoError(t, err)

	stub := &checkStub{started: make(chan struct{}, 8)}
	loop := &Loop{
		Root:     root,
		Source:   source,
		Debounce: 200 * time.Millisecond,
		Check:    stub.fn,
		Out:      io.Discard,
		ErrOut:   io.Discard,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- loop.Watch(ctx) }()

	// Give the initial recursive watch setup a moment to land before
	// the first real filesystem write.
	time.Sleep(50 * time.Millisecond)

	testFile := filepath.Join(root, "example_test.go")
	require.NoError(t, os.WriteFile(testFile, []byte("package example\n"), 0o644))

	require.Eventually(t, func() bool { return stub.callCount() >= 1 }, 5*time.Second, 20*time.Millisecond,
		"creating a relevant test file under a real watched directory must trigger a real run")

	sub := filepath.Join(root, "sub")
	require.NoError(t, os.Mkdir(sub, 0o755))
	time.Sleep(100 * time.Millisecond) // let the directory-add handling land

	before := stub.callCount()
	subFile := filepath.Join(sub, "nested_test.go")
	require.NoError(t, os.WriteFile(subFile, []byte("package sub\n"), 0o644))

	require.Eventually(t, func() bool { return stub.callCount() > before }, 5*time.Second, 20*time.Millisecond,
		"a relevant file inside a directory created after startup must also trigger a run")

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Watch did not shut down cleanly")
	}
}
