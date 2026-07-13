// site_localwalk.go - a local-git commit source for the site items walk.
//
// The site items walk reads every commit reachable from a bucket ref tip. When
// the pusher has the repo locally (the git-spawned helper always does; `site
// push` threads its workspace down), reading those commit objects from the local
// odb via `git cat-file --batch` avoids one serial network GET per commit — the
// difference between ~12 commits/s over a high-latency bucket and reading a local
// pack in memory.
//
// Correctness rests on one invariant: the walk starts from a tip that already
// exists on the bucket (a ref points at it) and only ever descends into parents,
// so every commit it visits is an ancestor of a bucket ref tip. A push uploads
// objects BEFORE it moves refs, so any commit reachable from a bucket ref tip is
// present in BOTH stores, byte-for-byte (git objects are content-addressed: a
// given sha's bytes are identical everywhere it exists). Reading such a commit
// locally therefore yields exactly what the bucket GET would. A local miss (a
// shallow clone, a gc race) is not an error: the walk falls back to the bucket
// GET for that one object.
//
// Git is invoked with os/exec directly (not core/git) for the same reason
// helper_push.go documents: the helper has GIT_DIR in its environment and no
// worktree path, and objstore stays free of a core/git dependency.

package objstore

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

// localCommitSource reads commit objects from a local git odb via a single
// long-lived `git cat-file --batch` process, so the whole walk pays one process
// spawn instead of one per commit. Not safe for concurrent use (the walk is
// single-goroutine); a nil *localCommitSource is inert (every read misses),
// which is how a no-local-repo context degrades to bucket-only reads.
type localCommitSource struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	broken bool // a protocol/IO error retired the process; every later read misses
}

// newLocalCommitSource starts a `git cat-file --batch` bound to gitDir (or a
// workdir when gitDir is ""). It returns nil (no error) when neither is usable
// so callers get a bucket-only walk with no special-casing. gitDir is passed via
// GIT_DIR; a workdir is passed via -C so the same helper serves both entry
// points.
func newLocalCommitSource(gitDir, workdir string) *localCommitSource {
	if gitDir == "" && workdir == "" {
		return nil
	}
	args := []string{}
	if workdir != "" {
		args = append(args, "-C", workdir)
	}
	args = append(args, "cat-file", "--batch")
	cmd := exec.Command("git", args...)
	if gitDir != "" {
		cmd.Env = append(cmd.Environ(), "GIT_DIR="+gitDir)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil
	}
	if err := cmd.Start(); err != nil {
		return nil
	}
	return &localCommitSource{cmd: cmd, stdin: stdin, stdout: bufio.NewReaderSize(stdout, 1<<20)}
}

// close shuts the cat-file process down. Safe on a nil source.
func (s *localCommitSource) close() {
	if s == nil || s.cmd == nil {
		return
	}
	s.stdin.Close()
	_ = s.cmd.Wait()
}

// commit reads one commit's raw object body (the bytes after git's "commit
// <size>\0" header) from the local odb. ok is false — never an error — when the
// object is absent locally or the process has gone bad, so the caller falls back
// to the bucket GET for that sha.
func (s *localCommitSource) commit(sha string) (body []byte, ok bool) {
	return s.object(sha, "commit")
}

// object reads one object's raw body by name (a sha, or any rev-spec cat-file
// accepts, e.g. "refs/heads/main:README.md" for the front page's README blob),
// requiring the given object type. ok is false — never an error — on a miss or
// a type mismatch. A hard IO/protocol error retires the process (every later
// read then misses) rather than failing the caller.
func (s *localCommitSource) object(name, wantType string) (body []byte, ok bool) {
	if s == nil {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.broken {
		return nil, false
	}
	if _, err := io.WriteString(s.stdin, name+"\n"); err != nil {
		s.broken = true
		return nil, false
	}
	header, err := s.stdout.ReadString('\n')
	if err != nil {
		s.broken = true
		return nil, false
	}
	fields := strings.Fields(strings.TrimSpace(header))
	// "<name> missing" — absent locally (shallow clone, gc race) or an
	// unresolvable rev-spec; a clean miss.
	if len(fields) == 2 && fields[1] == "missing" {
		return nil, false
	}
	if len(fields) != 3 {
		// Malformed: don't try to consume a body we can't size.
		s.broken = true
		return nil, false
	}
	var size int64
	if _, err := fmt.Sscanf(fields[2], "%d", &size); err != nil {
		s.broken = true
		return nil, false
	}
	content := make([]byte, size)
	if _, err := io.ReadFull(s.stdout, content); err != nil {
		s.broken = true
		return nil, false
	}
	if _, err := s.stdout.Discard(1); err != nil { // trailing newline
		s.broken = true
		return nil, false
	}
	// The stream is consumed either way; a wrong type is just a miss.
	return content, fields[1] == wantType
}

// getCommit returns one commit for the walk, preferring the local odb and
// falling back to the bucket GET on a local miss. src may be nil (bucket-only).
// The bucket parse and the local parse share parseBucketCommit, so both paths
// yield an identical bucketCommit for the same sha.
func getCommit(src *localCommitSource, client *Client, prefix, sha string) (bucketCommit, error) {
	if body, ok := src.commit(sha); ok {
		return parseBucketCommit(sha, body)
	}
	return getBucketCommit(client, prefix, sha)
}
