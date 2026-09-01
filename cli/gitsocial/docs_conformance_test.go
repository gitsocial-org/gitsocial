// docs_conformance_test.go - Asserts documentation/CLI.md matches the binary:
// global flags, exit codes, and the environment variables it advertises.
package main

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

const cliDocPath = "../../documentation/CLI.md"

// docSection returns the lines of the named "## " section of a markdown file.
func docSection(t *testing.T, path, heading string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var lines []string
	inSection := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "## ") {
			inSection = strings.TrimSpace(strings.TrimPrefix(line, "## ")) == heading
			continue
		}
		if inSection {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		t.Fatalf("%s: no %q section, or the layout changed", path, heading)
	}
	return lines
}

var backtickedToken = regexp.MustCompile("`([^`]+)`")

func TestGlobalFlagsMatchDocumentation(t *testing.T) {
	documented := map[string]bool{}
	for _, line := range docSection(t, cliDocPath, "Command Structure") {
		for _, name := range docFlagNames(line) {
			documented[name] = true
		}
	}
	if len(documented) == 0 {
		t.Fatalf("%s: no global flags parsed from the Command Structure section", cliDocPath)
	}

	registered := map[string]bool{"help": true, "version": true}
	buildRootCmd().PersistentFlags().VisitAll(func(f *pflag.Flag) { registered[f.Name] = true })

	for name := range documented {
		if !registered[name] {
			t.Errorf("--%s is documented as a global flag but is not registered", name)
		}
	}
	for name := range registered {
		if name != "help" && name != "version" && !documented[name] {
			t.Errorf("--%s is a registered global flag but is not documented in %s", name, cliDocPath)
		}
	}
}

// docFlagNames pulls the long flag names out of a markdown bullet line such as
// "- `--list, -l` - Fetch only repos from this list".
func docFlagNames(line string) []string {
	if !strings.HasPrefix(strings.TrimSpace(line), "- `-") {
		return nil
	}
	match := backtickedToken.FindStringSubmatch(line)
	if match == nil {
		return nil
	}
	var names []string
	for _, field := range strings.FieldsFunc(match[1], func(r rune) bool { return r == ',' || r == ' ' }) {
		if strings.HasPrefix(field, "--") {
			names = append(names, strings.TrimPrefix(field, "--"))
		}
	}
	return names
}

func TestPerCommandFlagsMatchDocumentation(t *testing.T) {
	data, err := os.ReadFile(cliDocPath)
	if err != nil {
		t.Fatalf("read %s: %v", cliDocPath, err)
	}
	tree := walkCommands(buildRootCmd())
	path := ""
	checked := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "#") {
			path = ""
			if rest, ok := strings.CutPrefix(line, "### gitsocial "); ok {
				path = strings.TrimSpace(rest)
			}
			continue
		}
		cmd, ok := tree[path]
		if !ok {
			continue
		}
		for _, name := range docFlagNames(line) {
			checked++
			if cmd.Flags().Lookup(name) == nil && cmd.InheritedFlags().Lookup(name) == nil {
				t.Errorf("gitsocial %s: --%s is documented in %s but not registered", path, name, cliDocPath)
			}
		}
	}
	if checked == 0 {
		t.Fatalf("%s: no per-command flags parsed, the layout changed", cliDocPath)
	}
}

func TestExitCodesMatchDocumentation(t *testing.T) {
	want := map[int]string{
		ExitSuccess:     "Success",
		ExitError:       "General error",
		ExitInvalidArgs: "Invalid arguments",
		ExitPermission:  "Permission denied",
		ExitNetwork:     "Network error",
		ExitNotRepo:     "Not a git repository",
	}
	got := map[int]string{}
	for _, line := range docSection(t, cliDocPath, "Exit Codes") {
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		if len(cells) != 2 {
			continue
		}
		code, err := strconv.Atoi(strings.TrimSpace(cells[0]))
		if err != nil {
			continue
		}
		got[code] = strings.TrimSpace(cells[1])
	}

	for code, meaning := range want {
		if got[code] != meaning {
			t.Errorf("exit code %d: %s documents %q, want %q", code, cliDocPath, got[code], meaning)
		}
	}
	for code := range got {
		if _, ok := want[code]; !ok {
			t.Errorf("exit code %d is documented in %s but no constant defines it", code, cliDocPath)
		}
	}
}

// envLookupsInSource collects every environment variable name read through a
// literal os.Getenv call across the CLI and the library.
func envLookupsInSource(t *testing.T) map[string]bool {
	t.Helper()
	pattern := regexp.MustCompile(`os\.(?:Getenv|LookupEnv)\("([A-Z][A-Z0-9_]*)"\)`)
	names := map[string]bool{}
	for _, root := range []string{"..", "../../library"} {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, match := range pattern.FindAllStringSubmatch(string(data), -1) {
				names[match[1]] = true
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	return names
}

func TestDocumentedEnvVarsAreRead(t *testing.T) {
	read := envLookupsInSource(t)
	// Registry keys at ScopeEnv are read through a dynamic os.Getenv(key).
	read["GITSOCIAL_PPROF"] = true
	read["GITSOCIAL_PERSONAL_REPO"] = true

	var missing []string
	for _, line := range docSection(t, cliDocPath, "Environment Variables") {
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		if len(cells) != 2 {
			continue
		}
		for _, match := range backtickedToken.FindAllStringSubmatch(cells[0], -1) {
			for _, name := range strings.Split(match[1], " / ") {
				name = strings.TrimSpace(name)
				if name != "" && !read[name] {
					missing = append(missing, name)
				}
			}
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("documented in %s but never read by the code: %v", cliDocPath, missing)
	}
}
