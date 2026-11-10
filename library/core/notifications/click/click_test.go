// click_test.go - Tests for the click socket: the delivery of a line, a dead socket and the files a listener removes.
package click

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// shortCacheDir returns a cache directory under /tmp, so a socket path stays under the limit of macOS.
func shortCacheDir(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the click socket is for macOS")
	}
	dir, err := os.MkdirTemp("/tmp", "gsclick")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestSend_delivers(t *testing.T) {
	path := SocketPath(shortCacheDir(t), os.Getpid(), time.Now())
	lines := make(chan string, 1)
	stop, err := Listen(path, func(line string) { lines <- line })
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer stop()
	ref := "https://example.com/owner/repo#commit:0123456789ab@main"
	if err := Send(path, ref); err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case line := <-lines:
		if line != "open "+ref {
			t.Errorf("line = %q, want open and the ref", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the listener got no line")
	}
}

func TestSend_deadSocket(t *testing.T) {
	cacheDir := shortCacheDir(t)
	dead := filepath.Join(cacheDir, "dead.sock")
	listener, err := net.Listen("unix", dead)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	listener.(*net.UnixListener).SetUnlinkOnClose(false)
	listener.Close()
	for _, path := range []string{dead, filepath.Join(cacheDir, "absent.sock")} {
		start := time.Now()
		if err := Send(path, ""); err == nil {
			t.Errorf("Send to %s = nil, want an error", filepath.Base(path))
		}
		if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
			t.Errorf("Send to %s took %v, want at most 1 s", filepath.Base(path), elapsed)
		}
	}
}

func TestListen_socketLifecycle(t *testing.T) {
	cacheDir := shortCacheDir(t)
	exited := exec.Command("true")
	if err := exited.Run(); err != nil {
		t.Fatalf("run a short process: %v", err)
	}
	deadSocket := SocketPath(cacheDir, exited.Process.Pid, time.Unix(1, 0))
	liveSocket := SocketPath(cacheDir, os.Getpid(), time.Unix(1, 0))
	other := filepath.Join(cacheDir, "click", "notes.txt")
	if err := os.MkdirAll(filepath.Dir(deadSocket), 0o700); err != nil {
		t.Fatalf("create click dir: %v", err)
	}
	for _, path := range []string{deadSocket, liveSocket, other} {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	own := SocketPath(cacheDir, os.Getpid(), time.Now())
	stop, err := Listen(own, func(string) {})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	if _, err := os.Stat(deadSocket); !os.IsNotExist(err) {
		t.Errorf("the socket of a dead pid after Listen: err = %v, want removed", err)
	}
	for _, path := range []string{liveSocket, other} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s after Listen: %v, want kept", filepath.Base(path), err)
		}
	}
	if info, err := os.Stat(own); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("own socket = %v, %v, want mode 0600", info, err)
	}
	if info, err := os.Stat(filepath.Dir(own)); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("click directory = %v, %v, want mode 0700", info, err)
	}
	stop()
	if _, err := os.Stat(own); !os.IsNotExist(err) {
		t.Errorf("own socket after stop: err = %v, want removed", err)
	}
	if _, err := Listen(filepath.Join(cacheDir, strings.Repeat("a", maxPathBytes)), func(string) {}); err == nil {
		t.Error("Listen on a path over the limit = nil, want an error")
	}
}
