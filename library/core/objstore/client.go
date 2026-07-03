// client.go - Minimal stdlib S3 client
package objstore

import (
	"bytes"
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
	return &Client{cfg: cfg, http: &http.Client{Timeout: 60 * time.Second}}, nil
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
		return nil, fmt.Errorf("objstore: %s %s: HTTP %d: %s", method, key, resp.StatusCode, strings.TrimSpace(string(respBody)))
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
		Key string `xml:"Key"`
	} `xml:"Contents"`
	IsTruncated           bool   `xml:"IsTruncated"`
	NextContinuationToken string `xml:"NextContinuationToken"`
}

// List returns every key under the given prefix (ListObjectsV2, paginated).
func (c *Client) List(prefix string) ([]string, error) {
	var keys []string
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
			keys = append(keys, obj.Key)
		}
		if !result.IsTruncated || result.NextContinuationToken == "" {
			return keys, nil
		}
		token = result.NextContinuationToken
	}
}
