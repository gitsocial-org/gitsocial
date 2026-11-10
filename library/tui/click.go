// click.go - The handler of the click socket: it brings the terminal to the front and gives the click to the model.
package tui

import (
	"os"
	"os/exec"
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

// activateTerminal brings the terminal app that started this process to the front; with no bundle id it does nothing.
func activateTerminal() {
	id := os.Getenv("__CFBundleIdentifier")
	if id == "" {
		return
	}
	if err := exec.Command("open", "-b", id).Run(); err != nil {
		log.Debug("activate terminal", "error", err)
	}
}
