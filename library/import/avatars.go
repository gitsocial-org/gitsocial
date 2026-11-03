// avatars.go - copy the picture of each workspace author into the avatar store and the site bucket
package importpkg

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/objstore"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/site"
)

const (
	avatarDefaultLimit = 200
	avatarMissTTL      = 30 * 24 * time.Hour
	avatarFetchTimeout = 10 * time.Second
	avatarMaxOrigins   = 4
)

// ErrRateLimited is the error a resolver returns when the forge refuses more lookups; the run stops on it.
var ErrRateLimited = errors.New("forge rate limit reached")

// AvatarOptions configures one avatar run.
type AvatarOptions struct {
	WorkDir   string
	RepoURL   string
	RemoteURL string // the s3 remote, "" to fill only the store
	CacheDir  string
	Limit     int
	DryRun    bool
	SetEmail  string
	SetFile   string
	Source    string                                                              // the forge name the memo records
	Resolve   func(email string, originURLs []string, sha string) (string, error) // the picture URL of an author, "" for none
	NoCall    func(email string) bool                                             // reports an email that resolves with no API call
}

// AvatarStats counts what a run found and did.
type AvatarStats struct {
	Done        int
	FreshMisses int
	NoCall      int
	Lookup      int
	Uploaded    int
	Missed      int
	Failed      int
	Remaining   int
	RateLimited bool
}

// avatarMemo is the memo entry of one email: a hit or a miss, when, and from which forge.
type avatarMemo struct {
	Status string    `json:"status"`
	At     time.Time `json:"at"`
	Source string    `json:"source"`
}

// avatarAuthor is one distinct author email with the origin URLs of up to avatarMaxOrigins items and the sha of one code commit.
type avatarAuthor struct {
	email      string
	originURLs []string
	sha        string
}

// avatarDownloadError is a download that the server answered with a status other than 200.
type avatarDownloadError struct{ status int }

// Error renders the status of the refused download.
func (e *avatarDownloadError) Error() string {
	return fmt.Sprintf("download avatar: HTTP %d", e.status)
}

// RunAvatars copies the picture of each author into the store and the bucket, then writes the manifest, then the memo; with no remote it fills only the store.
func RunAvatars(opts AvatarOptions, progress func(string)) (AvatarStats, error) {
	var stats AvatarStats
	client, prefix, listed, err := openAvatarBucket(opts.RemoteURL)
	if err != nil {
		return stats, err
	}
	storeDir := site.AvatarStoreDir(opts.CacheDir, opts.RepoURL)
	if opts.SetEmail != "" || opts.SetFile != "" {
		return setAvatar(opts, client, prefix, listed, storeDir)
	}
	stored, err := site.ListAvatarStore(storeDir)
	if err != nil {
		return stats, err
	}
	authors, err := collectAvatarAuthors(opts.WorkDir)
	if err != nil {
		return stats, err
	}
	memoPath := avatarMemoPath(opts.CacheDir, opts.RepoURL)
	memo, err := readAvatarMemo(memoPath)
	if err != nil {
		return stats, err
	}
	done := map[string]bool{}
	for _, k := range append(stored, listed...) {
		done[k] = true
	}
	now := time.Now()
	var todo []avatarAuthor
	for _, a := range authors {
		if done[site.AvatarKey(a.email)] {
			stats.Done++
			continue
		}
		if m, ok := memo[a.email]; ok && m.Status == "miss" && now.Sub(m.At) < avatarMissTTL {
			stats.FreshMisses++
			continue
		}
		free := opts.NoCall != nil && opts.NoCall(a.email)
		if !free && len(a.originURLs) == 0 && a.sha == "" {
			continue
		}
		if free {
			stats.NoCall++
		} else {
			stats.Lookup++
		}
		todo = append(todo, a)
	}
	if opts.DryRun {
		return stats, nil
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = avatarDefaultLimit
	}
	if len(todo) > limit {
		stats.Remaining, todo = len(todo)-limit, todo[:limit]
	}
	var uploaded []string
	var runErr error
	for i, a := range todo {
		if progress != nil {
			progress(fmt.Sprintf("avatar %d/%d %s", i+1, len(todo), a.email))
		}
		src, err := opts.Resolve(a.email, a.originURLs, a.sha)
		if errors.Is(err, ErrRateLimited) {
			stats.RateLimited, stats.Remaining = true, stats.Remaining+len(todo)-i
			break
		}
		var data []byte
		var contentType string
		if err == nil && src != "" {
			data, contentType, err = fetchAvatarImage(src)
		}
		if err != nil && !avatarMissError(err) {
			stats.Failed++
			continue
		}
		if err != nil || src == "" {
			memo[a.email] = avatarMemo{Status: "miss", At: now, Source: opts.Source}
			stats.Missed++
			continue
		}
		key := site.AvatarKey(a.email)
		if err := storeAvatar(client, prefix, storeDir, key, data, contentType); err != nil {
			runErr, stats.Remaining = err, stats.Remaining+len(todo)-i
			break
		}
		memo[a.email] = avatarMemo{Status: "hit", At: now, Source: opts.Source}
		uploaded = append(uploaded, key)
		stats.Uploaded++
	}
	if keys := append(listed, uploaded...); client != nil && len(keys) > 0 {
		if err := site.WriteAvatarManifest(client, prefix, keys); err != nil {
			return stats, err
		}
	}
	if err := writeAvatarMemo(memoPath, memo); err != nil {
		return stats, err
	}
	return stats, runErr
}

// setAvatar stores and uploads one local image under the key of one email and adds the key to the manifest.
func setAvatar(opts AvatarOptions, client *objstore.Client, prefix string, listed []string, storeDir string) (AvatarStats, error) {
	if opts.SetEmail == "" || opts.SetFile == "" {
		return AvatarStats{}, fmt.Errorf("set needs an email and a file")
	}
	raw, err := os.ReadFile(opts.SetFile)
	if err != nil {
		return AvatarStats{}, fmt.Errorf("read avatar: %w", err)
	}
	data, contentType, err := site.CheckAvatarImage(raw)
	if err != nil {
		return AvatarStats{}, err
	}
	if opts.DryRun {
		return AvatarStats{}, nil
	}
	key := site.AvatarKey(opts.SetEmail)
	if err := storeAvatar(client, prefix, storeDir, key, data, contentType); err != nil {
		return AvatarStats{}, err
	}
	if client != nil {
		if err := site.WriteAvatarManifest(client, prefix, append(listed, key)); err != nil {
			return AvatarStats{}, err
		}
	}
	return AvatarStats{Uploaded: 1}, nil
}

// storeAvatar writes one picture to the store through a temp file and a rename, then uploads it when there is a bucket.
func storeAvatar(client *objstore.Client, prefix, storeDir, key string, data []byte, contentType string) error {
	if err := os.MkdirAll(storeDir, 0700); err != nil {
		return fmt.Errorf("create avatar store: %w", err)
	}
	tmp, err := os.CreateTemp(storeDir, key+".*.tmp")
	if err != nil {
		return fmt.Errorf("create avatar store file: %w", err)
	}
	_, err = tmp.Write(data)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), filepath.Join(storeDir, key))
	}
	if err != nil {
		_ = os.Remove(tmp.Name()) // the temp file is garbage after a failed write
		return fmt.Errorf("write avatar store: %w", err)
	}
	if client == nil {
		return nil
	}
	return site.PutAvatar(client, prefix, key, data, contentType)
}

// collectAvatarAuthors lists the distinct author emails of the code branches and the gitmsg branches, sorted by email.
func collectAvatarAuthors(workdir string) ([]avatarAuthor, error) {
	byEmail := map[string]*avatarAuthor{}
	add := func(email, originURL, sha string) {
		email = strings.ToLower(strings.TrimSpace(email))
		if email == "" {
			return
		}
		a := byEmail[email]
		if a == nil {
			a = &avatarAuthor{email: email}
			byEmail[email] = a
		}
		if originURL != "" && len(a.originURLs) < avatarMaxOrigins && !slices.Contains(a.originURLs, originURL) {
			a.originURLs = append(a.originURLs, originURL)
		}
		// The log is newest first, so the last sha of an author is the oldest, the one a forge is most likely to hold.
		if sha != "" {
			a.sha = sha
		}
	}
	code, err := git.ExecGit(workdir, []string{"log", "--format=%ae%x00%H", "--exclude=gitmsg/*", "--branches"})
	if err != nil {
		return nil, fmt.Errorf("list code authors: %w", err)
	}
	for _, line := range strings.Split(code.Stdout, "\n") {
		if email, sha, ok := strings.Cut(line, "\x00"); ok {
			add(email, "", sha)
		}
	}
	refs, err := git.ExecGit(workdir, []string{"for-each-ref", "--format=%(refname)", "refs/heads/gitmsg/"})
	if err != nil {
		return nil, fmt.Errorf("list gitmsg branches: %w", err)
	}
	for _, ref := range strings.Fields(refs.Stdout) {
		out, err := git.ExecGit(workdir, []string{"log", "--format=%ae%x00%B%x00", ref})
		if err != nil {
			return nil, fmt.Errorf("list authors of %s: %w", ref, err)
		}
		fields := strings.Split(out.Stdout, "\x00")
		for i := 0; i+1 < len(fields); i += 2 {
			add(fields[i], "", "")
			msg := protocol.ParseMessage(fields[i+1])
			if msg == nil {
				continue
			}
			if origin := protocol.ExtractOrigin(&msg.Header); origin != nil {
				// The URL of an edit names its canonical item, whose author can be another person.
				if msg.Header.Fields["edits"] != "" {
					origin.URL = ""
				}
				add(origin.AuthorEmail, origin.URL, "")
			}
		}
	}
	authors := make([]avatarAuthor, 0, len(byEmail))
	for _, a := range byEmail {
		authors = append(authors, *a)
	}
	sort.Slice(authors, func(i, j int) bool { return authors[i].email < authors[j].email })
	return authors, nil
}

// openAvatarBucket returns the client, the prefix and the listed avatar keys of the s3 remote, a nil client with no remote.
func openAvatarBucket(remoteURL string) (*objstore.Client, string, []string, error) {
	if remoteURL == "" {
		return nil, "", nil, nil
	}
	client, prefix, err := objstore.ClientForRemote(remoteURL, objstore.HelperEnvFromOS())
	if err != nil {
		return nil, "", nil, err
	}
	listed, err := listAvatarKeys(client, prefix)
	return client, prefix, listed, err
}

// listAvatarKeys lists the avatar keys the bucket holds a picture for.
func listAvatarKeys(client *objstore.Client, prefix string) ([]string, error) {
	keys, err := client.List(prefix + site.AvatarsDir)
	if err != nil {
		return nil, fmt.Errorf("list avatars: %w", err)
	}
	listed := make([]string, 0, len(keys))
	for _, k := range keys {
		if name := strings.TrimPrefix(k, prefix+site.AvatarsDir); len(name) == 16 && !strings.Contains(name, "/") {
			listed = append(listed, name)
		}
	}
	return listed, nil
}

// fetchAvatarImage downloads a picture and checks that it is a PNG, JPEG or GIF of at most site.AvatarMaxBytes.
func fetchAvatarImage(src string) ([]byte, string, error) {
	resp, err := (&http.Client{Timeout: avatarFetchTimeout}).Get(src)
	if err != nil {
		return nil, "", fmt.Errorf("download avatar: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", &avatarDownloadError{status: resp.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, site.AvatarMaxBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("download avatar: %w", err)
	}
	return site.CheckAvatarImage(data)
}

// avatarMissError reports an error that makes a miss to memo: a download the server refused with a 4xx, or bytes that are not a picture; a transport error, a timeout or a 5xx is a failure to retry.
func avatarMissError(err error) bool {
	var de *avatarDownloadError
	if errors.As(err, &de) {
		return de.status >= 400 && de.status < 500
	}
	return errors.Is(err, site.ErrAvatarNotImage)
}

// avatarMemoPath is the memo file of a repository under the cache directory.
func avatarMemoPath(cacheDir, repoURL string) string {
	return filepath.Join(cacheDir, "imports", site.RepoSlug(repoURL)+".avatars.json")
}

// readAvatarMemo loads the memo; an absent file is an empty memo.
func readAvatarMemo(path string) (map[string]avatarMemo, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]avatarMemo{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read avatar memo: %w", err)
	}
	memo := map[string]avatarMemo{}
	if err := json.Unmarshal(data, &memo); err != nil {
		return nil, fmt.Errorf("avatar memo %s is corrupt: remove it", path)
	}
	return memo, nil
}

// writeAvatarMemo writes the memo through a temp file and a rename.
func writeAvatarMemo(path string, memo map[string]avatarMemo) error {
	data, err := json.MarshalIndent(memo, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal avatar memo: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create import dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0600); err != nil {
		return fmt.Errorf("write avatar memo: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename avatar memo: %w", err)
	}
	return nil
}
