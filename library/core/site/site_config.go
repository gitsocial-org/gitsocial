// site_config.go - push-maintained static-site PM board config artifact.
//
// The repo's PM config lives at refs/gitmsg/pm/config as a commit whose message
// is the PMConfig JSON (see extensions/pm/board.go). The static reader cannot
// resolve a board from a raw config the way the TUI does, so this resolves the
// configured board (framework columns or a custom board) at push time and emits
// it as .gitsocial/site/pm-config.json alongside the other mutable site
// artifacts. The reader loads it (no-cache, refreshed on every push) and drives
// its board columns from it, falling back to the built-in minimal default when
// the artifact is absent.

package site

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gitsocial-org/gitsocial/library/core/objstore"
)

// siteConfigKey is the resolved PM board config the static site reads.
const siteConfigKey = ".gitsocial/site/pm-config.json"

// sitePMConfig is the raw PM config as stored at refs/gitmsg/pm/config (mirrors
// pm.PMConfig; only the fields the site's board needs are kept).
type sitePMConfig struct {
	Framework string        `json:"framework,omitempty"`
	Boards    []sitePMBoard `json:"boards,omitempty"`
}

// sitePMBoard mirrors pm.BoardConfig (the fields the site board needs).
type sitePMBoard struct {
	ID              string         `json:"id,omitempty"`
	Name            string         `json:"name,omitempty"`
	Columns         []sitePMColumn `json:"columns,omitempty"`
	DefaultSwimlane string         `json:"defaultSwimlane,omitempty"`
}

// sitePMColumn mirrors pm.ColumnConfig.
type sitePMColumn struct {
	Name   string `json:"name"`
	Filter string `json:"filter"`
	WIP    *int   `json:"wip,omitempty"`
}

// siteResolvedBoard is the resolved board the reader consumes: a board name and
// its ordered columns (each with a WIP limit, 0 = none), and whether the
// framework has milestones and sprints. It mirrors what the TUI derives from
// PMConfig via ResolveBoardConfig and FrameworkFeatures, so the static site matches.
type siteResolvedBoard struct {
	Name            string         `json:"name"`
	Columns         []sitePMColumn `json:"columns"`
	DefaultSwimlane string         `json:"defaultSwimlane,omitempty"`
	Milestones      bool           `json:"milestones"`
	Sprints         bool           `json:"sprints"`
}

// siteFrameworkFeatures maps a framework name to whether it has milestones and sprints, mirroring extensions/pm/framework.go.
var siteFrameworkFeatures = map[string][2]bool{"minimal": {false, false}, "kanban": {true, false}, "scrum": {true, true}}

// siteFrameworkBoards maps a framework name to its board columns, mirroring
// extensions/pm/framework.go (minimal/kanban/scrum). Kept in sync by hand: a new
// framework there needs a matching entry here for the static board to honor it.
var siteFrameworkBoards = map[string][]sitePMColumn{
	"minimal": {
		{Name: "Open", Filter: "state:open"},
		{Name: "Closed", Filter: "state:closed"},
	},
	"kanban": {
		{Name: "Backlog", Filter: "state:open"},
		{Name: "In Progress", Filter: "status:in-progress", WIP: intPtrSite(3)},
		{Name: "Review", Filter: "status:review", WIP: intPtrSite(3)},
		{Name: "Done", Filter: "state:closed"},
	},
	"scrum": {
		{Name: "Backlog", Filter: "state:open"},
		{Name: "Sprint", Filter: "status:sprint-backlog"},
		{Name: "In Progress", Filter: "status:in-progress"},
		{Name: "Review", Filter: "status:review"},
		{Name: "Done", Filter: "state:closed"},
	},
}

// intPtrSite returns a pointer to an int (WIP limits are optional).
func intPtrSite(i int) *int { return &i }

// resolveSiteBoard derives the board columns the site shows from a PMConfig,
// mirroring pm.ResolveBoardConfig: a custom board (first one) wins, else the
// framework's columns, else the built-in minimal default.
func resolveSiteBoard(cfg sitePMConfig) siteResolvedBoard {
	features := siteFrameworkFeatures[cfg.Framework]
	board := siteResolvedBoard{Name: "Board", Columns: siteFrameworkBoards["minimal"], Milestones: features[0], Sprints: features[1]}
	if len(cfg.Boards) > 0 {
		b := cfg.Boards[0]
		if b.Name != "" {
			board.Name = b.Name
		}
		board.Columns, board.DefaultSwimlane = b.Columns, b.DefaultSwimlane
		return board
	}
	if cols, ok := siteFrameworkBoards[cfg.Framework]; ok {
		board.Columns = cols
	}
	return board
}

// readSitePMConfig resolves refs/gitmsg/pm/config and parses its PMConfig
// JSON, reading the commit from the local odb when src is available (nil =
// bucket-only) and otherwise from the bucket — loose key or, on a sealed
// bucket, the pack map. Returns ok=false (no error) when the ref is absent,
// the object is missing/not a commit, or the message is not valid config JSON —
// the caller then omits the artifact (reader falls back to the minimal default).
func readSitePMConfig(client *objstore.Client, prefix string, refs map[string]string, src *objstore.LocalCommitSource) (sitePMConfig, bool, error) {
	sha, present := refs["refs/gitmsg/pm/config"]
	if !present || len(sha) != 40 {
		return sitePMConfig{}, false, nil
	}
	c, err := getCommit(src, client, prefix, sha)
	if err != nil {
		return sitePMConfig{}, false, err
	}
	var cfg sitePMConfig
	if json.Unmarshal([]byte(strings.TrimSpace(c.item.Message)), &cfg) != nil {
		return sitePMConfig{}, false, nil
	}
	return cfg, true, nil
}

// writeSitePMConfig publishes the resolved PM board at .gitsocial/site/pm-config.json
// after every push, so the static board honors the repo's config. Absent config
// deletes the artifact (reader falls back to the minimal default). Best-effort by
// contract; written on the same refs-moved path that maintains refs.json.
func writeSitePMConfig(client *objstore.Client, prefix string, refs map[string]string, src *objstore.LocalCommitSource) error {
	cfg, ok, err := readSitePMConfig(client, prefix, refs, src)
	if err != nil {
		return err
	}
	if !ok {
		return client.Delete(prefix + siteConfigKey)
	}
	board := resolveSiteBoard(cfg)
	data, err := json.Marshal(board)
	if err != nil {
		return fmt.Errorf("marshal site pm config: %w", err)
	}
	if err := client.PutWithHeaders(prefix+siteConfigKey, data, map[string]string{"Content-Type": "application/json"}); err != nil {
		return fmt.Errorf("upload %s: %w", siteConfigKey, err)
	}
	return nil
}
