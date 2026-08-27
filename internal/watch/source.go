// Package watch implements paranoid's watch mode: a recursive
// filesystem watch over a repository that debounces changes and runs
// a static-only check pipeline on each settled batch.
package watch

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// Op is one kind of filesystem event, mirroring fsnotify's own
// bitmask so callers never need to import fsnotify directly. Keeping
// a package-local type, instead of aliasing fsnotify.Op, is what lets
// Source be implemented by a fake in tests with no real filesystem
// involved.
type Op uint32

// The event kinds a Source can report. Values match fsnotify.Op
// bit for bit, which fsnotifySource relies on (see its Events method).
const (
	Create Op = 1 << iota
	Write
	Remove
	Rename
	Chmod
)

// Has reports whether op includes the bit for kind.
func (op Op) Has(kind Op) bool { return op&kind != 0 }

// Event is one filesystem change.
type Event struct {
	Path string
	Op   Op
}

// Source abstracts a recursive filesystem watcher. The real
// implementation wraps fsnotify.Watcher, which only watches
// individual directories, not subtrees; a fake implementation in
// tests can synthesize events with no real filesystem at all, which
// is why the loop is tested against this interface instead of a real
// fsnotify.Watcher.
type Source interface {
	// Events returns the channel of filesystem events. Closed when
	// the source is closed.
	Events() <-chan Event
	// Errors returns the channel of watcher errors. Closed when the
	// source is closed.
	Errors() <-chan error
	// Add starts watching one directory (non-recursively; the caller
	// walks a tree and calls Add per directory).
	Add(dir string) error
	// Remove stops watching one directory. Removing a directory that
	// is not watched is not an error.
	Remove(dir string) error
	// Close releases the source. Events and Errors are closed once
	// Close returns.
	Close() error
}

// fsnotifySource adapts *fsnotify.Watcher to Source.
type fsnotifySource struct {
	w         *fsnotify.Watcher
	events    chan Event
	errs      chan error
	stop      chan struct{}
	closeOnce sync.Once
}

// NewFsnotifySource opens a real fsnotify watcher and starts the
// goroutine that translates its events into Source's own types. This
// is the only constructor in this package that touches a real
// filesystem watcher; the cli package's watch command uses it, and
// every test in this package uses a fake Source instead.
func NewFsnotifySource() (Source, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("opening a filesystem watcher: %w", err)
	}
	s := &fsnotifySource{
		w:      w,
		events: make(chan Event),
		errs:   make(chan error),
		stop:   make(chan struct{}),
	}
	go s.pump()
	return s, nil
}

// pump translates fsnotify's own channels into Source's channels
// until fsnotify closes them (which Close triggers), then closes
// Source's channels in turn. Every forwarding send also selects on
// stop, so a Close call that races a caller who has already stopped
// reading Events/Errors still makes this goroutine exit, instead of
// parking forever on a send nobody will ever receive.
func (s *fsnotifySource) pump() {
	defer close(s.events)
	defer close(s.errs)
	for {
		select {
		case ev, ok := <-s.w.Events:
			if !ok {
				return
			}
			select {
			case s.events <- Event{Path: ev.Name, Op: Op(ev.Op)}:
			case <-s.stop:
				return
			}
		case err, ok := <-s.w.Errors:
			if !ok {
				return
			}
			select {
			case s.errs <- err:
			case <-s.stop:
				return
			}
		case <-s.stop:
			return
		}
	}
}

func (s *fsnotifySource) Events() <-chan Event { return s.events }
func (s *fsnotifySource) Errors() <-chan error { return s.errs }

func (s *fsnotifySource) Add(dir string) error {
	if err := s.w.Add(dir); err != nil {
		return fmt.Errorf("watching %s: %w", dir, err)
	}
	return nil
}

func (s *fsnotifySource) Remove(dir string) error {
	// fsnotify errors when asked to remove a path it never watched
	// (for example, a directory the ignore rules already skipped);
	// that is not this caller's problem, so it is swallowed here.
	_ = s.w.Remove(dir)
	return nil
}

// Close closes the underlying watcher and signals pump to stop, so it
// exits even if it was mid-send to Events or Errors with nobody left
// reading. Close is safe to call more than once.
func (s *fsnotifySource) Close() error {
	s.closeOnce.Do(func() { close(s.stop) })
	if err := s.w.Close(); err != nil {
		return fmt.Errorf("closing the filesystem watcher: %w", err)
	}
	return nil
}

// WalkAndAdd recursively adds every directory under root to source,
// skipping directories Ignored rejects (and never descending into
// them). It is exported so both the real command and tests that want
// real directory recursion (without a real fsnotify.Watcher) can
// reuse it.
func WalkAndAdd(source Source, root string) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// A directory that vanished between the walk starting and
			// reaching it (a build tool's temp dir, for example) is not
			// fatal; skip it and keep walking.
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if Ignored(p) {
			return filepath.SkipDir
		}
		return source.Add(p)
	})
}
