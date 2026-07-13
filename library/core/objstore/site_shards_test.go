// site_shards_test.go - unit tests for the generic shard/manifest layer against
// an in-process S3 stub (memBucket): seal boundaries at N and N±1, content-hash
// key stability across the write and append paths, skip-existing (no re-PUT of a
// present shard), and manifest round-trip.

package objstore

import (
	"fmt"
	"net/http/httptest"
	"testing"
)

// testShardCount is the lowered sealed-shard size the shard tests run under, so
// multi-shard boundaries are exercised on tiny corpora.
const testShardCount = 4

// withTestShardCount runs fn with shardBodyCount lowered to testShardCount,
// restoring it after.
func withTestShardCount(fn func()) {
	prev := shardBodyCount
	shardBodyCount = testShardCount
	defer func() { shardBodyCount = prev }()
	fn()
}

// testClient spins a memBucket-backed httptest server and returns a path-style
// Client pointed at it plus the bucket (for PUT-count assertions).
func testClient(t *testing.T) (*Client, *memBucket) {
	t.Helper()
	bucket := newMemBucket()
	srv := httptest.NewServer(bucket)
	t.Cleanup(srv.Close)
	client, err := NewClient(Config{
		Endpoint: srv.URL, Bucket: "b", Region: "us-east-1",
		AccessKey: "k", SecretKey: "s", PathStyle: true,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client, bucket
}

// bodyEntries builds n synthetic body entries with distinct shas (0001…).
func bodyEntries(n int) []siteBodyEntry {
	out := make([]siteBodyEntry, n)
	for i := 0; i < n; i++ {
		sha := fmt.Sprintf("%040x", i+1)
		out[i] = siteBodyEntry{SHA: sha, Author: "a", TS: int64(i), Message: fmt.Sprintf("msg %d", i)}
	}
	return out
}

// newestFirst reverses an oldest-first entry list to the walk's newest-first
// order (the input shape writeCorpus expects).
func newestFirst(in []siteBodyEntry) []siteBodyEntry { return reverseGeneric(in) }

// writeCorpus fully (re)builds one corpus through the staged primitives (plan,
// then head, then manifest): the composition putSiteArtifacts interleaves across
// both corpora, driven here for one corpus at a time.
func writeCorpus[E shardEntry](client *Client, corpus shardCorpus[E], ext, tip string, entries []E, bodiesBytes int) (int, error) {
	plan, err := planSharded(client, corpus, "", ext, entries, nil, nil)
	if err != nil {
		return 0, err
	}
	if err := putHead(client, corpus, "", ext, tip, &plan); err != nil {
		return 0, err
	}
	return putManifest(client, corpus, "", ext, tip, plan, bodiesBytes, true)
}

func TestWriteBodiesSharded_SealBoundaries(t *testing.T) {
	cases := []struct {
		total, want int // want = number of sealed shards
	}{
		{total: 3, want: 0}, // < N: head only
		{total: 4, want: 1}, // == N: one full shard, empty head
		{total: 5, want: 1}, // N+1: one shard + head of 1
		{total: 7, want: 1}, // 2N-1
		{total: 8, want: 2}, // 2N
		{total: 9, want: 2}, // 2N+1
	}
	for _, tc := range cases {
		withTestShardCount(func() {
			client, _ := testClient(t)
			oldest := bodyEntries(tc.total)
			tip := oldest[len(oldest)-1].SHA
			if _, err := writeCorpus(client, bodiesCorpus, "social", tip, newestFirst(oldest), 0); err != nil {
				t.Fatalf("total=%d: writeCorpus: %v", tc.total, err)
			}
			m, err := readBodiesManifest(client, "", "social")
			if err != nil || m == nil {
				t.Fatalf("total=%d: readBodiesManifest: %v (nil=%v)", tc.total, err, m == nil)
			}
			if len(m.Shards) != tc.want {
				t.Errorf("total=%d: got %d sealed shards, want %d", tc.total, len(m.Shards), tc.want)
			}
			wantHead := tc.total - tc.want*4
			if m.Head.Count != wantHead {
				t.Errorf("total=%d: head count %d, want %d", tc.total, m.Head.Count, wantHead)
			}
			if !m.Complete {
				t.Errorf("total=%d: manifest not marked complete", tc.total)
			}
			if m.Tip != tip {
				t.Errorf("total=%d: manifest tip %s, want %s", tc.total, m.Tip, tip)
			}
		})
	}
}

func TestShardContentHash_StableAndOrdered(t *testing.T) {
	e := bodyEntries(4)
	h1 := shardContentHash(siteItemsVersion, e)
	h2 := shardContentHash(siteItemsVersion, bodyEntries(4))
	if h1 != h2 {
		t.Fatalf("same members yielded different hashes: %s vs %s", h1, h2)
	}
	// A different membership (one extra commit) yields a different key.
	if shardContentHash(siteItemsVersion, bodyEntries(5)) == h1 {
		t.Errorf("different membership produced the same content hash %s", h1)
	}
	// Reordered members yield a different key (ordering is part of identity).
	if shardContentHash(siteItemsVersion, reverseGeneric(e)) == h1 {
		t.Errorf("reordered members produced the same content hash %s", h1)
	}
	// A schema tick (a non-v4 version) salts the hash so the same membership
	// seals under a NEW key, while v4 stays byte-identical to the pre-salt hash.
	if shardContentHash(siteCodeItemsVersion, e) == h1 {
		t.Errorf("versioned hash matched the unsalted v4 hash %s", h1)
	}
}

func TestSealShard_StableKeyAcrossWriteAndAppend(t *testing.T) {
	withTestShardCount(func() {
		// Path A: a full rebuild of 8 commits seals two shards.
		clientA, _ := testClient(t)
		oldest := bodyEntries(8)
		tip8 := oldest[len(oldest)-1].SHA
		if _, err := writeCorpus(clientA, bodiesCorpus, "pm", tip8, newestFirst(oldest), 0); err != nil {
			t.Fatalf("writeCorpus: %v", err)
		}
		mA, _ := readBodiesManifest(clientA, "", "pm")

		// Path B: build 4, then append 4 more (sealing the second shard on append).
		clientB, _ := testClient(t)
		first := bodyEntries(4)
		tip4 := first[len(first)-1].SHA
		if _, err := writeCorpus(clientB, bodiesCorpus, "pm", tip4, newestFirst(first), 0); err != nil {
			t.Fatalf("writeCorpus first: %v", err)
		}
		mB0, _ := readBodiesManifest(clientB, "", "pm")
		headB, _ := readBodyDocItems(clientB, bodiesHeadKey("pm"))
		gapOldest := bodyEntries(8)[4:] // commits 5..8, oldest-first
		gapNewestFirst := reverseGeneric(gapOldest)
		planB, err := planBodiesAppend(clientB, "", "pm", gapNewestFirst, headB, mB0, nil)
		if err != nil {
			t.Fatalf("planBodiesAppend: %v", err)
		}
		if err := putBodiesHead(clientB, "", "pm", tip8, &planB); err != nil {
			t.Fatalf("putBodiesHead: %v", err)
		}
		if _, err := putBodiesManifest(clientB, "", "pm", tip8, planB, true); err != nil {
			t.Fatalf("putBodiesManifest: %v", err)
		}
		mB, _ := readBodiesManifest(clientB, "", "pm")

		if len(mA.Shards) != 2 || len(mB.Shards) != 2 {
			t.Fatalf("shard counts differ: A=%d B=%d (want 2,2)", len(mA.Shards), len(mB.Shards))
		}
		for i := range mA.Shards {
			if mA.Shards[i].Key != mB.Shards[i].Key || mA.Shards[i].Hash != mB.Shards[i].Hash {
				t.Errorf("shard %d key differs across write vs append: %s vs %s", i, mA.Shards[i].Key, mB.Shards[i].Key)
			}
			if mA.Shards[i].EndTip != mB.Shards[i].EndTip {
				t.Errorf("shard %d endTip differs: %s vs %s", i, mA.Shards[i].EndTip, mB.Shards[i].EndTip)
			}
		}
		if mB.Tip != tip8 {
			t.Errorf("append manifest tip %s, want %s", mB.Tip, tip8)
		}
	})
}

func TestSealShard_SkipExistingNoRePut(t *testing.T) {
	withTestShardCount(func() {
		client, bucket := testClient(t)
		oldest := bodyEntries(8)
		tip := oldest[len(oldest)-1].SHA
		if _, err := writeCorpus(client, bodiesCorpus, "review", tip, newestFirst(oldest), 0); err != nil {
			t.Fatalf("writeCorpus: %v", err)
		}
		m, _ := readBodiesManifest(client, "", "review")
		shardKey := bodiesShardKey("review", m.Shards[0].Hash)
		before := bucket.putCount(shardKey)
		if before != 1 {
			t.Fatalf("first build PUT the shard %d times, want 1", before)
		}
		// A second identical full rebuild must not re-PUT the already-present,
		// content-hash-keyed sealed shards.
		if _, err := writeCorpus(client, bodiesCorpus, "review", tip, newestFirst(oldest), 0); err != nil {
			t.Fatalf("re-writeCorpus: %v", err)
		}
		if after := bucket.putCount(shardKey); after != before {
			t.Errorf("sealed shard re-PUT on rebuild: %d PUTs, want %d", after, before)
		}
	})
}

func TestManifest_RoundTripFields(t *testing.T) {
	withTestShardCount(func() {
		client, _ := testClient(t)
		// Bodies: write and read back, checking count/bytes/endTip.
		oldest := bodyEntries(6)
		tip := oldest[len(oldest)-1].SHA
		bodiesBytes, err := writeCorpus(client, bodiesCorpus, "memo", tip, newestFirst(oldest), 0)
		if err != nil {
			t.Fatalf("writeCorpus: %v", err)
		}
		bm, _ := readBodiesManifest(client, "", "memo")
		if len(bm.Shards) != 1 || bm.Shards[0].Count != 4 {
			t.Fatalf("bodies shard count wrong: %+v", bm.Shards)
		}
		if bm.Shards[0].Bytes <= 0 {
			t.Errorf("bodies shard bytes not recorded: %d", bm.Shards[0].Bytes)
		}
		if bm.Shards[0].EndTip != oldest[3].SHA {
			t.Errorf("bodies shard endTip %s, want %s", bm.Shards[0].EndTip, oldest[3].SHA)
		}
		if bm.TotalBytes != bodiesBytes {
			t.Errorf("bodies manifest TotalBytes %d, want %d", bm.TotalBytes, bodiesBytes)
		}
		if bm.BodiesBytes != 0 {
			t.Errorf("bodies manifest carries a nonzero bodiesBytes %d", bm.BodiesBytes)
		}

		// Items: write with a threaded bodiesBytes and confirm it round-trips.
		meta := make([]siteMetaEntry, len(oldest))
		for i, e := range oldest {
			meta[i] = siteMetaEntry{SHA: e.SHA, Author: e.Author, TS: e.TS, Header: "", Subject: fmt.Sprintf("s%d", i)}
		}
		if _, err := writeCorpus(client, itemsCorpus, "memo", tip, reverseMeta(meta), bodiesBytes); err != nil {
			t.Fatalf("writeCorpus items: %v", err)
		}
		im, err := readItemsManifest(client, "", "memo")
		if err != nil || im == nil {
			t.Fatalf("readItemsManifest: %v (nil=%v)", err, im == nil)
		}
		if im.BodiesBytes != bodiesBytes {
			t.Errorf("items manifest bodiesBytes %d, want %d", im.BodiesBytes, bodiesBytes)
		}
		if !im.Complete {
			t.Errorf("items manifest not complete")
		}
		if im.Tip != tip {
			t.Errorf("items manifest tip %s, want %s", im.Tip, tip)
		}
		if len(im.Shards) != 1 || im.Shards[0].Count != 4 {
			t.Errorf("items shard shape wrong: %+v", im.Shards)
		}
	})
}

// reverseMeta reverses an oldest-first metadata slice to newest-first (the shape
// writeCorpus expects).
func reverseMeta(in []siteMetaEntry) []siteMetaEntry { return reverseGeneric(in) }

// TestReadCompressedJSON_TranscodedBody pins the provider-transcoding fallback:
// Cloudflare R2 transparently decompresses `Content-Encoding: br` objects when
// the requester doesn't advertise br support (Go's transport only advertises
// gzip), so a stored artifact arrives as plain JSON. Such a body must still be
// read — treating it as absent silently re-bootstrapped every corpus on every
// push and permanently blocked the HTML page layer.
func TestReadCompressedJSON_TranscodedBody(t *testing.T) {
	client, _ := testClient(t)
	tip := fmt.Sprintf("%040x", 42)
	meta := []siteMetaEntry{{SHA: tip, Author: "a", TS: 1, Subject: "s"}}
	if _, err := writeCorpus(client, itemsCorpus, "pm", tip, meta, 0); err != nil {
		t.Fatalf("writeCorpus: %v", err)
	}
	// Simulate the transcoding provider: replace the stored manifest with its
	// decompressed bytes (plain JSON, no Content-Encoding).
	compressed, err := client.Get(itemsCorpus.manifestKey("pm"))
	if err != nil {
		t.Fatalf("get manifest: %v", err)
	}
	plain, err := brotliDecompress(compressed)
	if err != nil {
		t.Fatalf("decompress manifest: %v", err)
	}
	if err := client.Put(itemsCorpus.manifestKey("pm"), plain); err != nil {
		t.Fatalf("put plain manifest: %v", err)
	}
	m, err := readItemsManifest(client, "", "pm")
	if err != nil {
		t.Fatalf("readItemsManifest: %v", err)
	}
	if m == nil {
		t.Fatalf("transcoded (plain JSON) manifest read as absent")
	}
	if m.Tip != tip || !m.Complete {
		t.Errorf("transcoded manifest content wrong: %+v", m)
	}
	// Garbage must still read as absent, not error.
	if err := client.Put(itemsCorpus.manifestKey("pm"), []byte("not json")); err != nil {
		t.Fatalf("put garbage: %v", err)
	}
	if m, err := readItemsManifest(client, "", "pm"); err != nil || m != nil {
		t.Errorf("garbage manifest: got (%+v, %v), want (nil, nil)", m, err)
	}
}
