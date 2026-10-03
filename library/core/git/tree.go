// tree.go - Tree and blob reads at a ref
package git

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// TreeEntry is one entry of a tree: a subtree, a blob with its size, or a submodule commit.
type TreeEntry struct {
	Name string
	Type string
	Size int64
}

// ListTree returns the entries of a directory of a ref, subtrees first and each group by name; an empty path is the root.
func ListTree(workdir, ref, path string) ([]TreeEntry, error) {
	spec := ref
	if path = strings.Trim(path, "/"); path != "" {
		spec = ref + ":" + path
	}
	out, err := execGitSimple(workdir, []string{"ls-tree", "-l", "-z", spec})
	if err != nil {
		return nil, fmt.Errorf("list tree %s: %w", spec, err)
	}
	var entries []TreeEntry
	for _, record := range strings.Split(out, "\x00") {
		meta, name, ok := strings.Cut(record, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) < 4 {
			continue
		}
		size, _ := strconv.ParseInt(fields[3], 10, 64) // a tree reports "-", which reads as 0
		entries = append(entries, TreeEntry{Name: name, Type: fields[1], Size: size})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if (entries[i].Type == "tree") != (entries[j].Type == "tree") {
			return entries[i].Type == "tree"
		}
		return entries[i].Name < entries[j].Name
	})
	return entries, nil
}

// ObjectType returns the git type of an object spec such as "main:src", "tree" for an empty path.
func ObjectType(workdir, ref, path string) (string, error) {
	if path = strings.Trim(path, "/"); path == "" {
		return "tree", nil
	}
	out, err := execGitSimple(workdir, []string{"cat-file", "-t", ref + ":" + path})
	if err != nil {
		return "", fmt.Errorf("object type of %s: %w", path, err)
	}
	return strings.TrimSpace(out), nil
}

// IsBinaryContent reports whether content has a NUL byte in its first 8000 bytes, the test git itself uses.
func IsBinaryContent(content string) bool {
	head := content
	if len(head) > 8000 {
		head = head[:8000]
	}
	return strings.IndexByte(head, 0) >= 0
}
