// site_prism_test.go - the extra-grammar selection/ordering that drives the
// push-published prism-extra.js: extensions map to grammars, dependency chains
// resolve deps-first, and the bundle carries only what a repo uses.

package objstore

import (
	"strings"
	"testing"
)

func TestGrammarsForExtensions(t *testing.T) {
	t.Run("known extensions select their grammars", func(t *testing.T) {
		need := grammarsForExtensions(map[string]bool{"py": true, "rs": true, "sql": true}, nil)
		for _, g := range []string{"python", "rust", "sql"} {
			if !need[g] {
				t.Fatalf("extension scan missed grammar %q; got %v", g, need)
			}
		}
	})

	t.Run("base-grammar and unknown extensions select nothing extra", func(t *testing.T) {
		need := grammarsForExtensions(map[string]bool{"go": true, "js": true, "ts": true, "xyz": true}, nil)
		if len(need) != 0 {
			t.Fatalf("base/unknown extensions selected extra grammars: %v", need)
		}
	})

	t.Run("extension-less basename selects docker", func(t *testing.T) {
		need := grammarsForExtensions(nil, map[string]bool{"dockerfile": true})
		if !need["docker"] {
			t.Fatalf("Dockerfile did not select the docker grammar; got %v", need)
		}
	})
}

func TestOrderPrismGrammarsDepsFirst(t *testing.T) {
	// cpp extends c, so c must be emitted before cpp even if only cpp is asked.
	ordered := orderPrismGrammars(map[string]bool{"cpp": true})
	if len(ordered) != 2 || ordered[0] != "c" || ordered[1] != "cpp" {
		t.Fatalf("cpp ordering = %v, want [c cpp]", ordered)
	}
}

func TestOrderPrismGrammarsStable(t *testing.T) {
	// The same input set must produce the same order across pushes (no-cache
	// artifact; a stable body avoids needless re-uploads and diff churn).
	in := map[string]bool{"rust": true, "python": true, "sql": true}
	a := orderPrismGrammars(in)
	b := orderPrismGrammars(in)
	if strings.Join(a, ",") != strings.Join(b, ",") {
		t.Fatalf("ordering not stable: %v vs %v", a, b)
	}
}

func TestBuildPrismExtra(t *testing.T) {
	t.Run("empty selection yields no bundle", func(t *testing.T) {
		bundle, err := buildPrismExtra(nil)
		if err != nil {
			t.Fatal(err)
		}
		if bundle != nil {
			t.Fatalf("empty selection produced a bundle: %q", bundle)
		}
	})

	t.Run("bundle carries a provenance header and the components in order", func(t *testing.T) {
		bundle, err := buildPrismExtra([]string{"c", "cpp"})
		if err != nil {
			t.Fatal(err)
		}
		s := string(bundle)
		if !strings.Contains(s, "Prism 1.30.0") || !strings.Contains(s, "Grammars: c, cpp") {
			t.Fatalf("bundle header missing provenance/grammar list:\n%s", s[:min(200, len(s))])
		}
		// c must appear before cpp (cpp extends c).
		ic, icpp := strings.Index(s, "languages.c="), strings.Index(s, `extend("c"`)
		if ic < 0 || icpp < 0 || ic > icpp {
			t.Fatalf("c grammar not emitted before cpp (c@%d cpp@%d)", ic, icpp)
		}
	})
}

// TestPrismComponentsEmbedded proves every grammar's component file is embedded
// and non-empty (a missing file would silently drop that language at push time).
func TestPrismComponentsEmbedded(t *testing.T) {
	for name, spec := range prismGrammars {
		data, err := PrismComponents.ReadFile("prismcomp/" + spec.file)
		if err != nil {
			t.Fatalf("grammar %q: embedded component %q missing: %v", name, spec.file, err)
		}
		if len(data) == 0 {
			t.Fatalf("grammar %q: embedded component %q is empty", name, spec.file)
		}
	}
}
