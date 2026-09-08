// anonymous_test.go - unsigned reads of a public bucket that denies listing.

package objstore

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// publicNoListBucket answers like a public bucket does to an unsigned reader:
// GETs served, listing denied, and an absent key 403 rather than 404.
func publicNoListBucket(t *testing.T, seed map[string]string) string {
	return publicBucket(t, seed, http.StatusForbidden)
}

// publicBucket serves GETs and answers listings and absent keys with status: 403
// from a bucket's own endpoint, 404 from a web domain in front of it.
func publicBucket(t *testing.T, seed map[string]string, status int) string {
	t.Helper()
	mem := newMemBucket()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("list-type") {
			http.Error(w, "AccessDenied", status)
			return
		}
		rec := httptest.NewRecorder()
		mem.ServeHTTP(rec, r)
		if rec.Code == http.StatusNotFound {
			http.Error(w, "AccessDenied", status)
			return
		}
		for k, vs := range rec.Header() {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(rec.Code)
		w.Write(rec.Body.Bytes())
	}))
	t.Cleanup(srv.Close)
	url := "s3://" + strings.TrimPrefix(srv.URL, "http://") + "/b/repo"
	t.Setenv("GITSOCIAL_S3_ACCESS_KEY", "k")
	t.Setenv("GITSOCIAL_S3_SECRET_KEY", "s")
	client, prefix, _, err := clientForRemote(url, HelperEnv{})
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range seed {
		if err := putObject(client, prefix, k, []byte(v), ""); err != nil {
			t.Fatal(err)
		}
	}
	return url
}

func anonClient(t *testing.T, url string) (*Client, string) {
	t.Helper()
	t.Setenv("GITSOCIAL_S3_ACCESS_KEY", "")
	t.Setenv("GITSOCIAL_S3_SECRET_KEY", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	client, prefix, _, err := clientForRemote(url, HelperEnv{})
	if err != nil {
		t.Fatal(err)
	}
	if !client.Anonymous() {
		t.Fatal("client should be anonymous")
	}
	return client, prefix
}

const shaA = "1111111111111111111111111111111111111111"
const shaB = "2222222222222222222222222222222222222222"
const shaStale = "3333333333333333333333333333333333333333"

func TestAnon_ManifestSource(t *testing.T) {
	// The manifest names both refs; refs/heads/main's own key holds a NEWER sha
	// than the manifest claims, so the GET must win.
	url := publicNoListBucket(t, map[string]string{
		bucketRefsKey:          `{"refs/heads/main":"` + shaStale + `","refs/heads/gitmsg/pm":"` + shaB + `"}`,
		"refs/heads/main":      shaA + "\n",
		"refs/heads/gitmsg/pm": shaB + "\n",
	})
	client, prefix := anonClient(t, url)
	refs, err := readRemoteRefs(client, prefix)
	if err != nil {
		t.Fatal(err)
	}
	if refs["refs/heads/main"] != shaA {
		t.Errorf("main = %s, want the key's sha %s to beat the stale manifest", refs["refs/heads/main"], shaA)
	}
	if refs["refs/heads/gitmsg/pm"] != shaB {
		t.Errorf("gitmsg/pm = %s, want %s", refs["refs/heads/gitmsg/pm"], shaB)
	}
}

func TestAnon_InfoRefsFallbackAndChainClaim(t *testing.T) {
	// No manifest: info/refs is the source. The bucket is in generation mode, so
	// the claims are the values: gitmsg/pm has no plain key (a chain), and main's
	// plain key is a leftover the claim overrides.
	url := publicNoListBucket(t, map[string]string{
		"info/refs":       shaA + "\trefs/heads/main\n" + shaB + "\trefs/heads/gitmsg/pm\n" + shaA + "\trefs/tags/v1^{}\n",
		"refs/heads/main": shaStale + "\n",
		refModeKey:        refModeGeneration + "\n",
	})
	client, prefix := anonClient(t, url)
	refs, err := readRemoteRefs(client, prefix)
	if err != nil {
		t.Fatal(err)
	}
	if refs["refs/heads/main"] != shaA || refs["refs/heads/gitmsg/pm"] != shaB {
		t.Errorf("refs = %v", refs)
	}
	if _, ok := refs["refs/tags/v1^{}"]; ok {
		t.Error("peel line became a ref")
	}
}

func TestAnon_NoSourceAndWriteRefused(t *testing.T) {
	url := publicNoListBucket(t, map[string]string{"HEAD": "ref: refs/heads/main\n"})
	client, prefix := anonClient(t, url)
	if _, err := readRemoteRefs(client, prefix); err == nil || !strings.Contains(err.Error(), "publishes no ref manifest") {
		t.Errorf("err = %v, want the no-manifest diagnosis", err)
	}
	if err := putObject(client, prefix, "x", []byte("y"), ""); err == nil || !strings.Contains(err.Error(), "credentials required") {
		t.Errorf("anonymous write err = %v, want credentials required", err)
	}
}

func TestAnon_DeletedRefDropped(t *testing.T) {
	// Etag mode: a name the manifest carries but whose key is gone was deleted
	// after the manifest was written, so it must not be resurrected.
	url := publicNoListBucket(t, map[string]string{
		bucketRefsKey:     `{"refs/heads/main":"` + shaA + `","refs/heads/gone":"` + shaB + `"}`,
		"refs/heads/main": shaA + "\n",
		refModeKey:        refModeETag + "\n",
	})
	client, prefix := anonClient(t, url)
	refs, err := readRemoteRefs(client, prefix)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := refs["refs/heads/gone"]; ok || refs["refs/heads/main"] != shaA {
		t.Errorf("refs = %v, want main only", refs)
	}
}

func TestAnon_PrivateBucketNamesCredentials(t *testing.T) {
	// Nothing readable at all: the reader cannot tell a private bucket from an
	// empty one, and says so with the credentials to set.
	url := publicNoListBucket(t, nil)
	client, prefix := anonClient(t, url)
	_, err := readRemoteRefs(client, prefix)
	if !errors.Is(err, ErrCredentialsRequired) {
		t.Errorf("err = %v, want ErrCredentialsRequired", err)
	}
}

func TestSigned_NoListFallsBackToManifest(t *testing.T) {
	// A signed key without s3:ListBucket takes the same manifest path as an
	// anonymous reader instead of failing on the listing's 403.
	url := publicNoListBucket(t, map[string]string{
		bucketRefsKey:     `{"refs/heads/main":"` + shaA + `"}`,
		"refs/heads/main": shaA + "\n",
	})
	client, prefix, _, err := clientForRemote(url, HelperEnv{})
	if err != nil {
		t.Fatal(err)
	}
	if client.Anonymous() {
		t.Fatal("client should be signed")
	}
	refs, err := readRemoteRefs(client, prefix)
	if err != nil || refs["refs/heads/main"] != shaA {
		t.Errorf("refs = %v, err = %v", refs, err)
	}
}

func TestSigned_RejectedCredentialKeepsItsError(t *testing.T) {
	// A signed reader folds only a denial: a rejected signature is a hard error
	// carrying the provider's code, never "not found".
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "<Error><Code>SignatureDoesNotMatch</Code></Error>", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GITSOCIAL_S3_ACCESS_KEY", "k")
	t.Setenv("GITSOCIAL_S3_SECRET_KEY", "s")
	client, prefix, _, err := clientForRemote("s3://"+strings.TrimPrefix(srv.URL, "http://")+"/b/repo", HelperEnv{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Get(prefix + "HEAD")
	if errors.Is(err, ErrNotFound) || err == nil || !strings.Contains(err.Error(), "SignatureDoesNotMatch") {
		t.Errorf("err = %v, want the provider's 403 code", err)
	}
}

func TestAnon_WebDomainAnswers404(t *testing.T) {
	// A public web domain in front of the bucket has no list API and answers 404
	// for it, as for absent keys; the manifest path still resolves the refs.
	url := publicBucket(t, map[string]string{
		bucketRefsKey:     `{"refs/heads/main":"` + shaA + `"}`,
		"refs/heads/main": shaA + "\n",
	}, http.StatusNotFound)
	client, prefix := anonClient(t, url)
	refs, err := readRemoteRefs(client, prefix)
	if err != nil || refs["refs/heads/main"] != shaA {
		t.Errorf("refs = %v, err = %v", refs, err)
	}
}

func TestGetWithETag_WeakETagIsStripped(t *testing.T) {
	// A CDN compressing the response marks the ETag weak; If-Match needs the
	// strong value inside.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `W/"abc"`)
		w.Write([]byte("{}"))
	}))
	t.Cleanup(srv.Close)
	client, prefix := anonClient(t, "s3://"+strings.TrimPrefix(srv.URL, "http://")+"/b/repo")
	if _, etag, err := client.GetWithETag(prefix + bucketRefsKey); err != nil || etag != `"abc"` {
		t.Errorf("etag = %q, err = %v, want the strong value", etag, err)
	}
}
