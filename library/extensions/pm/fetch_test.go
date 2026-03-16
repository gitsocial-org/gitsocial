// fetch_test.go - Tests for fetch wrapper functions
package pm

import (
	"testing"
)

func TestProcessors_returnsSlice(t *testing.T) {
	procs := Processors()
	if len(procs) != 1 {
		t.Errorf("Processors() returned %d processors, want 1", len(procs))
	}
	if procs[0] == nil {
		t.Error("Processors()[0] should not be nil")
	}
}
