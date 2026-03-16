// generate_test.go - Fixture generation (run with -generate flag)
package test

import (
	"flag"
	"testing"
)

var generateFlag = flag.Bool("generate", false, "generate fixture tarball and metadata")

func TestGenerateFixture(t *testing.T) {
	if !*generateFlag {
		t.Skip("skipping fixture generation — run with -generate to create testdata/fixture-repo.tar.gz")
	}
	generateFixture()
}
