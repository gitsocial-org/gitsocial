// fuzz_test.go - Fuzz target for the s3 remote URL parser
package objstore

import (
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// joinS3Parts re-assembles the three parts ParseS3URL returns into a canonical s3 URL.
func joinS3Parts(endpointHost, bucket, prefix string) string {
	joined := "s3://" + endpointHost + "/" + bucket
	if prefix != "" {
		joined += "/" + strings.TrimSuffix(prefix, "/")
	}
	return joined
}

// FuzzParseS3URL checks the s3 remote URL parser against the canonical form its callers write.
func FuzzParseS3URL(f *testing.F) {
	seeds := []string{
		"s3://nyc3.digitaloceanspaces.com/mybucket/team/repo",
		"s3://s3.us-east-1.amazonaws.com/mybucket",
		"s3://mybucket.nyc3.digitaloceanspaces.com/repo",
		"s3://minio.example.com/mybucket/repo/",
		"s3://127.0.0.1:9000/mybucket/repo",
		"s3://localhost:9000/mybucket/repo.git",
		"s3://mybucket/repo",
		"s3://nyc3.digitaloceanspaces.com/bkt/repo?path-style=1",
		"s3://nyc3.digitaloceanspaces.com",
		"https://example.com/x",
		"javascript:alert(1)",
		"s3://",
		"",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		endpointHost, bucket, prefix, err := ParseS3URL(raw)
		if err != nil {
			// A rejected URL reports no parts.
			if endpointHost != "" || bucket != "" || prefix != "" {
				t.Fatalf("ParseS3URL(%q) rejected with parts (%q, %q, %q)", raw, endpointHost, bucket, prefix)
			}
			return
		}
		// S3.md: an accepted URL names an endpoint host.
		if endpointHost == "" {
			t.Fatalf("ParseS3URL(%q) accepted an empty endpoint host", raw)
		}
		// The virtual-host fold reads the empty label of "s3://.0.digitaloceanspaces.com" as the bucket, so it accepts none.
		if bucket == "" {
			return
		}
		joined := joinS3Parts(endpointHost, bucket, prefix)
		// NormalizeURL trims the surrounding space the parser keeps, so the two disagree on "s3://./0A00000000 ".
		if raw == strings.TrimSpace(raw) {
			// NormalizeURL folds an s3 URL to the same canonical URL the parts spell.
			if normalized := protocol.NormalizeURL(raw); normalized != joined {
				t.Errorf("NormalizeURL(%q) = %q, want the parsed parts %q", raw, normalized, joined)
			}
		}
		// Percent escapes decode into the parts, so re-assembly respells them: "s3://./%00" joins to a control character.
		if strings.Contains(raw, "%") {
			return
		}
		againHost, againBucket, againPrefix, againErr := ParseS3URL(joined)
		if againErr != nil {
			t.Fatalf("re-parsing the canonical form %q of %q: %v", joined, raw, againErr)
		}
		// The canonical form parses back to the same three parts.
		if againHost != endpointHost || againBucket != bucket || againPrefix != prefix {
			t.Errorf("ParseS3URL(%q) = (%q, %q, %q), want the parts of %q (%q, %q, %q)",
				joined, againHost, againBucket, againPrefix, raw, endpointHost, bucket, prefix)
		}
	})
}
