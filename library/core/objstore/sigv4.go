// sigv4.go - AWS Signature Version 4 request signing (stdlib only)
package objstore

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// emptyPayloadSHA256 is the SHA-256 of an empty body, used for GET/HEAD/DELETE.
const emptyPayloadSHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// signRequest signs req in place with AWS SigV4 for the given service scope.
// payloadSHA256 must be the hex SHA-256 of the request body (use
// emptyPayloadSHA256 for bodyless requests).
func signRequest(req *http.Request, accessKey, secretKey, region, service, payloadSHA256 string, now time.Time) {
	amzDate := now.UTC().Format("20060102T150405Z")
	dateStamp := now.UTC().Format("20060102")
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadSHA256)
	if req.Header.Get("Host") == "" {
		req.Header.Set("Host", req.Host)
	}

	canonicalURI := canonicalURIEncode(req.URL.EscapedPath())
	canonicalQuery := canonicalQueryString(req.URL.Query())
	signedHeaders, canonicalHeaders := canonicalHeaderLines(req)

	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI,
		canonicalQuery,
		canonicalHeaders,
		signedHeaders,
		payloadSHA256,
	}, "\n")

	scope := strings.Join([]string{dateStamp, region, service, "aws4_request"}, "/")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		hexSHA256([]byte(canonicalRequest)),
	}, "\n")

	signingKey := hmacSHA256(hmacSHA256(hmacSHA256(hmacSHA256(
		[]byte("AWS4"+secretKey), dateStamp), region), service), "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))

	req.Header.Set("Authorization",
		"AWS4-HMAC-SHA256 Credential="+accessKey+"/"+scope+
			", SignedHeaders="+signedHeaders+
			", Signature="+signature)
}

// canonicalURIEncode normalizes an already-escaped path to SigV4's URI
// encoding rules (S3 style: no path normalization, each segment URI-encoded
// with '/' preserved).
func canonicalURIEncode(escapedPath string) string {
	if escapedPath == "" {
		return "/"
	}
	return escapedPath
}

// canonicalQueryString sorts and encodes query parameters per SigV4.
func canonicalQueryString(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		vals := append([]string(nil), q[k]...)
		sort.Strings(vals)
		for _, v := range vals {
			parts = append(parts, uriEncode(k)+"="+uriEncode(v))
		}
	}
	return strings.Join(parts, "&")
}

// uriEncode percent-encodes per SigV4 (RFC 3986 unreserved characters only).
func uriEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		default:
			b.WriteString("%" + strings.ToUpper(hex.EncodeToString([]byte{c})))
		}
	}
	return b.String()
}

// canonicalHeaderLines returns the signed-headers list and the canonical
// header block. Signs host plus every x-amz-* header present.
func canonicalHeaderLines(req *http.Request) (signedHeaders, canonicalHeaders string) {
	names := []string{"host"}
	for name := range req.Header {
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, "x-amz-") {
			names = append(names, lower)
		}
	}
	sort.Strings(names)
	lines := make([]string, 0, len(names))
	for _, name := range names {
		value := req.Header.Get(name)
		if name == "host" {
			value = req.Host
		}
		lines = append(lines, name+":"+strings.TrimSpace(value))
	}
	return strings.Join(names, ";"), strings.Join(lines, "\n") + "\n"
}

func hexSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}
