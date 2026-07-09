// site_config.go - push-maintained static-site PM board config artifact.
//
// The repo's PM config lives at refs/gitmsg/pm/config as a commit whose message
// is the PMConfig JSON (see extensions/pm/board.go). The static reader cannot
// resolve a board from a raw config the way the TUI does, so this resolves the
// configured board (framework columns or a custom board) at push time and emits
// it as .gitsocial/site/pm-config.json alongside the other mutable site
// artifacts. The reader loads it (no-cache, refreshed on every push) and drives
// its board columns from it, falling back to the built-in kanban default when
// the artifact is absent.

package objstore

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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
// its ordered columns (each with a WIP limit, 0 = none). It mirrors what the TUI
// derives from PMConfig via ResolveBoardConfig, so the static board matches.
type siteResolvedBoard struct {
	Name            string         `json:"name"`
	Columns         []sitePMColumn `json:"columns"`
	DefaultSwimlane string         `json:"defaultSwimlane,omitempty"`
}

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
// framework's columns, else the built-in kanban default.
func resolveSiteBoard(cfg sitePMConfig) siteResolvedBoard {
	if len(cfg.Boards) > 0 {
		b := cfg.Boards[0]
		name := b.Name
		if name == "" {
			name = "Board"
		}
		return siteResolvedBoard{Name: name, Columns: b.Columns, DefaultSwimlane: b.DefaultSwimlane}
	}
	if cols, ok := siteFrameworkBoards[cfg.Framework]; ok {
		return siteResolvedBoard{Name: "Board", Columns: cols}
	}
	return siteResolvedBoard{Name: "Board", Columns: siteFrameworkBoards["kanban"]}
}

// readSitePMConfig resolves refs/gitmsg/pm/config from the bucket's objects and
// parses its PMConfig JSON. Returns ok=false (no error) when the ref is absent,
// the object is missing/not a commit, or the message is not valid config JSON —
// the caller then omits the artifact (reader falls back to the kanban default).
func readSitePMConfig(client *Client, prefix string, refs map[string]string) (sitePMConfig, bool, error) {
	sha, present := refs["refs/gitmsg/pm/config"]
	if !present || len(sha) != 40 {
		return sitePMConfig{}, false, nil
	}
	c, err := getBucketCommit(client, prefix, sha)
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
// deletes the artifact (reader falls back to the kanban default). Best-effort by
// contract; written on the same refs-moved path that maintains refs.json.
func writeSitePMConfig(client *Client, prefix string, refs map[string]string) error {
	cfg, ok, err := readSitePMConfig(client, prefix, refs)
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
	resp, err := client.do(http.MethodPut, prefix+siteConfigKey, nil, data, map[string]string{"Content-Type": "application/json"})
	if err != nil {
		return fmt.Errorf("upload %s: %w", siteConfigKey, err)
	}
	resp.Body.Close()
	return nil
}
