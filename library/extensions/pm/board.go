// board.go - Board configuration and kanban view logic
package pm

import (
	"encoding/json"
	"strings"

	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/result"
)

// PMConfig represents the full PM configuration from refs/gitmsg/pm/config.
type PMConfig struct {
	Version   string        `json:"version"`
	Branch    string        `json:"branch,omitempty"`
	Framework string        `json:"framework,omitempty"`
	Boards    []BoardConfig `json:"boards,omitempty"`
}

// BoardConfig represents a named board configuration.
type BoardConfig struct {
	ID              string         `json:"id,omitempty"`
	Name            string         `json:"name,omitempty"`
	Columns         []ColumnConfig `json:"columns,omitempty"`
	DefaultSwimlane string         `json:"defaultSwimlane,omitempty"`
}

// ColumnConfig represents a board column definition.
type ColumnConfig struct {
	Name   string `json:"name"`
	Filter string `json:"filter"`
	WIP    *int   `json:"wip,omitempty"`
}

// BoardView represents the full board with all columns.
type BoardView struct {
	ID      string
	Name    string
	Columns []BoardColumn
}

// DefaultPMConfig returns the default PM configuration.
func DefaultPMConfig() PMConfig {
	return PMConfig{
		Version:   "0.1.0",
		Branch:    "gitmsg/pm",
		Framework: "kanban",
	}
}

// DefaultBoardConfig returns the default board configuration.
func DefaultBoardConfig() BoardConfig {
	return BoardConfig{
		ID:   "default",
		Name: "Board",
		Columns: []ColumnConfig{
			{Name: "Backlog", Filter: "state:open"},
			{Name: "In Progress", Filter: "status:in-progress"},
			{Name: "Review", Filter: "status:review"},
			{Name: "Done", Filter: "state:closed"},
		},
	}
}

// GetPMConfig reads the full PM configuration from refs/gitmsg/pm/config.
func GetPMConfig(workdir string) PMConfig {
	msg, err := git.GetCommitMessage(workdir, "refs/gitmsg/pm/config")
	if err != nil || msg == "" {
		return DefaultPMConfig()
	}
	var config PMConfig
	if err := json.Unmarshal([]byte(strings.TrimSpace(msg)), &config); err != nil {
		return DefaultPMConfig()
	}
	if config.Version == "" {
		config.Version = "0.1.0"
	}
	if config.Branch == "" {
		config.Branch = "gitmsg/pm"
	}
	return config
}

// GetBoardConfig reads the board configuration, deriving from framework if set.
func GetBoardConfig(workdir string, boardID string) BoardConfig {
	config := GetPMConfig(workdir)
	return ResolveBoardConfig(config, boardID)
}

// ResolveBoardConfig derives board config from PMConfig, using framework defaults.
func ResolveBoardConfig(config PMConfig, boardID string) BoardConfig {
	// Check for custom board by ID
	if boardID != "" && len(config.Boards) > 0 {
		for _, b := range config.Boards {
			if b.ID == boardID {
				return b
			}
		}
	}
	// Use first custom board if available
	if len(config.Boards) > 0 {
		return config.Boards[0]
	}
	// Fall back to framework
	if config.Framework != "" {
		fw := GetFramework(config.Framework)
		if fw != nil {
			return frameworkToBoardConfig(fw)
		}
	}
	return DefaultBoardConfig()
}

// frameworkToBoardConfig converts framework board definition to BoardConfig.
func frameworkToBoardConfig(fw *Framework) BoardConfig {
	columns := make([]ColumnConfig, len(fw.Board.Columns))
	for i, col := range fw.Board.Columns {
		columns[i] = ColumnConfig(col)
	}
	return BoardConfig{
		ID:      fw.Name,
		Name:    fw.Name + " Board",
		Columns: columns,
	}
}

// GetBoardView builds the board view by grouping issues into columns.
func GetBoardView(workdir string) Result[BoardView] {
	return GetBoardViewByID(workdir, "")
}

// GetBoardViewByID builds the board view for a specific board ID.
func GetBoardViewByID(workdir string, boardID string) Result[BoardView] {
	pmConfig := GetPMConfig(workdir)
	boardConfig := ResolveBoardConfig(pmConfig, boardID)

	branch := gitmsg.GetExtBranch(workdir, "pm")
	repoURL := gitmsg.ResolveRepoURL(workdir)

	q := PMQuery{
		Types:   []string{string(ItemTypeIssue)},
		RepoURL: repoURL,
		Branch:  branch,
		Limit:   1000,
	}
	items, err := GetPMItems(q)
	if err != nil {
		return result.Err[BoardView]("QUERY_FAILED", err.Error())
	}

	// Build columns from board config
	columns := make([]BoardColumn, len(boardConfig.Columns))
	filters := make([]string, len(boardConfig.Columns))
	for i, col := range boardConfig.Columns {
		columns[i] = BoardColumn{
			Name:  col.Name,
			Label: col.Filter,
			WIP:   col.WIP,
		}
		filters[i] = col.Filter
	}

	// Group issues into columns
	for _, item := range items {
		issue := PMItemToIssue(item)
		matchedCol := matchIssueToColumn(issue, filters)
		if matchedCol >= 0 && matchedCol < len(columns) {
			columns[matchedCol].Issues = append(columns[matchedCol].Issues, issue)
		} else if len(columns) > 0 {
			// No match: put in first column
			columns[0].Issues = append(columns[0].Issues, issue)
		}
	}

	return result.Ok(BoardView{
		ID:      boardConfig.ID,
		Name:    boardConfig.Name,
		Columns: columns,
	})
}

// matchIssueToColumn returns the best matching column index, or -1.
// Prefers specific label matches (status:x) over broad state matches (state:open).
func matchIssueToColumn(issue Issue, filters []string) int {
	stateMatch := -1
	for i, filter := range filters {
		if matchFilter(issue, filter) {
			// Prefer label filters over state filters
			if !strings.HasPrefix(filter, "state:") {
				return i
			}
			// Remember first state match as fallback
			if stateMatch < 0 {
				stateMatch = i
			}
		}
	}
	return stateMatch
}

// matchFilter checks if an issue matches a simple filter expression.
// Supports: state:open, state:closed, scope:value, scope:v1,scope:v2 (OR)
func matchFilter(issue Issue, filter string) bool {
	// Handle comma-separated OR filters
	parts := strings.Split(filter, ",")
	for _, part := range parts {
		if matchSingleFilter(issue, strings.TrimSpace(part)) {
			return true
		}
	}
	return false
}

// matchSingleFilter matches a single filter like "state:open" or "status:backlog".
func matchSingleFilter(issue Issue, filter string) bool {
	idx := strings.Index(filter, ":")
	if idx < 0 {
		return false
	}
	key := filter[:idx]
	value := filter[idx+1:]

	if key == "state" {
		return string(issue.State) == value
	}
	// Label match
	for _, label := range issue.Labels {
		if label.Scope == key && label.Value == value {
			return true
		}
	}
	return false
}

// SavePMConfig saves the PM configuration to refs/gitmsg/pm/config.
func SavePMConfig(workdir string, config PMConfig) error {
	if config.Version == "" {
		config.Version = "0.1.0"
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	ref := "refs/gitmsg/pm/config"
	var parent string
	if existing, err := git.ReadRef(workdir, ref); err == nil {
		parent = existing
	}
	hash, err := git.CreateCommitTree(workdir, string(data), parent)
	if err != nil {
		return err
	}
	return git.WriteRef(workdir, ref, hash)
}

// SetBoardConfig saves or updates a board configuration.
func SetBoardConfig(workdir string, boardConfig BoardConfig) error {
	config := GetPMConfig(workdir)
	// Find and update existing board or append new one
	found := false
	for i, b := range config.Boards {
		if b.ID == boardConfig.ID {
			config.Boards[i] = boardConfig
			found = true
			break
		}
	}
	if !found {
		config.Boards = append(config.Boards, boardConfig)
	}
	return SavePMConfig(workdir, config)
}
