// compression.go - brotli and JSON object encoding for bucket documents.
package objstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/andybalholm/brotli"
)

// brotliCompress encodes bytes at the given quality.
func brotliCompress(data []byte, quality int) ([]byte, error) {
	var buf bytes.Buffer
	w := brotli.NewWriterOptions(&buf, brotli.WriterOptions{Quality: quality})
	if _, err := w.Write(data); err != nil {
		return nil, fmt.Errorf("brotli write: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("brotli close: %w", err)
	}
	return buf.Bytes(), nil
}

// brotliDecompress decodes brotli bytes.
func brotliDecompress(data []byte) ([]byte, error) {
	return io.ReadAll(brotli.NewReader(bytes.NewReader(data)))
}

// compressJSON marshals a document and brotli-compresses it at the given quality.
func compressJSON(v any, quality int) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	compressed, err := brotliCompress(data, quality)
	if err != nil {
		return nil, fmt.Errorf("compress: %w", err)
	}
	return compressed, nil
}

// putCompressed uploads pre-compressed bytes with `Content-Encoding: br` object
// metadata so readers decode transparently.
func putCompressed(client *Client, key string, compressed []byte) error {
	return withRetry(func() error {
		resp, err := client.do(http.MethodPut, key, nil, compressed, map[string]string{
			"Content-Type":     "application/json",
			"Content-Encoding": "br",
		})
		if err != nil {
			return fmt.Errorf("upload %s: %w", key, err)
		}
		resp.Body.Close()
		return nil
	})
}

// readCompressedJSON fetches and brotli-decodes a document into v. found is
// false (no error) when the key is absent or not valid JSON — so callers fall
// back to a fresh walk. Bodies that fail the brotli decode are parsed as-is:
// some providers (Cloudflare R2) transparently decompress `Content-Encoding:
// br` objects when the requester doesn't advertise br support (Go's transport
// only advertises gzip), so the stored artifact arrives as plain JSON.
// Treating that as absent silently re-bootstrapped every corpus on every push
// and permanently blocked the HTML page layer behind siteItemsBootstrapPending.
func readCompressedJSON(client *Client, key string, v any) (found bool, err error) {
	data, err := client.GetRetry(key)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", key, err)
	}
	raw, err := brotliDecompress(data)
	if err != nil || !json.Valid(raw) {
		raw = data
	}
	if json.Unmarshal(raw, v) != nil {
		return false, nil
	}
	return true, nil
}
