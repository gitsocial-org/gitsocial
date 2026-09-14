// error_codes_test.go - Error codes the pm issue and milestone write paths return at the Result boundary
package pm

import (
	"database/sql"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
)

// TestCloseIssue_retracted asserts RETRACTED when the issue was retracted before the close ran.
func TestCloseIssue_retracted(t *testing.T) {
	workdir := initWorkspace(t)

	created := CreateIssue(workdir, "Crash on startup", "", CreateIssueOptions{})
	if !created.Success {
		t.Fatalf("CreateIssue: %s", created.Error.Message)
	}
	if res := RetractIssue(workdir, created.Data.ID); !res.Success {
		t.Fatalf("RetractIssue: %s", res.Error.Message)
	}

	res := CloseIssue(workdir, created.Data.ID)
	if res.Success || res.Error.Code != "RETRACTED" {
		t.Errorf("CloseIssue() on a retracted issue = %+v, want RETRACTED", res)
	}
}

// TestCreateMilestone_duplicate asserts DUPLICATE on a second milestone with the same title.
func TestCreateMilestone_duplicate(t *testing.T) {
	workdir := initWorkspace(t)

	if res := CreateMilestone(workdir, "v1.0", "", CreateMilestoneOptions{}); !res.Success {
		t.Fatalf("CreateMilestone: %s", res.Error.Message)
	}
	res := CreateMilestone(workdir, "v1.0", "again", CreateMilestoneOptions{})
	if res.Success || res.Error.Code != "DUPLICATE" {
		t.Errorf("CreateMilestone() with a taken title = %+v, want DUPLICATE", res)
	}

	allowed := CreateMilestone(workdir, "v1.0", "again", CreateMilestoneOptions{AllowDuplicate: true})
	if !allowed.Success {
		t.Errorf("CreateMilestone() with AllowDuplicate = %+v, want success", allowed)
	}
}

// TestUpdateIssue_linksFailed asserts LINKS_FAILED when the issue's declared links cannot be read.
func TestUpdateIssue_linksFailed(t *testing.T) {
	workdir := initWorkspace(t)

	blocked := CreateIssue(workdir, "Ship the parser", "", CreateIssueOptions{})
	if !blocked.Success {
		t.Fatalf("CreateIssue: %s", blocked.Error.Message)
	}
	created := CreateIssue(workdir, "Crash on startup", "", CreateIssueOptions{Blocks: []string{blocked.Data.ID}})
	if !created.Success {
		t.Fatalf("CreateIssue: %s", created.Error.Message)
	}

	// Dropping the link table leaves the item readable and its links unreadable.
	if err := cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`DROP TABLE pm_links`)
		return err
	}); err != nil {
		t.Fatalf("drop pm_links: %v", err)
	}

	subject := "Crash on empty config"
	res := UpdateIssue(workdir, created.Data.ID, UpdateIssueOptions{Subject: &subject})
	if res.Success || res.Error.Code != "LINKS_FAILED" {
		t.Errorf("UpdateIssue() without the link table = %+v, want LINKS_FAILED", res)
	}
}
