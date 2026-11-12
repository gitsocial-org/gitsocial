// click.go - The handler of the click socket: it brings the terminal tab of the TUI to the front on macOS and gives the click to the model.
package tui

import (
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/library/core/log"
	"github.com/gitsocial-org/gitsocial/library/core/notifications/desktop"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// clickHandler returns the handler of a line on the click socket: for open with an empty or a commit ref, it runs activate, then sends a ClickMsg from its own goroutine.
func clickHandler(activate func(), send func(tea.Msg)) func(line string) {
	return func(line string) {
		at := time.Now()
		ref, ok := clickRef(line)
		if !ok {
			return
		}
		activate()
		go send(tuicore.ClickMsg{Ref: ref, At: at})
	}
}

// clickRef returns the ref of a line and whether the line is open with an empty ref or a commit ref.
func clickRef(line string) (string, bool) {
	verb, ref, _ := strings.Cut(line, " ")
	if verb != "open" {
		return "", false
	}
	if ref == "" {
		return "", true
	}
	parsed := protocol.ParseRef(ref)
	return ref, parsed.Type == protocol.RefTypeCommit && parsed.Value != ""
}

// focusScript returns the AppleScript that selects the tab of tty in the terminal app of a bundle id, or "" for an app with none.
func focusScript(bundle, tty string) string {
	switch bundle {
	case "com.mitchellh.ghostty":
		return `tell application id "` + bundle + `" to focus (first terminal whose tty is "` + tty + `")`
	case "com.apple.Terminal":
		return `tell application id "` + bundle + `"
repeat with w in windows
repeat with t in tabs of w
if tty of t is "` + tty + `" then
set selected of t to true
set frontmost of w to true
activate
return
end if
end repeat
end repeat
error "no tab has the tty"
end tell`
	case "com.googlecode.iterm2":
		return `tell application id "` + bundle + `"
repeat with w in windows
repeat with t in tabs of w
repeat with s in sessions of t
if tty of s is "` + tty + `" then
select w
select t
select s
activate
return
end if
end repeat
end repeat
end repeat
error "no session has the tty"
end tell`
	}
	return ""
}

// processTTY returns the device path of the terminal of this process, or "".
func processTTY() string {
	out, err := exec.Command("ps", "-o", "tty=", "-p", strconv.Itoa(os.Getpid())).Output()
	name := strings.TrimSpace(string(out))
	if err != nil || name == "" || name == "??" {
		return ""
	}
	return "/dev/" + name
}

// tabBundles returns the terminal apps to search for the tab of a tmux client: the app of this process first, then each app with a focus script.
func tabBundles(id string) []string {
	bundles := []string{}
	for _, bundle := range []string{id, "com.mitchellh.ghostty", "com.apple.Terminal", "com.googlecode.iterm2"} {
		if bundle != "" && !slices.Contains(bundles, bundle) {
			bundles = append(bundles, bundle)
		}
	}
	return bundles
}

// runningOnly returns script with a first line that stops it when the app of bundle does not run, so the script starts no app.
func runningOnly(bundle, script string) string {
	return `if application id "` + bundle + `" is not running then error "not running"` + "\n" + script
}

// activateTerminal returns the function that brings the terminal of this process to the front on macOS: the tab of tty in a terminal with a focus script, else the app; inside tmux it selects the tmux window first and looks for the tab of the tmux client in each terminal that runs. On another OS it does nothing.
func activateTerminal(goos, tty string) func() {
	if goos != "darwin" {
		return func() {}
	}
	return func() {
		id := os.Getenv("__CFBundleIdentifier")
		device, bundles := tty, []string{id}
		if socket, pane, ok := desktop.TmuxPane(os.Getenv("TMUX"), os.Getenv("TMUX_PANE")); ok {
			desktop.TmuxSelect(socket, pane)
			if client := desktop.TmuxClientTTY(socket, pane); client != "" {
				device, bundles = client, tabBundles(id)
			}
		}
		for _, bundle := range bundles {
			script := focusScript(bundle, device)
			if script == "" || device == "" {
				continue
			}
			out, err := exec.Command("osascript", "-e", runningOnly(bundle, script)).CombinedOutput()
			if err == nil {
				return
			}
			log.Debug("select the terminal tab", "bundle", bundle, "error", err, "output", strings.TrimSpace(string(out)))
		}
		if id == "" {
			return
		}
		if err := exec.Command("open", "-b", id).Run(); err != nil {
			log.Debug("activate terminal", "error", err)
		}
	}
}
