// artifacts_push_test.go - Tests for pushing release artifacts to an s3 remote:
// the latest.txt advance rules, effective site URL derivation, and an
// integration run of PushArtifacts against an in-process S3 stub (mirroring
// the locals3 server's PUT/GET surface).
package release

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/objstore"
)

func TestShouldAdvanceLatest(t *testing.T) {
	tests := []struct {
		name    string
		current string
		version string
		want    bool
	}{
		{"missing current", "", "1.0.0", true},
		{"unparsable current", "not-a-version", "1.0.0", true},
		{"newer patch", "1.4.1", "1.4.2", true},
		{"newer minor with two digits", "1.9.0", "1.10.0", true},
		{"newer major", "1.9.9", "2.0.0", true},
		{"older", "1.4.2", "1.4.1", false},
		{"equal", "1.4.2", "1.4.2", false},
		{"prerelease never advances", "1.4.2", "2.0.0-rc1", false},
		{"prerelease over missing never advances", "", "1.0.0-beta.1", false},
		{"unparsable pushed version", "1.0.0", "latest", false},
		{"current with trailing newline", "1.4.1\n", "1.4.2", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldAdvanceLatest(tt.current, tt.version); got != tt.want {
				t.Errorf("shouldAdvanceLatest(%q, %q) = %v, want %v", tt.current, tt.version, got, tt.want)
			}
		})
	}
}

func TestEffectiveSiteURL(t *testing.T) {
	workdir := initTestRepo(t)
	if url := effectiveSiteURL(workdir, "origin"); url != "" {
		t.Fatalf("unconfigured site url = %q, want empty", url)
	}
	if err := objstore.WriteWorkspaceSiteCustomization(workdir, objstore.SiteCustomization{URL: "https://example.org"}); err != nil {
		t.Fatalf("WriteWorkspaceSiteCustomization: %v", err)
	}
	if url := effectiveSiteURL(workdir, "origin"); url != "https://example.org/" {
		t.Fatalf("workspace site url = %q, want normalized https://example.org/", url)
	}
	git.ExecGit(workdir, []string{"config", "remote.origin." + objstore.SiteOverrideURLKey, "https://next.example.org"})
	if url := effectiveSiteURL(workdir, "origin"); url != "https://next.example.org/" {
		t.Fatalf("override site url = %q, want https://next.example.org/", url)
	}
	git.ExecGit(workdir, []string{"config", "remote.origin." + objstore.SiteOverrideURLKey, "not a url"})
	if url := effectiveSiteURL(workdir, "origin"); url != "https://example.org/" {
		t.Fatalf("invalid override site url = %q, want workspace fallback", url)
	}
}

// artifactStub is a minimal S3 stub for PushArtifacts: unauthenticated
// path-style GET/PUT over an in-memory key space (the subset locals3 serves
// that this flow touches).
type artifactStub struct {
	mu   sync.Mutex
	objs map[string][]byte
}

func (s *artifactStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/")
	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.Method {
	case http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		s.objs[key] = body
		w.WriteHeader(200)
	case http.MethodGet:
		body, ok := s.objs[key]
		if !ok {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write(body)
	default:
		w.WriteHeader(405)
	}
}

// object returns a stored object under the stub lock.
func (s *artifactStub) object(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, ok := s.objs[key]
	return string(body), ok
}

// writeArtifactFile writes a named test artifact and returns its path.
func writeArtifactFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestPushArtifacts_integration(t *testing.T) {
	setupTestDB(t)
	workdir := initTestRepo(t)
	stub := &artifactStub{objs: map[string][]byte{}}
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	t.Setenv("GITSOCIAL_S3_ACCESS_KEY", "k")
	t.Setenv("GITSOCIAL_S3_SECRET_KEY", "s")

	remoteURL := "s3://" + strings.TrimPrefix(srv.URL, "http://") + "/bucket/repo"
	git.ExecGit(workdir, []string{"remote", "add", "site", remoteURL})
	git.ExecGit(workdir, []string{"config", "remote.site." + objstore.SiteOverrideURLKey, "http://localhost:8080/"})

	if res := CreateRelease(workdir, "v1.0.0", "", CreateReleaseOptions{Tag: "v1.0.0", Version: "1.0.0"}); !res.Success {
		t.Fatalf("CreateRelease: %s", res.Error.Message)
	}

	dir := t.TempDir()
	binPath := writeArtifactFile(t, dir, "app-linux-amd64.tar.gz", "binary v1")
	sumPath := writeArtifactFile(t, dir, "checksums.txt", "abc  app-linux-amd64.tar.gz\n")

	res := PushArtifacts(workdir, "1.0.0", []string{binPath, sumPath}, "site")
	if !res.Success {
		t.Fatalf("PushArtifacts: %s", res.Error.Message)
	}
	if res.Data.BaseURL != "http://localhost:8080/artifacts/1.0.0" {
		t.Errorf("BaseURL = %q", res.Data.BaseURL)
	}
	if !res.Data.LatestAdvanced {
		t.Error("first non-prerelease push should advance latest.txt")
	}
	if !res.Data.RecordUpdated {
		t.Error("record without artifact-url should be updated")
	}
	if body, ok := stub.object("bucket/repo/artifacts/1.0.0/app-linux-amd64.tar.gz"); !ok || body != "binary v1" {
		t.Errorf("artifact object = %q ok=%v", body, ok)
	}
	if body, _ := stub.object("bucket/repo/artifacts/latest.txt"); body != "1.0.0\n" {
		t.Errorf("latest.txt = %q, want %q", body, "1.0.0\n")
	}
	item, err := GetReleaseItemByTagOrVersion("1.0.0")
	if err != nil {
		t.Fatalf("GetReleaseItemByTagOrVersion: %v", err)
	}
	if item.ArtifactURL.String != "http://localhost:8080/artifacts/1.0.0" {
		t.Errorf("record artifact-url = %q", item.ArtifactURL.String)
	}

	t.Run("re-push same version is a silent overwrite", func(t *testing.T) {
		rebuilt := writeArtifactFile(t, t.TempDir(), "app-linux-amd64.tar.gz", "binary v1 rebuilt")
		res := PushArtifacts(workdir, "1.0.0", []string{rebuilt}, "site")
		if !res.Success {
			t.Fatalf("re-push: %s", res.Error.Message)
		}
		if res.Data.LatestAdvanced {
			t.Error("equal version must not advance latest.txt")
		}
		if res.Data.RecordUpdated {
			t.Error("record with artifact-url set must not be edited again")
		}
		if body, _ := stub.object("bucket/repo/artifacts/1.0.0/app-linux-amd64.tar.gz"); body != "binary v1 rebuilt" {
			t.Errorf("re-pushed object = %q, want overwrite", body)
		}
	})

	t.Run("prerelease uploads but does not advance latest", func(t *testing.T) {
		rc := writeArtifactFile(t, t.TempDir(), "app-linux-amd64.tar.gz", "binary rc")
		res := PushArtifacts(workdir, "1.1.0-rc1", []string{rc}, "site")
		if !res.Success {
			t.Fatalf("prerelease push: %s", res.Error.Message)
		}
		if res.Data.LatestAdvanced {
			t.Error("prerelease must not advance latest.txt")
		}
		if body, ok := stub.object("bucket/repo/artifacts/1.1.0-rc1/app-linux-amd64.tar.gz"); !ok || body != "binary rc" {
			t.Errorf("prerelease object = %q ok=%v", body, ok)
		}
		if body, _ := stub.object("bucket/repo/artifacts/latest.txt"); body != "1.0.0\n" {
			t.Errorf("latest.txt = %q, want unchanged 1.0.0", body)
		}
	})

	t.Run("newer version advances latest", func(t *testing.T) {
		next := writeArtifactFile(t, t.TempDir(), "app-linux-amd64.tar.gz", "binary v1.0.1")
		res := PushArtifacts(workdir, "1.0.1", []string{next}, "site")
		if !res.Success {
			t.Fatalf("newer push: %s", res.Error.Message)
		}
		if !res.Data.LatestAdvanced {
			t.Error("newer non-prerelease should advance latest.txt")
		}
		if body, _ := stub.object("bucket/repo/artifacts/latest.txt"); body != "1.0.1\n" {
			t.Errorf("latest.txt = %q, want 1.0.1", body)
		}
	})

	t.Run("no site url fails with config hint", func(t *testing.T) {
		bare := writeArtifactFile(t, t.TempDir(), "x.tar.gz", "x")
		git.ExecGit(workdir, []string{"remote", "add", "nourl", remoteURL})
		res := PushArtifacts(workdir, "1.0.2", []string{bare}, "nourl")
		if res.Success || res.Error.Code != "NO_SITE_URL" {
			t.Fatalf("push without site url = %+v, want NO_SITE_URL failure", res)
		}
	})
}

func TestArtifacts_externalStorageFallback(t *testing.T) {
	setupTestDB(t)
	workdir := initTestRepo(t)
	stub := &artifactStub{objs: map[string][]byte{}}
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	stub.mu.Lock()
	stub.objs["bucket/repo/artifacts/2.0.0/app.tar.gz"] = []byte("external bytes")
	stub.mu.Unlock()

	baseURL := srv.URL + "/bucket/repo/artifacts/2.0.0"
	res := CreateRelease(workdir, "v2.0.0", "", CreateReleaseOptions{
		Tag: "v2.0.0", Version: "2.0.0", Artifacts: []string{"app.tar.gz"}, ArtifactURL: baseURL,
	})
	if !res.Success {
		t.Fatalf("CreateRelease: %s", res.Error.Message)
	}

	list := ListArtifacts(workdir, "2.0.0")
	if !list.Success {
		t.Fatalf("ListArtifacts: %s", list.Error.Message)
	}
	if len(list.Data) != 1 || list.Data[0].Filename != "app.tar.gz" {
		t.Fatalf("ListArtifacts = %+v, want the record's filename", list.Data)
	}

	dest := filepath.Join(t.TempDir(), "app.tar.gz")
	exp := ExportArtifact(workdir, "", "2.0.0", "app.tar.gz", dest)
	if !exp.Success {
		t.Fatalf("ExportArtifact: %s", exp.Error.Message)
	}
	data, err := os.ReadFile(exp.Data)
	if err != nil || string(data) != "external bytes" {
		t.Fatalf("exported content = %q err=%v", data, err)
	}

	if missing := ExportArtifact(workdir, "", "2.0.0", "nope.tar.gz", dest); missing.Success || missing.Error.Code != "DOWNLOAD_FAILED" {
		t.Fatalf("missing external artifact = %+v, want DOWNLOAD_FAILED", missing)
	}
}

func TestPushArtifacts_nonS3Remote(t *testing.T) {
	workdir := initTestRepo(t)
	path := writeArtifactFile(t, t.TempDir(), "x.tar.gz", "x")
	res := PushArtifacts(workdir, "1.0.0", []string{path}, "origin")
	if res.Success || res.Error.Code != "NOT_S3" {
		t.Fatalf("push to non-s3 remote = %+v, want NOT_S3 failure", res)
	}
}
