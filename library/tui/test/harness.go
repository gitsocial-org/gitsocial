// harness.go - Headless TUI test driver for black-box testing through Bubbletea interface
package test

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	zone "github.com/lrstanley/bubblezone/v2"

	"github.com/gitsocial-org/gitsocial/tui"
	"github.com/gitsocial-org/gitsocial/tui/tuicore"
)

var zoneOnce sync.Once

// maxDrainDepth prevents infinite loops when draining commands
const maxDrainDepth = 50

// Harness wraps tui.Model for headless testing.
// It drives the model through Update/View without a real terminal.
type Harness struct {
	model        tui.Model
	t            *testing.T
	workdir      string
	cache        string
	width        int
	height       int
	SkippedExecN int // count of execMsg commands skipped (editor spawns, etc.)
}

// New creates a harness with a ready model at 120x40.
func New(t *testing.T, workdir, cacheDir string) *Harness {
	t.Helper()
	// Initialize bubblezone global manager (required by views that use zone.NewPrefix)
	zoneOnce.Do(func() { zone.NewGlobal() })
	model := tui.NewModel(workdir, cacheDir)
	model.SetHeadless(true)
	h := &Harness{
		model:   model,
		t:       t,
		workdir: workdir,
		cache:   cacheDir,
		width:   120,
		height:  40,
	}
	// Make the model ready by sending a window size message
	h.sendMsg(tea.WindowSizeMsg{Width: 120, Height: 40})
	// Run Init() and drain all startup commands
	cmd := h.model.Init()
	h.processCmds(cmd, 0)
	return h
}

// SetSize updates the terminal dimensions.
func (h *Harness) SetSize(w, h2 int) {
	h.width = w
	h.height = h2
	h.sendMsg(tea.WindowSizeMsg{Width: w, Height: h2})
}

// SendKey sends a single key press and drains resulting commands.
func (h *Harness) SendKey(key string) {
	msg := keyToMsg(key)
	h.sendMsg(msg)
}

// SendKeys sends multiple key presses sequentially.
func (h *Harness) SendKeys(keys ...string) {
	for _, key := range keys {
		h.SendKey(key)
	}
}

// Navigate sends a NavigateMsg to route to a specific path and drains.
func (h *Harness) Navigate(path string) {
	h.sendMsg(tuicore.NavigateMsg{
		Location: tuicore.Location{Path: path},
		Action:   tuicore.NavPush,
	})
}

// NavigateTo sends a NavigateMsg with params and drains.
func (h *Harness) NavigateTo(loc tuicore.Location) {
	h.sendMsg(tuicore.NavigateMsg{
		Location: loc,
		Action:   tuicore.NavPush,
	})
}

// DrainCmds re-runs Init() and drains — useful after Navigate to load view data.
func (h *Harness) DrainCmds() {
	// Activate the current view by sending a no-op navigate to trigger data loading
	cmd := h.model.Init()
	h.processCmds(cmd, 0)
}

// Rendered returns the current View() output.
func (h *Harness) Rendered() string {
	return h.model.View().Content
}

// CurrentPath returns the router's current path by inspecting the model.
func (h *Harness) CurrentPath() string {
	return h.model.Router().Location().Path
}

// CurrentContext returns the context for the current path.
func (h *Harness) CurrentContext() tuicore.Context {
	return tuicore.GetContextForPath(h.CurrentPath())
}

// BindingsForContext returns all registered bindings for a context.
func (h *Harness) BindingsForContext(ctx tuicore.Context) []tuicore.Binding {
	return h.model.Registry().ForContext(ctx)
}

// sendMsg feeds a message through Update and drains resulting commands.
func (h *Harness) sendMsg(msg tea.Msg) {
	m, cmd := h.model.Update(msg)
	h.model = toModel(m)
	h.processCmds(cmd, 0)
}

// processCmds executes tea.Cmd synchronously and feeds results back through Update.
func (h *Harness) processCmds(cmd tea.Cmd, depth int) {
	if cmd == nil || depth > maxDrainDepth {
		return
	}
	// Execute command with timeout to handle blocking commands (cursor blink, timers)
	msg := execCmd(cmd)
	if msg == nil {
		return
	}
	// tea.BatchMsg is []tea.Cmd — process each sub-command
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			h.processCmds(c, depth+1)
		}
		return
	}
	// Skip side-effectful messages that can't run headless
	skip, isExec := shouldSkipMsg(msg)
	if isExec {
		h.SkippedExecN++
	}
	if skip {
		return
	}
	// Feed message back through Update
	m, next := h.model.Update(msg)
	h.model = toModel(m)
	h.processCmds(next, depth+1)
}

// shouldSkipCmd returns true for commands known to block forever in headless mode.
func shouldSkipCmd(cmd tea.Cmd) bool {
	name := runtime.FuncForPC(reflect.ValueOf(cmd).Pointer()).Name()
	return strings.Contains(name, "BlinkCmd") ||
		strings.Contains(name, "Blink") ||
		strings.Contains(name, "blink") ||
		strings.Contains(name, "startFetch")
}

// execCmd runs a tea.Cmd synchronously with a timeout, skipping known blockers
// and any command that takes longer than the budget (likely a timer/sleep).
//
// Budget is generous enough to cover real work — view Activate functions that
// do a few SQL queries and git invocations. Each `git` invocation is ~15ms on
// modern hardware (process start + .git lookup), so a path with 2-3 git calls
// + a SQL query lands around 50-100ms; we want that to complete, not be
// classified as a timer. Tighten this only if test runtime balloons; loosen
// only if real Activate work starts timing out.
func execCmd(cmd tea.Cmd) tea.Msg {
	if shouldSkipCmd(cmd) {
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		return msg
	case <-time.After(500 * time.Millisecond):
		return nil
	}
}

// toModel converts tea.Model back to tui.Model, handling both value and pointer receivers.
func toModel(m tea.Model) tui.Model {
	switch v := m.(type) {
	case tui.Model:
		return v
	case *tui.Model:
		return *v
	default:
		panic(fmt.Sprintf("unexpected model type: %T", m))
	}
}

// shouldSkipMsg returns true for messages that shouldn't be processed headlessly.
func shouldSkipMsg(msg tea.Msg) (bool, bool) {
	typeName := fmt.Sprintf("%T", msg)
	switch {
	case typeName == "tea.QuitMsg":
		return true, false
	case strings.Contains(typeName, "setWindowTitleMsg"):
		return true, false
	case strings.Contains(typeName, "execMsg"):
		return true, true
	case strings.Contains(typeName, "cursor.BlinkMsg"):
		return true, false
	}
	return false, false
}

// keyToMsg converts a key string to a tea.KeyPressMsg.
func keyToMsg(key string) tea.KeyPressMsg {
	switch key {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	case "ctrl+d":
		return tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
	case "ctrl+u":
		return tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace}
	case "home":
		return tea.KeyPressMsg{Code: tea.KeyHome}
	case "end":
		return tea.KeyPressMsg{Code: tea.KeyEnd}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	default:
		runes := []rune(key)
		if len(runes) == 1 {
			return tea.KeyPressMsg{Code: runes[0], Text: key}
		}
		return tea.KeyPressMsg{Code: runes[0], Text: key}
	}
}
