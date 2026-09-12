// progress.go - progress reporting for the long push and site operations
package objstore

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Progress reports a phase's advance, with total 0 when unknown; calls within a phase are serialized, so an implementation need not be re-entrant.
type Progress func(phase string, done, total int)

// throttle rate-limits progress emissions to once per interval, and fires the terminal call regardless, so the final count lands.
type throttle struct {
	mu       sync.Mutex
	interval time.Duration
	now      func() time.Time // injectable for tests
	last     time.Time
	started  bool
}

// newThrottle builds a throttle firing at most once per interval.
func newThrottle(interval time.Duration) *throttle {
	return &throttle{interval: interval, now: time.Now}
}

// ready reports whether an emission should fire now: the first call, a terminal call, or once the interval has elapsed.
func (t *throttle) ready(done, total int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	terminal := total > 0 && done >= total
	// A phase marker names a step that has started, so the interval must not drop it.
	if isPhaseMarker(done, total) || !t.started || terminal || now.Sub(t.last) >= t.interval {
		t.started = true
		t.last = now
		return true
	}
	return false
}

// progressWriter renders Progress calls as lines on an io.Writer: a single in-place line on a TTY, slower newline lines otherwise.
type progressWriter struct {
	mu       sync.Mutex
	w        io.Writer
	tty      bool
	thr      *throttle
	lastLine string // last phase line rendered (for a clean final newline on TTY)
	dirty    bool   // a carriage-return line is pending its closing newline
}

// ttyProgressInterval throttles interactive single-line refreshes (~1s).
const ttyProgressInterval = time.Second

// pipeProgressInterval throttles non-interactive newline lines, so piped logs stay small.
const pipeProgressInterval = 10 * time.Second

// newProgressWriter builds a progressWriter; tty selects in-place single-line updates over newline lines.
func newProgressWriter(w io.Writer, tty bool) *progressWriter {
	interval := pipeProgressInterval
	if tty {
		interval = ttyProgressInterval
	}
	return &progressWriter{w: w, tty: tty, thr: newThrottle(interval)}
}

// StderrProgress returns a throttled stderr Progress hook plus the function that closes its pending TTY line, on the same policy the git-spawned helper uses.
func StderrProgress() (Progress, func()) {
	pw := newProgressWriter(os.Stderr, stderrIsTTY())
	return pw.Progress(), pw.finish
}

// Progress returns the hook bound to this writer, or nil for a nil writer.
func (p *progressWriter) Progress() Progress {
	if p == nil {
		return nil
	}
	return p.report
}

// report renders one progress update, honoring the throttle; a phase's terminal call closes the in-place TTY line.
func (p *progressWriter) report(phase string, done, total int) {
	terminal := total > 0 && done >= total
	if !p.thr.ready(done, total) {
		return
	}
	line := formatProgress(phase, done, total)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tty {
		fmt.Fprintf(p.w, "\r\033[K%s", line)
		p.lastLine = line
		p.dirty = true
		if terminal {
			fmt.Fprint(p.w, "\n")
			p.dirty = false
		}
		return
	}
	fmt.Fprintln(p.w, line)
}

// finish closes any pending in-place TTY line; call it once when all phases are done.
func (p *progressWriter) finish() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tty && p.dirty {
		fmt.Fprint(p.w, "\n")
		p.dirty = false
	}
}

// formatProgress renders one progress line, with a percentage when the total is known.
func formatProgress(phase string, done, total int) string {
	if isPhaseMarker(done, total) {
		return phase
	}
	if total <= 0 {
		return fmt.Sprintf("%s: %d", phase, done)
	}
	pct := done * 100 / total
	return fmt.Sprintf("%s: %d/%d (%d%%)", phase, done, total, pct)
}

// call invokes a Progress hook when it is non-nil.
func (p Progress) call(phase string, done, total int) {
	if p != nil {
		p(phase, done, total)
	}
}

// stderrIsTTY reports whether stderr is a character device, the same signal git reads to choose its progress shape.
func stderrIsTTY() bool {
	info, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// isPhaseMarker reports whether a call names a step but counts nothing, so it renders as the phase alone.
func isPhaseMarker(done, total int) bool {
	return done == 0 && total == 0
}
