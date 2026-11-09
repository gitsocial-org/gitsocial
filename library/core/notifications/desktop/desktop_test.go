// desktop_test.go - Tests for the desktop sender: a missing tool, the text as an argument and the timeout.
package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/notifications"
)

// testHost returns a host with no bundle, so no test posts through a GitSocial.app of the machine.
func testHost() host {
	return host{timeout: sendTimeout}
}

// writeStub writes an executable shell script.
func writeStub(t *testing.T, path, script string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
}

// stubTool puts a shell script named as the tool of the OS first on PATH.
func stubTool(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub tools are shell scripts")
	}
	tool := "notify-send"
	if runtime.GOOS == "darwin" {
		tool = "osascript"
	}
	dir := t.TempDir()
	writeStub(t, filepath.Join(dir, tool), script)
	t.Setenv("PATH", dir)
}

// recordArgs returns a script that writes its arguments to out, one per line.
func recordArgs(out string) string {
	return `for arg in "$@"; do printf '%s\n' "$arg"; done > '` + out + `'`
}

// readArgs returns the arguments a recording stub wrote.
func readArgs(t *testing.T, out string) []string {
	t.Helper()
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read stub output: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

// batch returns one message as a batch.
func batch(title, body, url string) []notifications.DesktopMessage {
	return []notifications.DesktopMessage{{Title: title, Body: body, URL: url}}
}

func TestSend_missingToolIsNotAnError(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := send(testHost(), batch("GitSocial", "Alice mentioned you", "")); err != nil {
		t.Errorf("Send without a tool = %v, want nil", err)
	}
}

func TestSend_textIsAnArgument(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "args")
	stubTool(t, recordArgs(out))
	title := `Git"Social`
	for _, body := range []string{`Alice "; touch pwned; $(whoami) ' <b>&</b>`, "-u critical"} {
		if err := send(testHost(), batch(title, body, "")); err != nil {
			t.Fatalf("send: %v", err)
		}
		args := readArgs(t, out)
		want := body
		if runtime.GOOS != "darwin" {
			want = escapeMarkup(body)
			if len(args) < 3 || args[len(args)-3] != "--" {
				t.Errorf("tool arguments = %q, want -- before the title and body", args)
			}
		}
		if len(args) < 2 || args[len(args)-2] != title || args[len(args)-1] != want {
			t.Errorf("tool arguments = %q, want the title and the body %q as the last two", args, want)
		}
		for _, arg := range args[:len(args)-2] {
			if strings.Contains(arg, body) {
				t.Errorf("the body is in the script argument %q", arg)
			}
		}
	}
}

// waitForFile polls for a file for ten seconds and reports whether it exists.
func waitForFile(path string) bool {
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

func TestSend_timeout(t *testing.T) {
	stubTool(t, "exec /bin/sleep 10")
	start := time.Now()
	err := send(host{timeout: 200 * time.Millisecond}, batch("GitSocial", "body", ""))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("send with a hanging tool = %v, want a deadline error", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("send returned after %v, want the tool killed at the timeout", elapsed)
	}
}

func TestDarwinSender_precedence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub tools are shell scripts")
	}
	root := t.TempDir()
	out := filepath.Join(root, "args")
	bundled := filepath.Join(root, "own", "GitSocial.app", "Contents", "MacOS")
	home := filepath.Join(root, "home", "Applications")
	system := filepath.Join(root, "Applications")
	notifierIn := func(apps string) string {
		return filepath.Join(apps, "GitSocial.app", "Contents", "MacOS", notifierName)
	}
	appDirs := []string{home, system}
	self := filepath.Join(bundled, "gitsocial")
	if got := darwinNotifier(self, appDirs); got != "" {
		t.Errorf("no notifier anywhere gave %q, want none, so osascript sends", got)
	}
	writeStub(t, notifierIn(system), recordArgs(out))
	if got := darwinNotifier(self, appDirs); got != notifierIn(system) {
		t.Errorf("a bundle in /Applications gave %q, want its notifier", got)
	}
	writeStub(t, notifierIn(home), recordArgs(out))
	if got := darwinNotifier(self, appDirs); got != notifierIn(home) {
		t.Errorf("bundles in both places gave %q, want the one in ~/Applications", got)
	}
	writeStub(t, filepath.Join(bundled, notifierName), recordArgs(out))
	if got := darwinNotifier(filepath.Join(root, "bin", "gitsocial"), appDirs); got != notifierIn(home) {
		t.Errorf("an executable outside a bundle gave %q, want the installed bundle", got)
	}
	if got := darwinNotifier(self, appDirs); got != filepath.Join(bundled, notifierName) {
		t.Errorf("an executable in a bundle gave %q, want the notifier next to it", got)
	}
	m := batch("GitSocial", "-u <b>", "https://site.example/")[0]
	if err := darwinSend(host{self: self, appDirs: appDirs, timeout: sendTimeout}, m); err != nil {
		t.Fatalf("darwinSend: %v", err)
	}
	if !waitForFile(out) {
		t.Fatal("the notifier did not run")
	}
	if got := readArgs(t, out); strings.Join(got, "|") != "notify|GitSocial|-u <b>|https://site.example/" {
		t.Errorf("notifier arguments = %q, want notify, the title, the body and the URL", got)
	}
}

func TestDarwinSender_detachedAndFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub tools are shell scripts")
	}
	root := t.TempDir()
	posted, fellBack := filepath.Join(root, "posted"), filepath.Join(root, "osascript")
	bin := t.TempDir()
	writeStub(t, filepath.Join(bin, "osascript"), recordArgs(fellBack))
	t.Setenv("PATH", bin)
	bundled := filepath.Join(root, "GitSocial.app", "Contents", "MacOS")
	h := host{self: filepath.Join(bundled, "gitsocial"), timeout: sendTimeout}
	m := batch("GitSocial", "body", "https://site.example/")[0]
	writeStub(t, filepath.Join(bundled, notifierName), `/bin/sleep 3; `+recordArgs(posted))
	if err := darwinSend(h, m); err != nil {
		t.Fatalf("darwinSend with a slow notifier: %v", err)
	}
	if _, err := os.Stat(posted); err == nil {
		t.Error("the notifier finished before darwinSend returned")
	}
	if !waitForFile(posted) {
		t.Fatal("the detached notifier did not run")
	}
	if _, err := os.Stat(fellBack); err == nil {
		t.Error("osascript ran although the notifier worked")
	}
	writeStub(t, filepath.Join(bundled, notifierName), `echo "no notification support" >&2; exit 1`)
	if err := darwinSend(h, m); err != nil {
		t.Fatalf("darwinSend with a failing notifier: %v", err)
	}
	if got := readArgs(t, fellBack); len(got) < 2 || got[len(got)-1] != "body" {
		t.Errorf("osascript arguments = %q, want the fallback with the body", got)
	}
}
