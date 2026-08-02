// info_refs_test.go - the dumb-HTTP transport surface: byte-for-byte parity of
// the generated info/refs with `git update-server-info` (including annotated-tag
// peel lines), the empty objects/info/packs, and the no-cache classification.
package objstore

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitInfoRefsRepo builds a bare-ish repo exercising every ref shape info/refs
// must render — branches, a lightweight tag, an annotated tag, and a nested
// tag-of-a-tag — then runs `git update-server-info` so the test can diff against
// git's own output. Returns the repo dir.
func gitInfoRefsRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	env := append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir(),
		"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@t.com",
		"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@t.com")
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	run("commit", "-qm", "first")
	run("branch", "feature/x")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", "b.txt")
	run("commit", "-qm", "second")
	run("tag", "light")                          // lightweight → no peel line
	run("tag", "-a", "annot", "-m", "annotated") // annotated → peel line
	run("-c", "advice.nestedTag=false", "tag", "-a", "annot2", "-m", "tag of tag", "annot")
	// The gitmsg data branch shape: an extra ref class git clients ignore but
	// info/refs still lists, exactly as update-server-info does.
	run("branch", "gitmsg/social", "main")
	run("update-server-info")
	return dir
}

// repoRefs reads a repo's refname → sha map the way the bucket carries it (the
// ref's own object, un-peeled).
func repoRefs(t *testing.T, dir string) map[string]string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "for-each-ref", "--format=%(refname) %(objectname)").Output()
	if err != nil {
		t.Fatal(err)
	}
	refs := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name, sha, ok := strings.Cut(line, " ")
		if ok {
			refs[name] = sha
		}
	}
	return refs
}

// uploadRepoObjectsAndRefs mirrors a repo's loose objects, refs, and HEAD into a
// bucket at prefix using the bucket layout, so writeDumbTransportInfo peels tags
// straight from the bucket (never the local odb).
func uploadRepoObjectsAndRefs(t *testing.T, client *Client, prefix, dir string) map[string]string {
	t.Helper()
	objectsDir := filepath.Join(dir, ".git", "objects")
	fans, err := os.ReadDir(objectsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, fan := range fans {
		if !fan.IsDir() || len(fan.Name()) != 2 {
			continue
		}
		files, err := os.ReadDir(filepath.Join(objectsDir, fan.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			data, err := os.ReadFile(filepath.Join(objectsDir, fan.Name(), f.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if err := client.Put(prefix+"objects/"+fan.Name()+"/"+f.Name(), data); err != nil {
				t.Fatal(err)
			}
		}
	}
	refs := repoRefs(t, dir)
	for name, sha := range refs {
		if err := client.Put(prefix+name, []byte(sha+"\n")); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.Put(prefix+"HEAD", []byte("ref: refs/heads/main\n")); err != nil {
		t.Fatal(err)
	}
	return refs
}

// TestWriteDumbTransportInfo_UpdateServerInfoParity is the byte-format guarantee:
// writeDumbTransportInfo, peeling tags straight from the bucket, reproduces
// `git update-server-info`'s info/refs and objects/info/packs byte-for-byte.
func TestWriteDumbTransportInfo_UpdateServerInfoParity(t *testing.T) {
	dir := gitInfoRefsRepo(t)
	wantRefs, err := os.ReadFile(filepath.Join(dir, ".git", "info", "refs"))
	if err != nil {
		t.Fatal(err)
	}
	wantPacks, err := os.ReadFile(filepath.Join(dir, ".git", "objects", "info", "packs"))
	if err != nil {
		t.Fatal(err)
	}

	client, _ := testClient(t)
	refs := uploadRepoObjectsAndRefs(t, client, "", dir)
	if err := writeDumbTransportInfo(client, "", refs); err != nil {
		t.Fatalf("writeDumbTransportInfo: %v", err)
	}

	gotRefs, err := client.Get(infoRefsKey)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotRefs) != string(wantRefs) {
		t.Errorf("info/refs mismatch with git update-server-info\n got: %q\nwant: %q", gotRefs, wantRefs)
	}
	gotPacks, err := client.Get(packsKey)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotPacks) != string(wantPacks) {
		t.Errorf("objects/info/packs mismatch\n got: %q\nwant: %q", gotPacks, wantPacks)
	}
}

// TestBuildInfoRefs_FormatAndPeelPlacement pins the pure rendering: sorted by
// refname, a peel line only for refs/tags entries the peeler resolves, and the
// peel line placed immediately after its tag's main line.
func TestBuildInfoRefs_FormatAndPeelPlacement(t *testing.T) {
	commit := strings.Repeat("a", 40)
	tagObj := strings.Repeat("b", 40)
	peeled := strings.Repeat("c", 40)
	refs := map[string]string{
		"refs/heads/main":    commit,
		"refs/tags/v1":       tagObj, // annotated: peels
		"refs/tags/light":    commit, // lightweight: no peel
		"refs/gitmsg/social": commit,
	}
	peel := func(sha string) (string, bool) {
		if sha == tagObj {
			return peeled, true
		}
		return "", false
	}
	got := string(buildInfoRefs(refs, peel))
	want := strings.Join([]string{
		commit + "\trefs/gitmsg/social",
		commit + "\trefs/heads/main",
		commit + "\trefs/tags/light",
		tagObj + "\trefs/tags/v1",
		peeled + "\trefs/tags/v1^{}",
		"",
	}, "\n")
	if got != want {
		t.Errorf("buildInfoRefs mismatch\n got: %q\nwant: %q", got, want)
	}
}

// TestDumbTransportKeysAreNoCache pins the cache policy: both transport keys are
// mutable, so cacheControlForKey (stamped by Client.do on every PUT and mirrored
// by the dev servers) must classify them no-cache — never served stale.
func TestDumbTransportKeysAreNoCache(t *testing.T) {
	for _, key := range []string{infoRefsKey, packsKey, "myrepo/" + infoRefsKey, "myrepo/" + packsKey} {
		if got := cacheControlForKey(key); got != cacheControlRevalidate {
			t.Errorf("cacheControlForKey(%q) = %q, want %q", key, got, cacheControlRevalidate)
		}
	}
}
