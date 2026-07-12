// helper_test.go - Remote URL parsing and option command tests
package objstore

import (
	"strings"
	"testing"
)

// TestHelperOption covers the "option" protocol command: cas leases are
// recorded (zero oid normalized to "" = must-not-exist), malformed values
// error, and every other option is declined with "unsupported".
func TestHelperOption(t *testing.T) {
	h := &remoteHelper{}
	oid := strings.Repeat("ab", 20)
	cases := []struct{ spec, reply string }{
		{"cas refs/heads/main:" + oid, "ok"},
		{"cas refs/heads/gone:" + zeroOID, "ok"},
		{"cas refs/heads/x", "error malformed cas value"},
		{"cas refs/heads/x:abc", "error malformed cas value"},
		{"cas :" + oid, "error malformed cas value"},
		{"verbosity 1", "unsupported"},
		{"dry-run true", "unsupported"},
		{"progress false", "unsupported"},
	}
	for _, c := range cases {
		if got := h.option(c.spec); got != c.reply {
			t.Errorf("option %q = %q, want %q", c.spec, got, c.reply)
		}
	}
	if got := h.leases["refs/heads/main"]; got != oid {
		t.Errorf("lease for main = %q, want %q", got, oid)
	}
	if got, ok := h.leases["refs/heads/gone"]; !ok || got != "" {
		t.Errorf("zero-oid lease = (%q, %v), want (\"\", true)", got, ok)
	}
	if _, ok := h.leases["refs/heads/x"]; ok {
		t.Error("malformed cas value must not record a lease")
	}
}

// TestCheckLease covers the write-time lease policy: no lease defers to
// fast-forward rules, a matching lease authorizes, a mismatch (including
// present-vs-absent in both directions) rejects with git's "stale info".
func TestCheckLease(t *testing.T) {
	oid := strings.Repeat("cd", 20)
	other := strings.Repeat("ef", 20)
	h := &remoteHelper{leases: map[string]string{"refs/heads/main": oid, "refs/heads/new": ""}}
	if leased, err := h.checkLease("refs/heads/unleased", other); leased || err != nil {
		t.Errorf("unleased ref = (%v, %v), want (false, nil)", leased, err)
	}
	if leased, err := h.checkLease("refs/heads/main", oid); !leased || err != nil {
		t.Errorf("matching lease = (%v, %v), want (true, nil)", leased, err)
	}
	for name, current := range map[string]string{"moved": other, "vanished": ""} {
		if _, err := h.checkLease("refs/heads/main", current); err == nil || err.Error() != "stale info" {
			t.Errorf("%s ref: err = %v, want stale info", name, err)
		}
	}
	if leased, err := h.checkLease("refs/heads/new", ""); !leased || err != nil {
		t.Errorf("must-not-exist lease on absent ref = (%v, %v), want (true, nil)", leased, err)
	}
	if _, err := h.checkLease("refs/heads/new", other); err == nil || err.Error() != "stale info" {
		t.Errorf("must-not-exist lease on existing ref: err = %v, want stale info", err)
	}
}

func TestParseS3URL(t *testing.T) {
	cases := []struct {
		in                           string
		endpointHost, bucket, prefix string
		wantErr                      string
	}{
		{in: "s3://nyc3.digitaloceanspaces.com/mybucket/team/repo",
			endpointHost: "nyc3.digitaloceanspaces.com", bucket: "mybucket", prefix: "team/repo/"},
		{in: "s3://s3.us-east-1.amazonaws.com/mybucket",
			endpointHost: "s3.us-east-1.amazonaws.com", bucket: "mybucket"},
		{in: "s3://mybucket.nyc3.digitaloceanspaces.com/repo",
			endpointHost: "nyc3.digitaloceanspaces.com", bucket: "mybucket", prefix: "repo/"},
		{in: "s3://minio.example.com/mybucket/repo/",
			endpointHost: "minio.example.com", bucket: "mybucket", prefix: "repo/"},
		{in: "s3://127.0.0.1:9000/mybucket/repo",
			endpointHost: "127.0.0.1:9000", bucket: "mybucket", prefix: "repo/"},
		{in: "s3://mybucket/repo", wantErr: "must name the endpoint host"},
		{in: "s3://nyc3.digitaloceanspaces.com/bkt/repo?path-style=1", wantErr: "take no parameters"},
		{in: "s3://nyc3.digitaloceanspaces.com", wantErr: "missing bucket"},
		{in: "https://example.com/x", wantErr: "not an s3 URL"},
		{in: "s3://", wantErr: "must name the endpoint host"},
	}
	for _, c := range cases {
		endpointHost, bucket, prefix, err := ParseS3URL(c.in)
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("ParseS3URL(%q) err = %v, want it to contain %q", c.in, err, c.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseS3URL(%q): %v", c.in, err)
			continue
		}
		if endpointHost != c.endpointHost || bucket != c.bucket || prefix != c.prefix {
			t.Errorf("ParseS3URL(%q) = (%q, %q, %q), want (%q, %q, %q)",
				c.in, endpointHost, bucket, prefix, c.endpointHost, c.bucket, c.prefix)
		}
	}
}
