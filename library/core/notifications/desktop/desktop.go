// desktop.go - Sends one desktop message through the notification tool of the OS.
package desktop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/notifications"
)

const sendTimeout = 5 * time.Second

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

// toolName returns the notification tool of the current OS.
func toolName() string {
	switch runtime.GOOS {
	case "darwin":
		return "osascript"
	case "windows":
		return "powershell"
	}
	return "notify-send"
}

// toolArgs returns the arguments of the tool; the title and the body are never part of a script.
func toolArgs(title, body string) []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"-e", "on run argv", "-e", "display notification (item 2 of argv) with title (item 1 of argv)", "-e", "end run", title, body}
	case "windows":
		return []string{"-NoProfile", "-NonInteractive", "-Command", windowsScript}
	}
	return []string{"--", title, escapeMarkup(body)}
}

// escapeMarkup escapes the characters that notify-send reads as markup in the body.
func escapeMarkup(text string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(text)
}

// Send shows each message; a missing tool is not an error, and a failure of one message does not stop the others.
func Send(messages []notifications.DesktopMessage) error {
	return send(sendTimeout, messages)
}

// send runs the tool for each message.
func send(timeout time.Duration, messages []notifications.DesktopMessage) error {
	path, err := exec.LookPath(toolName())
	if err != nil {
		return nil
	}
	var errs []error
	for _, m := range messages {
		errs = append(errs, run(timeout, path, m.Title, m.Body))
	}
	return errors.Join(errs...)
}

// run runs the tool for one message and kills it after the timeout.
func run(timeout time.Duration, path, title, body string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, toolArgs(title, body)...)
	if runtime.GOOS == "windows" {
		cmd.Env = append(os.Environ(), windowsTitleEnv+"="+title, windowsBodyEnv+"="+body)
	}
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("desktop notification: %w", ctx.Err())
		}
		return fmt.Errorf("desktop notification: %w", err)
	}
	return nil
}
