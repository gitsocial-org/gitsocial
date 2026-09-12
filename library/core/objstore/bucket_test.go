// bucket_test.go - the in-memory bucket fixture the transport tests share.
package objstore

import (
	"crypto/sha1"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/objstore/membucket"
)

// testClient spins a membucket-backed httptest server and returns a path-style Client pointed at it plus the bucket.
func testClient(t *testing.T) (*Client, *membucket.Bucket) {
	t.Helper()
	bucket := membucket.New()
	srv := httptest.NewServer(bucket)
	t.Cleanup(srv.Close)
	client, err := NewClient(Config{
		Endpoint: srv.URL, Bucket: "b", Region: "us-east-1",
		AccessKey: "k", SecretKey: "s", PathStyle: true,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	// Keep the attempt count and shrink the waits, so a retry test costs no real seconds.
	client.retryBackoff = []time.Duration{time.Millisecond, time.Millisecond}
	return client, bucket
}

// siteMarkerKey is a key in the namespace only the site layer claims, so a transport test can prove it wrote nothing there.
const siteMarkerKey = ".gitsocial/site/version"

// keyExists reports whether a bucket key is present.
func keyExists(client *Client, key string) bool {
	_, err := client.Get(key)
	return err == nil
}

// emptyTree is git's well-known empty tree sha.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// makeLooseCommit builds a git commit object (sha1-addressed, zlib loose format) with the given parent ("" for the root) and message.
func makeLooseCommit(t *testing.T, parent, message string, ts int64) (string, []byte) {
	t.Helper()
	body := "tree " + emptyTree + "\n"
	if parent != "" {
		body += "parent " + parent + "\n"
	}
	body += fmt.Sprintf("author Test User <test@example.com> %d +0000\n", ts)
	body += fmt.Sprintf("committer Test User <test@example.com> %d +0000\n\n", ts)
	body += message
	sha := fmt.Sprintf("%x", sha1.Sum([]byte(fmt.Sprintf("commit %d\x00%s", len(body), body))))
	loose, err := EncodeLooseObject("commit", []byte(body))
	if err != nil {
		t.Fatalf("EncodeLooseObject: %v", err)
	}
	return sha, loose
}
