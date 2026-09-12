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
	"strconv"
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
	cfg          Config
	http         *http.Client
	anonymous    bool            // no credentials: unsigned, read-only
	retryBackoff []time.Duration // waits between retry attempts; its length plus one is the attempt count
}

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
		return nil, errCredentialsRequired
	}
	return &Client{
		cfg:          cfg,
		http:         &http.Client{Timeout: 60 * time.Second, Transport: newTransport()},
		anonymous:    cfg.AccessKey == "",
		retryBackoff: defaultRetryBackoff(),
	}, nil
}

// defaultRetryBackoff paces the retry in do; a function, so no package-level slice can be mutated.
func defaultRetryBackoff() []time.Duration {
	return []time.Duration{500 * time.Millisecond, 2 * time.Second}
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

// The sentinels callers branch on; every other non-2xx status surfaces as an httpStatusError.
var (
	ErrNotFound           = fmt.Errorf("objstore: not found")
	errPreconditionFailed = fmt.Errorf("objstore: precondition failed")
	// errAccessDenied is a 403 on a read; it wraps ErrNotFound, since a bucket that denies listing answers 403 for absent keys too.
	errAccessDenied = fmt.Errorf("%w (access denied)", ErrNotFound)
	// errCredentialsRequired replaces the 403 an unsigned write would earn.
	errCredentialsRequired = fmt.Errorf("objstore: credentials required (`gitsocial config credentials set <remote>`, GITSOCIAL_S3_ACCESS_KEY / GITSOCIAL_S3_SECRET_KEY, or AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY)")
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
	if errors.Is(err, ErrNotFound) || errors.Is(err, errPreconditionFailed) || errors.Is(err, errCredentialsRequired) {
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

// do runs one request and returns its body with the response headers. Every bucket read and write retries a transient fault here, so no caller wraps a loop of its own.
func (c *Client) do(ctx context.Context, method, key string, query url.Values, body []byte, headers map[string]string) ([]byte, http.Header, error) {
	var respHeaders http.Header
	data, err := withReadRetry(ctx, c.retryBackoff, func() ([]byte, error) {
		data, attemptHeaders, err := c.doOnce(method, key, query, body, headers)
		respHeaders = attemptHeaders
		return data, err
	})
	return data, respHeaders, err
}

// doOnce signs and executes one request attempt, reading the whole body of a 2xx.
func (c *Client) doOnce(method, key string, query url.Values, body []byte, headers map[string]string) ([]byte, http.Header, error) {
	// Refuse an unsigned write here, so the caller reports the cause and not a 403.
	if c.anonymous && method != http.MethodGet && method != http.MethodHead {
		return nil, nil, errCredentialsRequired
	}
	u, err := c.objectURL(key)
	if err != nil {
		return nil, nil, err
	}
	if query != nil {
		u.RawQuery = query.Encode()
	}
	payloadHash := emptyPayloadSHA256
	if body != nil {
		payloadHash = hexSHA256(body)
	}
	debug := os.Getenv("GITSOCIAL_S3_DEBUG") == "1"
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, u.String(), reader)
	if err != nil {
		return nil, nil, err
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
	resp, err := c.http.Do(req)
	if err != nil {
		// No status reached us, so the retry treats it as transient; every call here is idempotent.
		return nil, nil, fmt.Errorf("objstore: %s %s: %w", method, key, err)
	}
	defer resp.Body.Close()
	if debug {
		fmt.Fprintf(os.Stderr, "objstore< %d request-id=%s\n", resp.StatusCode, resp.Header.Get("x-amz-request-id"))
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, nil, c.statusError(method, key, resp.StatusCode, snippet)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("objstore: read %s: %w", key, err)
	}
	return data, resp.Header, nil
}

// statusError maps a non-2xx status to the sentinel its callers branch on.
func (c *Client) statusError(method, key string, code int, snippet []byte) error {
	body := strings.TrimSpace(string(snippet))
	switch {
	case code == http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrNotFound, key)
	case code == http.StatusForbidden && (method == http.MethodGet || method == http.MethodHead):
		// A no-listing bucket spells absent as 403, and a HEAD carries no body to tell a denial from an absence.
		if c.anonymous || method == http.MethodHead || strings.Contains(body, "AccessDenied") {
			return fmt.Errorf("%w: %s", errAccessDenied, key)
		}
		return &httpStatusError{code: code, err: fmt.Errorf("objstore: %s %s: HTTP 403: %s", method, key, body)}
	case code == http.StatusPreconditionFailed || code == http.StatusConflict:
		// 412 is a failed If-Match or If-None-Match and 409 is AWS's conditional-write conflict; both mean re-read and retry.
		return fmt.Errorf("%w: %s (HTTP %d: %s)", errPreconditionFailed, key, code, body)
	}
	return &httpStatusError{code: code, err: fmt.Errorf("objstore: %s %s: HTTP %d: %s", method, key, code, body)}
}

// withReadRetry runs one request and retries a transient fault with bounded backoff; ctx aborts the wait when a pooled peer has already failed.
func withReadRetry[T any](ctx context.Context, backoff []time.Duration, fn func() (T, error)) (T, error) {
	var result T
	var err error
	for attempt := 0; ; attempt++ {
		if result, err = fn(); err == nil || attempt >= len(backoff) || !isTransientFault(err) {
			return result, err
		}
		timer := time.NewTimer(backoff[attempt])
		select {
		case <-ctx.Done():
			timer.Stop()
			return result, err
		case <-timer.C:
		}
	}
}

// Get downloads an object's full content.
func (c *Client) Get(key string) ([]byte, error) {
	return c.getContext(context.Background(), key)
}

// getContext is Get with a context that aborts the retry wait, for the bounded read pools.
func (c *Client) getContext(ctx context.Context, key string) ([]byte, error) {
	data, _, err := c.do(ctx, http.MethodGet, key, nil, nil, nil)
	return data, err
}

// getRange downloads one byte range of an object, end exclusive; a server that ignores Range sends the whole body, which is sliced locally.
func (c *Client) getRange(key string, start, end int64) ([]byte, error) {
	headers := map[string]string{"Range": fmt.Sprintf("bytes=%d-%d", start, end-1)}
	data, respHeaders, err := c.do(context.Background(), http.MethodGet, key, nil, nil, headers)
	if err != nil {
		return nil, err
	}
	if respHeaders.Get("Content-Range") != "" {
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

// getWithETag downloads an object and returns its ETag for a later If-Match write.
func (c *Client) getWithETag(key string) ([]byte, string, error) {
	data, respHeaders, err := c.do(context.Background(), http.MethodGet, key, nil, nil, nil)
	if err != nil {
		return nil, "", err
	}
	// A CDN that compresses on the fly marks its ETag weak, which If-Match cannot match; the value inside is still the object's.
	return data, strings.TrimPrefix(respHeaders.Get("ETag"), "W/"), nil
}

// Put uploads an object (unconditional write).
func (c *Client) Put(key string, data []byte) error {
	return c.putContext(context.Background(), key, data)
}

// putContext is Put with a context that aborts the retry wait, for the bounded upload pool.
func (c *Client) putContext(ctx context.Context, key string, data []byte) error {
	_, _, err := c.do(ctx, http.MethodPut, key, nil, data, nil)
	return err
}

// PutWithHeaders uploads an object with the caller's headers; an unset Cache-Control takes the key's default class.
func (c *Client) PutWithHeaders(key string, data []byte, headers map[string]string) error {
	_, _, err := c.do(context.Background(), http.MethodPut, key, nil, data, headers)
	return err
}

// HeadObject returns a key's stored size and ETag, without its body; size is 0 when the response carries no length.
func (c *Client) HeadObject(key string) (size int, etag string, err error) {
	_, respHeaders, err := c.do(context.Background(), http.MethodHead, key, nil, nil, nil)
	if err != nil {
		return 0, "", err
	}
	etag = respHeaders.Get("ETag")
	if n, convErr := strconv.Atoi(respHeaders.Get("Content-Length")); convErr == nil && n > 0 {
		size = n
	}
	return size, etag, nil
}

// putIfMatch writes an object only when its current ETag matches, and returns errPreconditionFailed when it changed underneath.
func (c *Client) putIfMatch(key string, data []byte, etag string) error {
	_, _, err := c.do(context.Background(), http.MethodPut, key, nil, data, map[string]string{"If-Match": etag})
	return err
}

// putIfAbsent writes an object only when the key does not exist yet, and returns errPreconditionFailed when it does.
func (c *Client) putIfAbsent(key string, data []byte) error {
	_, _, err := c.do(context.Background(), http.MethodPut, key, nil, data, map[string]string{"If-None-Match": "*"})
	return err
}

// Delete removes an object; deleting a missing key is not an error, since DELETE is idempotent.
func (c *Client) Delete(key string) error {
	_, _, err := c.do(context.Background(), http.MethodDelete, key, nil, nil, nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
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

// listedObject is one key and its ETag from a bucket listing.
type listedObject struct {
	Key  string
	ETag string
}

// List returns every key under the given prefix (ListObjectsV2, paginated).
func (c *Client) List(prefix string) ([]string, error) {
	objs, err := c.listWithETags(prefix)
	if err != nil {
		return nil, err
	}
	keys := make([]string, len(objs))
	for i, obj := range objs {
		keys[i] = obj.Key
	}
	return keys, nil
}

// listWithETags returns every key under a prefix with its ETag; the ETag comes free in the listing, so a change check needs no per-key GET.
func (c *Client) listWithETags(prefix string) ([]listedObject, error) {
	var objs []listedObject
	token := ""
	for {
		q := url.Values{}
		q.Set("list-type", "2")
		q.Set("prefix", prefix)
		if token != "" {
			q.Set("continuation-token", token)
		}
		data, _, err := c.do(context.Background(), http.MethodGet, "", q, nil, nil)
		if err != nil {
			return nil, err
		}
		var result listBucketResult
		if err := xml.Unmarshal(data, &result); err != nil {
			return nil, fmt.Errorf("objstore: decode list response: %w", err)
		}
		for _, obj := range result.Contents {
			objs = append(objs, listedObject{Key: obj.Key, ETag: obj.ETag})
		}
		if !result.IsTruncated {
			return objs, nil
		}
		// A short listing read as complete drops keys; a v1-marker provider lands here.
		if result.NextContinuationToken == "" {
			return nil, fmt.Errorf("objstore: list %s: the provider reported a truncated listing with no continuation token, so the remaining keys cannot be read", prefix)
		}
		token = result.NextContinuationToken
	}
}
