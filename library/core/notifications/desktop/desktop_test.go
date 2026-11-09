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

// stubTool puts a shell script named as the OS tool first on PATH.
func stubTool(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub tools are shell scripts")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, toolName()), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	t.Setenv("PATH", dir)
}

// batch returns one message as a batch.
func batch(title, body, url string) []notifications.DesktopMessage {
	return []notifications.DesktopMessage{{Title: title, Body: body, URL: url}}
}

func TestSend_missingToolIsNotAnError(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := Send(batch("GitSocial", "Alice mentioned you", "")); err != nil {
		t.Errorf("Send without a tool = %v, want nil", err)
	}
}

func TestSend_textIsAnArgument(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "args")
	stubTool(t, `for arg in "$@"; do printf '%s\n' "$arg"; done > '`+out+`'`)
	title := `Git"Social`
	for _, body := range []string{`Alice "; touch pwned; $(whoami) ' <b>&</b>`, "-u critical"} {
		if err := Send(batch(title, body, "")); err != nil {
			t.Fatalf("Send: %v", err)
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("read stub output: %v", err)
		}
		args := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
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

func TestSend_timeout(t *testing.T) {
	stubTool(t, "exec /bin/sleep 10")
	start := time.Now()
	err := send(200*time.Millisecond, batch("GitSocial", "body", ""))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("send with a hanging tool = %v, want a deadline error", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("send returned after %v, want the tool killed at the timeout", elapsed)
	}
}
