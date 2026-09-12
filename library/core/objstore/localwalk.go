// localwalk.go - a local-git object source for the bucket walks
//
// A walk descends only into parents from a tip a bucket ref names, and a push
// uploads objects before it moves refs, so every commit it visits holds the same
// bytes in both stores. A local miss falls back to the bucket GET.

package objstore

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

// LocalCommitSource reads objects from a local git odb through one long-lived cat-file batch; a nil source is inert, so every read misses.
type LocalCommitSource struct {
	mu      sync.Mutex
	gitDir  string // the odb the batch reads, for the callers that must run their own git
	workdir string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	broken  bool // a protocol/IO error retired the process; every later read misses
}

// NewLocalCommitSource starts a cat-file batch bound to gitDir, or to a workdir when gitDir is ""; nil means a bucket-only walk.
func NewLocalCommitSource(gitDir, workdir string) *LocalCommitSource {
	if gitDir == "" && workdir == "" {
		return nil
	}
	args := []string{}
	if workdir != "" {
		args = append(args, "-C", workdir)
	}
	args = append(args, "cat-file", "--batch")
	cmd := exec.Command("git", args...)
	// A miss must stay a cheap local miss, so a partial clone does not lazily fetch each probe from its promisor.
	cmd.Env = append(cmd.Environ(), "GIT_NO_LAZY_FETCH=1")
	if gitDir != "" {
		cmd.Env = append(cmd.Env, "GIT_DIR="+gitDir)
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
	return &LocalCommitSource{gitDir: gitDir, workdir: workdir, cmd: cmd, stdin: stdin, stdout: bufio.NewReaderSize(stdout, 1<<20)}
}

// GitDir returns the odb the batch reads, for a caller that must run its own git.
func (s *LocalCommitSource) GitDir() string {
	if s == nil {
		return ""
	}
	return s.gitDir
}

// Workdir returns the working tree the batch runs in, "" when it reads a bare odb.
func (s *LocalCommitSource) Workdir() string {
	if s == nil {
		return ""
	}
	return s.workdir
}

// Close shuts the cat-file process down. Safe on a nil source.
func (s *LocalCommitSource) Close() {
	if s == nil || s.cmd == nil {
		return
	}
	s.stdin.Close()
	_ = s.cmd.Wait()
}

// Commit reads one commit's raw object body from the local odb; ok is false, not an error, on a miss.
func (s *LocalCommitSource) Commit(sha string) (body []byte, ok bool) {
	return s.Object(sha, "commit")
}

// Object reads one object's raw body by name, requiring the given type; an IO or protocol error retires the process rather than failing the caller.
func (s *LocalCommitSource) Object(name, wantType string) (body []byte, ok bool) {
	objType, body, ok := s.typed(name)
	return body, ok && objType == wantType
}

// typed reads one object's type and raw body by name, for a caller that takes whatever type the odb holds.
func (s *LocalCommitSource) typed(name string) (objType string, body []byte, ok bool) {
	if s == nil {
		return "", nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.broken {
		return "", nil, false
	}
	if _, err := io.WriteString(s.stdin, name+"\n"); err != nil {
		s.broken = true
		return "", nil, false
	}
	header, err := s.stdout.ReadString('\n')
	if err != nil {
		s.broken = true
		return "", nil, false
	}
	fields := strings.Fields(strings.TrimSpace(header))
	// A "missing" answer is a clean miss: absent locally, or an unresolvable rev-spec.
	if len(fields) == 2 && fields[1] == "missing" {
		return "", nil, false
	}
	if len(fields) != 3 {
		// Malformed, so do not try to consume a body of unknown size.
		s.broken = true
		return "", nil, false
	}
	var size int64
	if _, err := fmt.Sscanf(fields[2], "%d", &size); err != nil {
		s.broken = true
		return "", nil, false
	}
	content := make([]byte, size)
	if _, err := io.ReadFull(s.stdout, content); err != nil {
		s.broken = true
		return "", nil, false
	}
	if _, err := s.stdout.Discard(1); err != nil { // trailing newline
		s.broken = true
		return "", nil, false
	}
	return fields[1], content, true
}
