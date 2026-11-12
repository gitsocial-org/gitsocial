// tmux_test.go - Tests for the tmux layer of a Linux click: the variables, the window selection, the client environment and the click inside tmux.
package desktop

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/notifications/click"
)

// testTmuxSocket is the TMUX socket of the tests; the fake tmux does not open it.
const testTmuxSocket = "/tmp/tmux-1000/default"

// stubTmux puts a tmux first on PATH that appends each command line to the returned file, prints the session $0 for display-message and clients for list-clients.
func stubTmux(t *testing.T, clients string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub tools are shell scripts")
	}
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	writeStub(t, filepath.Join(dir, "bin", "tmux"), `printf '%s\n' "$*" >> '`+calls+`'
case "$*" in
*display-message*) printf '%s\n' '$0' ;;
*list-clients*) printf '%s' '`+clients+`' ;;
esac`)
	t.Setenv("PATH", filepath.Join(dir, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	return calls
}

// writeEnviron writes the environ file of pid under procDir with the entries separated by NUL.
func writeEnviron(t *testing.T, procDir, pid string, entries ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(procDir, pid), 0o755); err != nil {
		t.Fatalf("create proc dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(procDir, pid, "environ"), []byte(strings.Join(entries, "\x00")+"\x00"), 0o644); err != nil {
		t.Fatalf("write environ: %v", err)
	}
}

// tmuxCalls returns the command lines that the fake tmux got, or none when it got no command.
func tmuxCalls(t *testing.T, calls string) []string {
	t.Helper()
	if _, err := os.Stat(calls); err != nil {
		return nil
	}
	return readArgs(t, calls)
}

func TestTmuxSocket_forms(t *testing.T) {
	for value, want := range map[string]string{
		"/tmp/tmux-1000/default,4242,0": "/tmp/tmux-1000/default",
		"/tmp/tmux-1000/default":        "/tmp/tmux-1000/default",
		",4242,0":                       "",
		"":                              "",
	} {
		if got := tmuxSocket(value); got != want {
			t.Errorf("tmuxSocket(%q) = %q, want %q", value, got, want)
		}
	}
}

func TestTmuxPane_forms(t *testing.T) {
	for value, want := range map[string]bool{"%0": true, "%12": true, "": false, "0": false, "%": false, "%1x": false, "-t": false, "%1 ": false} {
		if got := tmuxPane(value); got != want {
			t.Errorf("tmuxPane(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestTmuxPane_exported(t *testing.T) {
	if socket, pane, ok := TmuxPane("/tmp/tmux-1000/default,4242,0", "%3"); socket != "/tmp/tmux-1000/default" || pane != "%3" || !ok {
		t.Errorf("TmuxPane = %q, %q, %v; want the socket, the pane and true", socket, pane, ok)
	}
	for _, c := range [][2]string{{"", "%3"}, {"/tmp/tmux-1000/default,4242,0", ""}, {"/tmp/tmux-1000/default,4242,0", "-t"}} {
		if _, _, ok := TmuxPane(c[0], c[1]); ok {
			t.Errorf("TmuxPane(%q, %q) is in tmux, want not", c[0], c[1])
		}
	}
}

func TestTmuxClientTTY_latestClient(t *testing.T) {
	calls := stubTmux(t, "5 /dev/ttys001\n9 /dev/ttys004\n")
	if got := TmuxClientTTY(testTmuxSocket, "%3"); got != "/dev/ttys004" {
		t.Errorf("TmuxClientTTY = %q, want the tty of the client with the higher activity", got)
	}
	if got := tmuxCalls(t, calls); len(got) != 2 || !strings.Contains(got[1], "#{client_activity} #{client_tty}") {
		t.Errorf("tmux commands = %q, want the session and the client list with the tty", got)
	}
	stubTmux(t, "")
	if got := TmuxClientTTY(testTmuxSocket, "%3"); got != "" {
		t.Errorf("TmuxClientTTY with no client = %q, want none", got)
	}
}

func TestTmuxSelect_args(t *testing.T) {
	calls := stubTmux(t, "")
	tmuxSelect(testHost(), testTmuxSocket, "%3")
	want := "-S " + testTmuxSocket + " select-window -t %3|-S " + testTmuxSocket + " select-pane -t %3"
	if got := strings.Join(tmuxCalls(t, calls), "|"); got != want {
		t.Errorf("tmux commands = %q, want %q", got, want)
	}
}

func TestTmuxClientEnv_latestClient(t *testing.T) {
	calls := stubTmux(t, "100 111\n200 222\n50 333\n")
	h := testHost()
	h.procDir = t.TempDir()
	writeEnviron(t, h.procDir, "111", "TERM_PROGRAM=WezTerm")
	writeEnviron(t, h.procDir, "222", "TERM_PROGRAM=ghostty", "GHOSTTY_SURFACE_ID=0xa2680fb67d38fbf2", "EXTRA=a=b")
	writeEnviron(t, h.procDir, "333", "TERM_PROGRAM=WezTerm")
	getenv := tmuxClientEnv(h, testTmuxSocket, "%3")
	for name, want := range map[string]string{"TERM_PROGRAM": "ghostty", "GHOSTTY_SURFACE_ID": "0xa2680fb67d38fbf2", "EXTRA": "a=b", "KONSOLE_DBUS_SERVICE": ""} {
		if got := getenv(name); got != want {
			t.Errorf("getenv(%q) = %q, want %q", name, got, want)
		}
	}
	want := "-S " + testTmuxSocket + " display-message -p -t %3 #{session_id}|-S " + testTmuxSocket + " list-clients -t $0 -F #{client_activity} #{client_pid}"
	if got := strings.Join(tmuxCalls(t, calls), "|"); got != want {
		t.Errorf("tmux commands = %q, want %q", got, want)
	}
}

func TestTmuxClientEnv_failures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub tools are shell scripts")
	}
	for _, c := range []struct {
		name  string
		setup func(t *testing.T)
	}{
		{"no tmux tool", func(t *testing.T) { t.Setenv("PATH", t.TempDir()) }},
		{"a failing tmux", func(t *testing.T) {
			dir := t.TempDir()
			writeStub(t, filepath.Join(dir, "tmux"), "exit 1")
			t.Setenv("PATH", dir)
		}},
		{"no client line", func(t *testing.T) { stubTmux(t, "") }},
		{"a bad pid", func(t *testing.T) { stubTmux(t, "100 abc\n") }},
		{"no environ file", func(t *testing.T) { stubTmux(t, "100 111\n") }},
	} {
		t.Run(c.name, func(t *testing.T) {
			c.setup(t)
			h := testHost()
			h.procDir = t.TempDir()
			writeEnviron(t, h.procDir, "abc", "TERM_PROGRAM=ghostty", "GHOSTTY_SURFACE_ID=0xa2680fb67d38fbf2")
			getenv := tmuxClientEnv(h, testTmuxSocket, "%3")
			for _, name := range []string{"TERM_PROGRAM", "GHOSTTY_SURFACE_ID"} {
				if got := getenv(name); got != "" {
					t.Errorf("getenv(%q) = %q, want empty", name, got)
				}
			}
		})
	}
}

// listenClick starts a click socket that drops its lines and returns its path.
func listenClick(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the click socket is a Unix socket")
	}
	cacheDir, err := os.MkdirTemp("/tmp", "gsclick")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(cacheDir) })
	socket := click.SocketPath(cacheDir, os.Getpid(), time.Now())
	stop, err := click.Listen(socket, func(string) {})
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(stop)
	return socket
}

// tmuxWaitEnv sets the variables of a wait process inside tmux, with stale Konsole variables from the shell that started the tmux server.
func tmuxWaitEnv(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	stubNotifySend(t, filepath.Join(dir, "args"), filepath.Join(dir, "opened"))
	t.Setenv("TERM_PROGRAM", "tmux")
	t.Setenv("KONSOLE_DBUS_SERVICE", ":1.94")
	t.Setenv("KONSOLE_DBUS_SESSION", "/Sessions/3")
	t.Setenv("TMUX", testTmuxSocket+",4242,0")
	t.Setenv("TMUX_PANE", "%3")
}

func TestNotifyWait_tmux(t *testing.T) {
	socket := listenClick(t)
	selected := "-S " + testTmuxSocket + " select-window -t %3|-S " + testTmuxSocket + " select-pane -t %3"
	for _, c := range []struct{ name, clients, want string }{
		{"a client in Ghostty", "100 111\n200 222\n", "com.mitchellh.ghostty ActivateAction"},
		{"no client", "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			tmuxWaitEnv(t)
			calls := stubTmux(t, c.clients)
			h := testHost()
			h.procDir = t.TempDir()
			writeEnviron(t, h.procDir, "111", "TERM_PROGRAM=WezTerm")
			writeEnviron(t, h.procDir, "222", "TERM_PROGRAM=ghostty", "GHOSTTY_SURFACE_ID=0xa2680fb67d38fbf2")
			busCalls := make(chan busMessage, 64)
			record := answerAndRecord(busCalls, signalUS("ActivationToken", testID, "token"), signalUS("ActionInvoked", testID, clickAction))
			bus, _, done := fakeBus(t, "OK 0123\r\n", func(conn net.Conn, call busMessage) {
				record(conn, call)
				if call.member == "sessionList" {
					conn.Write(errorReply(call.serial, "org.freedesktop.DBus.Error.ServiceUnknown"))
				}
			})
			useBus(t, bus)
			if err := notifyWait(h, "GitSocial", "body", "", socket, "#commit:0123456789ab"); err != nil {
				t.Fatalf("notifyWait: %v", err)
			}
			if c.want == "" {
				assertNoCall(t, c.name, busCalls, done)
			} else {
				<-done
				select {
				case call := <-busCalls:
					if got := call.destination + " " + call.member; got != c.want {
						t.Errorf("first call = %q, want %q", got, c.want)
					}
				default:
					t.Error("no call to the terminal after the click")
				}
			}
			if got := strings.Join(tmuxCalls(t, calls), "|"); !strings.HasPrefix(got, selected+"|") {
				t.Errorf("tmux commands = %q, want select-window and select-pane of the pane first", got)
			}
		})
	}
}

func TestNotifyWait_tmuxNoSelectAfterFailedClick(t *testing.T) {
	tmuxWaitEnv(t)
	calls := stubTmux(t, "100 222\n")
	socket, _, _ := fakeBus(t, "OK 0123\r\n", answerAndRecord(make(chan busMessage, 64), signalUS("ActivationToken", testID, "token"), signalUS("ActionInvoked", testID, clickAction)))
	useBus(t, socket)
	err := notifyWait(testHost(), "GitSocial", "body", "", filepath.Join(t.TempDir(), "none.sock"), "#commit:0123456789ab")
	if err == nil || !strings.Contains(err.Error(), "send click") {
		t.Errorf("notifyWait with no listener = %v, want the send error", err)
	}
	if got := tmuxCalls(t, calls); len(got) != 0 {
		t.Errorf("tmux commands = %q, want none", got)
	}
}
