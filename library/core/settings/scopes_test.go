// scopes_test.go - Tests for the Registry, Validate helper, and Manager
// dispatch. Personal-config round-trips are exercised in
// personal_backend_test.go.
package settings

import "testing"

func TestRegistryUniqueKeys(t *testing.T) {
	seen := map[string]bool{}
	for _, k := range Registry {
		if seen[k.Key] {
			t.Errorf("duplicate key in Registry: %s", k.Key)
		}
		seen[k.Key] = true
	}
}

func TestRegistryEnumDefaults(t *testing.T) {
	for _, k := range Registry {
		if k.Type != KeyEnum {
			continue
		}
		if len(k.Enum) == 0 {
			t.Errorf("key %s declared KeyEnum but has empty Enum slice", k.Key)
			continue
		}
		if k.Default == "" {
			continue // optional default for env enums (e.g. GITSOCIAL_PPROF)
		}
		found := false
		for _, opt := range k.Enum {
			if opt == k.Default {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("key %s default %q not in Enum %v", k.Key, k.Default, k.Enum)
		}
	}
}

func TestLookup(t *testing.T) {
	if _, ok := Lookup("does.not.exist"); ok {
		t.Errorf("Lookup returned ok for missing key")
	}
	spec, ok := Lookup("output.color")
	if !ok {
		t.Fatalf("Lookup(output.color) returned !ok")
	}
	if spec.Type != KeyEnum {
		t.Errorf("output.color Type = %v, want KeyEnum", spec.Type)
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		key, val string
		wantErr  bool
	}{
		{"output.color", "never", false},
		{"output.color", "purple", true},
		{"fetch.parallel", "8", false},
		{"fetch.parallel", "not-a-number", true},
		{"display.show_email", "true", false},
		{"display.show_email", "maybe", true},
		{"does.not.exist", "x", true},
	}
	for _, c := range cases {
		err := Validate(c.key, c.val)
		if (err != nil) != c.wantErr {
			t.Errorf("Validate(%s, %s) err=%v wantErr=%v", c.key, c.val, err, c.wantErr)
		}
	}
}

func TestWriteRejectsUnknownKey(t *testing.T) {
	if err := NewManager().Write("not.a.real.key", "x"); err == nil {
		t.Errorf("Write to unknown key should error")
	}
}

func TestWriteRejectsEnvScope(t *testing.T) {
	if err := NewManager().Write("GITSOCIAL_PPROF", "cpu"); err == nil {
		t.Errorf("Write to env-scoped key should error")
	}
}

func TestEnvBackendReadsEnvironment(t *testing.T) {
	t.Setenv("GITSOCIAL_PPROF", "cpu")
	got, ok := NewManager().Resolve("GITSOCIAL_PPROF")
	if !ok || got != "cpu" {
		t.Errorf("Resolve(GITSOCIAL_PPROF) = (%q, %v); want (\"cpu\", true)", got, ok)
	}
}

func TestListIncludesEveryRegisteredKey(t *testing.T) {
	rows := NewManager().List()
	if len(rows) != len(Registry) {
		t.Errorf("List returned %d rows, want %d", len(rows), len(Registry))
	}
	seen := map[string]bool{}
	for _, r := range rows {
		seen[r.Key] = true
	}
	for _, spec := range Registry {
		if !seen[spec.Key] {
			t.Errorf("List missing key %s", spec.Key)
		}
	}
}

func TestResolveDefaults(t *testing.T) {
	t.Setenv("GITSOCIAL_PERSONAL_REPO", t.TempDir()+"/no-such-personal-repo")
	mgr := NewManager()
	for _, spec := range Registry {
		if spec.Scope == ScopeEnv {
			continue
		}
		got, ok := mgr.Resolve(spec.Key)
		if !ok {
			t.Errorf("Resolve(%s) returned !ok", spec.Key)
			continue
		}
		if got != spec.Default {
			t.Errorf("Resolve(%s) = %q, want default %q", spec.Key, got, spec.Default)
		}
	}
}
