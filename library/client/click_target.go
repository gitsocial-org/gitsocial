// click_target.go - The URL a click on a message opens: the serve URL of the workspace, else its site URL, else none.
package client

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/fetch"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/notifications"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/site"
)

// ServeDir returns the directory of the serve bucket of a workspace root.
func ServeDir(cacheDir, root string) string {
	return filepath.Join(cacheDir, "serve", fetch.URLHash(root))
}

// ServeURLPath returns the file in which a running serve keeps the base URL it serves for a workspace root.
func ServeURLPath(cacheDir, root string) string {
	return filepath.Join(ServeDir(cacheDir, root), "url")
}

// serveProbeTimeout bounds the probe of a serve URL before a message uses it.
const serveProbeTimeout = 300 * time.Millisecond

// ServeURLEntry returns the line a serve process keeps in its URL file: its pid and the base URL it serves.
func ServeURLEntry(pid int, url string) string {
	return strconv.Itoa(pid) + " " + url + "\n"
}

// clickBase returns the base URL a click opens: the URL of a serve that answers, else the site URL of the workspace, else "".
func clickBase(workdir, cacheDir string) string {
	if root, err := git.GetRootDir(workdir); err == nil {
		if data, err := os.ReadFile(ServeURLPath(cacheDir, root)); err == nil {
			if _, url, ok := strings.Cut(strings.TrimSpace(string(data)), " "); ok && serveAnswers(url) {
				return url
			}
		}
	}
	custom, err := site.ReadWorkspaceSiteCustomization(workdir)
	if err != nil {
		return ""
	}
	return custom.URL
}

// serveAnswers reports whether a GET of url answers with a 2xx or a 3xx within the probe timeout; a file left by a crashed serve does not.
func serveAnswers(url string) bool {
	client := &http.Client{Timeout: serveProbeTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

// itemClickURL returns the route of a workspace item under base, base for an item of another repository, and "" with no base.
func itemClickURL(base, workspaceURL string, n notifications.Notification) string {
	route := site.ItemRoute(n.Hash)
	if base == "" || route == "" || workspaceURL == "" || protocol.NormalizeURL(n.RepoURL) != workspaceURL {
		return base
	}
	return base + route
}

// messages returns the messages of new items, each with its click target.
func messages(workdir, cacheDir string, items []notifications.Notification) []notifications.DesktopMessage {
	base := clickBase(workdir, cacheDir)
	workspaceURL := protocol.NormalizeURL(gitmsg.ResolveRepoURL(workdir))
	itemURL := func(n notifications.Notification) string { return itemClickURL(base, workspaceURL, n) }
	return notifications.DesktopMessages(items, itemURL, base)
}
