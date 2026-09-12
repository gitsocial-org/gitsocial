// compression.go - brotli and JSON object encoding for bucket documents.
package objstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/andybalholm/brotli"
)

const (
	// BrotliQualityShard compresses a sealed document once, so max quality is worth the one-time wall time.
	BrotliQualityShard = 11
	// BrotliQualityFull compresses every rewritten document.
	BrotliQualityFull = 9
)

// BrotliCompress encodes bytes at the given quality.
func BrotliCompress(data []byte, quality int) ([]byte, error) {
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

// BrotliDecompress decodes brotli bytes.
func BrotliDecompress(data []byte) ([]byte, error) {
	return io.ReadAll(brotli.NewReader(bytes.NewReader(data)))
}

// CompressJSON marshals a document and brotli-compresses it at the given quality.
func CompressJSON(v any, quality int) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	compressed, err := BrotliCompress(data, quality)
	if err != nil {
		return nil, fmt.Errorf("compress: %w", err)
	}
	return compressed, nil
}

// PutCompressed uploads pre-compressed bytes with `Content-Encoding: br` object
// metadata so readers decode transparently. An empty cacheControl takes the
// key's default class; a writer that seals its key passes CacheControlImmutable.
func PutCompressed(client *Client, key string, compressed []byte, cacheControl string) error {
	headers := map[string]string{"Content-Type": "application/json", "Content-Encoding": "br"}
	if cacheControl != "" {
		headers["Cache-Control"] = cacheControl
	}
	if err := client.PutWithHeadersRetry(key, compressed, headers); err != nil {
		return fmt.Errorf("upload %s: %w", key, err)
	}
	return nil
}

// ReadCompressedJSON fetches and brotli-decodes a document into v. found is
// false (no error) when the key is absent or not valid JSON — so callers fall
// back to a fresh walk. Bodies that fail the brotli decode are parsed as-is:
// some providers (Cloudflare R2) transparently decompress `Content-Encoding:
// br` objects when the requester doesn't advertise br support (Go's transport
// only advertises gzip), so the stored artifact arrives as plain JSON.
// Treating that as absent re-bootstraps every corpus on every push.
func ReadCompressedJSON(client *Client, key string, v any) (found bool, err error) {
	data, err := client.GetRetry(key)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", key, err)
	}
	raw, err := BrotliDecompress(data)
	if err != nil || !json.Valid(raw) {
		raw = data
	}
	if json.Unmarshal(raw, v) != nil {
		return false, nil
	}
	return true, nil
}
