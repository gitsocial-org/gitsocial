// docs_conformance_test.go - Asserts documentation/RPC.md documents exactly the
// methods the server registers, in both directions.
package rpc

import (
	"bufio"
	"io"
	"os"
	"sort"
	"strings"
	"testing"
)

const rpcDocPath = "../../documentation/RPC.md"

// registeredMethods builds the registry the way `gitsocial rpc` does and returns
// every method name it exposes.
func registeredMethods(t *testing.T) map[string]bool {
	t.Helper()
	registry := NewRegistry()
	server := NewServer(registry, strings.NewReader(""), io.Discard)
	RegisterCoreMethods(server, "test")
	RegisterSearchMethods(server)
	RegisterSocialMethods(server)
	RegisterPMMethods(server)
	RegisterReviewMethods(server)
	RegisterReleaseMethods(server)
	// Run registers "shutdown" and returns immediately on the empty reader.
	if err := server.Run(); err != nil {
		t.Fatalf("server.Run: %v", err)
	}
	names := make(map[string]bool, len(registry.methods))
	for name := range registry.methods {
		names[name] = true
	}
	return names
}

// documentedMethods extracts every callable method RPC.md declares: the `####`
// headings inside "## 4. Methods" (one heading may list sibling methods separated
// by " / ") plus the "**Method:** `name`" lines used by the lifecycle sections.
func documentedMethods(t *testing.T) map[string]bool {
	t.Helper()
	file, err := os.Open(rpcDocPath)
	if err != nil {
		t.Fatalf("open %s: %v", rpcDocPath, err)
	}
	defer file.Close()

	names := map[string]bool{}
	inMethods := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), " ")
		switch {
		case strings.HasPrefix(line, "## 4. Methods"):
			inMethods = true
		case strings.HasPrefix(line, "## ") && !strings.HasPrefix(line, "## 4."):
			inMethods = false
		}
		if inMethods && strings.HasPrefix(line, "#### ") {
			for _, name := range strings.Split(strings.TrimPrefix(line, "#### "), " / ") {
				names[strings.TrimSpace(name)] = true
			}
			continue
		}
		if strings.HasPrefix(line, "**Method:** `") {
			rest := strings.TrimPrefix(line, "**Method:** `")
			if end := strings.Index(rest, "`"); end > 0 {
				names[rest[:end]] = true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read %s: %v", rpcDocPath, err)
	}
	if len(names) == 0 {
		t.Fatalf("no methods parsed from %s: the parser or the doc layout changed", rpcDocPath)
	}
	return names
}

// missingFrom returns the sorted names present in want but absent from have.
func missingFrom(want, have map[string]bool) []string {
	var out []string
	for name := range want {
		if !have[name] {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func TestRPCMethodsMatchDocumentation(t *testing.T) {
	registered := registeredMethods(t)
	documented := documentedMethods(t)

	if phantom := missingFrom(documented, registered); len(phantom) > 0 {
		t.Errorf("documented in %s but not registered (clients get method-not-found): %v", rpcDocPath, phantom)
	}
	if undocumented := missingFrom(registered, documented); len(undocumented) > 0 {
		t.Errorf("registered but not documented in %s: %v", rpcDocPath, undocumented)
	}
}
