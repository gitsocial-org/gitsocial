// client.go - Minimal stdlib S3 client
package objstore

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Config describes an S3-compatible endpoint and bucket.
type Config struct {
	Endpoint  string // e.g. https://s3.us-east-1.amazonaws.com or https://<account>.r2.cloudflarestorage.com
	Region    string // AWS region, or "auto" for R2
	Bucket    string
	AccessKey string
	SecretKey string
	PathStyle bool // path-style addressing (local test servers); virtual-host otherwise
}

// Client is a minimal S3 client over stdlib HTTP + SigV4.
type Client struct {
	cfg  Config
	http *http.Client
}

// NewClient builds a client. Credentials resolve from GITSOCIAL_S3_ACCESS_KEY /
// GITSOCIAL_S3_SECRET_KEY first (so a non-AWS bucket never silently picks up
// real AWS credentials exported for other tooling), then fall back to the
// S3-ecosystem-standard AWS env vars.
func NewClient(cfg Config) (*Client, error) {
	if cfg.AccessKey == "" {
		cfg.AccessKey = firstEnv("GITSOCIAL_S3_ACCESS_KEY", "AWS_ACCESS_KEY_ID")
	}
	if cfg.SecretKey == "" {
		cfg.SecretKey = firstEnv("GITSOCIAL_S3_SECRET_KEY", "AWS_SECRET_ACCESS_KEY")
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = "https://s3." + cfg.Region + ".amazonaws.com"
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("objstore: bucket required")
	}
	if cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("objstore: credentials required (GITSOCIAL_S3_ACCESS_KEY / GITSOCIAL_S3_SECRET_KEY, or AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY)")
	}
	return &Client{cfg: cfg, http: &http.Client{Timeout: 60 * time.Second, Transport: newTransport()}}, nil
}

// newTransport returns an HTTP transport sized for the concurrent push
// upload pool: stdlib's default keeps only 2 idle connections per host, so a
// pool of N workers would churn N-2 fresh TLS handshakes every round. Keep
// enough idle connections alive to match a generous pool and cap total
// connections so a large custom pool can't exhaust local sockets.
func newTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 128
	t.MaxIdleConnsPerHost = 64
	t.MaxConnsPerHost = 128
	return t
}

// firstEnv returns the first non-empty value among the named env vars.
func firstEnv(names ...string) string {
	for _, name := range names {
		if v := os.Getenv(name); v != "" {
			return v
		}
	}
	return ""
}

// objectURL builds the request URL for a key (or the bucket root when key is "").
func (c *Client) objectURL(key string) (*url.URL, error) {
	base, err := url.Parse(c.cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("objstore: parse endpoint: %w", err)
	}
	u := *base
	if c.cfg.PathStyle {
		u.Path = "/" + c.cfg.Bucket
		if key != "" {
			u.Path += "/" + key
		}
	} else {
		u.Host = c.cfg.Bucket + "." + base.Host
		u.Path = "/"
		if key != "" {
			u.Path += key
		}
	}
	u.RawPath = ""
	return &u, nil
}

// do signs and executes a request, returning the response. Non-2xx responses
// are returned as errors except 404 (ErrNotFound) and 412/409
// (ErrPreconditionFailed, the CAS-retry signal).
var (
	ErrNotFound           = fmt.Errorf("objstore: not found")
	ErrPreconditionFailed = fmt.Errorf("objstore: precondition failed")
)

// httpStatusError carries a non-2xx HTTP status code so callers (the GET retry)
// can tell a transient server fault (5xx, 429 — worth retrying) from a client
// error (4xx — not). A transport-level error (killed connection, DNS, TLS)
// carries no status code and is surfaced separately; the retry treats it as
// transient too.
type httpStatusError struct {
	code int
	err  error
}

func (e *httpStatusError) Error() string { return e.err.Error() }
func (e *httpStatusError) Unwrap() error { return e.err }

// isTransientReadError reports whether a failed GET/HEAD/LIST is worth retrying:
// a 5xx or 429 status (the provider is momentarily unavailable or throttling —
// e.g. Cloudflare's transient 503) or a transport-level error with no status
// (a dropped connection, a DNS/TLS blip). A 404/403/412 or any other 4xx is a
// definite answer and never retried. GETs are idempotent, so a retry is always
// safe.
func isTransientReadError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrPreconditionFailed) {
		return false
	}
	var se *httpStatusError
	if errors.As(err, &se) {
		return se.code == 429 || (se.code >= 500 && se.code <= 599)
	}
	// No HTTP status reached us: a transport-level failure (connection reset,
	// timeout, DNS). Idempotent read, so retry.
	return true
}

func (c *Client) do(method, key string, query url.Values, body []byte, headers map[string]string) (*http.Response, error) {
	u, err := c.objectURL(key)
	if err != nil {
		return nil, err
	}
	if query != nil {
		u.RawQuery = query.Encode()
	}
	var reader io.Reader
	payloadHash := emptyPayloadSHA256
	if body != nil {
		reader = bytes.NewReader(body)
		payloadHash = hexSHA256(body)
	}
	req, err := http.NewRequest(method, u.String(), reader)
	if err != nil {
		return nil, err
	}
	req.ContentLength = int64(len(body))
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	// Stamp every upload with its cache policy (immutable loose objects vs
	// always-revalidate mutable state) at this single chokepoint, so both the
	// git-push and site-push write paths get it. Cache-Control is not a signed
	// header, so this never affects SigV4.
	if method == http.MethodPut && req.Header.Get("Cache-Control") == "" {
		req.Header.Set("Cache-Control", cacheControlForKey(key))
	}
	signRequest(req, c.cfg.AccessKey, c.cfg.SecretKey, c.cfg.Region, "s3", payloadHash, time.Now())
	debug := os.Getenv("GITSOCIAL_S3_DEBUG") == "1"
	if debug {
		fmt.Fprintf(os.Stderr, "objstore> %s %s\n", method, u.String())
		for name, values := range req.Header {
			if name == "Authorization" {
				values = []string{"<redacted>"}
			}
			fmt.Fprintf(os.Stderr, "objstore>   %s: %s\n", name, strings.Join(values, ", "))
		}
	}
	resp, err := c.http.Do(req)
	if debug && resp != nil {
		fmt.Fprintf(os.Stderr, "objstore< %d request-id=%s\n", resp.StatusCode, resp.Header.Get("x-amz-request-id"))
	}
	if err != nil {
		return nil, fmt.Errorf("objstore: %s %s: %w", method, key, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	// 412 = failed If-Match / If-None-Match; 409 = AWS's concurrent
	// conditional-write conflict. Both mean "re-read and retry" to a CAS caller.
	// The provider's error body is kept for diagnosing conditional-write quirks.
	if resp.StatusCode == http.StatusPreconditionFailed || resp.StatusCode == http.StatusConflict {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		resp.Body.Close()
		return nil, fmt.Errorf("%w: %s (HTTP %d: %s)", ErrPreconditionFailed, key, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		resp.Body.Close()
		return nil, &httpStatusError{code: resp.StatusCode, err: fmt.Errorf("objstore: %s %s: HTTP %d: %s", method, key, resp.StatusCode, strings.TrimSpace(string(respBody)))}
	}
	return resp, nil
}

// Get downloads an object's full content.
func (c *Client) Get(key string) ([]byte, error) {
	resp, err := c.do(http.MethodGet, key, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("objstore: read %s: %w", key, err)
	}
	return data, nil
}

// retryBackoff paces read/PUT retries; len+1 = total attempts. A var so tests
// can shrink the waits (shared by putObjectWithRetry and the read retries).
var retryBackoff = []time.Duration{500 * time.Millisecond, 2 * time.Second}

// withReadRetry runs an idempotent read (GET/HEAD/LIST) and retries it on a
// transient fault (5xx, 429, or a transport-level error) with bounded backoff,
// so a single Cloudflare 503 or a dropped connection mid-walk costs one retried
// read instead of losing a long operation. A definite answer (success, 404,
// 403, any other 4xx) returns immediately; a fault that persists past the
// attempts surfaces the last error. ctx aborts the wait when a peer has already
// failed a pooled operation (pass context.TODO() for an un-pooled read).
func withReadRetry[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	var result T
	var err error
	for attempt := 0; ; attempt++ {
		if result, err = fn(); err == nil || attempt >= len(retryBackoff) || !isTransientReadError(err) {
			return result, err
		}
		timer := time.NewTimer(retryBackoff[attempt])
		select {
		case <-ctx.Done():
			timer.Stop()
			return result, err
		case <-timer.C:
		}
	}
}

// GetRetry is Get with transient-fault retry (see withReadRetry): the read path
// long operations depend on (object GETs in the site walk, ref reads, manifest
// reads) so a momentary provider hiccup doesn't lose the whole pass.
func (c *Client) GetRetry(key string) ([]byte, error) {
	return withReadRetry(context.TODO(), func() ([]byte, error) { return c.Get(key) })
}

// GetWithETag downloads an object and returns its ETag for a later If-Match write.
func (c *Client) GetWithETag(key string) ([]byte, string, error) {
	resp, err := c.do(http.MethodGet, key, nil, nil, nil)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("objstore: read %s: %w", key, err)
	}
	return data, resp.Header.Get("ETag"), nil
}

// Put uploads an object (unconditional write).
func (c *Client) Put(key string, data []byte) error {
	resp, err := c.do(http.MethodPut, key, nil, data, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// PutIfMatch writes an object only when its current ETag matches (compare-and-
// swap). Returns ErrPreconditionFailed when the object changed underneath.
func (c *Client) PutIfMatch(key string, data []byte, etag string) error {
	resp, err := c.do(http.MethodPut, key, nil, data, map[string]string{"If-Match": etag})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// PutIfAbsent writes an object only when the key doesn't exist yet
// (If-None-Match: *). Returns ErrPreconditionFailed when it already exists.
func (c *Client) PutIfAbsent(key string, data []byte) error {
	resp, err := c.do(http.MethodPut, key, nil, data, map[string]string{"If-None-Match": "*"})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// Delete removes an object; deleting a missing key is not an error (S3
// semantics: DELETE is idempotent).
func (c *Client) Delete(key string) error {
	resp, err := c.do(http.MethodDelete, key, nil, nil, nil)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	resp.Body.Close()
	return nil
}

// listBucketResult is the ListObjectsV2 response envelope.
type listBucketResult struct {
	Contents []struct {
		Key  string `xml:"Key"`
		ETag string `xml:"ETag"`
	} `xml:"Contents"`
	IsTruncated           bool   `xml:"IsTruncated"`
	NextContinuationToken string `xml:"NextContinuationToken"`
}

// ListedObject is one key and its ETag from a bucket listing.
type ListedObject struct {
	Key  string
	ETag string
}

// List returns every key under the given prefix (ListObjectsV2, paginated).
func (c *Client) List(prefix string) ([]string, error) {
	objs, err := c.ListWithETags(prefix)
	if err != nil {
		return nil, err
	}
	keys := make([]string, len(objs))
	for i, obj := range objs {
		keys[i] = obj.Key
	}
	return keys, nil
}

// ListWithETags returns every key under the given prefix with its ETag
// (ListObjectsV2, paginated). The ETag comes free in the listing, so a caller
// that only needs to know whether the listing changed (the site push-state
// digest) never issues a per-key GET.
func (c *Client) ListWithETags(prefix string) ([]ListedObject, error) {
	var objs []ListedObject
	token := ""
	for {
		q := url.Values{}
		q.Set("list-type", "2")
		q.Set("prefix", prefix)
		if token != "" {
			q.Set("continuation-token", token)
		}
		resp, err := c.do(http.MethodGet, "", q, nil, nil)
		if err != nil {
			return nil, err
		}
		var result listBucketResult
		err = xml.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("objstore: decode list response: %w", err)
		}
		for _, obj := range result.Contents {
			objs = append(objs, ListedObject{Key: obj.Key, ETag: obj.ETag})
		}
		if !result.IsTruncated || result.NextContinuationToken == "" {
			return objs, nil
		}
		token = result.NextContinuationToken
	}
}
