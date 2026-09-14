//go:build bench

// thread_bench_test.go - Thread reads over a seeded 100k-row cache, behind the bench tag
package social

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
)

// benchRows is the number of core_commits rows the seed writes, matched one to one in social_items.
const benchRows = 100000

// benchRepoCount is the number of repositories the seeded forest spans.
const benchRepoCount = 4

// benchBatch is the number of rows per InsertCommits and insertSocialItems call.
const benchBatch = 1000

// benchBranch is the branch every seeded commit lands on.
const benchBranch = "gitmsg/social"

// benchWideReplies is the reply count of the one root the wide case reads.
const benchWideReplies = 500

// benchDeepDepth is the length of the one reply chain the deep case reads from its leaf.
const benchDeepDepth = 40

// benchThreadSizes cycles the reply counts of the ordinary threads that fill the forest.
var benchThreadSizes = []int{0, 2, 5, 11, 23, 47}

// benchNode is one seeded commit: a root post, or a reply naming its thread root and its parent.
type benchNode struct {
	repoURL     string
	hash        string
	rootRepoURL string
	rootHash    string
	parentHash  string
}

// benchForest builds the seeded forest and returns it with the wide root and the deep leaf.
func benchForest() (nodes []benchNode, wideRoot, deepLeaf benchNode) {
	nodes = make([]benchNode, 0, benchRows)
	next := 0
	nextHash := func() string {
		next++
		return fmt.Sprintf("%012x", next)
	}
	repoURL := func(i int) string {
		return fmt.Sprintf("https://example.test/bench/repo%d", i%benchRepoCount)
	}

	nodes, wideRoot, _ = benchThread(nodes, repoURL(0), benchWideReplies, benchWideReplies, nextHash)
	nodes, _, deepLeaf = benchThread(nodes, repoURL(1), benchDeepDepth, 1, nextHash)
	for thread := 0; len(nodes) < benchRows; thread++ {
		size := benchThreadSizes[thread%len(benchThreadSizes)]
		nodes, _, _ = benchThread(nodes, repoURL(thread), size, 3, nextHash)
	}
	// Every parent precedes its children, so the cut keeps each reply's parent in the forest.
	return nodes[:benchRows], wideRoot, deepLeaf
}

// benchThread appends one root post and size replies, fanout replies per parent, and returns the root and the last reply.
func benchThread(nodes []benchNode, repoURL string, size, fanout int, nextHash func() string) ([]benchNode, benchNode, benchNode) {
	root := benchNode{repoURL: repoURL, hash: nextHash()}
	root.rootRepoURL, root.rootHash = repoURL, root.hash
	nodes = append(nodes, root)
	parents := []string{root.hash}
	last := root
	for i := 0; i < size; i++ {
		reply := benchNode{
			repoURL:     repoURL,
			hash:        nextHash(),
			rootRepoURL: repoURL,
			rootHash:    root.hash,
			parentHash:  parents[i/fanout],
		}
		nodes = append(nodes, reply)
		parents = append(parents, reply.hash)
		last = reply
	}
	return nodes, root, last
}

// benchCommit renders one node as the commit row the fetch path would insert.
func benchCommit(n benchNode, index int) cache.Commit {
	return cache.Commit{
		RepoURL:     n.repoURL,
		Hash:        n.hash,
		Branch:      benchBranch,
		AuthorName:  "Bench User",
		AuthorEmail: fmt.Sprintf("bench%d@example.test", index%97),
		Message:     "Bench item " + n.hash,
		Timestamp:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(index) * time.Minute),
	}
}

// benchItem renders one node as the social item the fetch path would insert.
func benchItem(n benchNode) SocialItem {
	item := SocialItem{RepoURL: n.repoURL, Hash: n.hash, Branch: benchBranch, Type: "post"}
	if n.parentHash == "" {
		return item
	}
	item.Type = "comment"
	item.OriginalRepoURL = cache.ToNullString(n.rootRepoURL)
	item.OriginalHash = cache.ToNullString(n.rootHash)
	item.OriginalBranch = cache.ToNullString(benchBranch)
	item.ReplyToRepoURL = cache.ToNullString(n.repoURL)
	item.ReplyToHash = cache.ToNullString(n.parentHash)
	item.ReplyToBranch = cache.ToNullString(benchBranch)
	return item
}

// benchSeedCache opens a fresh cache, writes the forest through the real write path, and analyzes it.
func benchSeedCache(b *testing.B) (wideRoot, deepLeaf benchNode) {
	b.Helper()
	cache.Reset()
	if err := cache.Open(b.TempDir()); err != nil {
		b.Fatalf("cache.Open() error = %v", err)
	}
	b.Cleanup(func() {
		cache.Reset()
		if testCacheDir != "" {
			_ = cache.Open(testCacheDir)
		}
	})

	nodes, wideRoot, deepLeaf := benchForest()
	start := time.Now()
	for from := 0; from < len(nodes); from += benchBatch {
		to := min(from+benchBatch, len(nodes))
		commits := make([]cache.Commit, 0, to-from)
		items := make([]SocialItem, 0, to-from)
		for i, n := range nodes[from:to] {
			commits = append(commits, benchCommit(n, from+i))
			items = append(items, benchItem(n))
		}
		if err := cache.InsertCommits(commits); err != nil {
			b.Fatalf("InsertCommits() error = %v", err)
		}
		if err := insertSocialItems(items); err != nil {
			b.Fatalf("insertSocialItems() error = %v", err)
		}
	}
	if err := cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec("ANALYZE")
		return err
	}); err != nil {
		b.Fatalf("ANALYZE error = %v", err)
	}
	b.Logf("seeded %d core_commits and %d social_items rows in %s",
		benchRowCount(b, "core_commits"), benchRowCount(b, "social_items"), time.Since(start).Round(time.Millisecond))
	return wideRoot, deepLeaf
}

// benchRowCount reads how many rows a seeded table holds.
func benchRowCount(b *testing.B, table string) int {
	b.Helper()
	count, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var n int
		err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n)
		return n, err
	})
	if err != nil {
		b.Fatalf("count %s error = %v", table, err)
	}
	return count
}

// benchAssertPlan pins the seeks one reader relies on, against the seeded statistics.
func benchAssertPlan(b *testing.B, reader string, indexes []string, query string, args ...interface{}) {
	b.Helper()
	plan := strings.Join(queryPlan(b, query, args...), "\n")
	for _, index := range indexes {
		if !strings.Contains(plan, index) {
			b.Errorf("%s plan does not use %s:\n%s", reader, index, plan)
		}
	}
	if strings.Contains(plan, "SCAN core_commits") {
		b.Errorf("%s plan scans core_commits:\n%s", reader, plan)
	}
	b.Logf("%s plan over %d rows:\n%s", reader, benchRows, plan)
}

// benchAssertReaderPlans pins the comment reader and the thread reader, the pins the tier tests hold.
func benchAssertReaderPlans(b *testing.B, root benchNode) {
	b.Helper()
	benchAssertPlan(b, "comment reader", []string{"idx_social_original"},
		commentsQuery(benchBranch), "", root.repoURL, root.hash, benchBranch)
	benchAssertPlan(b, "thread reader", threadPlanIndexes, threadQuery(1),
		root.repoURL, root.hash, benchBranch, root.hash, benchBranch, root.repoURL, "")
}

// BenchmarkGetThread reads a wide root and a deep leaf out of a seeded 100k-row cache.
func BenchmarkGetThread(b *testing.B) {
	wideRoot, deepLeaf := benchSeedCache(b)
	benchAssertReaderPlans(b, wideRoot)

	cases := []struct {
		name string
		node benchNode
	}{
		{"wide_root", wideRoot},
		{"deep_leaf", deepLeaf},
	}
	for _, tc := range cases {
		items, err := getThread(tc.node.repoURL, tc.node.hash, benchBranch, "", nil)
		if err != nil {
			b.Fatalf("getThread(%s) error = %v", tc.name, err)
		}
		b.Logf("%s reads %d items", tc.name, len(items))
	}
	b.ResetTimer()

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			for range b.N {
				if _, err := getThread(tc.node.repoURL, tc.node.hash, benchBranch, "", nil); err != nil {
					b.Fatalf("getThread() error = %v", err)
				}
			}
		})
	}
}
