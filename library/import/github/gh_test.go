// gh_test.go - Tests for the gh CLI wrapper: success, error shapes, retry and backoff
package github

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ghResponse is one canned reply from the faked gh process.
type ghResponse struct {
	stdout   string
	stderr   string
	exitCode int
}

// ghRecorder captures what the wrapper did: the commands it ran and the backoff
// it waited. Guarded by a mutex because user lookups run concurrently.
type ghRecorder struct {
	mu     sync.Mutex
	calls  [][]string
	delays []time.Duration
}

// record stores one gh invocation and returns how many have run so far.
func (r *ghRecorder) record(args []string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, args)
	return len(r.calls)
}

// callCount returns how many gh invocations were recorded.
func (r *ghRecorder) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// fakeGH swaps the exec and sleep seams for a scripted sequence of responses.
func fakeGH(t *testing.T, responses ...ghResponse) *ghRecorder {
	t.Helper()
	rec := &ghRecorder{}
	installGHSeams(t, rec, func(args []string, n int) ghResponse {
		if n > len(responses) {
			t.Errorf("gh ran %d times, only %d responses scripted", n, len(responses))
			return ghResponse{stderr: "unscripted call", exitCode: 1}
		}
		return responses[n-1]
	})
	return rec
}

// fakeGHRoutes swaps the exec seam for a router that answers each invocation by
// matching its arguments, so concurrent user lookups stay deterministic.
func fakeGHRoutes(t *testing.T, route func(args []string) ghResponse) *ghRecorder {
	t.Helper()
	rec := &ghRecorder{}
	installGHSeams(t, rec, func(args []string, _ int) ghResponse { return route(args) })
	return rec
}

// installGHSeams points execCommand and retrySleep at a recorder and a responder.
func installGHSeams(t *testing.T, rec *ghRecorder, respond func(args []string, n int) ghResponse) {
	t.Helper()
	origExec, origSleep := execCommand, retrySleep
	execCommand = func(name string, args ...string) *exec.Cmd {
		n := rec.record(append([]string{name}, args...))
		return scriptedCmd(respond(args, n))
	}
	retrySleep = func(d time.Duration) {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		rec.delays = append(rec.delays, d)
	}
	t.Cleanup(func() { execCommand, retrySleep = origExec, origSleep })
}

// scriptedCmd builds a shell command reproducing one canned gh response.
func scriptedCmd(r ghResponse) *exec.Cmd {
	script := fmt.Sprintf("printf %%s %s; printf %%s %s >&2; exit %d",
		shellQuote(r.stdout), shellQuote(r.stderr), r.exitCode)
	return exec.Command("/bin/sh", "-c", script)
}

// shellQuote single-quotes a string for safe embedding in a shell script.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func TestGH_Success(t *testing.T) {
	rec := fakeGH(t, ghResponse{stdout: `{"login":"octocat"}`})
	out, err := gh("api", "users/octocat")
	if err != nil {
		t.Fatalf("gh() error = %v", err)
	}
	if string(out) != `{"login":"octocat"}` {
		t.Errorf("output = %q", out)
	}
	if rec.callCount() != 1 {
		t.Fatalf("calls = %d, want 1", rec.callCount())
	}
	want := []string{"gh", "api", "users/octocat"}
	if strings.Join(rec.calls[0], " ") != strings.Join(want, " ") {
		t.Errorf("command = %v, want %v", rec.calls[0], want)
	}
	if len(rec.delays) != 0 {
		t.Errorf("delays = %v, want none", rec.delays)
	}
}

func TestGH_NonRetryableError(t *testing.T) {
	rec := fakeGH(t, ghResponse{stderr: "gh: Not Found (HTTP 404)", exitCode: 1})
	_, err := gh("api", "repos/acme/missing")
	if err == nil {
		t.Fatal("gh() error = nil, want failure")
	}
	if !strings.Contains(err.Error(), "gh api repos/acme/missing: gh: Not Found (HTTP 404)") {
		t.Errorf("error = %q", err)
	}
	if rec.callCount() != 1 {
		t.Errorf("calls = %d, want 1 (no retry for a non-retryable error)", rec.callCount())
	}
	if len(rec.delays) != 0 {
		t.Errorf("delays = %v, want none", rec.delays)
	}
}

func TestGH_RetriesThenSucceeds(t *testing.T) {
	rec := fakeGH(t,
		ghResponse{stderr: "API rate limit exceeded", exitCode: 1},
		ghResponse{stderr: "503 Service Unavailable", exitCode: 1},
		ghResponse{stdout: "[]"},
	)
	out, err := gh("api", "repos/acme/widgets/issues")
	if err != nil {
		t.Fatalf("gh() error = %v", err)
	}
	if string(out) != "[]" {
		t.Errorf("output = %q", out)
	}
	if rec.callCount() != 3 {
		t.Errorf("calls = %d, want 3", rec.callCount())
	}
	wantDelays := []time.Duration{time.Second, 2 * time.Second}
	if fmt.Sprint(rec.delays) != fmt.Sprint(wantDelays) {
		t.Errorf("delays = %v, want %v", rec.delays, wantDelays)
	}
}

func TestGH_ExhaustsRetries(t *testing.T) {
	limit := ghResponse{stderr: "You have exceeded a secondary rate limit", exitCode: 1}
	rec := fakeGH(t, limit, limit, limit, limit)
	_, err := gh("api", "graphql")
	if err == nil {
		t.Fatal("gh() error = nil, want failure")
	}
	if !strings.Contains(err.Error(), "secondary rate limit") {
		t.Errorf("error = %q, want the last stderr", err)
	}
	if rec.callCount() != 4 {
		t.Errorf("calls = %d, want 4 (initial + 3 retries)", rec.callCount())
	}
	wantDelays := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
	if fmt.Sprint(rec.delays) != fmt.Sprint(wantDelays) {
		t.Errorf("delays = %v, want %v", rec.delays, wantDelays)
	}
}

func TestGH_LaunchFailureIsNotRetried(t *testing.T) {
	origExec, origSleep := execCommand, retrySleep
	missing := filepath.Join(t.TempDir(), "no-such-binary")
	calls := 0
	execCommand = func(string, ...string) *exec.Cmd {
		calls++
		return exec.Command(missing)
	}
	retrySleep = func(time.Duration) { t.Error("retrySleep called for a launch failure") }
	t.Cleanup(func() { execCommand, retrySleep = origExec, origSleep })
	_, err := gh("auth", "status")
	if err == nil {
		t.Fatal("gh() error = nil, want failure")
	}
	if !strings.Contains(err.Error(), "gh auth status") {
		t.Errorf("error = %q", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestGHJSON(t *testing.T) {
	t.Run("decodes output", func(t *testing.T) {
		fakeGH(t, ghResponse{stdout: `{"name":"Octo Cat","email":"octo@example.com"}`})
		var user struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		}
		if err := ghJSON(&user, "api", "users/octocat"); err != nil {
			t.Fatalf("ghJSON() error = %v", err)
		}
		if user.Name != "Octo Cat" || user.Email != "octo@example.com" {
			t.Errorf("user = %+v", user)
		}
	})

	t.Run("empty output leaves dst untouched", func(t *testing.T) {
		fakeGH(t, ghResponse{})
		user := struct {
			Name string `json:"name"`
		}{Name: "unchanged"}
		if err := ghJSON(&user, "api", "users/ghost"); err != nil {
			t.Fatalf("ghJSON() error = %v", err)
		}
		if user.Name != "unchanged" {
			t.Errorf("Name = %q, want unchanged", user.Name)
		}
	})

	t.Run("propagates gh failure", func(t *testing.T) {
		fakeGH(t, ghResponse{stderr: "gh: Bad credentials", exitCode: 1})
		var user struct{}
		if err := ghJSON(&user, "api", "users/octocat"); err == nil {
			t.Fatal("ghJSON() error = nil, want failure")
		}
	})

	t.Run("reports malformed JSON", func(t *testing.T) {
		fakeGH(t, ghResponse{stdout: "not json"})
		var user struct{}
		if err := ghJSON(&user, "api", "users/octocat"); err == nil {
			t.Fatal("ghJSON() error = nil, want failure")
		}
	})
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   bool
	}{
		{"rate limit", "API rate limit exceeded for user", true},
		{"rate limit uppercase", "API Rate Limit exceeded", true},
		{"secondary rate", "You have exceeded a secondary rate limit", true},
		{"abuse detection", "You have triggered an abuse detection mechanism", true},
		{"bad gateway", "HTTP 502: Bad Gateway", true},
		{"service unavailable", "HTTP 503: Service Unavailable", true},
		{"not found", "gh: Not Found (HTTP 404)", false},
		{"unauthorized", "gh: Bad credentials (HTTP 401)", false},
		{"empty", "", false},
		{"unrelated", "fatal: repository not found", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableError(tt.stderr); got != tt.want {
				t.Errorf("isRetryableError(%q) = %v, want %v", tt.stderr, got, tt.want)
			}
		})
	}
}
