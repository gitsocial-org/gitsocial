// notify_wait_test.go - Tests for the hidden notify-wait command: the count of its arguments.
package main

import (
	"path/filepath"
	"testing"
)

func TestNotifyWait_args(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	runtimeDir := t.TempDir()
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(runtimeDir, "bus"))
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("PTYXIS_VERSION", "")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("GNOME_TERMINAL_SERVICE", "")
	t.Setenv("GNOME_TERMINAL_SCREEN", "")
	t.Setenv("KONSOLE_DBUS_SERVICE", "")
	t.Setenv("KONSOLE_DBUS_SESSION", "")
	dir, cacheDir := t.TempDir(), t.TempDir()
	for _, c := range []struct {
		args []string
		want int
	}{
		{[]string{"GitSocial", "body"}, ExitError},
		{[]string{"GitSocial", "body", "https://site.example/"}, ExitSuccess},
		{[]string{"GitSocial", "body", "", "/tmp/click/1-2.sock"}, ExitError},
		{[]string{"GitSocial", "body", "", "/tmp/click/1-2.sock", "#commit:0123456789ab"}, ExitSuccess},
		{[]string{"GitSocial", "body", "", "/tmp/click/1-2.sock", "#commit:0123456789ab", "extra"}, ExitError},
	} {
		_, stderr, code := runInProcess(t, dir, cacheDir, append([]string{"notify-wait", "--"}, c.args...)...)
		if code != c.want {
			t.Errorf("notify-wait with %d arguments: exit %d, want %d; stderr %q", len(c.args), code, c.want, stderr)
		}
	}
}
