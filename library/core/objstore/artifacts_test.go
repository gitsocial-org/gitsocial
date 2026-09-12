// artifacts_test.go - release artifact object uploads (artifacts/<version>/*,
// artifacts/latest.txt) against the in-process S3 stub, plus their cache policy.

package objstore

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/objstore/membucket"
)

// artifactsTestRemote spins a membucket-backed server and returns a loopback
// s3 remote URL (with repo prefix) plus the bucket for direct assertions.
func artifactsTestRemote(t *testing.T) (string, *membucket.Bucket) {
	t.Helper()
	bucket := membucket.New()
	srv := httptest.NewServer(bucket)
	t.Cleanup(srv.Close)
	t.Setenv("GITSOCIAL_S3_ACCESS_KEY", "k")
	t.Setenv("GITSOCIAL_S3_SECRET_KEY", "s")
	return "s3://" + strings.TrimPrefix(srv.URL, "http://") + "/b/repo", bucket
}

func TestPushArtifactObjects(t *testing.T) {
	remoteURL, bucket := artifactsTestRemote(t)
	env := HelperEnv{}
	files := map[string][]byte{
		"app-linux-amd64.tar.gz": []byte("linux bytes"),
		"checksums.txt":          []byte("abc  app-linux-amd64.tar.gz\n"),
	}

	advanced, err := PushArtifactObjects(remoteURL, env, "1.0.0", files, func(current string) bool {
		if current != "" {
			t.Errorf("current latest = %q, want empty on first push", current)
		}
		return true
	})
	if err != nil {
		t.Fatalf("PushArtifactObjects: %v", err)
	}
	if !advanced {
		t.Fatal("first push should advance latest.txt")
	}
	if body, ok := bucket.Object("repo/artifacts/1.0.0/app-linux-amd64.tar.gz"); !ok || body != "linux bytes" {
		t.Fatalf("artifact object = %q ok=%v", body, ok)
	}
	if body, ok := bucket.Object("repo/artifacts/1.0.0/checksums.txt"); !ok || body != "abc  app-linux-amd64.tar.gz\n" {
		t.Fatalf("checksums object = %q ok=%v", body, ok)
	}
	if body, _ := bucket.Object("repo/artifacts/latest.txt"); body != "1.0.0\n" {
		t.Fatalf("latest.txt = %q, want %q", body, "1.0.0\n")
	}

	t.Run("re-push overwrites silently and hands the callback the current latest", func(t *testing.T) {
		got := ""
		advanced, err := PushArtifactObjects(remoteURL, env, "1.0.0", map[string][]byte{
			"app-linux-amd64.tar.gz": []byte("rebuilt bytes"),
		}, func(current string) bool {
			got = current
			return false
		})
		if err != nil {
			t.Fatalf("re-push: %v", err)
		}
		if advanced {
			t.Fatal("advance=false must not rewrite latest.txt")
		}
		if got != "1.0.0" {
			t.Fatalf("callback current = %q, want 1.0.0", got)
		}
		if body, _ := bucket.Object("repo/artifacts/1.0.0/app-linux-amd64.tar.gz"); body != "rebuilt bytes" {
			t.Fatalf("re-pushed object = %q, want overwrite", body)
		}
		if n := bucket.PutCount("repo/artifacts/1.0.0/app-linux-amd64.tar.gz"); n != 2 {
			t.Fatalf("putCount = %d, want 2", n)
		}
		if body, _ := bucket.Object("repo/artifacts/latest.txt"); body != "1.0.0\n" {
			t.Fatalf("latest.txt = %q, want unchanged", body)
		}
	})

	t.Run("declined advance uploads objects but leaves latest.txt", func(t *testing.T) {
		advanced, err := PushArtifactObjects(remoteURL, env, "1.1.0-rc1", map[string][]byte{
			"app-linux-amd64.tar.gz": []byte("rc bytes"),
		}, func(string) bool { return false })
		if err != nil {
			t.Fatalf("prerelease push: %v", err)
		}
		if advanced {
			t.Fatal("declined advance reported true")
		}
		if body, ok := bucket.Object("repo/artifacts/1.1.0-rc1/app-linux-amd64.tar.gz"); !ok || body != "rc bytes" {
			t.Fatalf("prerelease object = %q ok=%v", body, ok)
		}
		if body, _ := bucket.Object("repo/artifacts/latest.txt"); body != "1.0.0\n" {
			t.Fatalf("latest.txt = %q, want unchanged 1.0.0", body)
		}
	})
}

func TestCacheControl_artifactKeys(t *testing.T) {
	if got := cacheControlForKey("repo/artifacts/1.4.2/app.tar.gz"); got != CacheControlImmutable {
		t.Errorf("version artifact = %q, want immutable", got)
	}
	if got := cacheControlForKey("artifacts/1.4.2/app.tar.gz"); got != CacheControlImmutable {
		t.Errorf("root-prefix version artifact = %q, want immutable", got)
	}
	if got := cacheControlForKey("repo/artifacts/latest.txt"); got != cacheControlRevalidate {
		t.Errorf("latest.txt = %q, want no-cache", got)
	}
	if got := cacheControlForKey("refs/heads/my-artifacts/x"); got != cacheControlRevalidate {
		t.Errorf("non-boundary artifacts key = %q, want no-cache", got)
	}
	if got := cacheControlForKey("repo/refs/heads/artifacts/v1"); got != cacheControlRevalidate {
		t.Errorf("artifacts-named ref key = %q, want no-cache", got)
	}
}
