// s3_test.go - Canonical s3 URL rule tests
package protocol

import "testing"

// r2acct is a stand-in for a real Cloudflare account id (always 32 hex).
const r2acct = "abcdef0123456789abcdef0123456789"

func TestNormalizeURL_s3Canonical(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"host form passthrough", "s3://s3.us-east-1.amazonaws.com/mybucket/dir", "s3://s3.us-east-1.amazonaws.com/mybucket/dir"},
		{"host form strips query and slash", "s3://nyc3.digitaloceanspaces.com/bkt/repo/?path-style=1", "s3://nyc3.digitaloceanspaces.com/bkt/repo"},
		{"dev host with port", "s3://127.0.0.1:9000/bkt/repo", "s3://127.0.0.1:9000/bkt/repo"},
		{"unknown dotted host stays host form", "s3://minio.example.com/bkt/repo", "s3://minio.example.com/bkt/repo"},
		{"virtual-host do folds", "s3://bkt.nyc3.digitaloceanspaces.com/repo", "s3://nyc3.digitaloceanspaces.com/bkt/repo"},
		{"virtual-host aws folds", "s3://bkt.s3.us-east-1.amazonaws.com/repo", "s3://s3.us-east-1.amazonaws.com/bkt/repo"},
		{"virtual-host r2 folds", "s3://bkt." + r2acct + ".r2.cloudflarestorage.com", "s3://" + r2acct + ".r2.cloudflarestorage.com/bkt"},
		{"r2 eu jurisdiction stays host form", "s3://" + r2acct + ".eu.r2.cloudflarestorage.com/bkt", "s3://" + r2acct + ".eu.r2.cloudflarestorage.com/bkt"},
		{"virtual-host r2 eu folds", "s3://bkt." + r2acct + ".eu.r2.cloudflarestorage.com", "s3://" + r2acct + ".eu.r2.cloudflarestorage.com/bkt"},
		{"bucket-only authority is invalid", "s3://mybucket/dir", ""},
		{"empty authority is invalid", "s3://", ""},
	}
	for _, c := range cases {
		if got := NormalizeURL(c.in); got != c.want {
			t.Errorf("%s: NormalizeURL(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestS3HostInfo(t *testing.T) {
	cases := []struct {
		host             string
		provider, region string
		ok               bool
	}{
		{"s3.us-east-1.amazonaws.com", "aws", "us-east-1", true},
		{"nyc3.digitaloceanspaces.com", "do", "nyc3", true},
		{r2acct + ".r2.cloudflarestorage.com", "r2", "auto", true},
		{r2acct + ".eu.r2.cloudflarestorage.com", "r2", "auto", true},
		{r2acct + ".fedramp.r2.cloudflarestorage.com", "r2", "auto", true},
		{"eu.r2.cloudflarestorage.com", "", "", false},                 // jurisdiction label, no account
		{"abc123.r2.cloudflarestorage.com", "", "", false},             // account not 32-hex
		{"bkt." + r2acct + ".r2.cloudflarestorage.com", "", "", false}, // virtual-host, not an endpoint
		{r2acct + ".xx.r2.cloudflarestorage.com", "", "", false},       // unknown jurisdiction
		{"bkt.nyc3.digitaloceanspaces.com", "", "", false},             // virtual-host, not a canonical host
		{"minio.example.com", "", "", false},
	}
	for _, c := range cases {
		provider, region, ok := S3HostInfo(c.host)
		if provider != c.provider || region != c.region || ok != c.ok {
			t.Errorf("S3HostInfo(%q) = (%q, %q, %v), want (%q, %q, %v)", c.host, provider, region, ok, c.provider, c.region, c.ok)
		}
	}
}
