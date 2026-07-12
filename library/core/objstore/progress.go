// progress.go - progress reporting for long s3 push/site operations.
//
// Long s3 operations (initial object upload, the site item-index walk + shard
// uploads) run for many minutes with no output. A small Progress hook threads
// through the objstore entry points that need it so callers can render live
// progress without objstore owning any I/O policy. nil = silent; there is no
// global state.
package objstore

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Progress reports a phase's advance. total is 0 when unknown. Within a phase
// the calls are serialized (a single walk/producer goroutine, or, for the
// concurrent object-upload and ref-read pools, a mutex guarding the emit), so
// implementations need not be re-entrant across phases; the stderr renderer
// guards its own writes regardless.
type Progress func(phase string, done, total int)

// throttle rate-limits progress emissions: it fires at most once per interval,
// and always fires the terminal call (done == total, total > 0) so the final
// count is never dropped. Counter-based batching is layered on top by the
// caller (emit every N items) so this stays purely time-based and testable
// with an injectable clock.
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

// ready reports whether an emission should fire now. The first call always
// fires; a terminal call (done == total with a known total) always fires so
// the completion line lands; otherwise it fires only once the interval has
// elapsed since the last fire.
func (t *throttle) ready(done, total int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	terminal := total > 0 && done >= total
	if !t.started || terminal || now.Sub(t.last) >= t.interval {
		t.started = true
		t.last = now
		return true
	}
	return false
}

// progressWriter renders Progress calls as human-readable lines on an
// io.Writer (the helper's stderr, or the CLI's). On a TTY it rewrites a single
// line in place with a carriage return; otherwise it emits newline-terminated
// lines at a much lower rate so non-interactive logs (CI) don't explode. It is
// safe for concurrent phase updates.
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

// pipeProgressInterval throttles non-interactive newline lines (~10s) so CI
// logs stay small.
const pipeProgressInterval = 10 * time.Second

// newProgressWriter builds a progressWriter. tty selects carriage-return
// single-line updates (~1s) vs newline lines at a much lower rate (~10s).
func newProgressWriter(w io.Writer, tty bool) *progressWriter {
	interval := pipeProgressInterval
	if tty {
		interval = ttyProgressInterval
	}
	return &progressWriter{w: w, tty: tty, thr: newThrottle(interval)}
}

// StderrProgress returns a Progress hook that renders throttled progress to
// stderr, plus a done() to call when all phases finish (closes any pending
// in-place TTY line). It uses the same TTY-vs-pipe policy as the git-spawned
// helper, so `gitsocial site push` and a plain `git push` show identical
// progress. The hook is nil-safe to call after done().
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

// report renders one progress update, honoring the throttle. The terminal call
// of a phase (done == total, total known) always renders so the final count
// shows, and on a TTY it closes the in-place line with a newline so the next
// phase starts clean.
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

// finish closes any pending in-place TTY line with a newline. A no-op off a
// TTY or when nothing is pending. Call once when all phases are done.
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

// formatProgress renders one progress line: "<phase>: <done>/<total> (NN%)" when
// the total is known, or "<phase>: <done>" when it is unknown (total == 0).
func formatProgress(phase string, done, total int) string {
	if total <= 0 {
		return fmt.Sprintf("%s: %d", phase, done)
	}
	pct := done * 100 / total
	return fmt.Sprintf("%s: %d/%d (%d%%)", phase, done, total, pct)
}

// call invokes a Progress hook if non-nil (nil-safe convenience for the
// threaded call sites).
func (p Progress) call(phase string, done, total int) {
	if p != nil {
		p(phase, done, total)
	}
}

// siteProgress carries the per-extension pass context threaded through the site
// item-index build: the progress hook, the extension being maintained (so both
// phases — the bounded commit walk and the shard uploads — report under a stable
// "<phase> <ext>" label), and an optional local commit source (the walk reads
// commits from the local odb when the pusher has the repo, falling back to the
// bucket per missing object; nil = bucket-only). A nil *siteProgress is silent
// and bucket-only; a nil hook or nil src inside one is fine.
type siteProgress struct {
	progress Progress
	ext      string
	src      *localCommitSource
}

// commitSource returns the pass's local commit source (nil-safe: a nil
// *siteProgress or an unset source yields nil, which getCommit treats as
// bucket-only).
func (sp *siteProgress) commitSource() *localCommitSource {
	if sp == nil {
		return nil
	}
	return sp.src
}

// walk reports commit-walk progress ("site index <ext>: <done>[/<total> (NN%)]").
// total is the honest ceiling on this walk only when the true remaining size is
// knowable; a percentage is NEVER shown against the walk budget, which is a
// per-push cap the walk usually won't reach (a 6k-commit branch against a 50k
// budget must not read "12%"). Every current caller passes 0 (plain count): the
// bootstrap/backfill budget is a cap, the append gap is unbounded, and neither
// the manifest nor the cursor tracks a real remaining count.
func (sp *siteProgress) walk(done, total int) {
	if sp != nil {
		sp.progress.call("site index "+sp.ext, done, total)
	}
}

// shards reports shard-upload progress ("site <corpus> shards <ext>: <done>/
// <total>"). corpus ("items" / "bodies") disambiguates the two corpora, which
// seal the same shard count in one pass: without it a single push printed the
// identical "site shards <ext>: N/N" twice, reading like duplicate work when it
// is just the two corpora advancing in lockstep.
func (sp *siteProgress) shards(corpus string, done, total int) {
	if sp != nil {
		sp.progress.call("site "+corpus+" shards "+sp.ext, done, total)
	}
}

// stderrIsTTY reports whether stderr is a character device (a terminal),
// stdlib-only so core stays free of an isatty dependency. This is the same
// signal git uses to decide between in-place progress and quiet output.
func stderrIsTTY() bool {
	info, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
