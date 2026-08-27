package watch

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alainrk/paranoid/internal/finding"
)

// testDebounce is short enough to keep these tests fast, and long
// enough that a burst of sends spaced well under it reliably
// coalesces on any machine running the suite.
const testDebounce = 20 * time.Millisecond

// checkStub is a CheckFunc test double that counts calls, optionally
// blocks until released (or the run's context is canceled), and
// records whether it observed a cancellation.
type checkStub struct {
	mu         sync.Mutex
	calls      int
	block      chan struct{}
	cancelSeen bool
	started    chan struct{} // sent once per call, if non-nil
	result     finding.Result
}

func (s *checkStub) fn(ctx context.Context) (finding.Result, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	if s.started != nil {
		s.started <- struct{}{}
	}
	if s.block != nil {
		select {
		case <-s.block:
		case <-ctx.Done():
			s.mu.Lock()
			s.cancelSeen = true
			s.mu.Unlock()
		}
	}
	return s.result, nil
}

func (s *checkStub) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *checkStub) wasCanceled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancelSeen
}

// newTestLoop builds a Loop over a fake source and a real, empty temp
// directory (so the initial recursive WalkAndAdd has something real
// and harmless to walk), with output discarded.
func newTestLoop(t *testing.T, source *fakeSource, check CheckFunc) *Loop {
	t.Helper()
	return &Loop{
		Root:     t.TempDir(),
		Source:   source,
		Debounce: testDebounce,
		Check:    check,
		Out:      io.Discard,
		ErrOut:   io.Discard,
	}
}

// runLoop starts Watch in a goroutine and returns a channel that
// receives its result once it returns.
func runLoop(ctx context.Context, l *Loop) <-chan error {
	done := make(chan error, 1)
	go func() { done <- l.Watch(ctx) }()
	return done
}

func testFileEvent(root string) Event {
	return Event{Path: filepath.Join(root, "pkg_test.go"), Op: Write}
}

func TestLoopDebouncesBurstIntoOneRun(t *testing.T) {
	source := newFakeSource()
	stub := &checkStub{}
	loop := newTestLoop(t, source, stub.fn)
	ctx, cancel := context.WithCancel(context.Background())
	done := runLoop(ctx, loop)

	ev := testFileEvent(loop.Root)
	for i := 0; i < 5; i++ {
		source.events <- ev
		time.Sleep(testDebounce / 4)
	}

	require.Eventually(t, func() bool { return stub.callCount() == 1 }, time.Second, 5*time.Millisecond,
		"a burst of relevant events, all closer together than the debounce window, must coalesce into exactly one run")

	cancel()
	require.NoError(t, <-done)
	require.Equal(t, 1, stub.callCount(), "no extra run after shutdown")
}

func TestLoopIgnoresIrrelevantChanges(t *testing.T) {
	source := newFakeSource()
	stub := &checkStub{}
	loop := newTestLoop(t, source, stub.fn)
	ctx, cancel := context.WithCancel(context.Background())
	done := runLoop(ctx, loop)

	source.events <- Event{Path: filepath.Join(loop.Root, "README.md"), Op: Write}
	source.events <- Event{Path: filepath.Join(loop.Root, "node_modules", "pkg", "index.js"), Op: Write}
	time.Sleep(4 * testDebounce)

	require.Equal(t, 0, stub.callCount(), "an irrelevant file and a path under an ignored directory must never trigger a run")

	cancel()
	require.NoError(t, <-done)
}

func TestLoopCoalescesEventsDuringRun(t *testing.T) {
	source := newFakeSource()
	stub := &checkStub{
		block:   make(chan struct{}),
		started: make(chan struct{}, 4),
	}
	loop := newTestLoop(t, source, stub.fn)
	ctx, cancel := context.WithCancel(context.Background())
	done := runLoop(ctx, loop)

	ev := testFileEvent(loop.Root)
	source.events <- ev
	select {
	case <-stub.started:
	case <-time.After(time.Second):
		t.Fatal("first run never started")
	}
	require.Equal(t, 1, stub.callCount())

	// Three more relevant changes arrive while the single worker is
	// still busy with the first run, each debounce cycle apart, so
	// each one reaches the "start a run" decision on its own. A
	// single worker must coalesce all three into exactly one rerun,
	// not one rerun per event.
	for i := 0; i < 3; i++ {
		source.events <- ev
		time.Sleep(2 * testDebounce)
	}
	require.Equal(t, 1, stub.callCount(), "no second run starts while the worker is still busy")

	close(stub.block) // let the first run finish
	select {
	case <-stub.started:
	case <-time.After(time.Second):
		t.Fatal("coalesced rerun never started")
	}
	require.Equal(t, 2, stub.callCount(), "exactly one coalesced rerun, not one per event received while busy")

	cancel()
	require.NoError(t, <-done)
}

func TestLoopShutdownWithNoRunInFlight(t *testing.T) {
	source := newFakeSource()
	stub := &checkStub{}
	loop := newTestLoop(t, source, stub.fn)
	ctx, cancel := context.WithCancel(context.Background())
	done := runLoop(ctx, loop)

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Watch did not return promptly after cancellation with no run in flight")
	}
	require.True(t, source.Closed(), "the source must always be closed on shutdown")
	require.Equal(t, 0, stub.callCount())
}

func TestLoopShutdownCancelsInFlightRun(t *testing.T) {
	source := newFakeSource()
	stub := &checkStub{
		block:   make(chan struct{}), // never closed: the run only ends via ctx cancellation
		started: make(chan struct{}, 1),
	}
	loop := newTestLoop(t, source, stub.fn)
	ctx, cancel := context.WithCancel(context.Background())
	done := runLoop(ctx, loop)

	source.events <- testFileEvent(loop.Root)
	select {
	case <-stub.started:
	case <-time.After(time.Second):
		t.Fatal("run never started")
	}

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Watch did not return promptly; an in-flight run must be canceled, not awaited forever")
	}
	require.True(t, stub.wasCanceled(), "the in-flight run's context must be canceled on shutdown")
	require.True(t, source.Closed())
}

func TestLoopWatchesNewlyCreatedDirectory(t *testing.T) {
	source := newFakeSource()
	stub := &checkStub{}
	loop := newTestLoop(t, source, stub.fn)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runLoop(ctx, loop)

	sub := filepath.Join(loop.Root, "pkg")
	require.NoError(t, os.Mkdir(sub, 0o755))
	source.events <- Event{Path: sub, Op: Create}

	require.Eventually(t, func() bool {
		for _, dir := range source.Added() {
			if dir == sub {
				return true
			}
		}
		return false
	}, time.Second, 5*time.Millisecond, "a newly created directory must be added to the recursive watch")

	cancel()
	<-done
}

func TestLoopUnwatchesRemovedDirectory(t *testing.T) {
	source := newFakeSource()
	stub := &checkStub{}
	loop := newTestLoop(t, source, stub.fn)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runLoop(ctx, loop)

	sub := filepath.Join(loop.Root, "pkg")
	source.events <- Event{Path: sub, Op: Remove}

	require.Eventually(t, func() bool {
		for _, dir := range source.Removed() {
			if dir == sub {
				return true
			}
		}
		return false
	}, time.Second, 5*time.Millisecond, "a removed directory must be dropped from the recursive watch")

	cancel()
	<-done
}

func TestLoopIgnoresPathUnderIgnoredDirectory(t *testing.T) {
	source := newFakeSource()
	stub := &checkStub{}
	loop := newTestLoop(t, source, stub.fn)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runLoop(ctx, loop)

	sub := filepath.Join(loop.Root, ".git", "objects")
	source.events <- Event{Path: sub, Op: Create}
	time.Sleep(4 * testDebounce)

	// The initial walk always adds Root itself; what must never
	// appear is anything under the ignored .git directory.
	for _, dir := range source.Added() {
		require.NotContains(t, dir, ".git", ".git must never be descended into or added to the watch")
	}

	cancel()
	<-done
}

func TestLoopNotifiesOnlyWhenFindingsFire(t *testing.T) {
	tests := []struct {
		name       string
		notify     bool
		findings   []finding.Finding
		wantCalled bool
	}{
		{name: "notify off, findings present", notify: false, findings: []finding.Finding{{RuleID: "TI001"}}, wantCalled: false},
		{name: "notify on, no findings", notify: true, findings: nil, wantCalled: false},
		{name: "notify on, findings present", notify: true, findings: []finding.Finding{{RuleID: "TI001"}}, wantCalled: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := newFakeSource()
			stub := &checkStub{result: finding.Result{Findings: tt.findings}, started: make(chan struct{}, 1)}
			var mu sync.Mutex
			var notifyCalls int
			loop := newTestLoop(t, source, stub.fn)
			loop.Notify = tt.notify
			loop.Notifier = func(context.Context, string, string) error {
				mu.Lock()
				notifyCalls++
				mu.Unlock()
				return nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := runLoop(ctx, loop)

			source.events <- testFileEvent(loop.Root)
			select {
			case <-stub.started:
			case <-time.After(time.Second):
				t.Fatal("run never started")
			}
			require.Eventually(t, func() bool { return stub.callCount() == 1 }, time.Second, 5*time.Millisecond)
			time.Sleep(testDebounce) // let reportOutcome (and any notify call) run

			cancel()
			require.NoError(t, <-done)

			mu.Lock()
			got := notifyCalls
			mu.Unlock()
			if tt.wantCalled {
				require.Equal(t, 1, got)
			} else {
				require.Equal(t, 0, got)
			}
		})
	}
}
