// docs_conformance_test.go - Asserts documentation/SETTINGS.md describes exactly
// the keys the Registry declares, with the same types and defaults.
package settings

import (
	"os"
	"sort"
	"strings"
	"testing"
)

const settingsDocPath = "../../../documentation/SETTINGS.md"

// typeLabels maps each KeyType to the label used in the SETTINGS.md Keys table.
var typeLabels = map[KeyType]string{
	KeyString: "string",
	KeyInt:    "int",
	KeyBool:   "bool",
	KeyEnum:   "enum",
}

// docKey is one row of the SETTINGS.md Keys table.
type docKey struct {
	keyType string
	def     string
}

// expandBraces turns "extensions.{a,b}" into "extensions.a", "extensions.b" and
// leaves any other key untouched.
func expandBraces(key string) []string {
	start := strings.Index(key, "{")
	end := strings.Index(key, "}")
	if start < 0 || end < start {
		return []string{key}
	}
	var out []string
	for _, part := range strings.Split(key[start+1:end], ",") {
		out = append(out, key[:start]+strings.TrimSpace(part)+key[end+1:])
	}
	return out
}

// parseSettingsDoc reads the Keys table of SETTINGS.md into key -> type/default.
func parseSettingsDoc(t *testing.T) map[string]docKey {
	t.Helper()
	data, err := os.ReadFile(settingsDocPath)
	if err != nil {
		t.Fatalf("read %s: %v", settingsDocPath, err)
	}
	keys := map[string]docKey{}
	inKeys := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "## ") {
			inKeys = strings.TrimSpace(strings.TrimPrefix(line, "## ")) == "Keys"
			continue
		}
		if !inKeys || !strings.HasPrefix(line, "| `") {
			continue
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		if len(cells) < 3 {
			continue
		}
		cell := func(i int) string { return strings.Trim(strings.TrimSpace(cells[i]), "`") }
		for _, key := range expandBraces(cell(0)) {
			keys[key] = docKey{keyType: cell(1), def: cell(2)}
		}
	}
	if len(keys) == 0 {
		t.Fatalf("%s: no keys parsed, the Keys table layout changed", settingsDocPath)
	}
	return keys
}

func TestRegistryMatchesSettingsDoc(t *testing.T) {
	documented := parseSettingsDoc(t)
	registered := map[string]bool{}

	for _, spec := range Registry {
		if spec.Scope != ScopePersonalConfig {
			continue
		}
		registered[spec.Key] = true
		doc, ok := documented[spec.Key]
		if !ok {
			t.Errorf("%s is in the Registry but not in %s", spec.Key, settingsDocPath)
			continue
		}
		if doc.keyType != typeLabels[spec.Type] {
			t.Errorf("%s: doc says type %q, Registry says %q", spec.Key, doc.keyType, typeLabels[spec.Type])
		}
		if doc.def != spec.Default {
			t.Errorf("%s: doc says default %q, Registry says %q", spec.Key, doc.def, spec.Default)
		}
	}

	var phantom []string
	for key := range documented {
		if !registered[key] {
			phantom = append(phantom, key)
		}
	}
	sort.Strings(phantom)
	if len(phantom) > 0 {
		t.Errorf("documented in %s but not in the Registry: %v", settingsDocPath, phantom)
	}
}

func TestEnvKeysAreDocumented(t *testing.T) {
	data, err := os.ReadFile(settingsDocPath)
	if err != nil {
		t.Fatalf("read %s: %v", settingsDocPath, err)
	}
	doc := string(data)
	for _, spec := range Registry {
		if spec.Scope == ScopeEnv && !strings.Contains(doc, "`"+spec.Key+"`") {
			t.Errorf("env key %s is in the Registry but not mentioned in %s", spec.Key, settingsDocPath)
		}
	}
}
