// provider_test.go - Provider capability mapping tests
package objstore

import "testing"

func TestHostCapability(t *testing.T) {
	cases := []struct {
		provider string
		want     writeCapability
	}{
		{"aws", capabilityFull},
		{"r2", capabilityFull},
		{"do", capabilityCreateOnly},
		{"", capabilityUnknown},
		{"minio", capabilityUnknown},
	}
	for _, c := range cases {
		if got := hostCapability(c.provider); got != c.want {
			t.Errorf("hostCapability(%q) = %v, want %v", c.provider, got, c.want)
		}
	}
}
