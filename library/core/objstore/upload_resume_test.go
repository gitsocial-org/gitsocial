// upload_resume_test.go - the LIST-based upload resume (filterPresentObjects):
// above the delta threshold, one LIST drops objects already on the bucket so an
// interrupted push resumes; below it, no LIST is issued.

package objstore

import (
	"fmt"
	"testing"
)

// putObjectKeys uploads placeholder bytes at the object key of each sha, marking
// them present on the bucket (content irrelevant — presence is what resume
// checks).
func putObjectKeys(t *testing.T, client *Client, shas []string) {
	t.Helper()
	for _, sha := range shas {
		if err := client.Put("objects/"+sha[:2]+"/"+sha[2:], []byte("x")); err != nil {
			t.Fatalf("seed object %s: %v", sha, err)
		}
	}
}

// makeShas returns n distinct synthetic 40-hex shas.
func makeShas(n int) []string {
	shas := make([]string, n)
	for i := 0; i < n; i++ {
		shas[i] = fmt.Sprintf("%040x", i+1)
	}
	return shas
}

// TestFilterPresent_SkipsPresentAboveThreshold: a delta at the threshold LISTs
// once and drops the objects already on the bucket, keeping only the missing.
func TestFilterPresent_SkipsPresentAboveThreshold(t *testing.T) {
	client, bucket := testClient(t)
	shas := makeShas(listResumeThreshold) // exactly at the threshold
	// Mark the first half as already uploaded (an interrupted push's progress).
	half := len(shas) / 2
	putObjectKeys(t, client, shas[:half])
	listsBefore := bucket.listCount()

	kept := filterPresentObjects(client, "", shas)

	if bucket.listCount() != listsBefore+1 {
		t.Errorf("expected exactly one LIST above the threshold, saw %d", bucket.listCount()-listsBefore)
	}
	if len(kept) != len(shas)-half {
		t.Fatalf("kept %d objects, want %d (the not-yet-present half)", len(kept), len(shas)-half)
	}
	present := map[string]bool{}
	for _, s := range shas[:half] {
		present[s] = true
	}
	for _, s := range kept {
		if present[s] {
			t.Errorf("kept object %s that is already present; resume should have dropped it", s[:8])
		}
	}
}

// TestFilterPresent_SkipsListBelowThreshold: a delta below the threshold issues
// no LIST (the listing would cost more than the redundant PUTs it saves) and
// returns the input unchanged.
func TestFilterPresent_SkipsListBelowThreshold(t *testing.T) {
	client, bucket := testClient(t)
	shas := makeShas(listResumeThreshold - 1) // one below
	putObjectKeys(t, client, shas[:10])       // some already present
	listsBefore := bucket.listCount()

	kept := filterPresentObjects(client, "", shas)

	if bucket.listCount() != listsBefore {
		t.Errorf("expected NO LIST below the threshold, saw %d extra", bucket.listCount()-listsBefore)
	}
	if len(kept) != len(shas) {
		t.Errorf("below the threshold the full delta is kept: got %d, want %d", len(kept), len(shas))
	}
}

// TestFilterPresent_AllPresentResumesToEmpty: an initial push that fully
// completed except for the ref move re-runs with every object present, so resume
// drops all of them (nothing re-PUT).
func TestFilterPresent_AllPresentResumesToEmpty(t *testing.T) {
	client, _ := testClient(t)
	shas := makeShas(listResumeThreshold)
	putObjectKeys(t, client, shas)
	if kept := filterPresentObjects(client, "", shas); len(kept) != 0 {
		t.Errorf("kept %d objects when all are present; want 0 (full resume, no re-upload)", len(kept))
	}
}
