//go:build !(darwin && cgo)

// notifier_other_test.go - Tests for the notifier of a build with no notification support.
package main

import "testing"

func TestStub_failsSoTheSenderFallsBack(t *testing.T) {
	if code := notify("GitSocial", "body", ""); code == 0 {
		t.Error("notify = 0, want a failure so the sender falls back to osascript")
	}
	if code := handleClick(); code == 0 {
		t.Error("handleClick = 0, want a failure")
	}
}
