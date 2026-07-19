// credentials_test.go - per-endpoint credentials: file parsing (missing,
// malformed, unknown host), the env > file > AWS_* precedence ladder, and
// host-keyed resolution through clientForRemote.

package objstore

import (
	"os"
	"path/filepath"
	"testing"
)

// setCredentialsFile points XDG_CONFIG_HOME at a temp dir and writes the given
// credentials.json content there (no file when content is "").
func setCredentialsFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "gitsocial", "credentials.json")
	if content != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write credentials: %v", err)
		}
	}
	return path
}

// clearCredentialEnv blanks every credential env var so precedence tests start
// from a clean slate regardless of the developer's shell.
func clearCredentialEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"GITSOCIAL_S3_ACCESS_KEY", "GITSOCIAL_S3_SECRET_KEY", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"} {
		t.Setenv(name, "")
	}
}

func TestResolveCredentials_precedence(t *testing.T) {
	fileJSON := `{"s3.example.com":{"accessKey":"file-ak","secretKey":"file-sk"}}`

	t.Run("gitsocial env pair beats the file", func(t *testing.T) {
		clearCredentialEnv(t)
		setCredentialsFile(t, fileJSON)
		t.Setenv("GITSOCIAL_S3_ACCESS_KEY", "env-ak")
		t.Setenv("GITSOCIAL_S3_SECRET_KEY", "env-sk")
		if a, s := resolveCredentials("s3.example.com"); a != "env-ak" || s != "env-sk" {
			t.Errorf("resolved %q/%q, want the env pair", a, s)
		}
	})

	t.Run("file entry beats the AWS pair", func(t *testing.T) {
		clearCredentialEnv(t)
		setCredentialsFile(t, fileJSON)
		t.Setenv("AWS_ACCESS_KEY_ID", "aws-ak")
		t.Setenv("AWS_SECRET_ACCESS_KEY", "aws-sk")
		if a, s := resolveCredentials("s3.example.com"); a != "file-ak" || s != "file-sk" {
			t.Errorf("resolved %q/%q, want the file entry (AWS_* must not shadow it)", a, s)
		}
	})

	t.Run("unknown host falls back to the AWS pair", func(t *testing.T) {
		clearCredentialEnv(t)
		setCredentialsFile(t, fileJSON)
		t.Setenv("AWS_ACCESS_KEY_ID", "aws-ak")
		t.Setenv("AWS_SECRET_ACCESS_KEY", "aws-sk")
		if a, s := resolveCredentials("other.example.com"); a != "aws-ak" || s != "aws-sk" {
			t.Errorf("resolved %q/%q, want the AWS fallback", a, s)
		}
	})

	t.Run("half an env pair does not win", func(t *testing.T) {
		clearCredentialEnv(t)
		setCredentialsFile(t, fileJSON)
		t.Setenv("GITSOCIAL_S3_ACCESS_KEY", "lone-ak")
		if a, s := resolveCredentials("s3.example.com"); a != "file-ak" || s != "file-sk" {
			t.Errorf("resolved %q/%q, want the file entry (a lone env var must not mix tiers)", a, s)
		}
	})

	t.Run("nothing resolves to empty", func(t *testing.T) {
		clearCredentialEnv(t)
		setCredentialsFile(t, "")
		if a, s := resolveCredentials("s3.example.com"); a != "" || s != "" {
			t.Errorf("resolved %q/%q, want empty", a, s)
		}
	})

	t.Run("malformed file falls through to AWS pair", func(t *testing.T) {
		clearCredentialEnv(t)
		setCredentialsFile(t, `{not json`)
		t.Setenv("AWS_ACCESS_KEY_ID", "aws-ak")
		t.Setenv("AWS_SECRET_ACCESS_KEY", "aws-sk")
		if a, s := resolveCredentials("s3.example.com"); a != "aws-ak" || s != "aws-sk" {
			t.Errorf("resolved %q/%q, want the AWS fallback past a malformed file", a, s)
		}
	})
}

func TestReadCredentialsFile(t *testing.T) {
	t.Run("missing file is an empty map, no error", func(t *testing.T) {
		setCredentialsFile(t, "")
		creds, err := ReadCredentialsFile()
		if err != nil || len(creds) != 0 {
			t.Fatalf("ReadCredentialsFile = %v, %v; want empty map, nil", creds, err)
		}
	})

	t.Run("malformed file is an error", func(t *testing.T) {
		setCredentialsFile(t, `{not json`)
		if _, err := ReadCredentialsFile(); err == nil {
			t.Fatal("malformed credentials file must surface an error to the CLI")
		}
	})

	t.Run("round trip through write preserves entries and 0600", func(t *testing.T) {
		path := setCredentialsFile(t, "")
		in := map[string]Credential{"127.0.0.1:9000": {AccessKey: "ak", SecretKey: "sk"}}
		if err := WriteCredentialsFile(in); err != nil {
			t.Fatalf("WriteCredentialsFile: %v", err)
		}
		out, err := ReadCredentialsFile()
		if err != nil || out["127.0.0.1:9000"] != in["127.0.0.1:9000"] {
			t.Fatalf("round trip = %v, %v", out, err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if info.Mode().Perm() != 0600 {
			t.Errorf("credentials file mode = %o, want 0600", info.Mode().Perm())
		}
	})
}

// TestClientForRemote_credentialsByHost pins the host keying end to end: two
// remotes on different endpoint hosts each pick their own file entry when
// building the signing client — the property that lets one `gitsocial push r2
// aws` invocation authenticate two providers.
func TestClientForRemote_credentialsByHost(t *testing.T) {
	clearCredentialEnv(t)
	setCredentialsFile(t, `{
		"a.example.com": {"accessKey": "ak-a", "secretKey": "sk-a"},
		"b.example.com": {"accessKey": "ak-b", "secretKey": "sk-b"}
	}`)
	for host, wantAK := range map[string]string{"a.example.com": "ak-a", "b.example.com": "ak-b"} {
		client, _, _, err := clientForRemote("s3://"+host+"/bucket/repo", HelperEnv{})
		if err != nil {
			t.Fatalf("clientForRemote(%s): %v", host, err)
		}
		if client.cfg.AccessKey != wantAK {
			t.Errorf("host %s signed with access key %q, want %q", host, client.cfg.AccessKey, wantAK)
		}
	}
}
