// helper_test.go - Remote URL parsing tests
package objstore

import (
	"strings"
	"testing"
)

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
