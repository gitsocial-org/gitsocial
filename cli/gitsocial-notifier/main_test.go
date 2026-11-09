// main_test.go - Tests for the arguments of the notifier; no test posts a notification.
package main

import (
	"bytes"
	"testing"
)

func TestParseArgs_actions(t *testing.T) {
	cases := []struct {
		args               []string
		action, title, url string
	}{
		{nil, "click", "", ""},
		{[]string{"notify", "GitSocial", "-body", "https://site.example/"}, "notify", "GitSocial", "https://site.example/"},
		{[]string{"notify", "GitSocial", "body"}, "", "", ""},
		{[]string{"post", "a", "b", "c"}, "", "", ""},
	}
	for _, c := range cases {
		action, title, _, url := parseArgs(c.args)
		if action != c.action || title != c.title || url != c.url {
			t.Errorf("parseArgs(%q) = %q %q %q, want %q %q %q", c.args, action, title, url, c.action, c.title, c.url)
		}
	}
}

func TestRun_usage(t *testing.T) {
	var errOut bytes.Buffer
	if code := run([]string{"post"}, &errOut); code != 2 || !bytes.Contains(errOut.Bytes(), []byte("usage")) {
		t.Errorf("run(post) = %d, %q, want 2 and the usage", code, errOut.String())
	}
}
