// progress_test.go - throttle + formatter + hook-plumbing tests.
package objstore

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock is a manually-advanced clock for the throttle tests (no sleeps).
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time      { return c.t }
func (c *fakeClock) add(d time.Duration) { c.t = c.t.Add(d) }

// TestThrottle_FirstAndIntervalAndTerminal: the first call fires, calls within
// the interval are suppressed, a call past the interval fires again, and a
// terminal call (done == total) always fires regardless of timing.
func TestThrottle_FirstAndIntervalAndTerminal(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	thr := newThrottle(time.Second)
	thr.now = clk.now

	if !thr.ready(1, 100) {
		t.Fatal("first call must fire")
	}
	if thr.ready(2, 100) {
		t.Fatal("call within interval must be suppressed")
	}
	clk.add(1100 * time.Millisecond)
	if !thr.ready(3, 100) {
		t.Fatal("call past interval must fire")
	}
	// A terminal call fires even though the interval hasn't elapsed.
	if !thr.ready(100, 100) {
		t.Fatal("terminal call must always fire")
	}
}

// TestThrottle_UnknownTotalNeverTerminal: with total == 0 (unknown), no call is
// treated as terminal, so only the interval gates emissions.
func TestThrottle_UnknownTotalNeverTerminal(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	thr := newThrottle(time.Second)
	thr.now = clk.now
	if !thr.ready(1, 0) {
		t.Fatal("first call must fire")
	}
	if thr.ready(9999, 0) {
		t.Fatal("unknown-total call within interval must not fire (never terminal)")
	}
}

// TestFormatProgress covers known and unknown totals.
func TestFormatProgress(t *testing.T) {
	cases := []struct {
		phase     string
		done, tot int
		want      string
	}{
		{"objects", 12000, 43983, "objects: 12000/43983 (27%)"},
		{"site index pm", 500, 9312, "site index pm: 500/9312 (5%)"},
		{"site shards pm", 3, 7, "site shards pm: 3/7 (42%)"},
		{"site index pm", 500, 0, "site index pm: 500"},
	}
	for _, c := range cases {
		if got := formatProgress(c.phase, c.done, c.tot); got != c.want {
			t.Errorf("formatProgress(%q,%d,%d) = %q, want %q", c.phase, c.done, c.tot, got, c.want)
		}
	}
}

// TestProgressWriter_TTYRewritesLine: on a TTY, updates rewrite one line via
// carriage return, and the terminal call closes it with a newline.
func TestProgressWriter_TTYRewritesLine(t *testing.T) {
	var buf strings.Builder
	pw := newProgressWriter(&buf, true)
	// Force every call through by shrinking the interval to zero.
	pw.thr.interval = 0
	p := pw.Progress()
	p("objects", 1, 3)
	p("objects", 3, 3) // terminal
	out := buf.String()
	if !strings.Contains(out, "\r") {
		t.Errorf("TTY output must use carriage returns, got %q", out)
	}
	if !strings.HasSuffix(out, "objects: 3/3 (100%)\n") {
		t.Errorf("TTY terminal line must end with a newline, got %q", out)
	}
	if strings.Count(out, "\n") != 1 {
		t.Errorf("TTY must emit exactly one newline (only the terminal), got %q", out)
	}
}

// TestProgressWriter_PipeNewlineLines: off a TTY, each emission is a full
// newline-terminated line and no carriage returns are used.
func TestProgressWriter_PipeNewlineLines(t *testing.T) {
	var buf strings.Builder
	pw := newProgressWriter(&buf, false)
	pw.thr.interval = 0
	p := pw.Progress()
	p("objects", 1, 3)
	p("objects", 3, 3)
	out := buf.String()
	if strings.Contains(out, "\r") {
		t.Errorf("non-TTY output must not use carriage returns, got %q", out)
	}
	if out != "objects: 1/3 (33%)\nobjects: 3/3 (100%)\n" {
		t.Errorf("non-TTY output = %q", out)
	}
}

// TestProgressWriter_ThrottleDropsMiddle: a throttled writer emits the first and
// the terminal call but drops the middle ones taken within the interval.
func TestProgressWriter_ThrottleDropsMiddle(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	var buf strings.Builder
	pw := newProgressWriter(&buf, false)
	pw.thr.now = clk.now
	p := pw.Progress()
	p("objects", 1, 100)   // first — fires
	p("objects", 2, 100)   // within interval — dropped
	p("objects", 3, 100)   // within interval — dropped
	p("objects", 100, 100) // terminal — fires
	got := buf.String()
	if strings.Contains(got, "2/100") || strings.Contains(got, "3/100") {
		t.Errorf("middle updates must be throttled out, got %q", got)
	}
	if !strings.Contains(got, "1/100") || !strings.Contains(got, "100/100") {
		t.Errorf("first and terminal must survive, got %q", got)
	}
}

// TestNilProgress_NilSafe: the nil-safe helpers never panic on a nil hook.
func TestNilProgress_NilSafe(t *testing.T) {
	var p Progress
	p.call("phase", 1, 2) // must not panic
	var sp *siteProgress
	sp.walk(5, 0) // must not panic
	sp.shards("items", 1, 2)
	var pw *progressWriter
	if pw.Progress() != nil {
		t.Error("nil progressWriter.Progress() must be nil")
	}
	pw.finish() // must not panic
}

// TestUploadEncodedObjects_ProgressFires: the object-upload pool reports each
// landed object through the hook, ending at done == total.
func TestUploadEncodedObjects_ProgressFires(t *testing.T) {
	client, _ := testClient(t)
	const n = 250
	produce, _ := feedObjects(n)
	var maxDone int64
	var calls int64
	hook := func(phase string, done, total int) {
		atomic.AddInt64(&calls, 1)
		if phase != "objects" {
			t.Errorf("phase = %q, want objects", phase)
		}
		if total != n {
			t.Errorf("total = %d, want %d", total, n)
		}
		for {
			cur := atomic.LoadInt64(&maxDone)
			if int64(done) <= cur || atomic.CompareAndSwapInt64(&maxDone, cur, int64(done)) {
				break
			}
		}
	}
	if err := uploadEncodedObjects(client, "repo/", 8, n, hook, produce); err != nil {
		t.Fatalf("uploadEncodedObjects: %v", err)
	}
	if atomic.LoadInt64(&calls) == 0 {
		t.Fatal("progress hook never fired")
	}
	if got := atomic.LoadInt64(&maxDone); got != n {
		t.Errorf("final progress done = %d, want %d", got, n)
	}
}

// TestUpdateSiteItemsIndex_ProgressPhases: a bootstrap over a seeded commit
// chain fires both site-maintenance phases with sane, labeled counts — the
// bounded walk ("site index <ext>") and the per-corpus shard uploads ("site
// items shards <ext>" / "site bodies shards <ext>").
func TestUpdateSiteItemsIndex_ProgressPhases(t *testing.T) {
	withTestShardCount(func() {
		client, _ := testClient(t)
		const n = 13
		shas := seedChain(t, client, "", "", n)
		tip := shas[n-1]

		var walkMax, shardsSeen int
		corpora := map[string]bool{}
		hook := func(phase string, done, total int) {
			switch {
			case strings.HasPrefix(phase, "site index social"):
				if done > walkMax {
					walkMax = done
				}
			case strings.HasPrefix(phase, "site items shards social"):
				corpora["items"] = true
				shardsSeen++
				if done < 1 || (total > 0 && done > total) {
					t.Errorf("shard progress out of range: %d/%d", done, total)
				}
			case strings.HasPrefix(phase, "site bodies shards social"):
				corpora["bodies"] = true
				shardsSeen++
				if done < 1 || (total > 0 && done > total) {
					t.Errorf("shard progress out of range: %d/%d", done, total)
				}
			default:
				t.Errorf("unexpected phase %q", phase)
			}
		}
		sp := &siteProgress{progress: hook, ext: "social"}
		if err := updateSiteItemsIndex(client, "", "social", tip, sp); err != nil {
			t.Fatalf("updateSiteItemsIndex: %v", err)
		}
		if walkMax != n {
			t.Errorf("walk progress reached %d, want %d (every commit walked once)", walkMax, n)
		}
		if shardsSeen == 0 {
			t.Error("shard-upload progress never fired")
		}
		if !corpora["items"] || !corpora["bodies"] {
			t.Errorf("both corpora must report shard progress under distinct labels, saw %v", corpora)
		}
	})
}
