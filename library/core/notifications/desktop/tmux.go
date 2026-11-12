// tmux.go - Selects the tmux window of the TUI and reads the environment of the tmux client that was active last.
package desktop

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/log"
)

// tmuxSocket returns the socket of the tmux server from the value of TMUX, or "".
func tmuxSocket(value string) string {
	socket, _, _ := strings.Cut(value, ",")
	return socket
}

// tmuxPane reports whether value is a tmux pane id: "%" and one or more digits.
func tmuxPane(value string) bool {
	digits, ok := strings.CutPrefix(value, "%")
	return ok && digits != "" && strings.Trim(digits, "0123456789") == ""
}

// tmuxSelect selects the window and the pane of pane on the tmux server at socket; a failure is logged and not returned.
func tmuxSelect(h host, socket, pane string) {
	tool := lookPath("tmux")
	for _, command := range []string{"select-window", "select-pane"} {
		if err := run(h.timeout, tool, []string{"-S", socket, command, "-t", pane}, nil); err != nil {
			log.Debug("tmux select failed", "command", command, "error", err)
		}
	}
}

// tmuxClientEnv returns a lookup of the environment of the client of the session of pane with the highest activity, or a lookup that gives "" for each name when tmux, the client or its environment is not available.
func tmuxClientEnv(h host, socket, pane string) func(string) string {
	none := func(string) string { return "" }
	tool := lookPath("tmux")
	if tool == "" {
		log.Debug("no tmux tool for the terminal of the click")
		return none
	}
	session, err := output(h.timeout, tool, "-S", socket, "display-message", "-p", "-t", pane, "#{session_id}")
	if err != nil {
		log.Debug("tmux session of the pane failed", "error", err)
		return none
	}
	clients, err := output(h.timeout, tool, "-S", socket, "list-clients", "-t", strings.TrimSpace(session), "-F", "#{client_activity} #{client_pid}")
	if err != nil {
		log.Debug("tmux client list failed", "error", err)
		return none
	}
	pid, latest := "", int64(-1)
	for _, line := range strings.Split(clients, "\n") {
		activity, candidate, _ := strings.Cut(strings.TrimSpace(line), " ")
		if n, err := strconv.ParseInt(activity, 10, 64); err == nil && n > latest {
			pid, latest = candidate, n
		}
	}
	if n, err := strconv.Atoi(pid); err != nil || n < 1 {
		log.Debug("no tmux client with a pid", "pid", pid)
		return none
	}
	data, err := os.ReadFile(filepath.Join(h.procDir, pid, "environ"))
	if err != nil {
		log.Debug("read tmux client environment failed", "error", err)
		return none
	}
	env := map[string]string{}
	for _, entry := range strings.Split(string(data), "\x00") {
		if name, value, ok := strings.Cut(entry, "="); ok {
			env[name] = value
		}
	}
	return func(name string) string { return env[name] }
}

// output runs a tool, kills it after the timeout and returns its standard output.
func output(timeout time.Duration, path string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, args...).Output()
	if err != nil {
		return "", fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return string(out), nil
}
