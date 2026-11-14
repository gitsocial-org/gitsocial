// desktop_test.go - Tests for the desktop sender: a missing tool, the text as an argument, the timeout and the click of each OS.
package desktop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/notifications"
	"github.com/gitsocial-org/gitsocial/library/core/notifications/click"
)

// testHost returns a host with no bundle and no proc file system, so no test posts through a GitSocial.app or reads a process of the machine.
func testHost() host {
	return host{procDir: "/nonexistent/proc", timeout: sendTimeout, replyTimeout: sendTimeout}
}

// noBus points the session bus variables at a directory with no bus, so no test reaches the bus of the machine.
func noBus(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(dir, "bus"))
	t.Setenv("XDG_RUNTIME_DIR", dir)
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
	noBus(t)
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
	noBus(t)
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
		}
		if len(args) < 3 || args[len(args)-3] != "--" {
			t.Errorf("tool arguments = %q, want -- before the title and body", args)
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
	stubNotifySend(t, out, filepath.Join(dir, "opened"))
	body := "-u critical <b>&</b>"
	if err := notifyWait(testHost(), title, body, "https://site.example/", "", ""); err != nil {
		t.Fatalf("notifyWait: %v", err)
	}
	want := []string{"--app-name=GitSocial", "--", title, "-u critical &lt;b&gt;&amp;&lt;/b&gt;"}
	if got := readArgs(t, out); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("notify-send arguments = %q, want %q", got, want)
	}
	url := "https://site.example/'; Remove-Item C:/ #"
	for _, script := range []string{windowsToastScript, windowsShortcutScript} {
		for _, arg := range windowsArgs(script) {
			if strings.Contains(arg, body) || strings.Contains(arg, url) {
				t.Errorf("the text is in the powershell argument %q", arg)
			}
		}
	}
	env := strings.Join(windowsEnv(title, body, url, windowsAppID), "\n")
	for _, value := range []string{windowsBodyEnv + "=" + body, windowsURLEnv + "=" + url, windowsAppIDEnv + "=GitSocial.GitSocial"} {
		if !strings.Contains(env, value) {
			t.Errorf("toast environment = %q, want %q", env, value)
		}
	}
}

// stubNotifySend puts a notify-send first on PATH that records its arguments to out, and an xdg-open that records its URL to opened, with no session bus, no terminal with a call and no tmux.
func stubNotifySend(t *testing.T, out, opened string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub tools are shell scripts")
	}
	bin := t.TempDir()
	writeStub(t, filepath.Join(bin, "notify-send"), recordArgs(out))
	writeStub(t, filepath.Join(bin, "xdg-open"), recordArgs(opened))
	t.Setenv("PATH", bin)
	t.Setenv("PTYXIS_VERSION", "")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("GHOSTTY_SURFACE_ID", "")
	t.Setenv("GNOME_TERMINAL_SERVICE", "")
	t.Setenv("GNOME_TERMINAL_SCREEN", "")
	t.Setenv("KONSOLE_DBUS_SERVICE", "")
	t.Setenv("KONSOLE_DBUS_SESSION", "")
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	noBus(t)
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

func TestSend_actionWaitIsDetached(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub tools are shell scripts")
	}
	noBus(t)
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	childArgs := filepath.Join(dir, "child")
	self := filepath.Join(dir, "gitsocial")
	writeStub(t, self, `/bin/sleep 3; `+recordArgs(childArgs))
	h := host{self: self, timeout: sendTimeout}
	if err := linuxSend(h, batch("GitSocial", "-u body", "https://site.example/")); err != nil {
		t.Fatalf("linuxSend: %v", err)
	}
	if _, err := os.Stat(childArgs); err == nil {
		t.Error("the child finished before linuxSend returned")
	}
	if !waitForFile(childArgs) {
		t.Fatal("the detached child did not run with no notify-send on PATH")
	}
	if got := strings.Join(readArgs(t, childArgs), "|"); got != "notify-wait|--|GitSocial|-u body|https://site.example/" {
		t.Errorf("child arguments = %q, want notify-wait, --, the title, the body and the URL", got)
	}
	out := filepath.Join(dir, "args")
	stubNotifySend(t, out, filepath.Join(dir, "opened"))
	if err := linuxSend(h, batch("GitSocial", "-u body", "")); err != nil {
		t.Fatalf("linuxSend with no click: %v", err)
	}
	if got := strings.Join(readArgs(t, out), "|"); got != "--app-name=GitSocial|--|GitSocial|-u body" {
		t.Errorf("notify-send arguments = %q, want the message with no action and no wait", got)
	}
}

func TestLinuxSender_socketArgs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub tools are shell scripts")
	}
	noBus(t)
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	ref := "https://example.com/owner/repo#commit:0123456789ab@main"
	for i, c := range []struct {
		message notifications.DesktopMessage
		want    string
	}{
		{notifications.DesktopMessage{Title: "GitSocial", Body: "body", Socket: "/tmp/click/1-2.sock", Ref: ref}, "notify-wait|--|GitSocial|body||/tmp/click/1-2.sock|" + ref},
		{notifications.DesktopMessage{Title: "GitSocial", Body: "5 new notifications", Socket: "/tmp/click/1-2.sock"}, "notify-wait|--|GitSocial|5 new notifications||/tmp/click/1-2.sock|"},
	} {
		out := filepath.Join(dir, fmt.Sprintf("child%d", i))
		self := filepath.Join(dir, "gitsocial")
		writeStub(t, self, `for arg in "$@"; do printf '%s|' "$arg"; done > '`+out+`.tmp'; /bin/mv '`+out+`.tmp' '`+out+`'`)
		if err := linuxSend(host{self: self, timeout: sendTimeout}, []notifications.DesktopMessage{c.message}); err != nil {
			t.Fatalf("linuxSend: %v", err)
		}
		if !waitForFile(out) {
			t.Fatal("the detached child did not run")
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("read child arguments: %v", err)
		}
		if got := strings.TrimSuffix(string(data), "|"); got != c.want {
			t.Errorf("child arguments = %q, want %q", got, c.want)
		}
	}
}

// clickBus starts a fake bus that sends the messages after Notify and points the session bus variables at it.
func clickBus(t *testing.T, messages ...[]byte) {
	t.Helper()
	socket, _, _ := fakeBus(t, "OK 0123\r\n", answerNotify(messages...))
	useBus(t, socket)
}

// assertNoFile fails the test when path exists.
func assertNoFile(t *testing.T, path, what string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Errorf("%s ran, want none", what)
	}
}

func TestNotifyWait_socketClick(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the click socket is a Unix socket")
	}
	cacheDir, err := os.MkdirTemp("/tmp", "gsclick")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(cacheDir) })
	socket := click.SocketPath(cacheDir, os.Getpid(), time.Now())
	lines := make(chan string, 2)
	stop, err := click.Listen(socket, func(line string) { lines <- line })
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer stop()
	dir := t.TempDir()
	out, opened := filepath.Join(dir, "args"), filepath.Join(dir, "opened")
	stubNotifySend(t, out, opened)
	clickBus(t, signalUS("ActionInvoked", testID, clickAction), closedSignal(testID))
	ref := "https://example.com/owner/repo#commit:0123456789ab@main"
	if err := notifyWait(testHost(), "GitSocial", "body", "https://site.example/", socket, ref); err != nil {
		t.Fatalf("notifyWait with a click: %v", err)
	}
	select {
	case line := <-lines:
		if line != "open "+ref {
			t.Errorf("line = %q, want open and the ref", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the socket got no line after the click")
	}
	assertNoFile(t, opened, "xdg-open for a message with a socket")
	assertNoFile(t, out, "notify-send for a message on the bus")
	clickBus(t, closedSignal(testID))
	if err := notifyWait(testHost(), "GitSocial", "body", "", socket, ref); err != nil {
		t.Fatalf("notifyWait with a close: %v", err)
	}
	select {
	case line := <-lines:
		t.Errorf("line after a close = %q, want none", line)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestNotifyWait_urlClick(t *testing.T) {
	dir := t.TempDir()
	out, opened := filepath.Join(dir, "args"), filepath.Join(dir, "opened")
	stubNotifySend(t, out, opened)
	clickBus(t, signalUS("ActionInvoked", testID, clickAction))
	if err := notifyWait(testHost(), "GitSocial", "body", "https://site.example/", "", ""); err != nil {
		t.Fatalf("notifyWait: %v", err)
	}
	if got := readArgs(t, opened); len(got) != 1 || got[0] != "https://site.example/" {
		t.Errorf("xdg-open arguments = %q, want the URL after the click", got)
	}
	assertNoFile(t, out, "notify-send for a message on the bus")
}

func TestNotifyWait_urlClickNoActivate(t *testing.T) {
	dir := t.TempDir()
	out, opened := filepath.Join(dir, "args"), filepath.Join(dir, "opened")
	stubNotifySend(t, out, opened)
	t.Setenv("PTYXIS_VERSION", "50.1")
	calls := make(chan busMessage, 4)
	socket, _, done := fakeBus(t, "OK 0123\r\n", answerAndRecord(calls, signalUS("ActivationToken", testID, "token"), signalUS("ActionInvoked", testID, clickAction)))
	useBus(t, socket)
	if err := notifyWait(testHost(), "GitSocial", "body", "https://site.example/", "", ""); err != nil {
		t.Fatalf("notifyWait: %v", err)
	}
	if got := readArgs(t, opened); len(got) != 1 || got[0] != "https://site.example/" {
		t.Errorf("xdg-open arguments = %q, want the URL after the click", got)
	}
	assertNoCall(t, "URL message in Ptyxis", calls, done)
}

func TestTerminalCall_byEnvironment(t *testing.T) {
	screen := "/org/gnome/Terminal/screen/40a8cc32_b75e_4b5e_a9b7_95dd6d352c3d"
	gnome := ":1.272 Activate|:1.272 ActivateResult"
	for _, c := range []struct{ name, termProgram, surface, service, screen, ptyxis, want string }{
		{"Ghostty with a surface", "ghostty", "0xa2680fb67d38fbf2", "", "", "", "com.mitchellh.ghostty ActivateAction"},
		{"Ghostty started from Ptyxis", "ghostty", "0x0000000000000001", "", "", "50.1", "com.mitchellh.ghostty ActivateAction"},
		{"Ghostty with no surface", "ghostty", "", "", "", "50.1", ""},
		{"Ghostty with no surface started from GNOME Terminal", "ghostty", "", ":1.272", screen, "", ""},
		{"Ghostty with surface zero", "ghostty", "0x0000000000000000", "", "", "", ""},
		{"Ghostty with a bad surface", "ghostty", "surface", "", "", "", ""},
		{"GNOME Terminal", "", "", ":1.272", screen, "", gnome},
		{"GNOME Terminal started from Ptyxis", "", "", ":1.272", screen, "50.1", gnome},
		{"GNOME Terminal with no service", "", "", "", screen, "50.1", "org.gnome.Ptyxis Activate"},
		{"a screen with another prefix in Ptyxis", "", "", ":1.272", "/org/gnome/Other/screen/40a8cc32", "50.1", "org.gnome.Ptyxis Activate"},
		{"a screen with another prefix", "", "", ":1.272", "/org/gnome/Other/screen/40a8cc32", "", ""},
		{"a screen with no UUID in Ptyxis", "", "", ":1.272", "/org/gnome/Terminal/screen/", "50.1", "org.gnome.Ptyxis Activate"},
		{"a screen with no UUID", "", "", ":1.272", "/org/gnome/Terminal/screen/", "", ""},
		{"Ptyxis", "", "", "", "", "50.1", "org.gnome.Ptyxis Activate"},
		{"a stale surface in another terminal", "", "0xa2680fb67d38fbf2", "", "", "", ""},
		{"another terminal", "WezTerm", "", "", "", "", ""},
	} {
		build := terminalCall(c.termProgram, c.surface, c.service, c.screen, c.ptyxis)
		var calls []string
		if build != nil {
			for reader := bytes.NewReader(build("token")); reader.Len() > 0; {
				m, ok, err := readBusMessage(reader)
				if err != nil || !ok {
					t.Fatalf("%s: readBusMessage = %v, %v", c.name, ok, err)
				}
				calls = append(calls, m.destination+" "+m.member)
			}
		}
		if got := strings.Join(calls, "|"); got != c.want {
			t.Errorf("%s: calls = %q, want %q", c.name, got, c.want)
		}
	}
	reader := bytes.NewReader(terminalCall("", "", ":1.272", screen, "")("token"))
	activate, _, err := readBusMessage(reader)
	if err != nil {
		t.Fatalf("read Activate: %v", err)
	}
	result, _, err := readBusMessage(reader)
	if err != nil {
		t.Fatalf("read ActivateResult: %v", err)
	}
	if activate.serial != 4 || activate.destination != ":1.272" || activate.member != "Activate" || activate.path != "/org/gnome/Terminal" {
		t.Errorf("first call = %d %s %s %s, want Activate of /org/gnome/Terminal on :1.272 with serial 4", activate.serial, activate.destination, activate.member, activate.path)
	}
	if result.serial != 5 || result.destination != ":1.272" || result.member != "ActivateResult" || result.path != "/org/gnome/Terminal/SearchProvider" {
		t.Errorf("second call = %d %s %s %s, want ActivateResult of /org/gnome/Terminal/SearchProvider on :1.272 with serial 5", result.serial, result.destination, result.member, result.path)
	}
	if !strings.Contains(string(result.body), "40a8cc32-b75e-4b5e-a9b7-95dd6d352c3d\x00") {
		t.Errorf("ActivateResult body = %q, want the UUID of the tab with hyphens", result.body)
	}
}

func TestNotifyWait_socketClickActivates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the click socket is a Unix socket")
	}
	cacheDir, err := os.MkdirTemp("/tmp", "gsclick")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(cacheDir) })
	socket := click.SocketPath(cacheDir, os.Getpid(), time.Now())
	lines := make(chan string, 2)
	stop, err := click.Listen(socket, func(line string) { lines <- line })
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer stop()
	dir := t.TempDir()
	stubNotifySend(t, filepath.Join(dir, "args"), filepath.Join(dir, "opened"))
	t.Setenv("TERM_PROGRAM", "ghostty")
	t.Setenv("GHOSTTY_SURFACE_ID", "0xa2680fb67d38fbf2")
	calls := make(chan busMessage, 4)
	bus, _, done := fakeBus(t, "OK 0123\r\n", answerAndRecord(calls, signalUS("ActivationToken", testID, "token"), signalUS("ActionInvoked", testID, clickAction)))
	useBus(t, bus)
	if err := notifyWait(testHost(), "GitSocial", "body", "", socket, "#commit:0123456789ab"); err != nil {
		t.Fatalf("notifyWait: %v", err)
	}
	if line := <-lines; line != "open #commit:0123456789ab" {
		t.Errorf("line = %q, want open and the ref", line)
	}
	<-done
	select {
	case call := <-calls:
		if call.destination != "com.mitchellh.ghostty" || call.member != "ActivateAction" {
			t.Errorf("call = %s %s, want ActivateAction of com.mitchellh.ghostty", call.destination, call.member)
		}
	default:
		t.Error("no call to the terminal after the click")
	}
}

func TestNotifyWait_konsole(t *testing.T) {
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
	defer stop()
	dir := t.TempDir()
	stubNotifySend(t, filepath.Join(dir, "args"), filepath.Join(dir, "opened"))
	t.Setenv("KONSOLE_DBUS_SERVICE", ":1.94")
	t.Setenv("KONSOLE_DBUS_SESSION", "/Sessions/3")
	for _, c := range []struct{ name, ptyxis, want string }{
		{"Konsole", "", ":1.94 /Windows/1 sessionList"},
		{"Ptyxis started from Konsole", "50.1", "org.gnome.Ptyxis /org/gnome/Ptyxis Activate"},
	} {
		t.Setenv("PTYXIS_VERSION", c.ptyxis)
		calls := make(chan busMessage, 64)
		record := answerAndRecord(calls, signalUS("ActivationToken", testID, "token"), signalUS("ActionInvoked", testID, clickAction))
		bus, _, done := fakeBus(t, "OK 0123\r\n", func(conn net.Conn, call busMessage) {
			record(conn, call)
			if call.member == "sessionList" {
				conn.Write(reply(call.serial, "as", appendStrings(nil, []string{"3"})))
			}
			if call.member == "currentSession" {
				conn.Write(errorReply(call.serial, "org.freedesktop.DBus.Error.UnknownMethod"))
			}
		})
		useBus(t, bus)
		if err := notifyWait(testHost(), "GitSocial", "body", "", socket, "#commit:0123456789ab"); err != nil {
			t.Fatalf("%s: notifyWait: %v", c.name, err)
		}
		<-done
		select {
		case call := <-calls:
			if got := call.destination + " " + call.path + " " + call.member; got != c.want {
				t.Errorf("%s: first call = %q, want %q", c.name, got, c.want)
			}
		default:
			t.Errorf("%s: no call to the terminal after the click", c.name)
		}
		if c.ptyxis != "" {
			assertNoCall(t, c.name+": a second call", calls, done)
		}
	}
}

func TestNotifyWait_noBus(t *testing.T) {
	dir := t.TempDir()
	out, opened := filepath.Join(dir, "args"), filepath.Join(dir, "opened")
	stubNotifySend(t, out, opened)
	h := testHost()
	h.dataDir = filepath.Join(dir, "data")
	if err := notifyWait(h, "GitSocial", "-u body", "https://site.example/", "", ""); err != nil {
		t.Fatalf("notifyWait: %v", err)
	}
	if got := strings.Join(readArgs(t, out), "|"); got != "--app-name=GitSocial|--icon=gitsocial|--|GitSocial|-u body" {
		t.Errorf("notify-send arguments = %q, want the message with the icon and no action", got)
	}
	icon, err := os.ReadFile(filepath.Join(dir, "data", "icons", "hicolor", "512x512", "apps", "gitsocial.png"))
	if err != nil || string(icon) != string(iconPNG) {
		t.Errorf("icon in the data directory: %v, want the embedded icon", err)
	}
	assertNoFile(t, opened, "xdg-open with no click")
}

func TestNotifyWait_noSecondMessage(t *testing.T) {
	dir := t.TempDir()
	out, opened := filepath.Join(dir, "args"), filepath.Join(dir, "opened")
	stubNotifySend(t, out, opened)
	clickBus(t)
	if err := notifyWait(testHost(), "GitSocial", "body", "https://site.example/", "", ""); err == nil {
		t.Error("notifyWait after a closed connection = nil, want the error of the wait")
	}
	assertNoFile(t, out, "notify-send after an accepted Notify")
	assertNoFile(t, opened, "xdg-open with no click")
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
	home := filepath.Join(root, "home", "Applications")
	system := filepath.Join(root, "Applications")
	notifierIn := func(apps string) string {
		return filepath.Join(apps, "GitSocial.app", "Contents", "MacOS", notifierName)
	}
	appDirs := []string{home, system}
	if got := darwinNotifier(appDirs); got != "" {
		t.Errorf("no notifier anywhere gave %q, want none, so osascript sends", got)
	}
	writeStub(t, notifierIn(system), recordArgs(out))
	if got := darwinNotifier(appDirs); got != notifierIn(system) {
		t.Errorf("a bundle in /Applications gave %q, want its notifier", got)
	}
	writeStub(t, notifierIn(home), recordArgs(out))
	if got := darwinNotifier(appDirs); got != notifierIn(home) {
		t.Errorf("bundles in both places gave %q, want the one in ~/Applications", got)
	}
	m := batch("GitSocial", "-u <b>", "https://site.example/")[0]
	if err := darwinSend(host{appDirs: appDirs, timeout: sendTimeout}, m); err != nil {
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
	h := host{appDirs: []string{root}, timeout: sendTimeout}
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

func TestDarwinSender_clickArgs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub tools are shell scripts")
	}
	root := t.TempDir()
	bundled := filepath.Join(root, "GitSocial.app", "Contents", "MacOS")
	h := host{appDirs: []string{root}, timeout: sendTimeout}
	ref := "https://example.com/owner/repo#commit:0123456789ab@main"
	for i, c := range []struct {
		message notifications.DesktopMessage
		want    string
	}{
		{notifications.DesktopMessage{Title: "GitSocial", Body: "body", URL: "https://site.example/", Ref: ref}, "notify|GitSocial|body|https://site.example/"},
		{notifications.DesktopMessage{Title: "GitSocial", Body: "body", Socket: "/tmp/click/1-2.sock", Ref: ref}, "notify|GitSocial|body||/tmp/click/1-2.sock|" + ref},
		{notifications.DesktopMessage{Title: "GitSocial", Body: "5 new notifications", Socket: "/tmp/click/1-2.sock"}, "notify|GitSocial|5 new notifications||/tmp/click/1-2.sock|"},
	} {
		out := filepath.Join(root, fmt.Sprintf("args%d", i))
		writeStub(t, filepath.Join(bundled, notifierName), `for arg in "$@"; do printf '%s|' "$arg"; done > '`+out+`.tmp'; mv '`+out+`.tmp' '`+out+`'`)
		if err := darwinSend(h, c.message); err != nil {
			t.Fatalf("darwinSend: %v", err)
		}
		if !waitForFile(out) {
			t.Fatal("the notifier did not run")
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("read notifier arguments: %v", err)
		}
		if got := strings.TrimSuffix(string(data), "|"); got != c.want {
			t.Errorf("notifier arguments = %q, want %q", got, c.want)
		}
	}
}

func TestDarwinSender_titleIsNotAnOption(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub tools are shell scripts")
	}
	bin, out := t.TempDir(), filepath.Join(t.TempDir(), "args")
	writeStub(t, filepath.Join(bin, "osascript"), `for arg in "$@"; do printf '%s\0' "$arg"; done > '`+out+`'`)
	t.Setenv("PATH", bin)
	m := notifications.DesktopMessage{Title: "-e do shell script \"touch x\"", Body: "line one\nowner/repo"}
	if err := darwinSend(host{appDirs: []string{t.TempDir()}, timeout: sendTimeout}, m); err != nil {
		t.Fatalf("darwinSend: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read osascript arguments: %v", err)
	}
	args := strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00")
	if len(args) < 3 || args[len(args)-3] != "--" || args[len(args)-2] != m.Title || args[len(args)-1] != m.Body {
		t.Errorf("osascript arguments = %q, want -- before the title and the body with its newline", args)
	}
}
