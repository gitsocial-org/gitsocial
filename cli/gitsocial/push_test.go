// push_test.go - the human-readable push report
package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/client"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/site"
)

// TestPush_forkPullsLine reports the fork pull request area on the site line when the key is on, and nothing about it otherwise.
func TestPush_forkPullsLine(t *testing.T) {
	push := &gitmsg.PushResult{Remote: "origin", RemoteURL: "s3://host/bucket/", Commits: 1}
	render := func(s client.SiteOutcome) string {
		var out bytes.Buffer
		printPushResult(&out, &client.Result{Push: push, Site: s}, false)
		return out.String()
	}
	on := render(client.SiteOutcome{Published: true, Complete: true, ForkPulls: &site.ForkPullArea{Count: 2, Bytes: 1363149}})
	if !strings.Contains(on, "Site: published (fork pull requests: 2, 1.3 MB)\n") {
		t.Errorf("site line with the key on:\n%s", on)
	}
	incomplete := render(client.SiteOutcome{Published: true, ForkPulls: &site.ForkPullArea{}})
	if !strings.Contains(incomplete, "Site: published (incomplete: a bootstrap is still in progress, push again to finish; fork pull requests: 0, 0.0 MB)\n") {
		t.Errorf("site line of an incomplete site:\n%s", incomplete)
	}
	off := render(client.SiteOutcome{Published: true, Complete: true})
	if !strings.Contains(off, "Site: published\n") || strings.Contains(off, "fork") {
		t.Errorf("site line with the key off:\n%s", off)
	}
}
