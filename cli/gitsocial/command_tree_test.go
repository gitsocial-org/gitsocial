// command_tree_test.go - Walks the whole cobra command tree and asserts every
// command carries usage metadata, answers --help, and (for the read-only,
// zero-argument commands) emits parsable JSON under --json.
package main

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// jsonSkipPaths lists the zero-argument commands excluded from the --json walk:
// they block on a terminal, reach the network, or emit a non-JSON document by
// design. Anything not listed here is walked, so a new command is covered
// automatically.
var jsonSkipPaths = map[string]bool{
	"tui":              true, // takes over the terminal
	"rpc":              true, // blocks reading stdin
	"__git-remote-s3":  true, // spawned by git, speaks the helper protocol on stdin
	"fetch":            true, // network
	"push":             true, // network
	"mirror":           true, // network
	"import":           true, // network
	"import all":       true, // network
	"import pm":        true, // network
	"import release":   true, // network
	"import review":    true, // network
	"import social":    true, // network
	"personal init":    true, // writes the personal repo
	"personal sync":    true, // network
	"docs keybindings": true, // generates markdown; --json does not apply
}

// walkCommands returns every command below root keyed by its full path, with
// cobra's generated help and completion commands left out.
func walkCommands(root *cobra.Command) map[string]*cobra.Command {
	found := map[string]*cobra.Command{}
	var visit func(cmd *cobra.Command, prefix string)
	visit = func(cmd *cobra.Command, prefix string) {
		for _, sub := range cmd.Commands() {
			if sub.Name() == "help" || sub.Name() == cobra.ShellCompRequestCmd {
				continue
			}
			path := strings.TrimSpace(prefix + " " + sub.Name())
			found[path] = sub
			visit(sub, path)
		}
	}
	visit(root, "")
	return found
}

// acceptsNoArgs reports whether the command can be invoked with no positional
// arguments. A nil Args is cobra's "anything goes", which includes none.
func acceptsNoArgs(cmd *cobra.Command) bool {
	return cmd.Args == nil || cmd.Args(cmd, nil) == nil
}

// commandPaths returns the sorted paths of every command in the tree.
func commandPaths(t *testing.T) []string {
	t.Helper()
	paths := make([]string, 0, 200)
	for path := range walkCommands(buildRootCmd()) {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func TestCommandTreeMetadata(t *testing.T) {
	for path, cmd := range walkCommands(buildRootCmd()) {
		if strings.TrimSpace(cmd.Use) == "" {
			t.Errorf("%s: empty Use", path)
		}
		if strings.TrimSpace(cmd.Short) == "" {
			t.Errorf("%s: empty Short", path)
		}
	}
}

func TestCommandTreeHelp(t *testing.T) {
	for _, path := range commandPaths(t) {
		t.Run(path, func(t *testing.T) {
			// A fresh tree per case: the global flag vars are package level.
			root := buildRootCmd()
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			root.SetArgs(append(strings.Fields(path), "--help"))
			// main exits with ExitError exactly when Execute returns an error,
			// so a nil error here is an exit code of 0.
			if err := root.Execute(); err != nil {
				t.Fatalf("--help returned %v\n%s", err, out.String())
			}
			if !strings.Contains(out.String(), "Usage:") {
				t.Errorf("--help printed no usage block:\n%s", out.String())
			}
		})
	}
}

func TestCommandTreeJSONOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns the binary once per command")
	}
	dir := initCLITestRepo(t)
	cacheDir := t.TempDir()
	for _, ext := range []string{"social", "pm", "review", "release"} {
		if _, stderr, code := runCLI(t, dir, cacheDir, ext, "init"); code != 0 {
			t.Fatalf("%s init: exit %d\n%s", ext, code, stderr)
		}
	}

	tree := walkCommands(buildRootCmd())
	for _, path := range commandPaths(t) {
		cmd := tree[path]
		if jsonSkipPaths[path] || !cmd.Runnable() || !acceptsNoArgs(cmd) {
			continue
		}
		t.Run(path, func(t *testing.T) {
			stdout, stderr, code := runCLI(t, dir, cacheDir, append([]string{"--json"}, strings.Fields(path)...)...)
			if code < ExitSuccess || code > ExitNotRepo {
				t.Fatalf("--json exit %d, want a documented exit code\n%s%s", code, stdout, stderr)
			}
			if strings.TrimSpace(stdout) == "" {
				return
			}
			if !json.Valid([]byte(stdout)) {
				t.Errorf("--json output is not JSON (exit %d):\n%s", code, stdout)
			}
		})
	}
}
