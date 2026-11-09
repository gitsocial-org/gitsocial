// desktop.go - Sends one desktop message through the notification tool of the OS.
package desktop

import (
	"context"
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

// host is what a sender reads from the machine: its own executable, the directories that can hold GitSocial.app and the timeout.
type host struct {
	self    string
	appDirs []string
	timeout time.Duration
}

// currentHost returns the host of this process.
func currentHost() host {
	self, _ := os.Executable() // an empty path finds no bundle of its own
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	h := host{self: self, timeout: sendTimeout}
	if home, err := os.UserHomeDir(); err == nil {
		h.appDirs = append(h.appDirs, filepath.Join(home, "Applications"))
	}
	h.appDirs = append(h.appDirs, "/Applications")
	return h
}

// Send shows each message, whose click opens its URL when the OS sender can open one; a missing tool is not an error.
func Send(messages []notifications.DesktopMessage) error {
	return send(currentHost(), messages)
}

// send runs the sender of the OS for a batch of messages; a failure of one message does not stop the others.
func send(h host, messages []notifications.DesktopMessage) error {
	var errs []error
	for _, m := range messages {
		switch runtime.GOOS {
		case "darwin":
			errs = append(errs, darwinSend(h, m))
		case "windows":
			errs = append(errs, run(h.timeout, lookPath("powershell"), []string{"-NoProfile", "-NonInteractive", "-Command", windowsScript}, []string{windowsTitleEnv + "=" + m.Title, windowsBodyEnv + "=" + m.Body}))
		default:
			errs = append(errs, run(h.timeout, lookPath("notify-send"), []string{"--", m.Title, escapeMarkup(m.Body)}, nil))
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
