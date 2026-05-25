// types.go - Memo extension data types
package memo

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/protocol"
	"github.com/gitsocial-org/gitsocial/library/core/result"
)

// Result is an alias for result.Result so callers can use memo.Result[T].
type Result[T any] = result.Result[T]

// Author identifies the writer of a memo.
type Author struct {
	Name  string
	Email string
}

// Memo is the public, hydrated form returned from the extension API.
//
// `Repository` and `ID` may carry an absolute `local:/<home>/...` path for
// memos on session/personal tiers. The struct keeps the absolute form so
// cache lookups round-trip; MarshalJSON tilde-collapses both fields on
// serialization so JSON exports don't leak the OS username.
type Memo struct {
	ID          string
	Repository  string
	Branch      string
	Tier        Tier
	Author      Author
	Timestamp   time.Time
	Subject     string
	Body        string
	Labels      []string
	IsEdited    bool
	IsRetracted bool
	IsVirtual   bool
	IsStale     bool
	Origin      *protocol.Origin
}

// MarshalJSON tilde-collapses Repository and the URL portion of ID so JSON
// exports don't leak the absolute home path.
func (m Memo) MarshalJSON() ([]byte, error) {
	type alias Memo
	view := alias(m)
	view.Repository = CollapseLocalURL(view.Repository)
	view.ID = collapseRefURL(view.ID)
	return json.Marshal(view)
}

// collapseRefURL tilde-collapses the repo portion of a ref string like
// `local:/<home>/foo#commit:abc` → `local:~/foo#commit:abc`. Refs without a
// repo prefix or with non-local URLs are returned unchanged.
func collapseRefURL(ref string) string {
	idx := strings.Index(ref, "#")
	if idx <= 0 {
		return ref
	}
	return CollapseLocalURL(ref[:idx]) + ref[idx:]
}
