// provider.go - Conditional-write capability levels and their mapping from
// recognized provider endpoint hosts
package objstore

// Capability declares which conditional writes a provider enforces.
type Capability int

const (
	// CapabilityUnknown means no preset knowledge — probed on first push.
	CapabilityUnknown Capability = iota
	// CapabilityFull enforces If-Match update CAS and If-None-Match: * create CAS.
	CapabilityFull
	// CapabilityCreateOnly enforces If-None-Match: * create CAS but rejects
	// If-Match overwrites (Ceph RGW behavior: 412 even on a matching ETag).
	CapabilityCreateOnly
)

// hostCapability maps a recognized provider (from protocol.S3HostInfo) to its
// conditional-write capability; unknown providers are probed on first push.
func hostCapability(provider string) Capability {
	switch provider {
	case "aws", "r2":
		return CapabilityFull
	case "do":
		return CapabilityCreateOnly
	}
	return CapabilityUnknown
}
