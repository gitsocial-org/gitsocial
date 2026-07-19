// credentials.go - per-endpoint S3 credentials from ~/.config/gitsocial/credentials.json.
//
// One process-wide env pair cannot authenticate two providers in one
// invocation (`gitsocial push r2 aws`), so credentials can be stored per
// endpoint host — the granularity providers actually scope keys to. The file
// is an object keyed by the canonical s3 URL's endpoint host (e.g.
// "<account>.r2.cloudflarestorage.com", "s3.us-east-1.amazonaws.com",
// "127.0.0.1:9000"), each value an accessKey/secretKey pair. Resolution
// precedence: the GITSOCIAL_S3_* env pair (explicit override) > the file entry
// for the endpoint host > the AWS_* env pair — an AWS pair exported for some
// other tool must not shadow a correct file entry.

package objstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/gitsocial-org/gitsocial/library/core/settings"
)

// Credential is one endpoint host's key pair as stored in credentials.json.
type Credential struct {
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
}

// CredentialsPath returns the credentials file location under the user-config
// directory ($XDG_CONFIG_HOME, else ~/.config), next to settings.json.
func CredentialsPath() (string, error) {
	dir, err := settings.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve config dir: %w", err)
	}
	return filepath.Join(dir, "gitsocial", "credentials.json"), nil
}

// ReadCredentialsFile loads the credentials file. A missing file is an empty
// map (no error); a malformed file is an error the caller decides how to
// handle (the hot-path resolver warns and falls through, the CLI surfaces it).
func ReadCredentialsFile() (map[string]Credential, error) {
	path, err := CredentialsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]Credential{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	warnLoosePermissions(path)
	var creds map[string]Credential
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if creds == nil {
		creds = map[string]Credential{}
	}
	return creds, nil
}

// WriteCredentialsFile persists the credentials map with 0600 permissions,
// creating the parent directory when needed. An empty map still writes a file
// (an explicit "no entries" state after the last remove).
func WriteCredentialsFile(creds map[string]Credential) error {
	path, err := CredentialsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// credWarnOnce gates the per-process one-line warnings (loose permissions,
// malformed file) so a multi-remote push reading the file per remote doesn't
// repeat them.
var (
	credPermWarnOnce  sync.Once
	credParseWarnOnce sync.Once
)

// warnLoosePermissions prints a one-line stderr warning (once per process) when
// the credentials file is readable by group/other. Warns, never refuses.
func warnLoosePermissions(path string) {
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm()&0077 == 0 {
		return
	}
	credPermWarnOnce.Do(func() {
		fmt.Fprintf(os.Stderr, "gitsocial: %s is readable by other users (mode %o); tighten with chmod 600\n", path, info.Mode().Perm())
	})
}

// resolveCredentials returns the key pair to sign requests to an endpoint
// host, by precedence: the GITSOCIAL_S3_* env pair (explicit override), the
// credentials-file entry for the host, then the AWS_* env pair. Each tier
// applies only when both halves are present, so a stray single variable can't
// mix tiers. Returns empty strings when nothing resolves (the client build
// then fails with its credentials-required error). A malformed credentials
// file warns once on stderr and falls through — a broken file must not break
// pushes that were working off env vars.
func resolveCredentials(endpointHost string) (access, secret string) {
	if a, s := os.Getenv("GITSOCIAL_S3_ACCESS_KEY"), os.Getenv("GITSOCIAL_S3_SECRET_KEY"); a != "" && s != "" {
		return a, s
	}
	creds, err := ReadCredentialsFile()
	if err != nil {
		credParseWarnOnce.Do(func() {
			fmt.Fprintf(os.Stderr, "gitsocial: ignoring credentials file: %v\n", err)
		})
	} else if c, ok := creds[endpointHost]; ok && c.AccessKey != "" && c.SecretKey != "" {
		return c.AccessKey, c.SecretKey
	}
	if a, s := os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_SECRET_ACCESS_KEY"); a != "" && s != "" {
		return a, s
	}
	return "", ""
}
