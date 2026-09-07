// client_test.go - Client credential modes
package objstore

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

// TestNewClient_credentialModes pins the three ways a config resolves: a full
// pair signs, no pair reads unsigned, half a pair is a typo. The env vars are
// set to show NewClient ignores them; resolveCredentials is the one resolver.
func TestNewClient_credentialModes(t *testing.T) {
	t.Setenv("GITSOCIAL_S3_ACCESS_KEY", "gs-key")
	t.Setenv("GITSOCIAL_S3_SECRET_KEY", "gs-secret")
	t.Setenv("AWS_ACCESS_KEY_ID", "aws-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "aws-secret")

	client, err := NewClient(Config{Bucket: "b", AccessKey: "explicit", SecretKey: "explicit-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if client.cfg.AccessKey != "explicit" || client.Anonymous() {
		t.Errorf("creds = %q, anonymous = %v; want the config pair, signed", client.cfg.AccessKey, client.Anonymous())
	}

	// No credentials in the config is anonymous, not an error: an unsigned client
	// reads a bucket that grants public GetObject. Writes name the missing pairs.
	client, err = NewClient(Config{Bucket: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if !client.Anonymous() {
		t.Error("client with no credentials in its config is not anonymous")
	}
	_, err = client.do(http.MethodPut, "k", nil, []byte("x"), nil)
	if !errors.Is(err, ErrCredentialsRequired) || !strings.Contains(err.Error(), "GITSOCIAL_S3_ACCESS_KEY") {
		t.Errorf("anonymous write error = %v, want it to name both variable sets", err)
	}

	// Half a pair is a typo, not a request for anonymous access.
	if _, err = NewClient(Config{Bucket: "b", AccessKey: "only-access"}); !errors.Is(err, ErrCredentialsRequired) {
		t.Errorf("half credential pair error = %v, want ErrCredentialsRequired", err)
	}
}

// TestClientForRemote_strayHalfPairStaysAnonymous pins that a lone AWS_* variable
// exported for other tooling leaves an anonymous read anonymous.
func TestClientForRemote_strayHalfPairStaysAnonymous(t *testing.T) {
	clearCredentialEnv(t)
	setCredentialsFile(t, "")
	t.Setenv("AWS_ACCESS_KEY_ID", "stray")
	client, _, _, err := clientForRemote("s3://s3.example.com/bucket/repo", HelperEnv{})
	if err != nil {
		t.Fatalf("clientForRemote: %v", err)
	}
	if !client.Anonymous() {
		t.Error("a stray half env pair must leave the client anonymous")
	}
}
