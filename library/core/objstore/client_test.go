// client_test.go - Client credential modes
package objstore

import (
	"context"
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
	if client.cfg.AccessKey != "explicit" || client.anonymous {
		t.Errorf("creds = %q, anonymous = %v; want the config pair, signed", client.cfg.AccessKey, client.anonymous)
	}

	// No credentials in the config is anonymous, not an error: an unsigned client
	// reads a bucket that grants public GetObject. Writes name the credentials command.
	client, err = NewClient(Config{Bucket: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if !client.anonymous {
		t.Error("client with no credentials in its config is not anonymous")
	}
	_, _, err = client.do(context.Background(), http.MethodPut, "k", nil, []byte("x"), nil)
	if !errors.Is(err, errCredentialsRequired) || !strings.Contains(err.Error(), "gitsocial config credentials set") {
		t.Errorf("anonymous write error = %v, want it to name the credentials command", err)
	}

	// Half a pair is a typo, not a request for anonymous access.
	if _, err = NewClient(Config{Bucket: "b", AccessKey: "only-access"}); !errors.Is(err, errCredentialsRequired) {
		t.Errorf("half credential pair error = %v, want errCredentialsRequired", err)
	}
}

// TestClientForRemote_strayHalfPairStaysAnonymous pins that a lone AWS_* variable
// exported for other tooling leaves an anonymous read anonymous.
func TestClientForRemote_strayHalfPairStaysAnonymous(t *testing.T) {
	clearCredentialEnv(t)
	setCredentialsFile(t, "")
	t.Setenv("AWS_ACCESS_KEY_ID", "stray")
	client, _, err := ClientForRemote("s3://s3.example.com/bucket/repo", HelperEnv{})
	if err != nil {
		t.Fatalf("ClientForRemote: %v", err)
	}
	if !client.anonymous {
		t.Error("a stray half env pair must leave the client anonymous")
	}
}

// TestPutGetList_KeysWithReservedCharacters: a key holding characters SigV4 escapes stores, reads, lists and deletes under its own name, so the wire encoding is applied once.
func TestPutGetList_KeysWithReservedCharacters(t *testing.T) {
	client, bucket := testClient(t)
	keys := []string{
		"site/f/pkg/afl++/LICENSE.html",
		"site/f/a b/c.html",
		"site/f/q&a/what's~here.html",
		"site/f/héllo/café.html",
	}
	for _, key := range keys {
		if err := client.Put(key, []byte(key)); err != nil {
			t.Fatalf("Put(%q): %v", key, err)
		}
		if body, ok := bucket.Object(key); !ok || body != key {
			t.Errorf("Put(%q) stored %q under some other name (found = %v)", key, body, ok)
		}
		got, err := client.Get(key)
		if err != nil {
			t.Fatalf("Get(%q): %v", key, err)
		}
		if string(got) != key {
			t.Errorf("Get(%q) = %q", key, got)
		}
	}
	listed, err := client.List("site/f/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != len(keys) {
		t.Errorf("List returned %d keys, want %d: %v", len(listed), len(keys), listed)
	}
	for _, key := range keys {
		if err := client.Delete(key); err != nil {
			t.Fatalf("Delete(%q): %v", key, err)
		}
	}
	if rest, err := client.List("site/f/"); err != nil || len(rest) != 0 {
		t.Errorf("after delete: List = %v, err = %v", rest, err)
	}
}
