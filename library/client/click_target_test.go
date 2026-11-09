// click_target_test.go - Tests for the click target of a message: the serve URL, then the site URL, then none.
package client

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/notifications"
	"github.com/gitsocial-org/gitsocial/library/core/site"
	"github.com/gitsocial-org/gitsocial/library/internal/testutil"
)

func TestClickTarget_precedence(t *testing.T) {
	template, err := testutil.NewRepoTemplate()
	if err != nil {
		t.Fatalf("repo template: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(template) })
	workdir, cacheDir := testutil.CopyRepo(t, template), t.TempDir()
	own := notifications.Notification{RepoURL: gitmsg.ResolveRepoURL(workdir), Hash: "0123456789abcdef0123", Type: "mention"}
	other := notifications.Notification{RepoURL: "https://example.com/other/repo", Hash: "fedcba9876543210fedc", Type: "mention"}
	urls := func() []string {
		all := messages(workdir, cacheDir, []notifications.Notification{own, other})
		out := make([]string, 0, len(all))
		for _, m := range all {
			out = append(out, m.URL)
		}
		return out
	}
	if got := urls(); got[0] != "" || got[1] != "" {
		t.Errorf("no site and no serve gave %q, want no click target", got)
	}
	if err := site.WriteWorkspaceSiteCustomization(workdir, site.SiteCustomization{URL: "https://site.example/"}); err != nil {
		t.Fatalf("write site url: %v", err)
	}
	if got := urls(); got[0] != "https://site.example/index.html#commit:0123456789ab" || got[1] != "https://site.example/" {
		t.Errorf("site url gave %q, want the item route and the site root for another repository", got)
	}
	root, err := git.GetRootDir(workdir)
	if err != nil {
		t.Fatalf("root: %v", err)
	}
	if err := os.MkdirAll(ServeDir(cacheDir, root), 0o755); err != nil {
		t.Fatalf("serve dir: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	serveURL := server.URL + "/gitsocial/repo/"
	if err := os.WriteFile(ServeURLPath(cacheDir, root), []byte(ServeURLEntry(os.Getpid(), serveURL)), 0o644); err != nil {
		t.Fatalf("write serve url: %v", err)
	}
	if got := urls(); got[0] != serveURL+"index.html#commit:0123456789ab" {
		t.Errorf("a running serve gave %q, want the serve URL before the site URL", got)
	}
	summary := messages(workdir, cacheDir, []notifications.Notification{own, own, own, own})
	if len(summary) != 1 || summary[0].URL != serveURL {
		t.Errorf("summary = %+v, want the serve root", summary)
	}
}

func TestClickTarget_staleServe(t *testing.T) {
	template, err := testutil.NewRepoTemplate()
	if err != nil {
		t.Fatalf("repo template: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(template) })
	workdir, cacheDir := testutil.CopyRepo(t, template), t.TempDir()
	if err := site.WriteWorkspaceSiteCustomization(workdir, site.SiteCustomization{URL: "https://site.example/"}); err != nil {
		t.Fatalf("write site url: %v", err)
	}
	root, err := git.GetRootDir(workdir)
	if err != nil {
		t.Fatalf("root: %v", err)
	}
	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closed.Close()
	if err := os.MkdirAll(ServeDir(cacheDir, root), 0o755); err != nil {
		t.Fatalf("serve dir: %v", err)
	}
	if err := os.WriteFile(ServeURLPath(cacheDir, root), []byte(ServeURLEntry(1, closed.URL+"/gitsocial/repo/")), 0o644); err != nil {
		t.Fatalf("write serve url: %v", err)
	}
	if got := clickBase(workdir, cacheDir); got != "https://site.example/" {
		t.Errorf("a serve file of a closed port gave %q, want the site URL", got)
	}
}
