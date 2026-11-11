// click.go - The handler of the click socket: it brings the terminal tab of the TUI to the front on macOS and gives the click to the model.
package tui

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/library/core/log"
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

// activateTerminal returns the function that brings the terminal of this process to the front on macOS: the tab of tty in a terminal with a focus script, else the app; on another OS or with no bundle id it does nothing.
func activateTerminal(goos, tty string) func() {
	if goos != "darwin" {
		return func() {}
	}
	return func() {
		id := os.Getenv("__CFBundleIdentifier")
		if id == "" {
			return
		}
		if script := focusScript(id, tty); script != "" && tty != "" {
			out, err := exec.Command("osascript", "-e", script).CombinedOutput()
			if err == nil {
				return
			}
			log.Debug("select the terminal tab", "bundle", id, "error", err, "output", strings.TrimSpace(string(out)))
		}
		if err := exec.Command("open", "-b", id).Run(); err != nil {
			log.Debug("activate terminal", "error", err)
		}
	}
}
