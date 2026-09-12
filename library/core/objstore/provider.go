// provider.go - Conditional-write capability levels and their mapping from
// recognized provider endpoint hosts
package objstore

// writeCapability declares which conditional writes a provider enforces.
type writeCapability int

const (
	// capabilityUnknown means no preset knowledge, probed on first push.
	capabilityUnknown writeCapability = iota
	// capabilityFull enforces If-Match update CAS and If-None-Match: * create CAS.
	capabilityFull
	// capabilityCreateOnly enforces If-None-Match: * create CAS but rejects
	// If-Match overwrites (Ceph RGW behavior: 412 even on a matching ETag).
	capabilityCreateOnly
)

// hostCapability maps a recognized provider (from protocol.S3HostInfo) to its
// conditional-write capability; unknown providers are probed on first push.
func hostCapability(provider string) writeCapability {
	switch provider {
	case "aws", "r2":
		return capabilityFull
	case "do":
		return capabilityCreateOnly
	}
	return capabilityUnknown
}
