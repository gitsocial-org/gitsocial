//go:build sitetest

// sitetest_shots_test.go - the repo-shape screenshot goldens

package site

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// updateShots rewrites every golden from the run's screenshots.
var updateShots = flag.Bool("update", false, "update the screenshot goldens")

// shotsGoldenDir holds one PNG per fixture, route, width and theme.
const shotsGoldenDir = "sitetest/goldens"

// chromePath returns the Chrome chrome.js finds, or "" when there is none.
func chromePath(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("node", "-e", `process.stdout.write(require("./sitetest/chrome.js").find())`).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// TestSiteShapeGoldens screenshots every repo-shape route and compares it to its golden.
func TestSiteShapeGoldens(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	if chromePath(t) == "" {
		t.Skip("chrome not available")
	}
	shots := t.TempDir()
	out, err := exec.Command("node", filepath.Join("sitetest", "shots.js"), shots).CombinedOutput()
	if err != nil {
		t.Fatalf("sitetest/shots.js: %v\n%s", err, out)
	}
	names, err := filepath.Glob(filepath.Join(shots, "*.png"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no screenshots in %s: %v", shots, err)
	}
	if *updateShots {
		if err := os.RemoveAll(shotsGoldenDir); err != nil {
			t.Fatalf("clear goldens: %v", err)
		}
		if err := os.MkdirAll(shotsGoldenDir, 0o755); err != nil {
			t.Fatalf("make golden dir: %v", err)
		}
	}
	for _, shot := range names {
		t.Run(strings.TrimSuffix(filepath.Base(shot), ".png"), func(t *testing.T) {
			compareShot(t, shot, filepath.Join(shotsGoldenDir, filepath.Base(shot)))
		})
	}
}

// compareShot checks one screenshot against its golden, or writes the golden under -update.
func compareShot(t *testing.T, shot, golden string) {
	t.Helper()
	got, err := readPNG(shot)
	if err != nil {
		t.Fatalf("read screenshot: %v", err)
	}
	if *updateShots {
		data, err := os.ReadFile(shot)
		if err != nil {
			t.Fatalf("read screenshot: %v", err)
		}
		if err := os.WriteFile(golden, data, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := readPNG(golden)
	if err != nil {
		t.Fatalf("read golden: %v: run with -update to create it", err)
	}
	res := comparePNGs(got, want)
	if res.fraction <= diffFraction {
		return
	}
	diff := strings.TrimSuffix(golden, ".png") + ".diff.png"
	if err := writePNG(diff, res.image); err != nil {
		t.Errorf("write diff image: %v", err)
	}
	t.Errorf("%d of %d pixels differ (%.4f over the %.4f threshold): see %s", res.differing, res.total, res.fraction, diffFraction, diff)
}
