// registry_test.go - Tests for forge registry lookup
package forge

import (
	"context"
	"testing"
)

type stubForge struct{ host string }

func (s *stubForge) Host() string { return s.host }
func (s *stubForge) FetchGPGKeys(ctx context.Context, user string) ([]GPGKey, error) {
	return nil, nil
}
func (s *stubForge) FetchCommitVerification(ctx context.Context, owner, repo, sha string) (*CommitVerification, error) {
	return nil, nil
}

func TestRegisterAndLookup(t *testing.T) {
	Register(&stubForge{host: "test-registry.example.com"})
	if got := Lookup("test-registry.example.com"); got == nil {
		t.Fatal("Lookup returned nil for registered host")
	}
	if got := Lookup("TEST-REGISTRY.EXAMPLE.COM"); got == nil {
		t.Error("Lookup should be case-insensitive")
	}
	if got := Lookup("never-registered.example.com"); got != nil {
		t.Error("Lookup should return nil for unregistered host")
	}
}

func TestRegisterReplacesExisting(t *testing.T) {
	Register(&stubForge{host: "replace-test.example.com"})
	first := Lookup("replace-test.example.com")
	Register(&stubForge{host: "replace-test.example.com"})
	second := Lookup("replace-test.example.com")
	if first == second {
		t.Error("Register should replace existing adapter, not return identical instance")
	}
}

func TestLookupForRepo(t *testing.T) {
	Register(&stubForge{host: "lookup-repo-test.example.com"})
	t.Run("registered host", func(t *testing.T) {
		f, owner, repo, err := LookupForRepo("https://lookup-repo-test.example.com/alice/foo")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if f == nil {
			t.Fatal("forge should be non-nil for registered host")
		}
		if owner != "alice" || repo != "foo" {
			t.Errorf("got (%q, %q); want (alice, foo)", owner, repo)
		}
	})
	t.Run("unregistered host returns owner+repo with nil forge", func(t *testing.T) {
		f, owner, repo, err := LookupForRepo("https://unregistered.example.invalid/alice/foo")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if f != nil {
			t.Error("forge should be nil for unregistered host")
		}
		if owner != "alice" || repo != "foo" {
			t.Errorf("got (%q, %q); want (alice, foo)", owner, repo)
		}
	})
	t.Run("malformed URL returns error", func(t *testing.T) {
		_, _, _, err := LookupForRepo("not-a-url")
		if err == nil {
			t.Error("expected error for malformed URL")
		}
	})
}
