// webhook.go - Posts each message of a fetch as JSON to the URL in notifications.webhook.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/log"
	"github.com/gitsocial-org/gitsocial/library/core/notifications"
)

const (
	webhookTimeout    = 5 * time.Second
	webhookRetryDelay = time.Second
)

// postWebhook posts the messages at the same time, each as one JSON document with one retry after delay on a network error or a 5xx, under one deadline for the batch of two request timeouts of the client, or of webhookTimeout, and the delay; a failure is a debug log line.
func postWebhook(httpClient *http.Client, url string, messages []notifications.DesktopMessage, delay time.Duration) {
	timeout := httpClient.Timeout
	if timeout == 0 {
		timeout = webhookTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*timeout+delay)
	defer cancel()
	var wg sync.WaitGroup
	for _, message := range messages {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := postWithRetry(ctx, httpClient, url, message, delay); err != nil {
				log.Debug("webhook failed", "error", err)
			}
		}()
	}
	wg.Wait()
}

// postWithRetry posts one message and retries once after delay when the failure is worth it.
func postWithRetry(ctx context.Context, httpClient *http.Client, url string, message notifications.DesktopMessage, delay time.Duration) error {
	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode webhook message: %w", err)
	}
	retry, err := postJSON(ctx, httpClient, url, body)
	if !retry {
		return err
	}
	select {
	case <-ctx.Done():
		return err
	case <-time.After(delay):
	}
	_, err = postJSON(ctx, httpClient, url, body)
	return err
}

// postJSON posts one JSON document and reports whether a failure is worth one retry: a network error or a 5xx.
func postJSON(ctx context.Context, httpClient *http.Client, url string, body []byte) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return true, fmt.Errorf("post webhook: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body) // a drained body lets the client reuse the connection
	switch {
	case resp.StatusCode >= 500:
		return true, fmt.Errorf("webhook answered %d", resp.StatusCode)
	case resp.StatusCode >= 300:
		return false, fmt.Errorf("webhook answered %d", resp.StatusCode)
	}
	return false, nil
}
