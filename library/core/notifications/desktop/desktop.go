// desktop.go - Sends one desktop message through the notification tool of the OS.
package desktop

import (
	"bufio"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/log"
	"github.com/gitsocial-org/gitsocial/library/core/notifications"
	"github.com/gitsocial-org/gitsocial/library/core/notifications/click"
)

const sendTimeout = 5 * time.Second

// notifierGrace is how long the sender waits for the darwin notifier to fail before it leaves the notifier to finish alone.
const notifierGrace = 500 * time.Millisecond

// waitTimeout bounds the wait of a Linux message for its click.
const waitTimeout = time.Hour

// clickAction is the action name that a notification service calls for a click on the message itself; any other name is a button.
const clickAction = "default"

// linuxIconName is the name of the icon in the hicolor theme of the user.
const linuxIconName = "gitsocial"

//go:embed gitsocial-icon.png
var iconPNG []byte

// notifierName is the executable of GitSocial.app that posts under the GitSocial name and icon.
const notifierName = "gitsocial-notifier"

// host is what a sender reads from the machine: its own executable, the directories that can hold GitSocial.app, the data directory, the Start Menu, the proc file system, the timeout and the reply timeout of the session bus.
type host struct {
	self         string
	appDirs      []string
	dataDir      string
	startMenu    string
	procDir      string
	timeout      time.Duration
	replyTimeout time.Duration
}

// currentHost returns the host of this process.
func currentHost() host {
	self, _ := os.Executable() // an empty path finds no bundle of its own
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	h := host{self: self, dataDir: os.Getenv("XDG_DATA_HOME"), procDir: "/proc", timeout: sendTimeout, replyTimeout: sendTimeout}
	if home, err := os.UserHomeDir(); err == nil {
		h.appDirs = append(h.appDirs, filepath.Join(home, "Applications"))
		if h.dataDir == "" {
			h.dataDir = filepath.Join(home, ".local", "share")
		}
	}
	h.appDirs = append(h.appDirs, "/Applications")
	if appData := os.Getenv("APPDATA"); appData != "" {
		h.startMenu = filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs")
	}
	return h
}

// Send shows each message, whose click opens its URL when the OS sender can open one; a missing tool is not an error.
func Send(messages []notifications.DesktopMessage) error {
	return send(currentHost(), messages)
}

// NotifyWait shows one message on Linux, waits for its click or its close, and on a click sends ref to socket when socket is set, else opens url; the hidden notify-wait command runs it in a detached process.
func NotifyWait(title, body, url, socket, ref string) error {
	return notifyWait(currentHost(), title, body, url, socket, ref)
}

// send runs the sender of the OS for a batch of messages; a failure of one message does not stop the others.
func send(h host, messages []notifications.DesktopMessage) error {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		return linuxSend(h, messages)
	}
	var errs []error
	for _, m := range messages {
		if runtime.GOOS == "darwin" {
			errs = append(errs, darwinSend(h, m))
		} else {
			errs = append(errs, windowsSend(h, m))
		}
	}
	return errors.Join(errs...)
}

// darwinSend starts the notifier of GitSocial.app detached, and falls back to osascript, which has no click, when the notifier is missing or fails; a message with a socket also gives the socket and the ref.
func darwinSend(h host, m notifications.DesktopMessage) error {
	if notifier := darwinNotifier(h.appDirs); notifier != "" {
		args := []string{"notify", m.Title, m.Body, m.URL}
		if m.Socket != "" {
			args = append(args, m.Socket, m.Ref)
		}
		err := startDetached(notifierGrace, notifier, args...)
		if err == nil {
			return nil
		}
		log.Debug("darwin notifier failed, using osascript", "error", err)
	}
	return run(h.timeout, lookPath("osascript"), []string{"-e", "on run argv", "-e", "display notification (item 2 of argv) with title (item 1 of argv)", "-e", "end run", "--", m.Title, m.Body}, nil)
}

// linuxSend gives each message with a click to a detached child of self that shows it and waits for the click, so the caller does not; it shows a message with no click with notify-send.
func linuxSend(h host, messages []notifications.DesktopMessage) error {
	var errs []error
	var direct []notifications.DesktopMessage
	for _, m := range messages {
		if hasClick(m) && h.self != "" {
			args := []string{"notify-wait", "--", m.Title, m.Body, m.URL}
			if m.Socket != "" {
				args = append(args, m.Socket, m.Ref)
			}
			errs = append(errs, startDetached(0, h.self, args...))
			continue
		}
		direct = append(direct, m)
	}
	tool := lookPath("notify-send")
	if len(direct) > 0 && tool != "" {
		icon := linuxIcon(h.dataDir)
		for _, m := range direct {
			errs = append(errs, run(h.timeout, tool, linuxArgs(icon, m.Title, m.Body), nil))
		}
	}
	return errors.Join(errs...)
}

// hasClick reports whether a click on the message has a target: a URL or the socket of a TUI.
func hasClick(m notifications.DesktopMessage) bool {
	return m.URL != "" || m.Socket != ""
}

// notifyWait shows the message over the session bus with the click action and waits for the click or the close; on a click it sends ref to socket, selects the tmux window of the TUI and brings the terminal app to the front when socket is set, else opens url with xdg-open. A message that the service did not get goes to notify-send with no click.
func notifyWait(h host, title, body, url, socket, ref string) error {
	icon := linuxIcon(h.dataDir)
	var activate func(conn net.Conn, reader *bufio.Reader, token string)
	onClick := func() error { return run(h.timeout, lookPath("xdg-open"), []string{url}, nil) }
	if socket != "" {
		tmuxServer, pane := tmuxSocket(os.Getenv("TMUX")), os.Getenv("TMUX_PANE")
		inTmux := tmuxServer != "" && tmuxPane(pane)
		activate = func(conn net.Conn, reader *bufio.Reader, token string) {
			getenv := os.Getenv
			if inTmux {
				getenv = tmuxClientEnv(h, tmuxServer, pane)
			}
			if exchange := terminalActivate(h, getenv); exchange != nil {
				exchange(conn, reader, token)
			}
		}
		onClick = func() error {
			if err := click.Send(socket, ref); err != nil {
				return fmt.Errorf("send click: %w", err)
			}
			if inTmux {
				tmuxSelect(h, tmuxServer, pane)
			}
			return nil
		}
	}
	sent, clicked, err := dbusNotify(h, busAddress(os.Getenv("DBUS_SESSION_BUS_ADDRESS"), os.Getenv("XDG_RUNTIME_DIR")), activate, title, body, icon, onClick)
	if !sent {
		log.Debug("session bus not usable, using notify-send", "error", err)
		return run(h.timeout, lookPath("notify-send"), linuxArgs(icon, title, body), nil)
	}
	if err != nil && !clicked {
		return fmt.Errorf("desktop message: %w", err)
	}
	return err
}

// terminalActivate returns the exchange that brings the terminal that getenv names to the front: a call of terminalCall, else the Konsole exchange, else nil.
func terminalActivate(h host, getenv func(string) string) func(conn net.Conn, reader *bufio.Reader, token string) {
	if build := terminalCall(getenv("TERM_PROGRAM"), getenv("GHOSTTY_SURFACE_ID"), getenv("GNOME_TERMINAL_SERVICE"), getenv("GNOME_TERMINAL_SCREEN"), getenv("PTYXIS_VERSION")); build != nil {
		return writeCalls(h, build)
	}
	service := getenv("KONSOLE_DBUS_SERVICE")
	if session, ok := konsoleSession(service, getenv("KONSOLE_DBUS_SESSION")); ok {
		return konsoleActivate(h, service, session)
	}
	return nil
}

// terminalCall returns the builder of the calls that bring the terminal of this process to the front from a token: the surface in Ghostty, the tab in GNOME Terminal, the app in Ptyxis, nil for a terminal with no tested call.
func terminalCall(termProgram, ghosttySurface, gnomeService, gnomeScreen, ptyxisVersion string) func(token string) []byte {
	if termProgram == "ghostty" {
		surface, err := strconv.ParseUint(strings.TrimPrefix(ghosttySurface, "0x"), 16, 64)
		if err != nil || surface == 0 {
			return nil
		}
		return func(token string) []byte { return presentSurfaceCall(4, "com.mitchellh.ghostty", surface, token) }
	}
	if screen, ok := strings.CutPrefix(gnomeScreen, "/org/gnome/Terminal/screen/"); ok && screen != "" && gnomeService != "" {
		uuid := strings.ReplaceAll(screen, "_", "-")
		return func(token string) []byte {
			return append(activateCall(4, gnomeService, "/org/gnome/Terminal", token), activateResultCall(5, gnomeService, uuid)...)
		}
	}
	if ptyxisVersion != "" {
		return func(token string) []byte {
			return activateCall(4, "org.gnome.Ptyxis", appPath("org.gnome.Ptyxis"), token)
		}
	}
	return nil
}

// linuxArgs returns the arguments of notify-send.
func linuxArgs(icon, title, body string) []string {
	args := []string{"--app-name=GitSocial"}
	if icon != "" {
		args = append(args, "--icon="+icon)
	}
	return append(args, "--", title, escapeMarkup(body))
}

// linuxIcon writes the icon into the hicolor theme under dataDir when it is missing and returns its name, or "" when it cannot.
func linuxIcon(dataDir string) string {
	if dataDir == "" {
		return ""
	}
	path := filepath.Join(dataDir, "icons", "hicolor", "512x512", "apps", linuxIconName+".png")
	if _, err := os.Stat(path); err == nil {
		return linuxIconName
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return ""
	}
	if err := os.WriteFile(path, iconPNG, 0o644); err != nil {
		return ""
	}
	return linuxIconName
}

// startDetached starts path with args in its own session with the standard streams on the null device, waits up to grace for it to fail, and reaps it in the background.
func startDetached(grace time.Duration, path string, args ...string) error {
	cmd := exec.Command(path, args...)
	cmd.SysProcAttr = detached()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", filepath.Base(path), err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err := <-exited:
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
	case <-time.After(grace):
	}
	return nil
}

// darwinNotifier returns the notifier of GitSocial.app in the first of appDirs that has one, else "".
func darwinNotifier(appDirs []string) string {
	for _, dir := range appDirs {
		candidate := filepath.Join(dir, "GitSocial.app", "Contents", "MacOS", notifierName)
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return candidate
		}
	}
	return ""
}

// lookPath returns the path of a tool on PATH, or "".
func lookPath(tool string) string {
	path, err := exec.LookPath(tool)
	if err != nil {
		return ""
	}
	return path
}

// escapeMarkup escapes the characters that the notification service reads as markup in the body.
func escapeMarkup(text string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(text)
}

// run runs a tool with extra environment and kills it after the timeout; a tool that is not found is not an error.
func run(timeout time.Duration, path string, args, env []string) error {
	if path == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("desktop notification: %w", ctx.Err())
		}
		return fmt.Errorf("desktop notification: %w", err)
	}
	return nil
}
