// types.go - Release extension data types
package release

import (
	"time"

	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/core/result"
)

type Result[T any] = result.Result[T]

type Author struct {
	Name  string
	Email string
}

type Release struct {
	ID          string
	Repository  string
	Branch      string
	Author      Author
	Timestamp   time.Time
	Subject     string
	Body        string
	Version     string
	Tag         string
	Prerelease  bool
	Artifacts   []string
	ArtifactURL string
	Checksums   string
	SignedBy    string
	SBOM        string
	IsEdited    bool
	IsRetracted bool
	IsUnpushed  bool
	Comments    int
	Origin      *protocol.Origin
}

type ArtifactResult struct {
	Version string
	Files   []ArtifactInfo
}

type ArtifactInfo struct {
	Filename string
	Size     int64
	SHA256   string
}
