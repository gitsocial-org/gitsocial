// site_prism.go - push-maintained extra syntax-highlighting grammars artifact.
//
// The base site shell bundles a small set of Prism grammars (prism.js: core,
// markup, css, clike, javascript, typescript, json, yaml, bash, go, markdown,
// diff). To keep that shell small for every repo while still highlighting the
// long tail of languages, this scans the default branch's tree at push time for
// file extensions and publishes .gitsocial/site/prism-extra.js carrying ONLY the
// Prism grammar components the repo actually uses (each an official minified
// Prism 1.30.0 component, embedded in the binary). The shell loads it after
// prism.js when present; absent it, only the base grammars highlight. Grammars
// are emitted in dependency order (a component that extends another is preceded
// by it), so the concatenation loads cleanly.

package objstore

import (
	"bytes"
	"compress/zlib"
	"embed"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// PrismComponents holds the official Prism 1.30.0 minified grammar components
// (from cdnjs, MIT, github.com/PrismJS/prism tag v1.30.0) not already bundled in
// the base prism.js. Emitted individually into prism-extra.js as a repo needs
// them.
//
//go:embed prismcomp
var PrismComponents embed.FS

// sitePrismExtraKey is the extra-grammars bundle the static site loads after
// prism.js (no-cache, refreshed on every push).
const sitePrismExtraKey = ".gitsocial/site/prism-extra.js"

// prismGrammar names an extra Prism grammar: its component file (under
// prismcomp/) and the grammars it depends on (which must load first). Base
// grammars already in prism.js (clike, markup, css, etc.) are not listed as
// deps — only extra grammars that must be co-bundled are.
type prismGrammar struct {
	file string   // component filename under prismcomp/
	deps []string // other extra grammars that must precede this one
}

// prismGrammars maps a grammar name to its component. Dependency chains within
// this extra set are explicit (cpp extends c); every grammar here otherwise
// depends only on base grammars already in prism.js (clike/markup/css), so no
// dep entry is needed for those.
var prismGrammars = map[string]prismGrammar{
	"python":   {file: "prism-python.min.js"},
	"rust":     {file: "prism-rust.min.js"},
	"c":        {file: "prism-c.min.js"},
	"cpp":      {file: "prism-cpp.min.js", deps: []string{"c"}},
	"java":     {file: "prism-java.min.js"},
	"sql":      {file: "prism-sql.min.js"},
	"ruby":     {file: "prism-ruby.min.js"},
	"kotlin":   {file: "prism-kotlin.min.js"},
	"swift":    {file: "prism-swift.min.js"},
	"toml":     {file: "prism-toml.min.js"},
	"php":      {file: "prism-php.min.js"},
	"csharp":   {file: "prism-csharp.min.js"},
	"ini":      {file: "prism-ini.min.js"},
	"docker":   {file: "prism-docker.min.js"},
	"protobuf": {file: "prism-protobuf.min.js"},
	"lua":      {file: "prism-lua.min.js"},
}

// prismExtByExt maps a lowercase file extension to the extra grammar it needs.
// Kept in sync with EXT_LANG in gs-render.js (the reader side) so a highlighted
// file's grammar is always bundled. Base-grammar extensions (go/js/ts/...) are
// absent here (their grammar ships in prism.js).
var prismExtByExt = map[string]string{
	"py": "python", "pyi": "python", "pyw": "python",
	"rs":  "rust",
	"c":   "c",
	"h":   "c",
	"cpp": "cpp", "cc": "cpp", "cxx": "cpp", "hpp": "cpp", "hh": "cpp",
	"java": "java",
	"sql":  "sql",
	"rb":   "ruby",
	"kt":   "kotlin", "kts": "kotlin",
	"swift": "swift",
	"toml":  "toml",
	"php":   "php",
	"cs":    "csharp",
	"ini":   "ini",
	"proto": "protobuf",
	"lua":   "lua",
}

// prismExtByName maps a full lowercase basename to a grammar, for the handful of
// extension-less files worth highlighting (a Dockerfile has no extension).
var prismExtByName = map[string]string{
	"dockerfile": "docker",
}

// prismTreeScanCap bounds the tree fan-out during the extension scan (the same
// spirit as DIFF_TREE_SCAN_CAP on the reader): a huge monorepo stops scanning
// once enough of the tree is seen to have found its languages.
const prismTreeScanCap = 5000

// grammarsForExtensions turns a set of seen extensions/basenames into the set
// of extra grammar names to bundle.
func grammarsForExtensions(exts map[string]bool, names map[string]bool) map[string]bool {
	need := map[string]bool{}
	for ext := range exts {
		if g, ok := prismExtByExt[ext]; ok {
			need[g] = true
		}
	}
	for name := range names {
		if g, ok := prismExtByName[name]; ok {
			need[g] = true
		}
	}
	return need
}

// orderPrismGrammars returns the needed grammars closed over their dependencies
// and topologically ordered (deps before dependents), deterministically.
func orderPrismGrammars(need map[string]bool) []string {
	ordered := []string{}
	placed := map[string]bool{}
	var visit func(g string)
	visit = func(g string) {
		if placed[g] {
			return
		}
		spec, ok := prismGrammars[g]
		if !ok {
			return
		}
		for _, dep := range spec.deps {
			visit(dep)
		}
		placed[g] = true
		ordered = append(ordered, g)
	}
	// Sort the roots so the output is stable across pushes.
	roots := make([]string, 0, len(need))
	for g := range need {
		roots = append(roots, g)
	}
	sort.Strings(roots)
	for _, g := range roots {
		visit(g)
	}
	return ordered
}

// buildPrismExtra concatenates the component files for the ordered grammars into
// one bundle with a provenance header, or returns nil when no extra grammar is
// needed.
func buildPrismExtra(ordered []string) ([]byte, error) {
	if len(ordered) == 0 {
		return nil, nil
	}
	var b strings.Builder
	b.WriteString("// prism-extra.js - repo-specific Prism 1.30.0 grammars, published by\n")
	b.WriteString("// gitsocial site push. Official minified components (cdnjs, MIT,\n")
	b.WriteString("// github.com/PrismJS/prism v1.30.0), loaded after prism.js in the shell.\n")
	b.WriteString("// Grammars: " + strings.Join(ordered, ", ") + "\n")
	for _, g := range ordered {
		spec := prismGrammars[g]
		data, err := PrismComponents.ReadFile("prismcomp/" + spec.file)
		if err != nil {
			return nil, fmt.Errorf("read embedded %s: %w", spec.file, err)
		}
		b.Write(data)
		b.WriteString("\n")
	}
	return []byte(b.String()), nil
}

// scanTreeExtensions walks the tree rooted at rootSha (the default branch's tip
// tree) breadth-first over the bucket's objects, collecting the distinct file
// extensions and extension-less basenames it finds, bounded by prismTreeScanCap
// entries. It is best-effort: a missing object stops that subtree, not the scan.
func scanTreeExtensions(client *Client, prefix, rootSha string) (exts map[string]bool, names map[string]bool) {
	exts = map[string]bool{}
	names = map[string]bool{}
	visited := map[string]bool{}
	frontier := []string{rootSha}
	seen := 0
	for len(frontier) > 0 && seen < prismTreeScanCap {
		sha := frontier[0]
		frontier = frontier[1:]
		if visited[sha] {
			continue
		}
		visited[sha] = true
		body, ok := getBucketTreeBody(client, prefix, sha)
		if !ok {
			continue
		}
		for _, e := range parseTreeEntries(body) {
			seen++
			if e.isTree {
				frontier = append(frontier, e.sha)
				continue
			}
			base := e.name
			if dot := strings.LastIndex(base, "."); dot >= 0 && dot < len(base)-1 {
				exts[strings.ToLower(base[dot+1:])] = true
			} else {
				names[strings.ToLower(base)] = true
			}
		}
	}
	return exts, names
}

// treeEntry is a parsed tree row: its name, target sha, and whether it is a
// subtree (mode 40000).
type treeEntry struct {
	name   string
	sha    string
	isTree bool
}

// parseTreeEntries parses the binary tree format ("<mode> <name>\0" + 20-byte
// sha), skipping gitlinks (mode 160000). Truncation ends parsing rather than
// erroring — this is a best-effort scan.
func parseTreeEntries(body []byte) []treeEntry {
	var out []treeEntry
	for len(body) > 0 {
		nul := indexByteSite(body, 0)
		if nul < 0 || len(body) < nul+21 {
			break
		}
		mode, name, _ := strings.Cut(string(body[:nul]), " ")
		if mode != "160000" {
			out = append(out, treeEntry{name: name, sha: fmt.Sprintf("%x", body[nul+1:nul+21]), isTree: mode == "40000"})
		}
		body = body[nul+21:]
	}
	return out
}

// indexByteSite returns the index of the first b in s, or -1.
func indexByteSite(s []byte, b byte) int {
	for i := range s {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// getBucketLooseObject inflates a loose object from the bucket and returns its
// type and body. Best-effort: any fetch/inflate/format error is a miss.
func getBucketLooseObject(client *Client, prefix, sha string) (objType string, body []byte, ok bool) {
	if len(sha) != 40 {
		return "", nil, false
	}
	compressed, err := client.Get(prefix + "objects/" + sha[:2] + "/" + sha[2:])
	if err != nil {
		return "", nil, false
	}
	zr, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return "", nil, false
	}
	raw, err := io.ReadAll(zr)
	zr.Close()
	if err != nil {
		return "", nil, false
	}
	nul := indexByteSite(raw, 0)
	if nul < 0 {
		return "", nil, false
	}
	t, _, _ := strings.Cut(string(raw[:nul]), " ")
	return t, raw[nul+1:], true
}

// getBucketTreeBody returns a loose object's body when it is a tree, else
// ok=false.
func getBucketTreeBody(client *Client, prefix, sha string) (body []byte, ok bool) {
	t, b, ok := getBucketLooseObject(client, prefix, sha)
	return b, ok && t == "tree"
}

// writeSitePrismExtra scans the default branch's tree for the languages it uses
// and publishes .gitsocial/site/prism-extra.js with just those grammars, or
// deletes it when none are needed (reader falls back to the base grammars). The
// default branch is HEAD's symref target; absent a resolvable HEAD/tree the
// artifact is deleted (nothing to scan). Best-effort by contract, on the same
// refs-moved path as the other site artifacts.
func writeSitePrismExtra(client *Client, prefix string, refs map[string]string) error {
	rootSha := defaultBranchTreeSha(client, prefix, refs)
	if rootSha == "" {
		return client.Delete(prefix + sitePrismExtraKey)
	}
	exts, names := scanTreeExtensions(client, prefix, rootSha)
	ordered := orderPrismGrammars(grammarsForExtensions(exts, names))
	bundle, err := buildPrismExtra(ordered)
	if err != nil {
		return err
	}
	if bundle == nil {
		return client.Delete(prefix + sitePrismExtraKey)
	}
	comp, err := brotliCompress(bundle, brotliQualityShard)
	if err != nil {
		return fmt.Errorf("compress prism-extra: %w", err)
	}
	resp, err := client.do(http.MethodPut, prefix+sitePrismExtraKey, nil, comp, map[string]string{
		"Content-Type": "text/javascript; charset=utf-8", "Content-Encoding": "br",
	})
	if err != nil {
		return fmt.Errorf("upload %s: %w", sitePrismExtraKey, err)
	}
	resp.Body.Close()
	return nil
}

// defaultBranchTreeSha resolves the root tree sha of the default branch (HEAD's
// symref target) from the bucket, or "" when HEAD/branch/commit is unresolvable.
func defaultBranchTreeSha(client *Client, prefix string, refs map[string]string) string {
	branch := readBucketHeadBranch(client, prefix)
	if branch == "" {
		return ""
	}
	sha := refs["refs/heads/"+branch]
	if len(sha) != 40 {
		return ""
	}
	objType, body, ok := getBucketLooseObject(client, prefix, sha)
	if !ok || objType != "commit" {
		return ""
	}
	for _, line := range strings.Split(string(body), "\n") {
		if line == "" {
			break
		}
		if rest, ok := strings.CutPrefix(line, "tree "); ok && len(rest) >= 40 {
			return rest[:40]
		}
	}
	return ""
}

// readBucketHeadBranch reads the bucket's HEAD symref ("ref: refs/heads/<b>")
// and returns the branch name, or "".
func readBucketHeadBranch(client *Client, prefix string) string {
	data, err := client.Get(prefix + "HEAD")
	if err != nil {
		return ""
	}
	text := strings.TrimSpace(string(data))
	if rest, ok := strings.CutPrefix(text, "ref:"); ok {
		return strings.TrimPrefix(strings.TrimSpace(rest), "refs/heads/")
	}
	return ""
}
