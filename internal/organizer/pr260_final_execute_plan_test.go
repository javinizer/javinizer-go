package organizer

import (
	"github.com/spf13/afero"
	"strings"
	"testing"
)

func TestFinalExecutePlanRejectsNilPlan(t *testing.T) {
	org, _ := dupBatchFixture(t)
	result, err := org.ExecuteOrganizePlan(nil, true, LinkModeNone)
	if result != nil || err == nil || err.Error() != "organization plan is nil" {
		t.Fatalf("nil plan: result=%#v err=%v", result, err)
	}
}

func TestFinalExecutePlanRejectsUnauthorizedConflictWithoutPublishing(t *testing.T) {
	org, fs := dupBatchFixture(t)
	plan := &OrganizePlan{SourcePath: "/in/B.mkv", TargetDir: "/dest/ABC-123", TargetFile: "ABC-123.mkv", TargetPath: "/dest/ABC-123/ABC-123.mkv", WillMove: true, Conflicts: []PlanConflict{{Path: "/dest/ABC-123/ABC-123.mkv", Kind: ConflictDuplicate}}}
	result, err := org.ExecuteOrganizePlan(plan, false, LinkModeNone)
	if result != nil || err == nil || !strings.Contains(err.Error(), "organization validation failed") {
		t.Fatalf("unauthorized conflict: result=%#v err=%v", result, err)
	}
	if plan.LinkMode != LinkModeNone {
		t.Fatalf("link mode changed: %v", plan.LinkMode)
	}
	if exists, e := afero.Exists(fs, plan.TargetPath); e != nil || exists {
		t.Fatalf("conflicting destination published: exists=%v err=%v", exists, e)
	}
}
