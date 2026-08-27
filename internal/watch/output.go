package watch

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/alainrk/paranoid/internal/finding"
	"github.com/alainrk/paranoid/internal/report"
)

// timeFormat is the clock format every printed line starts with.
const timeFormat = "15:04:05"

// printEvent writes one compact line for a relevant filesystem event.
func (l *Loop) printEvent(ev Event) {
	_, _ = fmt.Fprintf(l.out(), "%s  %-6s %s\n", l.now().Format(timeFormat), opLabel(ev.Op), l.relPath(ev.Path))
}

// opLabel picks one short word for an event, in priority order when
// fsnotify reports more than one bit set on the same event.
func opLabel(op Op) string {
	switch {
	case op.Has(Remove):
		return "remove"
	case op.Has(Rename):
		return "rename"
	case op.Has(Create):
		return "create"
	case op.Has(Write):
		return "write"
	case op.Has(Chmod):
		return "chmod"
	default:
		return "change"
	}
}

// relPath renders a path relative to Root when possible, for a
// shorter and more readable compact line.
func (l *Loop) relPath(p string) string {
	rel, err := filepath.Rel(l.Root, p)
	if err != nil {
		return p
	}
	return rel
}

// reportOutcome prints one finished (or failed) check run. A run with
// no findings gets one compact status line; a run with findings gets
// that same status line followed by the full report, so nothing is
// hidden behind a summary count when something actually fired.
func (l *Loop) reportOutcome(out runOutcome) {
	ts := l.now().Format(timeFormat)
	if out.err != nil {
		_, _ = fmt.Fprintf(l.errOut(), "%s  check failed: %v\n", ts, out.err)
		return
	}
	dur := out.dur.Round(time.Millisecond)
	if len(out.res.Findings) == 0 {
		_, _ = fmt.Fprintf(l.out(), "%s  ok  score %d  verdict %s  0 findings  (%s)\n", ts, out.res.Score, out.res.Verdict, dur)
		return
	}
	_, _ = fmt.Fprintf(l.out(), "%s  %d %s  score %d  verdict %s  (%s)\n",
		ts, len(out.res.Findings), countNoun(len(out.res.Findings)), out.res.Score, out.res.Verdict, dur)
	_, _ = fmt.Fprint(l.out(), report.RenderText(out.res, false))
	if l.Notify {
		l.notify(out.res)
	}
}

// countNoun picks "finding" or "findings".
func countNoun(n int) string {
	if n == 1 {
		return "finding"
	}
	return "findings"
}

// notify sends a desktop notification for a run that produced
// findings. A failure to notify is logged, never fatal: it is a
// display nicety, not a pipeline result.
func (l *Loop) notify(res finding.Result) {
	ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
	defer cancel()
	notifier := l.Notifier
	if notifier == nil {
		notifier = Send
	}
	title := "paranoid watch"
	message := fmt.Sprintf("%d %s, score %d, verdict %s", len(res.Findings), countNoun(len(res.Findings)), res.Score, res.Verdict)
	if err := notifier(ctx, title, message); err != nil {
		_, _ = fmt.Fprintf(l.errOut(), "%s  notify failed: %v\n", l.now().Format(timeFormat), err)
	}
}
