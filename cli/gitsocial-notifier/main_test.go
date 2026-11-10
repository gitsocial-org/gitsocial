// main_test.go - Tests for the arguments of the notifier; no test posts a notification.
package main

import (
	"bytes"
	"testing"
)

func TestParseArgs_actions(t *testing.T) {
	cases := []struct {
		args                            []string
		action, title, url, socket, ref string
	}{
		{nil, "click", "", "", "", ""},
		{[]string{"notify", "GitSocial", "-body", "https://site.example/"}, "notify", "GitSocial", "https://site.example/", "", ""},
		{[]string{"notify", "GitSocial", "body", "", "/tmp/click/1-2.sock", "#commit:0123456789ab"}, "notify", "GitSocial", "", "/tmp/click/1-2.sock", "#commit:0123456789ab"},
		{[]string{"notify", "GitSocial", "body", "", "/tmp/click/1-2.sock", ""}, "notify", "GitSocial", "", "/tmp/click/1-2.sock", ""},
		{[]string{"notify", "GitSocial", "body", "", "", "#commit:0123456789ab"}, "", "", "", "", ""},
		{[]string{"notify", "GitSocial", "body", "", "/tmp/click/1-2.sock"}, "", "", "", "", ""},
		{[]string{"notify", "GitSocial", "body"}, "", "", "", "", ""},
		{[]string{"post", "a", "b", "c"}, "", "", "", "", ""},
	}
	for _, c := range cases {
		action, title, _, url, socket, ref := parseArgs(c.args)
		if action != c.action || title != c.title || url != c.url || socket != c.socket || ref != c.ref {
			t.Errorf("parseArgs(%q) = %q %q %q %q %q, want %q %q %q %q %q", c.args, action, title, url, socket, ref, c.action, c.title, c.url, c.socket, c.ref)
		}
	}
}

func TestRun_usage(t *testing.T) {
	var errOut bytes.Buffer
	if code := run([]string{"post"}, &errOut); code != 2 || !bytes.Contains(errOut.Bytes(), []byte("usage")) {
		t.Errorf("run(post) = %d, %q, want 2 and the usage", code, errOut.String())
	}
}
