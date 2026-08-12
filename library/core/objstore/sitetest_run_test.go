//go:build sitetest

// sitetest_run_test.go - runs the browser-side site test battery
// (scripts/site-test.sh) from `go test`.
//
// The verifier suites under sitetest/ cover contracts no Go test can see: the
// page-entry boot reveal, prism deferral and its deadline, the browser pack
// reader, absent-key probes, and the per-route request budgets. They were
// reachable only from a shell script, so `go test ./...` stayed green with all
// of it broken. This wires them into the same command.
//
// Behind a build tag rather than on by default because one run builds the whole
// fixture (the CLI binary, ten buckets on a local S3 server) and takes minutes:
//
//	go test -tags sitetest -timeout 30m ./library/core/objstore/
package objstore

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestSiteTestBattery execs scripts/site-test.sh and fails on any suite failure,
// attaching the runner's own per-suite table to the test log.
func TestSiteTestBattery(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	script := filepath.Join("..", "..", "..", "scripts", "site-test.sh")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("scripts/site-test.sh: %v", err)
	}
	out, err := exec.Command("bash", script).CombinedOutput()
	t.Logf("scripts/site-test.sh output:\n%s", out)
	if err != nil {
		t.Fatalf("scripts/site-test.sh: %v", err)
	}
}
