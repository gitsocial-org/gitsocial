// sigv4_test.go - SigV4 signing verified against the AWS S3 documentation example
package objstore

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestSignRequest_s3DocExample reproduces the "GET Bucket Lifecycle" worked
// example from the S3 SigV4 documentation (Authenticating Requests: Using the
// Authorization Header) and asserts the documented signature.
func TestSignRequest_s3DocExample(t *testing.T) {
	req, err := http.NewRequest("GET", "https://examplebucket.s3.amazonaws.com/?lifecycle", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)
	signRequest(req,
		"AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"us-east-1", "s3", emptyPayloadSHA256, now)

	auth := req.Header.Get("Authorization")
	wantSig := "Signature=fea454ca298b7da1c68078a5d1bdbfbbe0d65c699e0f91ac7a200a0136783543"
	if !strings.Contains(auth, wantSig) {
		t.Errorf("Authorization = %q\nwant it to contain %q", auth, wantSig)
	}
	wantCred := "Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request"
	if !strings.Contains(auth, wantCred) {
		t.Errorf("Authorization = %q\nwant it to contain %q", auth, wantCred)
	}
	if !strings.Contains(auth, "SignedHeaders=host;x-amz-content-sha256;x-amz-date") {
		t.Errorf("Authorization = %q\nwant host;x-amz-content-sha256;x-amz-date signed", auth)
	}
}

// TestCanonicalURIEncode asserts SigV4's own encoding of a path Go escapes differently.
func TestCanonicalURIEncode(t *testing.T) {
	cases := []struct{ path, want string }{
		{"", "/"},
		{"/", "/"},
		{"/refs/heads/main", "/refs/heads/main"},
		{"/a+b/c@d", "/a%2Bb/c%40d"},
		{"/artifacts/1.0.0+build.5/gitsocial", "/artifacts/1.0.0%2Bbuild.5/gitsocial"},
		{"/tag=v1,rc2", "/tag%3Dv1%2Crc2"},
		{"/a%20b", "/a%20b"},
		{"/a%25b", "/a%25b"},
		{"/h%C3%A9llo", "/h%C3%A9llo"},
		{"/a~b_c-d.e", "/a~b_c-d.e"},
	}
	for _, c := range cases {
		if got := canonicalURIEncode(c.path); got != c.want {
			t.Errorf("canonicalURIEncode(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// TestSignRequest_encodesPathPerSigV4 pins that a key Go escapes loosely reaches the signature in AWS's form.
func TestSignRequest_encodesPathPerSigV4(t *testing.T) {
	client, err := NewClient(Config{Endpoint: "https://s3.example.com", Bucket: "b", Region: "us-east-1", AccessKey: "k", SecretKey: "s", PathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	u, err := client.objectURL("artifacts/1.0.0+build.5/tool")
	if err != nil {
		t.Fatal(err)
	}
	if got := canonicalURIEncode(u.EscapedPath()); got != "/b/artifacts/1.0.0%2Bbuild.5/tool" {
		t.Errorf("canonical URI = %q, want the + escaped", got)
	}
}

// TestObjectURL_WireMatchesSignature pins the pair: the path a request carries is the path the signature covers.
func TestObjectURL_WireMatchesSignature(t *testing.T) {
	keys := []string{
		"refs/heads/main",
		"ghostty/f/pkg/afl++/LICENSE.html",
		"artifacts/1.0.0+build.5/tool",
		"site/a b/c.html",
		"site/q&a/what's~this.html",
		"site/héllo/café.html",
		"site/100%/done.html",
	}
	for _, style := range []bool{true, false} {
		client, err := NewClient(Config{Endpoint: "https://s3.example.com", Bucket: "b", Region: "us-east-1", AccessKey: "k", SecretKey: "s", PathStyle: style})
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range keys {
			u, err := client.objectURL(key)
			if err != nil {
				t.Fatalf("objectURL(%q): %v", key, err)
			}
			wire := u.EscapedPath()
			if signed := canonicalURIEncode(wire); signed != wire {
				t.Errorf("pathStyle=%v key %q: wire %q, signed %q", style, key, wire, signed)
			}
			if !strings.HasSuffix(u.Path, key) {
				t.Errorf("key %q: decoded path %q no longer ends in the key, so it was escaped twice", key, u.Path)
			}
		}
	}
}

// TestDoOnce_QueryWireMatchesSignature drives a real request and pins the query a provider receives, where Go writes a space as + and SigV4 writes %20.
func TestDoOnce_QueryWireMatchesSignature(t *testing.T) {
	var raw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<ListBucketResult></ListBucketResult>`))
	}))
	defer srv.Close()
	client, err := NewClient(Config{Endpoint: srv.URL, Bucket: "b", Region: "us-east-1", AccessKey: "k", SecretKey: "s", PathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.List("site/a b+c/"); err != nil {
		t.Fatalf("List: %v", err)
	}
	if strings.Contains(raw, "+") {
		t.Errorf("wire query %q carries a raw +, which a provider reads as a space", raw)
	}
	parsed, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	if signed := canonicalQueryString(parsed); signed != raw {
		t.Errorf("wire query %q, signed query %q", raw, signed)
	}
	if got := parsed.Get("prefix"); got != "site/a b+c/" {
		t.Errorf("prefix reached the provider as %q", got)
	}
}
