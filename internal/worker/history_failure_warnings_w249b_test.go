package worker

// PR #249 codex follow-up (F4) — the worker history consumer must fold a
// FAILED lane's OrganizeResult.Warnings into the failure row's metadata:
// the organizer now binds the force-overwrite displacement crumb at the
// destruction (F2), so the failed organize result carries warnings whose
// evidence must survive into the FAILED history row, not just into success
// rows.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditOrganizeFailure_FailedResultWarningsRideHistoryMetadata(t *testing.T) {
	db := newHistoryTestDB(t)
	repos := db.Repositories()
	inputs := applyPhaseInputs{JobID: "w249b-job", HistoryRepo: repos.HistoryRepo, OperationMode: "organize"}
	movie := &models.Movie{ID: "W249B-001"}
	crumb := "overwrite authorized: replaced existing destination /dest/lib/W249B-001.mkv"
	result := &workflow.ApplyResult{
		OrganizeResult: &organizer.OrganizeResult{
			NewPath:  "/dest/lib/W249B-001.mkv",
			Warnings: []string{crumb},
		},
	}

	auditOrganizeFailure(inputs, movie, "/in/w249b.mkv", result, assert.AnError, ApplyPhaseConfig{})

	records, err := repos.HistoryRepo.FindByMovieID(context.Background(), "W249B-001")
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, models.HistoryStatusFailed, records[0].Status)
	var meta map[string]any
	require.NoError(t, json.Unmarshal([]byte(records[0].Metadata), &meta))
	warns, ok := meta["warnings"].([]any)
	require.True(t, ok, "the FAILED history row's metadata folds the displacement crumb")
	require.Len(t, warns, 1)
	assert.Equal(t, crumb, warns[0].(string))
}
