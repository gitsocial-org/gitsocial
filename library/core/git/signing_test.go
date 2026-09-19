// signing_test.go - Tests for signer-key extraction from signed commits
package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupSSHSigningRepo builds a repo configured to sign with a throwaway SSH key
// and to trust that key when verifying. Returns the repo dir and the key's
// SHA256 fingerprint.
func setupSSHSigningRepo(t *testing.T) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not available")
	}
	base := t.TempDir()
	keyPath := filepath.Join(base, "signing_key")
	out, err := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-C", "signer@example.com", "-q", "-f", keyPath).CombinedOutput()
	if err != nil {
		t.Skipf("ssh-keygen failed: %v: %s", err, out)
	}
	fingerprint, err := exec.Command("ssh-keygen", "-lf", keyPath+".pub").Output()
	if err != nil {
		t.Fatalf("ssh-keygen -lf: %v", err)
	}
	fields := strings.Fields(string(fingerprint))
	if len(fields) < 2 {
		t.Fatalf("unexpected ssh-keygen -lf output: %q", fingerprint)
	}

	pub, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatalf("read public key: %v", err)
	}
	allowed := filepath.Join(base, "allowed_signers")
	entry := "signer@example.com namespaces=\"git\" " + strings.TrimSpace(string(pub)) + "\n"
	if err := os.WriteFile(allowed, []byte(entry), 0600); err != nil {
		t.Fatalf("write allowed_signers: %v", err)
	}

	dir := filepath.Join(base, "repo")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	if err := Init(dir, "main"); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	for _, cfg := range [][]string{
		{"config", "user.email", "signer@example.com"},
		{"config", "user.name", "Signer"},
		{"config", "gpg.format", "ssh"},
		{"config", "user.signingkey", keyPath + ".pub"},
		{"config", "gpg.ssh.allowedSignersFile", allowed},
	} {
		if _, err := ExecGit(dir, cfg); err != nil {
			t.Fatalf("git %v: %v", cfg, err)
		}
	}
	return dir, fields[1]
}

// signedCommit writes a signed empty-tree commit and returns its full hash.
func signedCommit(t *testing.T, dir, message, parent string) string {
	t.Helper()
	args := []string{"commit-tree", emptyTree, "-m", message, "-S"}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	hash, err := execGitSimple(dir, args)
	if err != nil {
		t.Fatalf("signed commit-tree: %v", err)
	}
	return strings.TrimSpace(hash)
}

func TestGetCommitSignerKey_sshSignedCommits(t *testing.T) {
	dir, fingerprint := setupSSHSigningRepo(t)

	hash := signedCommit(t, dir, "signed root", "")
	if len(hash) < 40 {
		t.Fatalf("signed commit = %q, want a full object id", hash)
	}

	format, key, err := GetCommitSignerKey(dir, hash)
	if err != nil {
		t.Fatalf("GetCommitSignerKey() error = %v", err)
	}
	if format != "ssh" {
		t.Errorf("format = %q, want ssh", format)
	}
	if key != fingerprint {
		t.Errorf("key = %q, want %q", key, fingerprint)
	}

	child := signedCommit(t, dir, "signed child", hash)
	parents, err := ExecGit(dir, []string{"log", "-1", "--format=%P", child})
	if err != nil {
		t.Fatalf("read parents: %v", err)
	}
	if strings.TrimSpace(parents.Stdout) != hash {
		t.Errorf("child parent = %q, want %q", strings.TrimSpace(parents.Stdout), hash)
	}

	keys, err := GetCommitSignerKeys(dir, []string{hash, child})
	if err != nil {
		t.Fatalf("GetCommitSignerKeys() error = %v", err)
	}
	if keys[hash] != fingerprint || keys[child] != fingerprint {
		t.Errorf("GetCommitSignerKeys() = %v, want both hashes at %q", keys, fingerprint)
	}
	if keys[hash[:12]] != fingerprint {
		t.Errorf("GetCommitSignerKeys() should also key on the 12-char abbreviation, got %v", keys)
	}
}

func TestGetCommitSignerKey_unsigned(t *testing.T) {
	dir := initTestRepo(t)
	commits, err := GetCommits(dir, &GetCommitsOptions{Branch: "main"})
	if err != nil || len(commits) == 0 {
		t.Fatalf("GetCommits() = %d commits, err = %v", len(commits), err)
	}

	if _, _, err := GetCommitSignerKey(dir, commits[0].Hash); err == nil {
		t.Error("GetCommitSignerKey() on an unsigned commit should error")
	}
	keys, err := GetCommitSignerKeys(dir, []string{commits[0].Hash})
	if err != nil {
		t.Fatalf("GetCommitSignerKeys() error = %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("GetCommitSignerKeys() = %v, want no entries for unsigned commits", keys)
	}
}

func TestGetCommitSignerKeys_empty(t *testing.T) {
	dir := initTestRepo(t)
	keys, err := GetCommitSignerKeys(dir, nil)
	if err != nil {
		t.Fatalf("GetCommitSignerKeys(nil) error = %v", err)
	}
	if keys != nil {
		t.Errorf("GetCommitSignerKeys(nil) = %v, want nil", keys)
	}
}
