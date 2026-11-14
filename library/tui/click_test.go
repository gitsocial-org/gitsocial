// click_test.go - Tests for the handler of the click socket: the lines it takes, the order of its steps and the script that selects its tab.
package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gitsocial-org/gitsocial/library/tui/tuicore"
)

// clickRecorder returns a handler whose activation and sends append to one ordered list, and that list.
func clickRecorder() (func(string), func() []string) {
	var mu sync.Mutex
	var steps []string
	record := func(step string) {
		mu.Lock()
		defer mu.Unlock()
		steps = append(steps, step)
	}
	handle := clickHandler(func() { record("activate") }, func(msg tea.Msg) {
		if click, ok := msg.(tuicore.ClickMsg); ok {
			record("send " + click.Ref)
		}
	})
	return handle, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), steps...)
	}
}

func TestClickHandler_rejectsOtherLines(t *testing.T) {
	handle, steps := clickRecorder()
	for _, line := range []string{"", "open-x", "close #commit:0123456789ab", "open #branch:main", "open #commit:xyz", "open https://example.com/owner/repo", "OPEN #commit:0123456789ab"} {
		handle(line)
	}
	time.Sleep(100 * time.Millisecond)
	if got := steps(); len(got) != 0 {
		t.Errorf("steps for lines that are not open with an empty or commit ref = %q, want none", got)
	}
}

func TestClickHandler_activatesFirst(t *testing.T) {
	ref := "https://example.com/owner/repo#commit:0123456789ab@main"
	for _, c := range []struct{ line, ref string }{{"open " + ref, ref}, {"open ", ""}, {"open", ""}} {
		handle, steps := clickRecorder()
		handle(c.line)
		deadline := time.Now().Add(5 * time.Second)
		for len(steps()) < 2 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if got := steps(); len(got) != 2 || got[0] != "activate" || got[1] != "send "+c.ref {
			t.Errorf("steps for %q = %q, want activate, then send of the ref", c.line, got)
		}
	}
}

func TestFocusScript_byTerminal(t *testing.T) {
	for _, bundle := range []string{"com.mitchellh.ghostty", "com.apple.Terminal", "com.googlecode.iterm2"} {
		script := focusScript(bundle, "/dev/ttys007")
		if !strings.Contains(script, `application id "`+bundle+`"`) || !strings.Contains(script, `is "/dev/ttys007"`) || !strings.Contains(script, "\nactivate\n") {
			t.Errorf("focusScript(%s) = %q, want a script for the app that names the tty and activates the app", bundle, script)
		}
	}
	if script := focusScript("com.example.terminal", "/dev/ttys007"); script != "" {
		t.Errorf("focusScript of an unknown terminal = %q, want none", script)
	}
}

func TestActivateTerminal_otherOS(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stub tools are shell scripts")
	}
	bin := t.TempDir()
	ran := filepath.Join(bin, "ran")
	for _, tool := range []string{"osascript", "open"} {
		if err := os.WriteFile(filepath.Join(bin, tool), []byte("#!/bin/sh\necho "+tool+" >> '"+ran+"'\n"), 0o755); err != nil {
			t.Fatalf("write stub: %v", err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("__CFBundleIdentifier", "com.apple.Terminal")
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	activateTerminal("linux", "/dev/pts/3")()
	if _, err := os.Stat(ran); err == nil {
		t.Error("the activation on linux ran a tool, want none")
	}
	activateTerminal("darwin", "/dev/ttys007")()
	if _, err := os.Stat(ran); err != nil {
		t.Errorf("the activation on darwin ran no tool: %v", err)
	}
}

// stubTerminalTools puts on PATH a tmux that prints the session and clients, an osascript that succeeds only for a script with found, and an open; each appends its name and arguments to the returned file.
func stubTerminalTools(t *testing.T, clients, found string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub tools are shell scripts")
	}
	bin := t.TempDir()
	ran := filepath.Join(bin, "ran")
	record := "#!/bin/sh\nprintf '%s %s\\n' \"$(basename \"$0\")\" \"$*\" | tr '\\n' ' ' >> '" + ran + "'\necho >> '" + ran + "'\n"
	scripts := map[string]string{
		"tmux":      record + "case \"$*\" in\n*display-message*) printf '%s\\n' '$0' ;;\n*list-clients*) printf '%s' '" + clients + "' ;;\nesac\n",
		"osascript": record + "case \"$*\" in\n*'" + found + "'*) exit 0 ;;\nesac\nexit 1\n",
		"open":      record,
	}
	for tool, script := range scripts {
		if err := os.WriteFile(filepath.Join(bin, tool), []byte(script), 0o755); err != nil {
			t.Fatalf("write stub: %v", err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
	return ran
}

// toolLines returns the lines of the file that the stub tools wrote, or none.
func toolLines(t *testing.T, ran string) []string {
	t.Helper()
	data, err := os.ReadFile(ran)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func TestActivateTerminal_tmux(t *testing.T) {
	ran := stubTerminalTools(t, "5 /dev/ttys001\n9 /dev/ttys004\n", `tell application id "com.apple.Terminal"`)
	t.Setenv("__CFBundleIdentifier", "com.mitchellh.ghostty")
	t.Setenv("TMUX", "/private/tmp/tmux-501/default,4242,0")
	t.Setenv("TMUX_PANE", "%3")
	activateTerminal("darwin", "/dev/ttys020")()
	lines := toolLines(t, ran)
	want := []string{
		"tmux -S /private/tmp/tmux-501/default select-window -t %3",
		"tmux -S /private/tmp/tmux-501/default select-pane -t %3",
		"tmux -S /private/tmp/tmux-501/default display-message",
		"tmux -S /private/tmp/tmux-501/default list-clients",
		`osascript -e if application id "com.mitchellh.ghostty" is not running`,
		`osascript -e if application id "com.apple.Terminal" is not running`,
	}
	if len(lines) != len(want) {
		t.Fatalf("tools = %q, want %d commands and no open", lines, len(want))
	}
	for i, prefix := range want {
		if !strings.HasPrefix(lines[i], prefix) {
			t.Errorf("command %d = %q, want it to start with %q", i, lines[i], prefix)
		}
	}
	for _, line := range lines[4:] {
		if !strings.Contains(line, `"/dev/ttys004"`) || strings.Contains(line, "/dev/ttys020") {
			t.Errorf("script = %q, want the tty of the tmux client that was active last", line)
		}
	}
}

func TestActivateTerminal_tmuxNoClient(t *testing.T) {
	ran := stubTerminalTools(t, "", "no script succeeds")
	t.Setenv("__CFBundleIdentifier", "com.mitchellh.ghostty")
	t.Setenv("TMUX", "/private/tmp/tmux-501/default,4242,0")
	t.Setenv("TMUX_PANE", "%3")
	activateTerminal("darwin", "/dev/ttys020")()
	lines := toolLines(t, ran)
	if len(lines) != 6 || !strings.HasPrefix(lines[4], "osascript") || !strings.Contains(lines[4], `"/dev/ttys020"`) || !strings.HasPrefix(lines[5], "open -b com.mitchellh.ghostty") {
		t.Errorf("tools = %q, want the tmux commands, one script for the tty of the process and open of the app", lines)
	}
}

func TestTabBundles_order(t *testing.T) {
	for id, want := range map[string]string{
		"":                      "com.mitchellh.ghostty com.apple.Terminal com.googlecode.iterm2",
		"com.googlecode.iterm2": "com.googlecode.iterm2 com.mitchellh.ghostty com.apple.Terminal",
		"com.example.terminal":  "com.example.terminal com.mitchellh.ghostty com.apple.Terminal com.googlecode.iterm2",
	} {
		if got := strings.Join(tabBundles(id), " "); got != want {
			t.Errorf("tabBundles(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestRunningOnly_firstLine(t *testing.T) {
	got := runningOnly("com.apple.Terminal", "tell application id \"com.apple.Terminal\" to activate")
	first, rest, _ := strings.Cut(got, "\n")
	if first != `if application id "com.apple.Terminal" is not running then error "not running"` || rest != "tell application id \"com.apple.Terminal\" to activate" {
		t.Errorf("runningOnly = %q, want the guard line and then the script", got)
	}
}
