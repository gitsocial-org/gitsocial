// refs_test.go - Generation-key format/parse and ref resolution tests
package objstore

import "testing"

func TestGenKeyRoundTrip(t *testing.T) {
	key := genKey("repo/", "refs/heads/main", 42)
	if key != "repo/refs/heads/main/.gen/0000000042" {
		t.Fatalf("genKey = %q", key)
	}
	refName, gen, isGen, err := parseGenKey("refs/heads/main/.gen/0000000042")
	if err != nil || !isGen || refName != "refs/heads/main" || gen != 42 {
		t.Errorf("parseGenKey = (%q, %d, %v, %v)", refName, gen, isGen, err)
	}
	refName, _, isGen, err = parseGenKey("refs/heads/main")
	if err != nil || isGen || refName != "refs/heads/main" {
		t.Errorf("parseGenKey plain = (%q, %v, %v), want the key back unchanged", refName, isGen, err)
	}
}

func TestParseGenKey(t *testing.T) {
	cases := []struct {
		key     string
		isGen   bool
		wantErr bool
	}{
		{key: "refs/heads/main", isGen: false},
		{key: "refs/gitmsg/core/forks/deadbeef", isGen: false},
		{key: "refs/heads/main/.gen/0000000001", isGen: true},
		{key: "refs/heads/gitmsg/social/.gen/9999999999", isGen: true},
		{key: "refs/heads/main/.gen/1", wantErr: true},           // not zero-padded to width
		{key: "refs/heads/main/.gen/00000000001", wantErr: true}, // too wide
		{key: "refs/heads/main/.gen/00000000ab", wantErr: true},  // non-decimal
		{key: "refs/heads/main/.gen/-000000001", wantErr: true},  // signed
		{key: "refs/heads/main/.gen/0000000001x", wantErr: true}, // trailing junk
	}
	for _, c := range cases {
		_, _, isGen, err := parseGenKey(c.key)
		if (err != nil) != c.wantErr {
			t.Errorf("parseGenKey(%q) err = %v, wantErr %v", c.key, err, c.wantErr)
			continue
		}
		if err == nil && isGen != c.isGen {
			t.Errorf("parseGenKey(%q) isGen = %v, want %v", c.key, isGen, c.isGen)
		}
	}
}
