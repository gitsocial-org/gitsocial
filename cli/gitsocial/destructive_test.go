// destructive_test.go - The destructive CLI verbs through the binary: create, refuse a wrong target, act, read the state back
package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// destructiveRepo returns a repo with origin set and two feature branches off main.
func destructiveRepo(t *testing.T) string {
	t.Helper()
	dir := initCLITestRepo(t)
	for _, branch := range []string{"feature-merge", "feature-close"} {
		runGit(t, dir, "checkout", "-b", branch)
		file := branch + ".txt"
		if err := os.WriteFile(filepath.Join(dir, file), []byte("a change on "+branch+"\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}
		runGit(t, dir, "add", file)
		runGit(t, dir, "commit", "-m", "add "+file)
		runGit(t, dir, "checkout", "main")
	}
	return dir
}

// runGit runs one git command in dir and fails the test on a non-zero exit.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// mustRunCLI runs the CLI, fails on a non-zero exit, and returns stdout.
func mustRunCLI(t *testing.T, dir, cacheDir string, args ...string) string {
	t.Helper()
	stdout, stderr, code := runCLI(t, dir, cacheDir, args...)
	if code != ExitSuccess {
		t.Fatalf("%v: exit %d\n%s%s", args, code, stdout, stderr)
	}
	return stdout
}

// createdID runs a create command with --json and returns the ID it reports.
func createdID(t *testing.T, dir, cacheDir string, args ...string) string {
	t.Helper()
	stdout := mustRunCLI(t, dir, cacheDir, append([]string{"--json"}, args...)...)
	var created struct{ ID string }
	if err := json.Unmarshal([]byte(stdout), &created); err != nil {
		t.Fatalf("%v --json output is not JSON: %v\n%s", args, err, stdout)
	}
	if created.ID == "" {
		t.Fatalf("%v --json reported no ID\n%s", args, stdout)
	}
	return created.ID
}

// refuses asserts the command exits with ExitError and names the missing target.
func refuses(t *testing.T, dir, cacheDir string, args ...string) {
	t.Helper()
	stdout, stderr, code := runCLI(t, dir, cacheDir, args...)
	if code != ExitError {
		t.Errorf("%v on a wrong target: exit %d, want %d\n%s%s", args, code, ExitError, stdout, stderr)
	}
	if !strings.Contains(strings.ToLower(stderr), "not") {
		t.Errorf("%v stderr = %q, want a message naming the missing target", args, stderr)
	}
}

// TestCLI_destructiveVerbs_refuseWrongTargetThenAct drives every destructive verb
// in one workspace: a wrong target is refused and the real target still acts, so
// the refusal changed nothing. The final read backs assert the resulting state.
func TestCLI_destructiveVerbs_refuseWrongTargetThenAct(t *testing.T) {
	dir := destructiveRepo(t)
	cacheDir := t.TempDir()
	const listID = "reading"
	const listedRepo = "https://github.com/other/followed"
	const forkURL = "https://github.com/other/fork"
	const missingRef = "#commit:000000000000"

	for _, ext := range []string{"social", "pm", "release", "review"} {
		mustRunCLI(t, dir, cacheDir, ext, "init")
	}
	mustRunCLI(t, dir, cacheDir, "memo", "project", "init")

	postID := createdID(t, dir, cacheDir, "social", "post", "a post to retract")
	issueID := createdID(t, dir, cacheDir, "pm", "issue", "create", "an issue to close")
	releaseID := createdID(t, dir, cacheDir, "release", "create", "Release 1.0.0", "--version", "1.0.0", "--tag", "v1.0.0")
	memoID := createdID(t, dir, cacheDir, "memo", "create", "a memo to retract", "--scope", "project", "--body", "the body")
	mergePR := createdID(t, dir, cacheDir, "review", "pr", "create", "Merge me", "--base", "main", "--head", "feature-merge")
	closePR := createdID(t, dir, cacheDir, "review", "pr", "create", "Close me", "--base", "main", "--head", "feature-close")
	mustRunCLI(t, dir, cacheDir, "social", "list", "create", listID, "-n", "Reading")
	mustRunCLI(t, dir, cacheDir, "social", "list", "add", listID, listedRepo)
	mustRunCLI(t, dir, cacheDir, "fork", "add", forkURL)

	// social retract
	refuses(t, dir, cacheDir, "social", "retract", missingRef)
	mustRunCLI(t, dir, cacheDir, "social", "retract", postID)

	// pm issue close
	refuses(t, dir, cacheDir, "pm", "issue", "close", missingRef)
	mustRunCLI(t, dir, cacheDir, "pm", "issue", "close", issueID)

	// release retract
	refuses(t, dir, cacheDir, "release", "retract", missingRef)
	mustRunCLI(t, dir, cacheDir, "release", "retract", releaseID)

	// memo retract
	refuses(t, dir, cacheDir, "memo", "retract", missingRef)
	mustRunCLI(t, dir, cacheDir, "memo", "retract", memoID)

	// review pr merge, then review pr close on the second pull request
	refuses(t, dir, cacheDir, "review", "pr", "merge", missingRef)
	mustRunCLI(t, dir, cacheDir, "review", "pr", "merge", mergePR)
	refuses(t, dir, cacheDir, "review", "pr", "close", missingRef)
	mustRunCLI(t, dir, cacheDir, "review", "pr", "close", closePR)

	// social list remove, then social list delete
	refuses(t, dir, cacheDir, "social", "list", "remove", listID, "https://github.com/other/never-added")
	mustRunCLI(t, dir, cacheDir, "social", "list", "remove", listID, listedRepo)
	refuses(t, dir, cacheDir, "social", "list", "delete", "no-such-list")
	mustRunCLI(t, dir, cacheDir, "social", "list", "delete", listID)

	// fork remove: an unregistered URL exits 0 and leaves the registered fork alone.
	if stdout, stderr, code := runCLI(t, dir, cacheDir, "fork", "remove", "https://github.com/other/never-added"); code != ExitSuccess {
		t.Errorf("fork remove on an unregistered url: exit %d, want %d\n%s%s", code, ExitSuccess, stdout, stderr)
	}
	var forksBefore []struct{ URL string }
	decodeCLIJSON(t, mustRunCLI(t, dir, cacheDir, "--json", "fork", "list"), &forksBefore)
	if len(forksBefore) != 1 || forksBefore[0].URL != forkURL {
		t.Errorf("fork list = %+v after removing an unregistered url, want the one fork added", forksBefore)
	}
	mustRunCLI(t, dir, cacheDir, "fork", "remove", forkURL)

	assertDestructiveState(t, dir, cacheDir, postID, issueID, memoID, mergePR, closePR)
}

// assertDestructiveState reads every acted-on item back through the binary's --json.
func assertDestructiveState(t *testing.T, dir, cacheDir, postID, issueID, memoID, mergePR, closePR string) {
	t.Helper()

	var timeline []struct{ ID string }
	decodeCLIJSON(t, mustRunCLI(t, dir, cacheDir, "--json", "social", "timeline", "-r", "workspace"), &timeline)
	for _, post := range timeline {
		if post.ID == postID {
			t.Errorf("social timeline still serves the retracted post %s", postID)
		}
	}

	var lists []struct{ ID string }
	decodeCLIJSON(t, mustRunCLI(t, dir, cacheDir, "--json", "social", "list", "ls"), &lists)
	if len(lists) != 0 {
		t.Errorf("social list ls = %+v after the delete, want none", lists)
	}

	var issues []struct{ ID, State string }
	decodeCLIJSON(t, mustRunCLI(t, dir, cacheDir, "--json", "pm", "issue", "list", "--state", "closed"), &issues)
	if len(issues) != 1 || issues[0].ID != issueID || issues[0].State != "closed" {
		t.Errorf("pm issue list --state closed = %+v, want the closed issue %s", issues, issueID)
	}

	var releases []struct{ ID string }
	decodeCLIJSON(t, mustRunCLI(t, dir, cacheDir, "--json", "release", "list"), &releases)
	if len(releases) != 0 {
		t.Errorf("release list = %+v after the retract, want none", releases)
	}

	var memos []struct{ ID string }
	decodeCLIJSON(t, mustRunCLI(t, dir, cacheDir, "--json", "memo", "list", "--tier", "project"), &memos)
	for _, m := range memos {
		if m.ID == memoID {
			t.Errorf("memo list still serves the retracted memo %s", memoID)
		}
	}

	var prs []struct{ ID, State string }
	decodeCLIJSON(t, mustRunCLI(t, dir, cacheDir, "--json", "review", "pr", "list", "--state", "merged,closed"), &prs)
	states := map[string]string{}
	for _, pr := range prs {
		states[pr.ID] = pr.State
	}
	if states[mergePR] != "merged" {
		t.Errorf("pull request %s is %q, want merged", mergePR, states[mergePR])
	}
	if states[closePR] != "closed" {
		t.Errorf("pull request %s is %q, want closed", closePR, states[closePR])
	}
	mainTip := gitOutput(t, dir, "rev-parse", "main")
	if headTip := gitOutput(t, dir, "rev-parse", "feature-merge"); mainTip != headTip {
		t.Errorf("main is at %s, want the merged head %s", mainTip, headTip)
	}

	var forks []struct{ URL string }
	decodeCLIJSON(t, mustRunCLI(t, dir, cacheDir, "--json", "fork", "list"), &forks)
	if len(forks) != 0 {
		t.Errorf("fork list = %+v after the remove, want none", forks)
	}
}

// decodeCLIJSON unmarshals CLI --json output into target.
func decodeCLIJSON(t *testing.T, stdout string, target any) {
	t.Helper()
	if err := json.Unmarshal([]byte(stdout), target); err != nil {
		t.Fatalf("--json output is not JSON: %v\n%s", err, stdout)
	}
}

// gitOutput returns the trimmed stdout of one git command in dir.
func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}
