// desktop.go - Sends one desktop message through the notification tool of the OS.
package desktop

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/log"
	"github.com/gitsocial-org/gitsocial/library/core/notifications"
)

const sendTimeout = 5 * time.Second

// notifierGrace is how long the sender waits for the darwin notifier to fail before it leaves the notifier to finish alone.
const notifierGrace = 500 * time.Millisecond

// waitTimeout bounds the wait of a Linux message for its click.
const waitTimeout = time.Hour

// linuxIconName is the name of the icon in the hicolor theme of the user.
const linuxIconName = "gitsocial"

//go:embed gitsocial-icon.png
var iconPNG []byte

// notifierName is the executable of GitSocial.app that posts under the GitSocial name and icon.
const notifierName = "gitsocial-notifier"

const (
	windowsTitleEnv = "GITSOCIAL_NOTIFY_TITLE"
	windowsBodyEnv  = "GITSOCIAL_NOTIFY_BODY"
)

// windowsScript shows a toast; it reads the text from the environment because powershell joins trailing arguments into the script.
const windowsScript = `[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] > $null
$template = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$text = $template.GetElementsByTagName('text')
$text.Item(0).AppendChild($template.CreateTextNode($env:` + windowsTitleEnv + `)) > $null
$text.Item(1).AppendChild($template.CreateTextNode($env:` + windowsBodyEnv + `)) > $null
$toast = [Windows.UI.Notifications.ToastNotification]::new($template)
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe').Show($toast)`

// host is what a sender reads from the machine: its own executable, the directories that can hold GitSocial.app, the data directory and the timeout.
type host struct {
	self    string
	appDirs []string
	dataDir string
	timeout time.Duration
}

// currentHost returns the host of this process.
func currentHost() host {
	self, _ := os.Executable() // an empty path finds no bundle of its own
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	h := host{self: self, dataDir: os.Getenv("XDG_DATA_HOME"), timeout: sendTimeout}
	if home, err := os.UserHomeDir(); err == nil {
		h.appDirs = append(h.appDirs, filepath.Join(home, "Applications"))
		if h.dataDir == "" {
			h.dataDir = filepath.Join(home, ".local", "share")
		}
	}
	h.appDirs = append(h.appDirs, "/Applications")
	return h
}

// Send shows each message, whose click opens its URL when the OS sender can open one; a missing tool is not an error.
func Send(messages []notifications.DesktopMessage) error {
	return send(currentHost(), messages)
}

// NotifyWait shows one message on Linux, waits for its click or its close, and opens url on a click; the hidden notify-wait command runs it in a detached process.
func NotifyWait(title, body, url string) error {
	return notifyWait(currentHost(), title, body, url)
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
			errs = append(errs, run(h.timeout, lookPath("powershell"), []string{"-NoProfile", "-NonInteractive", "-Command", windowsScript}, []string{windowsTitleEnv + "=" + m.Title, windowsBodyEnv + "=" + m.Body}))
		}
	}
	return errors.Join(errs...)
}

// darwinSend starts the notifier of GitSocial.app detached, and falls back to osascript, which has no click, when the notifier is missing or fails.
func darwinSend(h host, m notifications.DesktopMessage) error {
	if notifier := darwinNotifier(h.self, h.appDirs); notifier != "" {
		err := startDetached(notifierGrace, notifier, "notify", m.Title, m.Body, m.URL)
		if err == nil {
			return nil
		}
		log.Debug("darwin notifier failed, using osascript", "error", err)
	}
	return run(h.timeout, lookPath("osascript"), []string{"-e", "on run argv", "-e", "display notification (item 2 of argv) with title (item 1 of argv)", "-e", "end run", m.Title, m.Body}, nil)
}

// linuxSend shows messages with notify-send; a message with a URL, when notify-send has --action, goes to a detached child of self that waits for the click so the caller does not.
func linuxSend(h host, messages []notifications.DesktopMessage) error {
	tool := lookPath("notify-send")
	if tool == "" {
		return nil
	}
	action := false
	for _, m := range messages {
		if m.URL != "" && h.self != "" {
			action = hasAction(tool)
			break
		}
	}
	var errs []error
	var direct []notifications.DesktopMessage
	for _, m := range messages {
		if action && m.URL != "" {
			errs = append(errs, startDetached(0, h.self, "notify-wait", "--", m.Title, m.Body, m.URL))
			continue
		}
		direct = append(direct, m)
	}
	if len(direct) > 0 {
		icon := linuxIcon(h.dataDir)
		for _, m := range direct {
			errs = append(errs, run(h.timeout, tool, linuxArgs(icon, false, m.Title, m.Body), nil))
		}
	}
	return errors.Join(errs...)
}

// notifyWait runs notify-send with the open action until the click or the close and opens url with xdg-open on a click.
func notifyWait(h host, title, body, url string) error {
	tool := lookPath("notify-send")
	if tool == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, tool, linuxArgs(linuxIcon(h.dataDir), true, title, body)...).Output()
	if err != nil {
		return fmt.Errorf("notify-send: %w", err)
	}
	opener := lookPath("xdg-open")
	if strings.TrimSpace(string(out)) != "open" || opener == "" {
		return nil
	}
	return run(h.timeout, opener, []string{url}, nil)
}

// linuxArgs returns the arguments of notify-send; with wait, also the open action and the wait for it.
func linuxArgs(icon string, wait bool, title, body string) []string {
	args := []string{"--app-name=GitSocial"}
	if icon != "" {
		args = append(args, "--icon="+icon)
	}
	if wait {
		args = append(args, "--action=open=Open", "--wait")
	}
	return append(args, "--", title, escapeMarkup(body))
}

// hasAction reports whether notify-send has --action, which libnotify has from 0.7.9.
func hasAction(tool string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	out, _ := exec.CommandContext(ctx, tool, "--help").Output() // no help output means no action
	return strings.Contains(string(out), "--action")
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

// darwinNotifier returns the notifier next to self when self is in a bundle, else the notifier of GitSocial.app in the first of appDirs that has one, else "".
func darwinNotifier(self string, appDirs []string) string {
	var candidates []string
	if strings.Contains(self, ".app/Contents/MacOS/") {
		candidates = append(candidates, filepath.Join(filepath.Dir(self), notifierName))
	}
	for _, dir := range appDirs {
		candidates = append(candidates, filepath.Join(dir, "GitSocial.app", "Contents", "MacOS", notifierName))
	}
	for _, candidate := range candidates {
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

// escapeMarkup escapes the characters that notify-send reads as markup in the body.
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
