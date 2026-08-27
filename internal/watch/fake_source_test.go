package watch

import "sync"

// fakeSource is a test-only Source with channels the test drives
// directly, so the debounce and shutdown behavior in Watch can be
// exercised deterministically without a real filesystem watcher. Add
// and Remove are recorded, not acted on, so a test can assert on
// directory add/remove handling without touching a real fsnotify
// watcher. All bookkeeping is behind a mutex: Watch's own goroutine
// calls Add/Remove/Close while the test goroutine reads them back.
type fakeSource struct {
	events chan Event
	errs   chan error

	mu      sync.Mutex
	added   []string
	removed []string
	closed  bool
}

func newFakeSource() *fakeSource {
	return &fakeSource{
		events: make(chan Event, 32),
		errs:   make(chan error, 4),
	}
}

func (f *fakeSource) Events() <-chan Event { return f.events }
func (f *fakeSource) Errors() <-chan error { return f.errs }

func (f *fakeSource) Add(dir string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.added = append(f.added, dir)
	return nil
}

func (f *fakeSource) Remove(dir string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, dir)
	return nil
}

func (f *fakeSource) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	close(f.events)
	close(f.errs)
	return nil
}

// Added returns a snapshot of every directory Add was called with.
func (f *fakeSource) Added() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.added...)
}

// Removed returns a snapshot of every directory Remove was called with.
func (f *fakeSource) Removed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.removed...)
}

// Closed reports whether Close has been called.
func (f *fakeSource) Closed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}
