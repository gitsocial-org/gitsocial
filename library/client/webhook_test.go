// webhook_test.go - Tests for the webhook post: the JSON document of each message, the one retry and the deadline of a batch.
package client

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/notifications"
)

// webhookRecorder is a test server that records each posted document and answers with the status that fail gives for it.
func webhookRecorder(t *testing.T, fail func(doc map[string]any, seen int) int) (*httptest.Server, func() []map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var docs []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("request = %s %q, want a POST of application/json", r.Method, r.Header.Get("Content-Type"))
		}
		data, _ := io.ReadAll(r.Body)
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Errorf("body %q is not JSON: %v", data, err)
		}
		mu.Lock()
		seen := 0
		for _, d := range docs {
			if d["body"] == doc["body"] {
				seen++
			}
		}
		docs = append(docs, doc)
		mu.Unlock()
		w.WriteHeader(fail(doc, seen))
	}))
	t.Cleanup(server.Close)
	return server, func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any(nil), docs...)
	}
}

func TestWebhook_payloadAndRetry(t *testing.T) {
	item := notifications.DesktopMessage{Title: "GitSocial", Body: "Alice mentioned you · owner/repo", URL: "https://site.example/index.html#commit:0123456789ab", Type: "mention", Repo: "https://example.com/owner/repo", Hash: "0123456789abcdef", Socket: "/tmp/click/1-2.sock", Ref: "https://example.com/owner/repo#commit:0123456789ab"}
	summary := notifications.DesktopMessage{Title: "GitSocial", Body: "5 new notifications", URL: "https://site.example/"}
	server, docs := webhookRecorder(t, func(doc map[string]any, seen int) int {
		if doc["body"] == item.Body && seen == 0 {
			return http.StatusBadGateway
		}
		return http.StatusOK
	})
	postWebhook(server.Client(), server.URL, []notifications.DesktopMessage{item, summary}, 10*time.Millisecond)
	want := map[string]any{"title": item.Title, "body": item.Body, "url": item.URL, "type": item.Type, "repo": item.Repo, "hash": item.Hash}
	items, summaries := 0, 0
	for _, doc := range docs() {
		if doc["body"] == summary.Body {
			summaries++
			if len(doc) != 3 || doc["url"] != summary.URL {
				t.Errorf("summary post = %v, want title, body and url only", doc)
			}
			continue
		}
		items++
		if len(doc) != len(want) {
			t.Errorf("item post = %v, want only the keys %v", doc, want)
		}
		for key, value := range want {
			if doc[key] != value {
				t.Errorf("item post %s = %v, want %v", key, doc[key], value)
			}
		}
	}
	if items != 2 || summaries != 1 {
		t.Errorf("posts = %d of the item and %d of the summary, want the item again after a 502 and the summary once", items, summaries)
	}
	failing, failed := webhookRecorder(t, func(map[string]any, int) int { return http.StatusServiceUnavailable })
	postWebhook(failing.Client(), failing.URL, []notifications.DesktopMessage{item}, 10*time.Millisecond)
	if got := len(failed()); got != 2 {
		t.Errorf("posts to a server that always fails = %d, want one try and one retry", got)
	}
}

func TestWebhook_batchDeadline(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	httpClient := server.Client()
	httpClient.Timeout = 200 * time.Millisecond
	message := notifications.DesktopMessage{Title: "GitSocial", Body: "body"}
	batch := []notifications.DesktopMessage{message, message, message, message}
	start := time.Now()
	postWebhook(httpClient, server.URL, batch, 50*time.Millisecond)
	cycle := 2*httpClient.Timeout + 50*time.Millisecond
	if elapsed := time.Since(start); elapsed > cycle+300*time.Millisecond {
		t.Errorf("a batch of %d to a hanging server took %v, want about one cycle of %v", len(batch), elapsed, cycle)
	}
}
