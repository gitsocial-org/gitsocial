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
	cfg       Config
	http      *http.Client
	anonymous bool // no credentials: unsigned, read-only
}

// Anonymous reports whether the client has no credentials, so callers can name
// the reason a write is impossible before attempting one.
func (c *Client) Anonymous() bool { return c.anonymous }

// NewClient builds a client from a resolved config; one carrying neither credential half builds an anonymous, read-only client.
func NewClient(cfg Config) (*Client, error) {
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = "https://s3." + cfg.Region + ".amazonaws.com"
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("objstore: bucket required")
	}
	// Half a pair is a typo, not a request for anonymous access.
	if (cfg.AccessKey == "") != (cfg.SecretKey == "") {
		return nil, ErrCredentialsRequired
	}
	return &Client{
		cfg:       cfg,
		http:      &http.Client{Timeout: 60 * time.Second, Transport: newTransport()},
		anonymous: cfg.AccessKey == "",
	}, nil
}

// newTransport returns an HTTP transport sized for the concurrent upload pool, so it does not churn a TLS handshake per worker per round.
func newTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 128
	t.MaxIdleConnsPerHost = 64
	t.MaxConnsPerHost = 128
	return t
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
	// ErrAccessDenied is a 403 on a read; it wraps ErrNotFound, since a bucket that denies listing answers 403 for absent keys too.
	ErrAccessDenied = fmt.Errorf("%w (access denied)", ErrNotFound)
	// ErrCredentialsRequired replaces the 403 an unsigned write would earn.
	ErrCredentialsRequired = fmt.Errorf("objstore: credentials required (`gitsocial config credentials set <remote>`, GITSOCIAL_S3_ACCESS_KEY / GITSOCIAL_S3_SECRET_KEY, or AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY)")
)

// httpStatusError carries a non-2xx status code, so the retry can tell a transient server fault from a client error.
type httpStatusError struct {
	code int
	err  error
}

func (e *httpStatusError) Error() string { return e.err.Error() }
func (e *httpStatusError) Unwrap() error { return e.err }

// isTransientFault reports whether a failed request is worth retrying; a 404, 403, 412 or other 4xx is a definite answer.
func isTransientFault(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrPreconditionFailed) || errors.Is(err, ErrCredentialsRequired) {
		return false
	}
	var se *httpStatusError
	if errors.As(err, &se) {
		// AWS answers a stalled upload with a 400 it documents as retryable.
		return se.code == 429 || (se.code >= 500 && se.code <= 599) || (se.code == 400 && strings.Contains(se.err.Error(), "RequestTimeout"))
	}
	// No status reached the caller, so this is a transport-level failure; the operation is idempotent, so retry.
	return true
}

func (c *Client) do(method, key string, query url.Values, body []byte, headers map[string]string) (*http.Response, error) {
	// Refuse an unsigned write here, so the caller reports the cause and not a 403.
	if c.anonymous && method != http.MethodGet && method != http.MethodHead {
		return nil, ErrCredentialsRequired
	}
	u, err := c.objectURL(key)
	if err != nil {
		return nil, err
	}
	if query != nil {
		u.RawQuery = query.Encode()
	}
	payloadHash := emptyPayloadSHA256
	if body != nil {
		payloadHash = hexSHA256(body)
	}
	debug := os.Getenv("GITSOCIAL_S3_DEBUG") == "1"
	// A transport failure surfaces from Do with no response; the body is in memory and every call here is idempotent, so the request is rebuilt and re-signed.
	var resp *http.Response
	for attempt := 1; ; attempt++ {
		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}
		req, err := http.NewRequest(method, u.String(), reader)
		if err != nil {
			return nil, err
		}
		req.ContentLength = int64(len(body))
		for name, value := range headers {
			req.Header.Set(name, value)
		}
		// Stamp every upload's cache policy at this one chokepoint; Cache-Control is not signed, so it does not affect SigV4.
		if method == http.MethodPut && req.Header.Get("Cache-Control") == "" {
			req.Header.Set("Cache-Control", cacheControlForKey(key))
		}
		if !c.anonymous {
			signRequest(req, c.cfg.AccessKey, c.cfg.SecretKey, c.cfg.Region, "s3", payloadHash, time.Now())
		}
		if debug {
			fmt.Fprintf(os.Stderr, "objstore> %s %s\n", method, u.String())
			for name, values := range req.Header {
				if name == "Authorization" {
					values = []string{"<redacted>"}
				}
				fmt.Fprintf(os.Stderr, "objstore>   %s: %s\n", name, strings.Join(values, ", "))
			}
		}
		resp, err = c.http.Do(req)
		if debug && resp != nil {
			fmt.Fprintf(os.Stderr, "objstore< %d request-id=%s\n", resp.StatusCode, resp.Header.Get("x-amz-request-id"))
		}
		if err == nil {
			break
		}
		if attempt == 3 {
			return nil, fmt.Errorf("objstore: %s %s: %w", method, key, err)
		}
		if debug {
			fmt.Fprintf(os.Stderr, "objstore! transport error, retrying (%d/3): %v\n", attempt, err)
		}
		time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	// A no-listing bucket spells absent as 403. A signed reader folds only a denial, and a denied write stays a hard error.
	if resp.StatusCode == http.StatusForbidden && (method == http.MethodGet || method == http.MethodHead) {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		resp.Body.Close()
		if c.anonymous || method == http.MethodHead || strings.Contains(string(respBody), "AccessDenied") {
			return nil, fmt.Errorf("%w: %s", ErrAccessDenied, key)
		}
		return nil, &httpStatusError{code: resp.StatusCode, err: fmt.Errorf("objstore: %s %s: HTTP 403: %s", method, key, strings.TrimSpace(string(respBody)))}
	}
	// 412 is a failed If-Match or If-None-Match and 409 is AWS's conditional-write conflict; both mean re-read and retry.
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

// GetRange downloads one byte range of an object, end exclusive; a server that ignores Range answers 200, and the body is sliced locally.
func (c *Client) GetRange(key string, start, end int64) ([]byte, error) {
	headers := map[string]string{"Range": fmt.Sprintf("bytes=%d-%d", start, end-1)}
	resp, err := c.do(http.MethodGet, key, nil, nil, headers)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("objstore: read %s: %w", key, err)
	}
	if resp.StatusCode == http.StatusPartialContent {
		return data, nil
	}
	if start >= int64(len(data)) {
		return nil, fmt.Errorf("objstore: range %d-%d of %s: object is %d bytes", start, end, key, len(data))
	}
	if end > int64(len(data)) {
		end = int64(len(data))
	}
	return data[start:end], nil
}

// GetRangeRetry is GetRange with transient-fault retry (see withReadRetry).
func (c *Client) GetRangeRetry(key string, start, end int64) ([]byte, error) {
	return withReadRetry(context.TODO(), func() ([]byte, error) { return c.GetRange(key, start, end) })
}

// retryBackoff paces read and PUT retries; its length plus one is the attempt count. A var so tests can shrink the waits.
var retryBackoff = []time.Duration{500 * time.Millisecond, 2 * time.Second}

// withRetry runs an idempotent request and retries a transient fault with bounded backoff; a PUT of the same key and bytes is idempotent, so a retry lands or fails for good.
func withRetry(fn func() error) error {
	var err error
	for attempt := 0; ; attempt++ {
		if err = fn(); err == nil || attempt >= len(retryBackoff) || !isTransientFault(err) {
			return err
		}
		time.Sleep(retryBackoff[attempt])
	}
}

// withReadRetry is withRetry for a read, with ctx to abort the wait when a pooled peer has already failed.
func withReadRetry[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	var result T
	var err error
	for attempt := 0; ; attempt++ {
		if result, err = fn(); err == nil || attempt >= len(retryBackoff) || !isTransientFault(err) {
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

// GetRetry is Get with transient-fault retry, the read path a long operation depends on.
func (c *Client) GetRetry(key string) ([]byte, error) {
	return withReadRetry(context.TODO(), func() ([]byte, error) { return c.Get(key) })
}

// GetWithETag downloads an object and returns its ETag for a later If-Match write.
func (c *Client) GetWithETag(key string) ([]byte, string, error) {
	var data []byte
	var etag string
	err := withRetry(func() error {
		resp, err := c.do(http.MethodGet, key, nil, nil, nil)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if data, err = io.ReadAll(resp.Body); err != nil {
			return fmt.Errorf("objstore: read %s: %w", key, err)
		}
		// A CDN that compresses on the fly marks its ETag weak, which If-Match cannot match; the value inside is still the object's.
		etag = strings.TrimPrefix(resp.Header.Get("ETag"), "W/")
		return nil
	})
	return data, etag, err
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

// PutIfMatch writes an object only when its current ETag matches, and returns ErrPreconditionFailed when it changed underneath.
func (c *Client) PutIfMatch(key string, data []byte, etag string) error {
	return withRetry(func() error {
		return c.putIfMatchOnce(key, data, etag)
	})
}

// putIfMatchOnce is one PutIfMatch attempt.
func (c *Client) putIfMatchOnce(key string, data []byte, etag string) error {
	resp, err := c.do(http.MethodPut, key, nil, data, map[string]string{"If-Match": etag})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// PutIfAbsent writes an object only when the key does not exist yet, and returns ErrPreconditionFailed when it does.
func (c *Client) PutIfAbsent(key string, data []byte) error {
	return withRetry(func() error {
		resp, err := c.do(http.MethodPut, key, nil, data, map[string]string{"If-None-Match": "*"})
		if err != nil {
			return err
		}
		resp.Body.Close()
		return nil
	})
}

// Delete removes an object; deleting a missing key is not an error, since DELETE is idempotent.
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

// ListWithETags returns every key under a prefix with its ETag; the ETag comes free in the listing, so a change check needs no per-key GET.
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
