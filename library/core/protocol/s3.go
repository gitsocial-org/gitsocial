// s3.go - Canonical s3:// repo URL rules shared by identity normalization
// (NormalizeURL) and the objstore transport. An s3 repo URL is always
// "s3://<endpoint-host>/<bucket>/<prefix>": the host locates the provider
// unambiguously, and identity comparison is string equality. The only other
// accepted spelling is a known provider's virtual-host form, which folds into
// the same canonical URL.
package protocol

import (
	"fmt"
	"net/url"
	"strings"
)

// awsConsoleHost is the base host of AWS S3 web-console URLs; a pasted console
// URL is translated to a canonical s3:// remote at the CLI boundary only.
const awsConsoleHost = "console.aws.amazon.com"

// ResolveS3URL translates a user-supplied remote URL into a canonical s3://
// identity for use at the CLI boundary (remote add, clone). It accepts the
// canonical form, a known provider's virtual-host form, an http(s) endpoint or
// virtual-host URL for a recognized S3 host (region/account ride in the host,
// so they are carried verbatim), and a pasted AWS S3 console URL
// (https://<region>.console.aws.amazon.com/s3/buckets/<bucket>). A resolved URL
// must name a bucket. isS3 reports whether the input named an s3 remote at all,
// so a caller can fall back to treating it as a plain git remote; err is
// non-nil only when the input looked like an s3/console URL but could not be
// resolved.
func ResolveS3URL(input string) (canonical string, isS3 bool, err error) {
	u, perr := url.Parse(strings.TrimSpace(input))
	if perr != nil {
		return "", false, nil
	}
	host := strings.ToLower(u.Host)
	if host == awsConsoleHost || strings.HasSuffix(host, "."+awsConsoleHost) {
		c, ok := awsConsoleToCanonical(u, host)
		if !ok {
			return "", true, fmt.Errorf("could not read region and bucket from AWS console URL %q (expected https://<region>.console.aws.amazon.com/s3/buckets/<bucket>)", input)
		}
		return c, true, nil
	}
	if strings.EqualFold(u.Scheme, "s3") {
		c := canonicalS3URL(input)
		if c == "" {
			return "", true, fmt.Errorf("invalid s3 URL %q (expected s3://<endpoint-host>/<bucket>/<prefix>)", input)
		}
		if !s3URLNamesBucket(c) {
			return "", true, fmt.Errorf("missing bucket in %q (expected s3://<endpoint-host>/<bucket>/<prefix>: append the bucket name to the endpoint)", input)
		}
		return c, true, nil
	}
	// http(s) endpoint or virtual-host URL for a recognized S3 host. Gated on
	// S3HostInfo so an ordinary website (github.com, ...) passes through as a
	// plain git remote; a self-hosted S3 endpoint still needs the s3:// scheme.
	if strings.EqualFold(u.Scheme, "https") || strings.EqualFold(u.Scheme, "http") {
		if c, ok := recognizedEndpointToCanonical(host, u.Path); ok {
			if !s3URLNamesBucket(c) {
				return "", true, fmt.Errorf("missing bucket in %q (expected s3://<endpoint-host>/<bucket>/<prefix>: append the bucket name to the endpoint)", input)
			}
			return c, true, nil
		}
	}
	return "", false, nil
}

// s3URLNamesBucket reports whether a canonical s3 URL carries a bucket, i.e. has
// a path segment after the endpoint host (canonical form is "s3://<host>" with
// an optional "/<bucket>/<prefix>").
func s3URLNamesBucket(canonical string) bool {
	return strings.Contains(strings.TrimPrefix(canonical, "s3://"), "/")
}

// recognizedEndpointToCanonical folds an http(s) URL into canonical form when
// its host is a known S3 endpoint (path-style) or a known provider's
// virtual-host (<bucket>.<endpoint-host>). ok=false for unrecognized hosts.
// The path-style check runs first, so an R2 jurisdiction endpoint
// (<account>.eu.r2…) resolves as an endpoint before the virtual-host split can
// mistake its account label for a bucket.
func recognizedEndpointToCanonical(host, rawPath string) (string, bool) {
	trail := strings.Trim(rawPath, "/")
	if _, _, known := S3HostInfo(host); known {
		return joinS3(host, trail), true
	}
	if bucket, remainder, ok := strings.Cut(host, "."); ok {
		if _, _, known := S3HostInfo(remainder); known {
			return joinS3(remainder, joinS3Path(bucket, trail)), true
		}
	}
	return "", false
}

// awsConsoleToCanonical builds a canonical aws s3 URL from a parsed console URL:
// region from the subdomain (or ?region= for the global console host), bucket
// from the path segment after "buckets", and an optional prefix from ?prefix=.
func awsConsoleToCanonical(u *url.URL, host string) (string, bool) {
	region := ""
	if sub := strings.TrimSuffix(host, "."+awsConsoleHost); sub != host && sub != "" && sub != "s3" {
		region = sub
	}
	if region == "" {
		region = u.Query().Get("region")
	}
	bucket := ""
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i, s := range segs {
		if s == "buckets" && i+1 < len(segs) {
			bucket = segs[i+1]
			break
		}
	}
	if region == "" || strings.Contains(region, ".") || bucket == "" {
		return "", false
	}
	prefix := strings.Trim(u.Query().Get("prefix"), "/")
	return joinS3("s3."+region+".amazonaws.com", joinS3Path(bucket, prefix)), true
}

// S3HostInfo reverse-maps a canonical endpoint host to its provider and
// region; ok=false for hosts no preset recognizes (custom endpoints).
func S3HostInfo(host string) (provider, region string, ok bool) {
	switch {
	case strings.HasPrefix(host, "s3.") && strings.HasSuffix(host, ".amazonaws.com"):
		region = strings.TrimSuffix(strings.TrimPrefix(host, "s3."), ".amazonaws.com")
		if region == "" || strings.Contains(region, ".") {
			return "", "", false
		}
		return "aws", region, true
	case strings.HasSuffix(host, ".digitaloceanspaces.com"):
		region = strings.TrimSuffix(host, ".digitaloceanspaces.com")
		if region == "" || strings.Contains(region, ".") {
			return "", "", false // dotted remainder = virtual-host form, not a canonical host
		}
		return "do", region, true
	case strings.HasSuffix(host, ".r2.cloudflarestorage.com"):
		// Endpoint host is "<account>" or "<account>.<jurisdiction>". The account
		// is a 32-hex id, which distinguishes a jurisdiction endpoint
		// (<account>.eu.r2…) from a virtual-host bucket label (<bucket>.<account>.r2…);
		// without it the two are ambiguous and buckets get mis-parsed as accounts.
		account, jurisdiction, hasJur := strings.Cut(strings.TrimSuffix(host, ".r2.cloudflarestorage.com"), ".")
		if !isR2AccountID(account) {
			return "", "", false
		}
		if hasJur && jurisdiction != "eu" && jurisdiction != "fedramp" {
			return "", "", false
		}
		return "r2", "auto", true
	}
	return "", "", false
}

// isR2AccountID reports whether s is a Cloudflare account id (32 lowercase hex).
func isR2AccountID(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// canonicalS3URL normalizes an s3 URL to its canonical identity, stripping
// query and trailing slash and folding a known provider's virtual-host
// spelling (<bucket>.<endpoint-host>) into path form. Authorities that don't
// name an endpoint host (bare bucket names) are invalid and normalize to "".
func canonicalS3URL(rawURL string) string {
	u, err := url.Parse(rawURL)
	// A dot or a port marks a real endpoint host; a bare bucket name has neither
	// (bucket names can't contain ":"), so localhost:8000 passes and s3://bucket/repo doesn't.
	if err != nil || (!strings.Contains(u.Host, ".") && !strings.Contains(u.Host, ":")) {
		return ""
	}
	authority := strings.ToLower(u.Host)
	trail := strings.Trim(u.Path, "/")
	if bucket, remainder, ok := strings.Cut(authority, "."); ok {
		if _, _, known := S3HostInfo(remainder); known {
			return joinS3(remainder, joinS3Path(bucket, trail))
		}
	}
	return joinS3(authority, trail)
}

// joinS3 renders an s3 URL from an authority and an optional path remainder.
func joinS3(authority, trail string) string {
	if trail == "" {
		return "s3://" + authority
	}
	return "s3://" + authority + "/" + trail
}

// joinS3Path joins two path fragments, tolerating an empty second fragment.
func joinS3Path(first, rest string) string {
	if rest == "" {
		return first
	}
	return first + "/" + rest
}
