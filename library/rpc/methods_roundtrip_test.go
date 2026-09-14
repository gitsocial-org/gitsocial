// methods_roundtrip_test.go - One round trip per namespace: a create, a read back, an error code, and the params RPC.md calls accepted and ignored
package rpc

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/internal/testutil"
)

// registerAll registers every namespace on the server, the way `gitsocial rpc` does.
func registerAll(s *Server) {
	RegisterCoreMethods(s, "test")
	RegisterSearchMethods(s)
	RegisterSocialMethods(s)
	RegisterPMMethods(s)
	RegisterReviewMethods(s)
	RegisterReleaseMethods(s)
}

// roundTripServer returns a server on a fresh workspace with the named extensions initialized.
func roundTripServer(t *testing.T, extensions ...string) *Server {
	t.Helper()
	testutil.OpenTempCache(t, "")
	template, err := testutil.NewRepoTemplate()
	if err != nil {
		t.Fatalf("NewRepoTemplate() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(template) })
	workdir := testutil.CopyRepo(t, template)
	origin := t.TempDir()
	if err := git.EnsureBareRepo(origin); err != nil {
		t.Fatalf("EnsureBareRepo() error = %v", err)
	}
	if _, err := git.ExecGit(workdir, []string{"remote", "add", "origin", origin}); err != nil {
		t.Fatalf("remote add origin: %v", err)
	}
	server := NewServer(NewRegistry(), strings.NewReader(""), io.Discard)
	registerAll(server)
	server.session.Workdir = workdir
	server.session.CacheDir = t.TempDir()
	server.session.RepoURL = gitmsg.ResolveRepoURL(workdir)
	server.session.Initialized = true
	for _, ext := range extensions {
		call(t, server, "core.initExtension", fmt.Sprintf(`{"extension":%q}`, ext))
	}
	return server
}

// call dispatches one request through the server and returns the response.
func call(t *testing.T, server *Server, method, params string) Response {
	t.Helper()
	return server.processRequest(Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  method,
		Params:  json.RawMessage(params),
	})
}

// decodeResult re-marshals a successful response into target, as a client on the wire reads it.
func decodeResult(t *testing.T, resp Response, method string, target any) {
	t.Helper()
	if resp.Error != nil {
		t.Fatalf("%s error = %+v", method, resp.Error)
	}
	encoded, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal %s result: %v", method, err)
	}
	if err := json.Unmarshal(encoded, target); err != nil {
		t.Fatalf("unmarshal %s result: %v (%s)", method, err, encoded)
	}
}

// assertAppError fails unless the response carries the given JSON-RPC code and data.appCode.
func assertAppError(t *testing.T, resp Response, method string, code int, appCode string) {
	t.Helper()
	if resp.Error == nil {
		t.Fatalf("%s returned %v, want an error", method, resp.Result)
	}
	if resp.Error.Code != code {
		t.Errorf("%s error code = %d, want %d (%s)", method, resp.Error.Code, code, resp.Error.Message)
	}
	if appCode == "" {
		return
	}
	data, ok := resp.Error.Data.(map[string]any)
	if !ok {
		t.Fatalf("%s error data = %#v, want a map carrying appCode", method, resp.Error.Data)
	}
	if data["appCode"] != appCode {
		t.Errorf("%s appCode = %v, want %q", method, data["appCode"], appCode)
	}
}

// postShape is the subset of RPC.md's Post type these tests read.
type postShape struct {
	ID           string
	Type         string
	Content      string
	CleanContent string
	IsRetracted  bool
}

// TestSocialRoundTrip_postCreateReadBack drives social.createPost, reads it back
// and pins the INVALID_SCOPE error and the ignored sort param.
func TestSocialRoundTrip_postCreateReadBack(t *testing.T) {
	server := roundTripServer(t, "social")

	var created postShape
	decodeResult(t, call(t, server, "social.createPost", `{"content":"a first post"}`), "social.createPost", &created)
	if created.ID == "" || created.Type != "post" {
		t.Fatalf("social.createPost returned %+v, want an id and type post", created)
	}
	if !strings.Contains(created.CleanContent, "a first post") {
		t.Errorf("CleanContent = %q, want the post body", created.CleanContent)
	}

	var second postShape
	decodeResult(t, call(t, server, "social.createPost", `{"content":"a second post"}`), "social.createPost", &second)

	var posts []postShape
	decodeResult(t, call(t, server, "social.getPosts", `{"scope":"repository:workspace"}`), "social.getPosts", &posts)
	if len(posts) != 2 {
		t.Fatalf("social.getPosts served %d posts, want the 2 just created", len(posts))
	}
	if !slices.Contains(ids(posts), created.ID) || !slices.Contains(ids(posts), second.ID) {
		t.Errorf("social.getPosts served %v, want both created posts", ids(posts))
	}

	// RPC.md 4.1: sort is accepted and ignored.
	var sorted []postShape
	decodeResult(t, call(t, server, "social.getPosts", `{"scope":"repository:workspace","sort":"oldest"}`), "social.getPosts", &sorted)
	if !slices.Equal(ids(sorted), ids(posts)) {
		t.Errorf("sort changed the order: %v against %v", ids(sorted), ids(posts))
	}

	assertAppError(t, call(t, server, "social.getPosts", `{"scope":"nonsense"}`), "social.getPosts", CodeAppInternal, "INVALID_SCOPE")
	assertAppError(t, call(t, server, "social.createPost", `{}`), "social.createPost", CodeInvalidParams, "")
}

// ids returns the post ids in order, for a failure message.
func ids(posts []postShape) []string {
	out := make([]string, 0, len(posts))
	for _, p := range posts {
		out = append(out, p.ID)
	}
	return out
}

// TestSocialRoundTrip_listCreateAndDelete pins the empty-object results RPC.md gives the list verbs.
func TestSocialRoundTrip_listCreateAndDelete(t *testing.T) {
	server := roundTripServer(t, "social")

	var created struct{ ID, Name string }
	decodeResult(t, call(t, server, "social.createList", `{"id":"reading","name":"Reading"}`), "social.createList", &created)
	if created.ID != "reading" || created.Name != "Reading" {
		t.Fatalf("social.createList returned %+v", created)
	}

	var lists []struct{ ID string }
	decodeResult(t, call(t, server, "social.getLists", `{}`), "social.getLists", &lists)
	if len(lists) != 1 || lists[0].ID != "reading" {
		t.Fatalf("social.getLists served %+v, want the list just created", lists)
	}

	// RPC.md 4.1 gives deleteList the result {}, which is Result[struct{}] on the wire.
	deleted := call(t, server, "social.deleteList", `{"id":"reading"}`)
	if deleted.Error != nil {
		t.Fatalf("social.deleteList error = %+v", deleted.Error)
	}
	if encoded, err := json.Marshal(deleted.Result); err != nil || string(encoded) != "{}" {
		t.Errorf("social.deleteList result = %s (%v), want {}", encoded, err)
	}

	var after []struct{ ID string }
	decodeResult(t, call(t, server, "social.getLists", `{}`), "social.getLists", &after)
	if len(after) != 0 {
		t.Errorf("social.getLists still serves %+v after a delete", after)
	}

	assertAppError(t, call(t, server, "social.deleteList", `{}`), "social.deleteList", CodeInvalidParams, "")
}

// issueShape is the subset of RPC.md's Issue type these tests read.
type issueShape struct {
	ID      string
	Subject string
	State   string
}

// TestPMRoundTrip_issueCreateCloseAndMissingRef drives pm.createIssue through
// close and pins the NOT_FOUND code a missing ref maps to.
func TestPMRoundTrip_issueCreateCloseAndMissingRef(t *testing.T) {
	server := roundTripServer(t, "pm")

	var created issueShape
	decodeResult(t, call(t, server, "pm.createIssue", `{"subject":"the timeline scrolls twice"}`), "pm.createIssue", &created)
	if created.ID == "" || created.State != "open" {
		t.Fatalf("pm.createIssue returned %+v, want an id and state open", created)
	}

	var fetched issueShape
	decodeResult(t, call(t, server, "pm.getIssue", fmt.Sprintf(`{"ref":%q}`, created.ID)), "pm.getIssue", &fetched)
	if fetched.Subject != created.Subject {
		t.Errorf("pm.getIssue served subject %q, want %q", fetched.Subject, created.Subject)
	}

	var closed issueShape
	decodeResult(t, call(t, server, "pm.closeIssue", fmt.Sprintf(`{"ref":%q}`, created.ID)), "pm.closeIssue", &closed)
	if closed.State != "closed" {
		t.Errorf("pm.closeIssue served state %q, want closed", closed.State)
	}

	var open []issueShape
	decodeResult(t, call(t, server, "pm.getIssues", `{"states":["open"]}`), "pm.getIssues", &open)
	if len(open) != 0 {
		t.Errorf("pm.getIssues states=[open] served %+v after the close", open)
	}

	assertAppError(t, call(t, server, "pm.getIssue", `{"ref":"#commit:000000000000"}`), "pm.getIssue", CodeNotFound, "NOT_FOUND")
	assertAppError(t, call(t, server, "pm.closeIssue", `{}`), "pm.closeIssue", CodeInvalidParams, "")
}

// prShape is the subset of RPC.md's PullRequest type these tests read.
type prShape struct {
	ID      string
	Subject string
	State   string
	Base    string
	Head    string
}

// TestReviewRoundTrip_mergeIgnoresStrategy merges a pull request over RPC and
// asserts the merge is fast-forward whatever strategy the caller sends.
func TestReviewRoundTrip_mergeIgnoresStrategy(t *testing.T) {
	server := roundTripServer(t, "review")
	workdir := server.session.Workdir
	headTip := commitOnBranch(t, workdir, "feature-x", "head.txt")

	var created prShape
	decodeResult(t, call(t, server, "review.createPR",
		`{"subject":"Ship the head branch","base":"main","head":"feature-x"}`), "review.createPR", &created)
	if created.ID == "" || created.State != "open" {
		t.Fatalf("review.createPR returned %+v, want an id and state open", created)
	}

	var listed []prShape
	decodeResult(t, call(t, server, "review.getPullRequests", `{"states":["open"]}`), "review.getPullRequests", &listed)
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("review.getPullRequests served %+v, want the pull request just created", listed)
	}

	// RPC.md 4.3: mergePR merges fast-forward, and the CLI's other strategies are unreachable.
	var merged prShape
	decodeResult(t, call(t, server, "review.mergePR",
		fmt.Sprintf(`{"ref":%q,"strategy":"squash"}`, created.ID)), "review.mergePR", &merged)
	if merged.State != "merged" {
		t.Errorf("review.mergePR served state %q, want merged", merged.State)
	}
	mainTip, err := git.ReadRef(workdir, "main")
	if err != nil {
		t.Fatalf("ReadRef(main) error = %v", err)
	}
	if mainTip != headTip {
		t.Errorf("main is at %s, want the head tip %s: strategy squash reached the merge", mainTip, headTip)
	}

	assertAppError(t, call(t, server, "review.getPR", `{"ref":"#commit:000000000000"}`), "review.getPR", CodeNotFound, "NOT_FOUND")
	assertAppError(t, call(t, server, "review.mergePR", `{}`), "review.mergePR", CodeInvalidParams, "")
}

// commitOnBranch forks branch off main, commits one file on it, and returns its tip.
func commitOnBranch(t *testing.T, workdir, branch, filename string) string {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		if _, err := git.ExecGit(workdir, args); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	run("checkout", "-b", branch)
	if err := os.WriteFile(filepath.Join(workdir, filename), []byte("a change on the head branch\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", filename, err)
	}
	run("add", filename)
	run("commit", "-m", "add "+filename)
	run("checkout", "main")
	tip, err := git.ReadRef(workdir, branch)
	if err != nil {
		t.Fatalf("ReadRef(%s) error = %v", branch, err)
	}
	return tip
}

// TestReviewRoundTrip_createPRWithoutBaseOrHead: only subject is validated, and
// the pull request that results cannot be merged.
func TestReviewRoundTrip_createPRWithoutBaseOrHead(t *testing.T) {
	server := roundTripServer(t, "review")

	var created prShape
	decodeResult(t, call(t, server, "review.createPR", `{"subject":"No base or head"}`), "review.createPR", &created)
	if created.Base != "" || created.Head != "" {
		t.Fatalf("review.createPR returned base %q and head %q, want both empty", created.Base, created.Head)
	}

	assertAppError(t, call(t, server, "review.mergePR", fmt.Sprintf(`{"ref":%q}`, created.ID)),
		"review.mergePR", CodeAppInternal, "MERGE_INCOMPLETE")
	assertAppError(t, call(t, server, "review.createPR", `{}`), "review.createPR", CodeInvalidParams, "")
}

// TestReviewRoundTrip_forkAddAndRemove pins the boolean results the fork verbs return.
func TestReviewRoundTrip_forkAddAndRemove(t *testing.T) {
	server := roundTripServer(t, "review")
	const forkURL = "https://example.com/other/fork.git"
	const storedURL = "https://example.com/other/fork"

	if resp := call(t, server, "review.addFork", fmt.Sprintf(`{"url":%q}`, forkURL)); resp.Error != nil || resp.Result != true {
		t.Fatalf("review.addFork = %v, %+v, want true", resp.Result, resp.Error)
	}
	var forks []string
	decodeResult(t, call(t, server, "review.getForks", `{}`), "review.getForks", &forks)
	if len(forks) != 1 || forks[0] != storedURL {
		t.Fatalf("review.getForks served %v, want the normalized %q", forks, storedURL)
	}

	if resp := call(t, server, "review.removeFork", fmt.Sprintf(`{"url":%q}`, forkURL)); resp.Error != nil || resp.Result != true {
		t.Fatalf("review.removeFork = %v, %+v, want true", resp.Result, resp.Error)
	}
	decodeResult(t, call(t, server, "review.getForks", `{}`), "review.getForks", &forks)
	if len(forks) != 0 {
		t.Errorf("review.getForks still serves %v after a remove", forks)
	}

	assertAppError(t, call(t, server, "review.addFork", `{}`), "review.addFork", CodeInvalidParams, "")
	assertAppError(t, call(t, server, "review.removeFork", fmt.Sprintf(`{"url":%q}`, forkURL)),
		"review.removeFork", CodeNotFound, "NOT_FOUND")
}

// releaseShape is the subset of RPC.md's Release type these tests read.
type releaseShape struct {
	ID      string
	Subject string
	Version string
	Tag     string
}

// TestReleaseRoundTrip_createReadBackAndRetract drives release.createRelease
// through retract and pins the NOT_FOUND code a missing ref maps to.
func TestReleaseRoundTrip_createReadBackAndRetract(t *testing.T) {
	server := roundTripServer(t, "release")

	var created releaseShape
	decodeResult(t, call(t, server, "release.createRelease",
		`{"subject":"Release 1.0.0","version":"1.0.0","tag":"v1.0.0"}`), "release.createRelease", &created)
	if created.ID == "" || created.Version != "1.0.0" || created.Tag != "v1.0.0" {
		t.Fatalf("release.createRelease returned %+v", created)
	}

	var listed []releaseShape
	decodeResult(t, call(t, server, "release.getReleases", `{}`), "release.getReleases", &listed)
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("release.getReleases served %+v, want the release just created", listed)
	}

	var fetched releaseShape
	decodeResult(t, call(t, server, "release.getRelease", fmt.Sprintf(`{"ref":%q}`, created.ID)), "release.getRelease", &fetched)
	if fetched.Subject != created.Subject {
		t.Errorf("release.getRelease served subject %q, want %q", fetched.Subject, created.Subject)
	}

	// RPC.md 4.4 gives retractRelease the result true. The retraction is a marker
	// on the edit chain, so the release stays resolvable.
	if resp := call(t, server, "release.retractRelease", fmt.Sprintf(`{"ref":%q}`, created.ID)); resp.Error != nil || resp.Result != true {
		t.Fatalf("release.retractRelease = %v, %+v, want true", resp.Result, resp.Error)
	}
	decodeResult(t, call(t, server, "release.getRelease", fmt.Sprintf(`{"ref":%q}`, created.ID)), "release.getRelease", &fetched)
	if fetched.ID != created.ID {
		t.Errorf("release.getRelease served %q after a retract, want the release ref", fetched.ID)
	}

	assertAppError(t, call(t, server, "release.getRelease", `{"ref":"#commit:000000000000"}`), "release.getRelease", CodeNotFound, "NOT_FOUND")
	assertAppError(t, call(t, server, "release.retractRelease", `{}`), "release.retractRelease", CodeInvalidParams, "")
}

// TestSearchRoundTrip_findsPostThroughBothNames searches for a post through both
// registered names and pins the NOT_READY code the uninitialized server maps.
func TestSearchRoundTrip_findsPostThroughBothNames(t *testing.T) {
	server := roundTripServer(t, "social")
	decodeResult(t, call(t, server, "social.createPost", `{"content":"the migration notes"}`), "social.createPost", &postShape{})

	type searchShape struct {
		Query   string `json:"query"`
		Total   int    `json:"total"`
		Results []struct {
			RepoURL   string `json:"repo_url"`
			Hash      string `json:"hash"`
			Branch    string `json:"branch"`
			Type      string `json:"type"`
			Extension string `json:"extension"`
		} `json:"results"`
		TotalSearched int  `json:"total_searched"`
		HasMore       bool `json:"has_more"`
	}
	var found searchShape
	decodeResult(t, call(t, server, "search", `{"query":"migration","scope":"repository:my"}`), "search", &found)
	if found.Query != "migration" || found.Total != 1 {
		t.Fatalf("search served %+v, want one hit for migration", found)
	}
	hit := found.Results[0]
	if hit.Extension != "social" || hit.Type != "post" || hit.Branch != "gitmsg/social" || hit.Hash == "" {
		t.Errorf("search hit = %+v, want the social post just created", hit)
	}

	var alias searchShape
	decodeResult(t, call(t, server, "social.search", `{"query":"migration","scope":"repository:my"}`), "social.search", &alias)
	if alias.Total != found.Total || len(alias.Results) != len(found.Results) {
		t.Errorf("social.search served %d hits, want the %d search serves", alias.Total, found.Total)
	}

	// RPC.md 3: a call before initialize returns -32010 NOT_READY.
	fresh := NewServer(NewRegistry(), strings.NewReader(""), io.Discard)
	registerAll(fresh)
	assertAppError(t, call(t, fresh, "search", `{"query":"migration"}`), "search", CodeNotReady, "NOT_READY")
}
