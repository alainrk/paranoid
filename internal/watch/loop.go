package watch

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/alainrk/paranoid/internal/finding"
)

// DefaultDebounce is how long the loop waits after the last relevant
// filesystem event before it runs the check pipeline. A burst of
// several saves from an editor, or a git checkout touching many
// files, coalesces into one run instead of one run per file.
const DefaultDebounce = 500 * time.Millisecond

// CheckFunc runs the static-only pipeline for one debounced batch of
// changes and returns the resulting report. The cli package supplies
// the real implementation (test integrity plus DEP005, base fixed at
// startup, head the worktree); tests supply a stub.
type CheckFunc func(ctx context.Context) (finding.Result, error)

// Loop watches a directory tree and runs Check once per settled batch
// of relevant changes. It is built around the Source interface, not a
// concrete fsnotify.Watcher, so its debounce, coalescing, and
// shutdown behavior can be tested with a fake event source and no
// real filesystem. The lifecycle invariants this type implements: a
// single worker (never two checks running at once), coalesced events
// (a burst becomes one run, and a change that arrives mid-run
// schedules exactly one more, not a growing queue), and a clean
// shutdown that lets an in-flight run finish or cancels it, never
// leaving it orphaned.
type Loop struct {
	// Root is the directory the recursive watch starts from.
	Root string
	// Source is the event source; the cli package wires in a real
	// fsnotify-backed one, tests wire in a fake.
	Source Source
	// Debounce overrides DefaultDebounce when positive.
	Debounce time.Duration
	// Check runs the pipeline. Required.
	Check CheckFunc
	// Notify, when true, sends a desktop notification whenever a run
	// produces findings.
	Notify bool
	// Notifier overrides Send for tests; nil means Send.
	Notifier Notifier
	// Out and ErrOut default to os.Stdout and os.Stderr.
	Out    io.Writer
	ErrOut io.Writer
	// Now overrides time.Now for the timestamps printed on each line;
	// nil means time.Now.
	Now func() time.Time
}

// runOutcome is one completed (or failed) run of Check, timed.
type runOutcome struct {
	res finding.Result
	err error
	dur time.Duration
}

// Watch performs the initial recursive watch setup, then runs the
// event loop until ctx is canceled or the source closes on its own.
// It always closes Source before returning. A canceled context is a
// clean shutdown (nil error); an unexpected source closure is not.
func (l *Loop) Watch(ctx context.Context) error {
	if err := WalkAndAdd(l.Source, l.Root); err != nil {
		_ = l.Source.Close()
		return fmt.Errorf("setting up the recursive watch under %s: %w", l.Root, err)
	}
	defer func() { _ = l.Source.Close() }()

	var timer *time.Timer
	var timerC <-chan time.Time
	resetTimer := func() {
		d := l.debounce()
		if timer == nil {
			timer = time.NewTimer(d)
		} else {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(d)
		}
		timerC = timer.C
	}
	stopTimer := func() {
		if timer != nil {
			timer.Stop()
		}
	}

	var (
		busy   bool
		rerun  bool
		cancel context.CancelFunc
	)
	outcomes := make(chan runOutcome, 1)

	startRun := func() {
		busy = true
		var runCtx context.Context
		runCtx, cancel = context.WithCancel(ctx) // #nosec G118 -- cancel is called by every select branch below, gosec cannot see through the outer variable
		check := l.Check
		start := l.now()
		go func() {
			res, err := check(runCtx)
			outcomes <- runOutcome{res: res, err: err, dur: l.now().Sub(start)}
		}()
	}
	trigger := func() {
		if busy {
			// A single worker only ever runs one check at a time; a
			// change that lands while one is in flight is coalesced
			// into exactly one more run once it finishes, never a
			// growing queue.
			rerun = true
			return
		}
		startRun()
	}

	for {
		select {
		case <-ctx.Done():
			stopTimer()
			if cancel != nil {
				cancel()
			}
			if busy {
				<-outcomes // let the in-flight run finish or observe the cancel
			}
			return nil

		case ev, ok := <-l.Source.Events():
			if !ok {
				if busy {
					<-outcomes
				}
				if cancel != nil {
					cancel()
				}
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("the filesystem watcher closed unexpectedly")
			}
			l.handleEvent(ev, resetTimer)

		case err, ok := <-l.Source.Errors():
			if !ok {
				continue
			}
			_, _ = fmt.Fprintf(l.errOut(), "%s  watch error: %v\n", l.now().Format(timeFormat), err)

		case <-timerC:
			timerC = nil
			trigger()

		case out := <-outcomes:
			busy = false
			cancel()
			cancel = nil
			l.reportOutcome(out)
			if rerun {
				rerun = false
				startRun()
			}
		}
	}
}

// handleEvent classifies one filesystem event: a new directory is
// added to the recursive watch, a removed or renamed path is dropped
// from it (a no-op if it was never watched, which is the common case
// for an ordinary file), and only a path Relevant selects prints a
// line and resets the debounce timer.
func (l *Loop) handleEvent(ev Event, resetTimer func()) {
	if Ignored(ev.Path) {
		return
	}
	if ev.Op.Has(Create) {
		if info, err := os.Stat(ev.Path); err == nil && info.IsDir() {
			if err := WalkAndAdd(l.Source, ev.Path); err != nil {
				_, _ = fmt.Fprintf(l.errOut(), "%s  watch error: adding %s: %v\n", l.now().Format(timeFormat), ev.Path, err)
			}
			return
		}
	}
	if ev.Op.Has(Remove) || ev.Op.Has(Rename) {
		_ = l.Source.Remove(ev.Path)
	}
	if !Relevant(ev.Path) {
		return
	}
	l.printEvent(ev)
	resetTimer()
}

func (l *Loop) debounce() time.Duration {
	if l.Debounce > 0 {
		return l.Debounce
	}
	return DefaultDebounce
}

func (l *Loop) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

func (l *Loop) out() io.Writer {
	if l.Out != nil {
		return l.Out
	}
	return os.Stdout
}

func (l *Loop) errOut() io.Writer {
	if l.ErrOut != nil {
		return l.ErrOut
	}
	return os.Stderr
}
