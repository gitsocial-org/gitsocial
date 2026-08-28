// s3_thin_test.go - End-to-end thin fork bucket tests: a push that excludes
// upstream's objects, the gitmsg-is-never-thinned invariant, frontier drift and
// the offline fallback, the read overlay, and the thin bucket's surface guards.
package main

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// emptyTreeSHA is git's empty tree — the tree every gitmsg data commit carries.
const emptyTreeSHA = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// newFixtureServer serves an s3Fixture for the duration of a test.
func newFixtureServer(t *testing.T, f *s3Fixture) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(f)
	t.Cleanup(server.Close)
	return server
}

// thinFixture is an upstream bucket, a fork clone of it, and the fork's own
// (thin) bucket prefix, all served by one in-process S3 fixture.
type thinFixture struct {
	fixture        *s3Fixture
	env            []string
	endpoint       string
	upstreamDir    string
	upstreamRemote string
	forkDir        string
	forkRemote     string
}

// dropUpstream deletes every key under the upstream prefix: upstream is gone,
// the hard case a thin bucket depends on not happening.
func (tf *thinFixture) dropUpstream() {
	tf.fixture.mu.Lock()
	defer tf.fixture.mu.Unlock()
	for key := range tf.fixture.objects {
		if strings.HasPrefix(key, "upstream/") {
			delete(tf.fixture.objects, key)
		}
	}
}

// gitInOr runs git and returns its output, or "" when the command fails (for
// values a test reports rather than asserts).
func gitInOr(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// forkHasObject reports whether the fork's bucket prefix carries an object as a
// loose key.
func forkHasObject(f *s3Fixture, sha string) bool {
	_, ok := f.object("fork/objects/" + sha[:2] + "/" + sha[2:])
	return ok
}

// emptyTreeCommit writes a gitmsg-shaped commit (empty tree, optional parent)
// and returns its sha, so a data branch's history stays disjoint from the code
// branches — exactly the shape the never-thinned invariant is about.
func emptyTreeCommit(t *testing.T, dir string, env []string, parent, msg string) string {
	t.Helper()
	args := []string{"commit-tree", emptyTreeSHA, "-m", msg}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	return strings.TrimSpace(gitIn(t, dir, env, args...))
}

// newThinFork builds the upstream repo (two code branches plus a gitmsg data
// branch), publishes it to its own bucket prefix, clones it as a fork, and
// records the fork's thin push relationship in the clone's git config.
func newThinFork(t *testing.T) *thinFixture {
	t.Helper()
	baseEnv := append(os.Environ(), "HOME="+t.TempDir(), "GIT_CONFIG_NOSYSTEM=1")
	fixture := &s3Fixture{bucket: "thin-bucket", objects: map[string][]byte{}}
	server := newFixtureServer(t, fixture)
	env := s3HelperEnv(t, server.URL, baseEnv)

	upstream := t.TempDir()
	gitIn(t, upstream, env, "init", "-q", "-b", "main")
	gitIn(t, upstream, env, "config", "user.email", "upstream@test.com")
	gitIn(t, upstream, env, "config", "user.name", "Upstream")
	commitIn(t, upstream, env, "a.txt", "upstream one")
	commitIn(t, upstream, env, "b.txt", "upstream two")
	// A second code branch, so a later drift can drop one tip while the other holds.
	gitIn(t, upstream, env, "branch", "release", "main")
	commitIn(t, upstream, env, "c.txt", "release two")
	gitIn(t, upstream, env, "update-ref", "refs/heads/release", strings.TrimSpace(gitIn(t, upstream, env, "rev-parse", "HEAD")))
	gitIn(t, upstream, env, "reset", "--hard", "HEAD~1")
	// The gitmsg data branch: its own root, empty trees, upstream-owned.
	dataRoot := emptyTreeCommit(t, upstream, env, "", "Upstream post\n\nGitMsg: ext=\"social\" v=\"0.1.0\" type=\"post\"")
	gitIn(t, upstream, env, "update-ref", "refs/heads/gitmsg/social", dataRoot)

	upstreamRemote := s3FixtureRemote(server.URL, "thin-bucket/upstream")
	gitIn(t, upstream, env, "push", upstreamRemote, "main", "release", "gitmsg/social")

	workdir := t.TempDir()
	gitIn(t, workdir, env, "clone", upstreamRemote, "fork")
	fork := filepath.Join(workdir, "fork")
	gitIn(t, fork, env, "config", "user.email", "fork@test.com")
	gitIn(t, fork, env, "config", "user.name", "Fork")
	gitIn(t, fork, env, "branch", "gitmsg/social", "origin/gitmsg/social")
	gitIn(t, fork, env, "branch", "release", "origin/release")

	forkRemote := s3FixtureRemote(server.URL, "thin-bucket/fork")
	gitIn(t, fork, env, "remote", "add", "fork", forkRemote)
	gitIn(t, fork, env, "config", "remote.fork.gitsocial-thin", "true")
	gitIn(t, fork, env, "config", "remote.fork.gitsocial-upstream", upstreamRemote)

	return &thinFixture{
		fixture:        fixture,
		env:            env,
		endpoint:       server.URL,
		upstreamDir:    upstream,
		upstreamRemote: upstreamRemote,
		forkDir:        fork,
		forkRemote:     forkRemote,
	}
}

// TestThinPush_uploadsOnlyTheForksOwnObjects: a thin push of one commit on top
// of upstream uploads that commit's objects and no more — the whole point of a
// thin fork bucket.
func TestThinPush_uploadsOnlyTheForksOwnObjects(t *testing.T) {
	tf := newThinFork(t)
	upstreamTip := strings.TrimSpace(gitIn(t, tf.forkDir, tf.env, "rev-parse", "main"))
	commitIn(t, tf.forkDir, tf.env, "fork.txt", "fork work")
	forkTip := strings.TrimSpace(gitIn(t, tf.forkDir, tf.env, "rev-parse", "main"))

	gitIn(t, tf.forkDir, tf.env, "push", "fork", "main")

	if !forkHasObject(tf.fixture, forkTip) {
		t.Error("the fork's own commit is missing from its bucket")
	}
	if forkHasObject(tf.fixture, upstreamTip) {
		t.Error("a thin push uploaded upstream's commit; the frontier excluded nothing")
	}
	// The frontier the push used is recorded for readers and for the next push.
	doc, ok := tf.fixture.object("fork/.gitsocial/upstream")
	if !ok {
		t.Fatal(".gitsocial/upstream not written by a thin push")
	}
	for _, want := range []string{`"version":1`, tf.upstreamRemote, upstreamTip} {
		if !strings.Contains(string(doc), want) {
			t.Errorf(".gitsocial/upstream = %s, want it to contain %q", doc, want)
		}
	}
}

// TestThinPush_gitmsgStaysComplete is the guard for the load-bearing invariant:
// gitmsg branches are never thinned, so fork metadata reads from a thin bucket
// with upstream UNREACHABLE — no overlay, no upstream reachability at all.
func TestThinPush_gitmsgStaysComplete(t *testing.T) {
	tf := newThinFork(t)
	upstreamData := strings.TrimSpace(gitIn(t, tf.forkDir, tf.env, "rev-parse", "gitmsg/social"))
	forkData := emptyTreeCommit(t, tf.forkDir, tf.env, upstreamData, "Fork post\n\nGitMsg: ext=\"social\" v=\"0.1.0\" type=\"post\"")
	gitIn(t, tf.forkDir, tf.env, "update-ref", "refs/heads/gitmsg/social", forkData)
	commitIn(t, tf.forkDir, tf.env, "fork.txt", "fork work")
	gitIn(t, tf.forkDir, tf.env, "push", "fork", "main", "gitmsg/social")

	tf.dropUpstream() // upstream is gone: the gitmsg branch must still read

	reader := t.TempDir()
	gitIn(t, reader, tf.env, "init", "-q", "-b", "main")
	gitIn(t, reader, tf.env, "fetch", tf.forkRemote, "+refs/heads/gitmsg/*:refs/heads/gitmsg/*")
	if got := strings.TrimSpace(gitIn(t, reader, tf.env, "rev-parse", "refs/heads/gitmsg/social")); got != forkData {
		t.Errorf("fetched gitmsg/social = %s, want %s", got, forkData)
	}
	if got := strings.TrimSpace(gitIn(t, reader, tf.env, "rev-list", "--count", "refs/heads/gitmsg/social")); got != "2" {
		t.Errorf("gitmsg/social walks %s commits from a thin bucket, want 2 (the branch must reach its root without upstream)", got)
	}
	gitIn(t, reader, tf.env, "fsck", "--strict")
}

// TestThinPush_pinDriftUploadsUncoveredObjects: upstream force-pushes past a
// pinned tip, so the next thin push drops that tip from the frontier and uploads
// the objects it no longer covers.
func TestThinPush_pinDriftUploadsUncoveredObjects(t *testing.T) {
	tf := newThinFork(t)
	releaseTip := strings.TrimSpace(gitIn(t, tf.forkDir, tf.env, "rev-parse", "release"))
	releaseBase := strings.TrimSpace(gitIn(t, tf.forkDir, tf.env, "rev-parse", "release~1"))

	// First thin push: a topic off main. The release tip is pinned, not uploaded.
	gitIn(t, tf.forkDir, tf.env, "switch", "-q", "-c", "topic", "main")
	commitIn(t, tf.forkDir, tf.env, "topic.txt", "topic work")
	gitIn(t, tf.forkDir, tf.env, "push", "fork", "topic")
	if forkHasObject(tf.fixture, releaseTip) {
		t.Fatal("precondition: the release tip should be covered by the frontier, not uploaded")
	}

	// Upstream force-pushes release back one commit: the pinned tip is gone.
	gitIn(t, tf.upstreamDir, tf.env, "update-ref", "refs/heads/release", releaseBase)
	gitIn(t, tf.upstreamDir, tf.env, "push", "--force", tf.upstreamRemote, "release")

	// New work based on the now-uncovered commit must carry it to the bucket.
	gitIn(t, tf.forkDir, tf.env, "switch", "-q", "-c", "topic2", "release")
	commitIn(t, tf.forkDir, tf.env, "topic2.txt", "more work")
	gitIn(t, tf.forkDir, tf.env, "push", "fork", "topic2")

	if !forkHasObject(tf.fixture, releaseTip) {
		t.Error("upstream dropped the pinned tip but the fork never uploaded the objects it stopped covering")
	}
	doc, _ := tf.fixture.object("fork/.gitsocial/upstream")
	if strings.Contains(string(doc), releaseTip) {
		t.Errorf(".gitsocial/upstream still pins the dropped tip %s: %s", releaseTip[:8], doc)
	}
}

// TestThinPush_offlineFallsBackToRecordedPins: with the upstream fetch
// unavailable the push falls back to the pins the bucket records and still
// excludes upstream's history.
func TestThinPush_offlineFallsBackToRecordedPins(t *testing.T) {
	tf := newThinFork(t)
	upstreamTip := strings.TrimSpace(gitIn(t, tf.forkDir, tf.env, "rev-parse", "main"))
	commitIn(t, tf.forkDir, tf.env, "fork.txt", "fork work")
	gitIn(t, tf.forkDir, tf.env, "push", "fork", "main")

	// Upstream goes unreachable (a dead endpoint, not an empty bucket).
	gitIn(t, tf.forkDir, tf.env, "config", "remote.fork.gitsocial-upstream", "s3://127.0.0.1:1/dead/repo")

	// A fresh branch, so only the recorded pins can exclude upstream's history
	// (the bucket carries no tip that covers it).
	gitIn(t, tf.forkDir, tf.env, "switch", "-q", "-c", "offline", "main")
	commitIn(t, tf.forkDir, tf.env, "offline.txt", "offline work")
	offlineTip := strings.TrimSpace(gitIn(t, tf.forkDir, tf.env, "rev-parse", "offline"))
	gitIn(t, tf.forkDir, tf.env, "push", "fork", "offline")

	if !forkHasObject(tf.fixture, offlineTip) {
		t.Error("the offline push did not land the fork's own commit")
	}
	if forkHasObject(tf.fixture, upstreamTip) {
		t.Error("the offline push uploaded upstream's history; the recorded pins were not used")
	}
}

// thinPushMain publishes the fork's own commit to its thin bucket and returns
// (upstream tip, fork tip) — the setup every read-side test starts from.
func thinPushMain(t *testing.T, tf *thinFixture) (upstreamTip, forkTip string) {
	t.Helper()
	upstreamTip = strings.TrimSpace(gitIn(t, tf.forkDir, tf.env, "rev-parse", "main"))
	commitIn(t, tf.forkDir, tf.env, "fork.txt", "fork work")
	forkTip = strings.TrimSpace(gitIn(t, tf.forkDir, tf.env, "rev-parse", "main"))
	gitIn(t, tf.forkDir, tf.env, "push", "fork", "main", "gitmsg/social")
	if forkHasObject(tf.fixture, upstreamTip) {
		t.Fatal("precondition: the thin push kept upstream's commit out of the bucket")
	}
	return upstreamTip, forkTip
}

// TestThinRead_roundTrip: cloning a thin bucket with upstream reachable yields
// the full history, a clean `git fsck --strict`, and remote.upstream.url set.
func TestThinRead_roundTrip(t *testing.T) {
	tf := newThinFork(t)
	_, forkTip := thinPushMain(t, tf)
	wantLog := gitIn(t, tf.forkDir, tf.env, "log", "--format=%H %s", "main")

	// Plain `git clone` through the helper: the overlay resolves upstream's half.
	workdir := t.TempDir()
	gitIn(t, workdir, tf.env, "clone", tf.forkRemote, "plain")
	plain := filepath.Join(workdir, "plain")
	if got := strings.TrimSpace(gitIn(t, plain, tf.env, "rev-parse", "HEAD")); got != forkTip {
		t.Errorf("cloned HEAD = %s, want %s", got, forkTip)
	}
	if got := gitIn(t, plain, tf.env, "log", "--format=%H %s", "HEAD"); got != wantLog {
		t.Errorf("cloned log mismatch:\n got: %s\nwant: %s", got, wantLog)
	}
	if content, err := os.ReadFile(filepath.Join(plain, "a.txt")); err != nil || string(content) != "upstream one\n" {
		t.Errorf("upstream file in the clone = %q (err %v); the overlay did not supply it", content, err)
	}
	gitIn(t, plain, tf.env, "fsck", "--strict")
	// The helper writes remote.upstream.url mid-clone; log what actually stuck.
	t.Logf("after plain git clone, remote.upstream.url = %q",
		strings.TrimSpace(gitInOr(t, plain, tf.env, "config", "--local", "--get", "remote.upstream.url")))

	// `gitsocial clone` re-asserts the upstream remote afterwards, so it is always
	// recorded no matter what the mid-clone write did.
	t.Setenv("AWS_ACCESS_KEY_ID", "test-access-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret-key")
	t.Setenv("GITSOCIAL_S3_ENDPOINT", tf.endpoint)
	t.Setenv("GITSOCIAL_S3_PATH_STYLE", "1")
	cliDir := t.TempDir()
	stdout, stderr, code := runCLI(t, cliDir, t.TempDir(), "clone", tf.forkRemote, "gsclone")
	if code != 0 {
		t.Fatalf("gitsocial clone of a thin bucket: exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	gsclone := filepath.Join(cliDir, "gsclone")
	if got := strings.TrimSpace(gitIn(t, gsclone, tf.env, "config", "--local", "--get", "remote.upstream.url")); got != tf.upstreamRemote {
		t.Errorf("remote.upstream.url = %q, want %q", got, tf.upstreamRemote)
	}
	gitIn(t, gsclone, tf.env, "fsck", "--strict")
}

// TestThinRead_missingUpstreamNamesTheCommit: with upstream gone the clone fails
// naming the commit and the upstream URL, not a bare missing-object error.
func TestThinRead_missingUpstreamNamesTheCommit(t *testing.T) {
	tf := newThinFork(t)
	upstreamTip, _ := thinPushMain(t, tf)
	tf.dropUpstream()

	out := gitInErr(t, t.TempDir(), tf.env, "clone", tf.forkRemote, "broken")
	if !strings.Contains(out, tf.upstreamRemote) {
		t.Errorf("clone failure does not name the upstream URL %s:\n%s", tf.upstreamRemote, out)
	}
	if !strings.Contains(out, upstreamTip) {
		t.Errorf("clone failure does not name the missing commit %s:\n%s", upstreamTip, out)
	}
}

// TestThinRead_hostileUpstreamURLRefused: .gitsocial/upstream is bucket content,
// so an ext:: URL is refused naming the key — and no git fetch is spawned.
func TestThinRead_hostileUpstreamURLRefused(t *testing.T) {
	tf := newThinFork(t)
	thinPushMain(t, tf)
	sentinel := filepath.Join(t.TempDir(), "spawned")
	tf.fixture.putObject("fork/.gitsocial/upstream",
		[]byte(`{"version":1,"url":"ext::touch `+sentinel+`","pins":[]}`))

	out := gitInErr(t, t.TempDir(), tf.env, "clone", tf.forkRemote, "hostile")
	if !strings.Contains(out, ".gitsocial/upstream") {
		t.Errorf("refusal does not name the key:\n%s", out)
	}
	if !strings.Contains(out, "ext::") {
		t.Errorf("refusal does not name the offending URL:\n%s", out)
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Error("the hostile transport ran: a git fetch was spawned before the URL was validated")
	}
}

// setCLIBucketEnv points the CLI binary at the fixture bucket (the CLI reads its
// endpoint and credentials from the process environment, not from tf.env).
func setCLIBucketEnv(t *testing.T, tf *thinFixture) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "test-access-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret-key")
	t.Setenv("GITSOCIAL_S3_ENDPOINT", tf.endpoint)
	t.Setenv("GITSOCIAL_S3_PATH_STYLE", "1")
}

// TestThinBucket_noStockCloneUntilFull: a thin bucket publishes no ref
// advertisement, so a stock dumb-HTTP clone fails at ref discovery — and
// `gitsocial push --full` restores stock cloneability end to end.
func TestThinBucket_noStockCloneUntilFull(t *testing.T) {
	tf := newThinFork(t)
	thinPushMain(t, tf)

	if _, ok := tf.fixture.object("fork/info/refs"); ok {
		t.Error("a thin bucket must publish no info/refs (a stock clone would walk into missing objects)")
	}
	if _, ok := tf.fixture.object("fork/objects/info/packs"); !ok {
		t.Error("objects/info/packs must stay: the helper's own read path needs it")
	}
	stockEnv := append(append([]string(nil), os.Environ()...), "HOME="+t.TempDir(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	publicURL := tf.endpoint + "/thin-bucket/fork"
	t.Logf("stock clone of a thin bucket fails at ref discovery: %s",
		strings.TrimSpace(gitInErr(t, t.TempDir(), stockEnv, "clone", publicURL, "stock")))

	// Detach: everything the bucket left to upstream uploads, info/refs comes back.
	setCLIBucketEnv(t, tf)
	stdout, stderr, code := runCLI(t, tf.forkDir, t.TempDir(), "push", "--full", "fork")
	if code != 0 {
		t.Fatalf("gitsocial push --full: exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if _, ok := tf.fixture.object("fork/.gitsocial/upstream"); ok {
		t.Error("push --full left the thin marker on the bucket")
	}
	if _, ok := tf.fixture.object("fork/info/refs"); !ok {
		t.Error("push --full did not restore info/refs")
	}
	if got := gitInOr(t, tf.forkDir, tf.env, "config", "--local", "--get", "remote.fork.gitsocial-thin"); strings.TrimSpace(got) != "" {
		t.Errorf("push --full left the thin flag set: %q", got)
	}
	// The money shot: stock git, no helper, over the bucket's public URL. (No
	// `fsck --strict` here: stock git's dumb walker leaves the empty tree of the
	// gitmsg data commits unmaterialized, which fsck flags on any bucket — see
	// TestS3Helper_dumbHTTPClone.)
	workdir := t.TempDir()
	gitIn(t, workdir, stockEnv, "clone", publicURL, "stock")
	stock := filepath.Join(workdir, "stock")
	if content, err := os.ReadFile(filepath.Join(stock, "a.txt")); err != nil || string(content) != "upstream one\n" {
		t.Errorf("stock clone after --full is missing upstream's file: %q (err %v)", content, err)
	}
	wantLog := gitIn(t, tf.forkDir, tf.env, "log", "--format=%H %s", "main")
	if got := gitIn(t, stock, stockEnv, "log", "--format=%H %s", "HEAD"); got != wantLog {
		t.Errorf("stock clone history after --full:\n got: %s\nwant: %s", got, wantLog)
	}
}

// TestThinBucket_siteRefused: `gitsocial push --site-only` refuses against a thin
// bucket, and the site keys already there are left in place.
func TestThinBucket_siteRefused(t *testing.T) {
	tf := newThinFork(t)
	thinPushMain(t, tf)
	tf.fixture.putObject("fork/.gitsocial/site/version", []byte("test\n"))
	tf.fixture.putObject("fork/index.html", []byte("<html>pre-existing site</html>"))

	setCLIBucketEnv(t, tf)
	stdout, stderr, code := runCLI(t, tf.forkDir, t.TempDir(), "push", "--site-only", "fork")
	if code == 0 {
		t.Fatalf("push --site-only against a thin bucket succeeded; want a refusal\nstdout: %s\nstderr: %s", stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "thin fork bucket") {
		t.Errorf("refusal does not say why:\nstdout: %s\nstderr: %s", stdout, stderr)
	}
	if page, ok := tf.fixture.object("fork/index.html"); !ok || !strings.Contains(string(page), "pre-existing") {
		t.Error("the pre-existing site was not left in place")
	}
}

// TestThinBucket_statusReportsTheRelationship: `gitsocial status` says the push
// remote is a thin fork and how many tips it pins.
func TestThinBucket_statusReportsTheRelationship(t *testing.T) {
	tf := newThinFork(t)
	thinPushMain(t, tf)
	gitIn(t, tf.forkDir, tf.env, "config", "gitsocial.pushRemote", "fork")

	setCLIBucketEnv(t, tf)
	stdout, stderr, code := runCLI(t, tf.forkDir, t.TempDir(), "status")
	if code != 0 {
		t.Fatalf("gitsocial status: exit %d\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "Thin fork of "+tf.upstreamRemote) {
		t.Errorf("status does not report the thin relationship:\n%s", stdout)
	}
	if !strings.Contains(stdout, "pinned at 3 tips") {
		t.Errorf("status does not report the pinned tip count (upstream has 3 branches):\n%s", stdout)
	}
}
