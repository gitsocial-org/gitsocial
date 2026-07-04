// main.go - streams `go test -json` events as per-test progress (stdlib only)
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// modulePrefix is trimmed from package paths for compact display.
const modulePrefix = "github.com/gitsocial-org/gitsocial/"

// ANSI escape codes, applied only when stdout is a terminal.
const (
	colGreen = "\x1b[32m"
	colRed   = "\x1b[31m"
	colDim   = "\x1b[2m"
	colBold  = "\x1b[1m"
	colReset = "\x1b[0m"
)

// event mirrors the test2json record emitted by `go test -json`.
type event struct {
	Action     string
	Package    string
	Test       string
	Elapsed    float64
	Output     string
	ImportPath string
}

// topTest accumulates a top-level test and its rolled-up subtests.
type topTest struct {
	pkg      string
	name     string
	elapsed  float64
	subTotal int
	subFail  int
	output   []string
}

// failure holds a failed test's label and captured output for end replay.
type failure struct {
	label  string
	output []string
}

// state carries mutable run state threaded through the handlers.
type state struct {
	color     bool
	tops      map[string]*topTest
	pkgOut    map[string][]string
	pkgFailed map[string]bool
	failures  []failure
	passed    int
	failed    int
	skipped   int
	anyFail   bool
}

// main reads test2json from stdin, streams progress, and exits non-zero on failure.
func main() {
	st := &state{
		color:     isTerminal(os.Stdout),
		tops:      map[string]*topTest{},
		pkgOut:    map[string][]string{},
		pkgFailed: map[string]bool{},
	}
	start := time.Now()
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		text := scanner.Text()
		var ev event
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			// Non-JSON line (plain build error, etc.) — pass through verbatim.
			fmt.Println(text)
			continue
		}
		handle(st, &ev)
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "testfmt: read error: %v\n", err)
		st.anyFail = true
	}
	replayFailures(st)
	printSummary(st, time.Since(start))
	if st.anyFail || st.failed > 0 {
		os.Exit(1)
	}
}

// handle dispatches a single event to test-level or package-level handling.
func handle(st *state, ev *event) {
	switch ev.Action {
	case "build-output":
		fmt.Print(ev.Output)
		return
	case "build-fail":
		path := ev.ImportPath
		if path == "" {
			path = ev.Package
		}
		fmt.Println(paint(st, colRed, "✗ "+shortPkg(path)+" [build failed]"))
		st.anyFail = true
		return
	}
	if ev.Test != "" {
		handleTest(st, ev)
		return
	}
	handlePackage(st, ev)
}

// handleTest rolls subtests into their top-level parent and streams completions.
func handleTest(st *state, ev *event) {
	top := topName(ev.Test)
	key := ev.Package + "\x00" + top
	tt := st.tops[key]
	if tt == nil {
		tt = &topTest{pkg: ev.Package, name: top}
		st.tops[key] = tt
	}
	isSub := strings.Contains(ev.Test, "/")
	switch ev.Action {
	case "output":
		tt.output = append(tt.output, ev.Output)
	case "pass":
		if isSub {
			tt.subTotal++
			return
		}
		tt.elapsed = ev.Elapsed
		st.passed++
		fmt.Println(topLine(st, tt, "pass"))
		delete(st.tops, key)
	case "skip":
		if isSub {
			tt.subTotal++
			return
		}
		tt.elapsed = ev.Elapsed
		st.skipped++
		fmt.Println(topLine(st, tt, "skip"))
		delete(st.tops, key)
	case "fail":
		if isSub {
			tt.subTotal++
			tt.subFail++
			return
		}
		tt.elapsed = ev.Elapsed
		st.failed++
		fmt.Println(topLine(st, tt, "fail"))
		st.pkgFailed[ev.Package] = true
		st.failures = append(st.failures, failure{
			label:  shortPkg(tt.pkg) + " " + tt.name,
			output: tt.output,
		})
		delete(st.tops, key)
	}
}

// handlePackage prints per-package results and captures build/setup failures.
func handlePackage(st *state, ev *event) {
	switch ev.Action {
	case "output":
		st.pkgOut[ev.Package] = append(st.pkgOut[ev.Package], ev.Output)
	case "pass":
		fmt.Println(pkgLine(st, "ok", ev.Package, ev.Elapsed))
		delete(st.pkgOut, ev.Package)
	case "skip":
		// Package with no test files — stay quiet.
		delete(st.pkgOut, ev.Package)
	case "fail":
		fmt.Println(pkgLine(st, "FAIL", ev.Package, ev.Elapsed))
		st.anyFail = true
		if !st.pkgFailed[ev.Package] {
			// No test reported a failure — this is a build/setup failure; keep its output.
			st.failures = append(st.failures, failure{
				label:  shortPkg(ev.Package) + " [package]",
				output: st.pkgOut[ev.Package],
			})
		}
		delete(st.pkgOut, ev.Package)
	}
}

// topLine formats a completed top-level test line with subtest rollup.
func topLine(st *state, tt *topTest, status string) string {
	glyph, code := statusStyle(status)
	extra := ""
	if tt.subTotal > 0 {
		if tt.subFail > 0 {
			extra = fmt.Sprintf(", %d subtests, %d failed", tt.subTotal, tt.subFail)
		} else {
			extra = fmt.Sprintf(", %d subtests", tt.subTotal)
		}
	}
	line := fmt.Sprintf("%s %s %s (%.1fs%s)", glyph, shortPkg(tt.pkg), tt.name, tt.elapsed, extra)
	return paint(st, code, line)
}

// pkgLine formats a per-package result line.
func pkgLine(st *state, status, pkg string, elapsed float64) string {
	code := colDim
	if status == "FAIL" {
		code = colRed
	}
	return paint(st, code, fmt.Sprintf("%s %s (%.1fs)", status, shortPkg(pkg), elapsed))
}

// replayFailures reprints all failed tests' captured output at the end.
func replayFailures(st *state) {
	if len(st.failures) == 0 {
		return
	}
	fmt.Println()
	fmt.Println(paint(st, colBold+colRed, "--- FAILURES ---"))
	for _, f := range st.failures {
		fmt.Println(paint(st, colRed, "✗ "+f.label))
		for _, o := range f.output {
			fmt.Print(o)
		}
	}
}

// printSummary prints the final counts and wall time.
func printSummary(st *state, wall time.Duration) {
	line := fmt.Sprintf("%d passed, %d failed, %d skipped in %.1fs",
		st.passed, st.failed, st.skipped, wall.Seconds())
	code := colGreen
	if st.failed > 0 || st.anyFail {
		code = colRed
	}
	fmt.Println(paint(st, code, line))
}

// statusStyle maps a status to its glyph and color code.
func statusStyle(status string) (string, string) {
	switch status {
	case "pass":
		return "✓", colGreen
	case "fail":
		return "✗", colRed
	default:
		return "○", colDim
	}
}

// topName returns the top-level test name (part before the first "/").
func topName(test string) string {
	if i := strings.IndexByte(test, '/'); i >= 0 {
		return test[:i]
	}
	return test
}

// shortPkg trims the module prefix for compact package display.
func shortPkg(pkg string) string {
	return strings.TrimPrefix(pkg, modulePrefix)
}

// paint wraps s in an ANSI color code when color output is enabled.
func paint(st *state, code, s string) string {
	if !st.color {
		return s
	}
	return code + s + colReset
}

// isTerminal reports whether f is a character device (a terminal).
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
