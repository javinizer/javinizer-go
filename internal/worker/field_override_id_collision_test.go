package worker

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestApplyFieldOverride_IDRekeyCollisionRejectsBeforeAnyMutation pins Codex
// P2 (batch_job_interface.go): an "id" override whose destination ID is
// ALREADY used by another result must be REJECTED under the held
// (origin, destination) lock pair BEFORE the asset move — otherwise
// MovePosterAssets would replace the destination family's cache with the
// origin's assets while that family keeps its own poster URL/crop state, and
// a later crop of either movie would fan bounds measured on one family out
// over both (Organize cropping each from the wrong source).
func TestApplyFieldOverride_IDRekeyCollisionRejectsBeforeAnyMutation(t *testing.T) {
	const (
		jobID = "job-idcollision"
		oldID = "AAA-ORIG"
		newID = "BBB-HELD"
	)
	oldPath := "/source/" + oldID + ".mp4"
	otherPath := "/source/" + newID + ".mp4"

	tracker := resultstore.New(2, []string{oldPath, otherPath})
	tracker.UpdateFileResult(oldPath, &resultstore.MovieResult{
		ResultID:      "res-collide",
		FileMatchInfo: models.FileMatchInfo{Path: oldPath, MovieID: oldID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: oldID, Title: "Origin", Poster: models.PosterState{
			PosterURL: "https://old.invalid/poster.jpg",
		}},
	})
	tracker.UpdateFileResult(otherPath, &resultstore.MovieResult{
		ResultID:      "res-other",
		FileMatchInfo: models.FileMatchInfo{Path: otherPath, MovieID: newID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: newID, Title: "Other", Poster: models.PosterState{
			PosterURL:        "https://other.invalid/poster.jpg",
			CroppedPosterURL: "/api/v1/temp/posters/" + jobID + "/" + newID + ".jpg?v=other",
		}},
	})
	tracker.SetProvenance(oldPath, &resultstore.ProvenanceData{
		FieldSources:   map[string]string{"id": "r18dev"},
		ScraperResults: []*models.ScraperResult{{Source: "dmm", ID: newID}, {Source: "r18dev", ID: oldID}},
	})

	gen := &moverStubGen{}
	je := &jobEditorImpl{store: tracker, jobID: jobID, posterGen: gen}
	persistRan := false
	je.persistEnvelope = func() error { persistRan = true; return nil }

	_, _, err := je.ApplyFieldOverride(context.Background(), "res-collide", "id", "dmm")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already uses that movie ID",
		"the collision rejection names the in-use destination ID")
	assert.NotContains(t, err.Error(), "not found",
		"the handler maps any 'not found' message to 404 — a validation rejection must stay 400")

	// REJECTED BEFORE ANY ASSET MOVE OR STATE MUTATION.
	assert.Empty(t, gen.calls, "MovePosterAssets must never start on a colliding re-key")
	assert.Equal(t, 0, gen.restores, "nothing ran, so nothing needs restoring")
	assert.False(t, persistRan, "the envelope persist must not run for a rejected override")

	// Both movies' job state is untouched.
	assert.Equal(t, oldID, tracker.GetCurrentMovieID(oldPath))
	assert.Equal(t, newID, tracker.GetCurrentMovieID(otherPath))
	origin, getErr := tracker.GetMovieResult(oldPath)
	require.NoError(t, getErr)
	assert.Equal(t, oldID, origin.Movie.ID)
	assert.Equal(t, "Origin", origin.Movie.Title)
	other, getErr := tracker.GetMovieResult(otherPath)
	require.NoError(t, getErr)
	assert.Equal(t, newID, other.Movie.ID)
	assert.Equal(t, "/api/v1/temp/posters/"+jobID+"/"+newID+".jpg?v=other",
		other.Movie.Poster.CroppedPosterURL,
		"the pre-existing B result keeps its own preview state — its cache was never displaced")

	assertPosterSourceLockFree(t, jobID, oldID)
	assertPosterSourceLockFree(t, jobID, newID)
}

// TestApplyFieldOverride_IDRekeySameFamilyDestinationIsNotCollision pins the
// exclusion half of the Codex P2 check: a path indexed at the destination key
// that belongs to the SAME movie family (here a multipart sibling whose
// FileMatchInfo.MovieID already equals the destination ID) is the normal
// fan-out case, not a collision — the override migrates the whole family.
func TestApplyFieldOverride_IDRekeySameFamilyDestinationIsNotCollision(t *testing.T) {
	const (
		jobID = "job-idfamily"
		oldID = "CCC-ORIG"
		newID = "DDD-NEW"
	)
	part1 := "/source/CCC-ORIG-cd1.mp4"
	part2 := "/source/CCC-ORIG-cd2.mp4"

	tracker := resultstore.New(2, []string{part1, part2})
	tracker.UpdateFileResult(part1, &resultstore.MovieResult{
		ResultID:      "res-part1",
		FileMatchInfo: models.FileMatchInfo{Path: part1, MovieID: oldID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: oldID, Title: "Family", OriginalFileName: "cd1"},
	})
	tracker.UpdateFileResult(part2, &resultstore.MovieResult{
		ResultID:      "res-part2",
		FileMatchInfo: models.FileMatchInfo{Path: part2, MovieID: newID}, // same family, already indexed at the destination key
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: oldID, Title: "Family", OriginalFileName: "cd2"},
	})
	tracker.SetProvenance(part1, &resultstore.ProvenanceData{
		FieldSources:   map[string]string{"id": "r18dev"},
		ScraperResults: []*models.ScraperResult{{Source: "dmm", ID: newID}, {Source: "r18dev", ID: oldID}},
	})

	gen := &moverStubGen{}
	je := &jobEditorImpl{store: tracker, jobID: jobID, posterGen: gen}

	updated, _, err := je.ApplyFieldOverride(context.Background(), "res-part1", "id", "dmm")
	require.NoError(t, err, "a same-family path at the destination key is the fan-out case, not a collision")
	require.NotNil(t, updated)
	assert.Equal(t, newID, updated.Movie.ID)

	// The migration DID run and the whole family re-keyed.
	assert.Equal(t, [][2]string{{oldID, newID}}, gen.calls)
	assert.Equal(t, newID, tracker.GetCurrentMovieID(part1))
	assert.Equal(t, newID, tracker.GetCurrentMovieID(part2))
	sibling, getErr := tracker.GetMovieResult(part2)
	require.NoError(t, getErr)
	assert.Equal(t, newID, sibling.Movie.ID)
	assert.Equal(t, "cd2", sibling.Movie.OriginalFileName,
		"the sibling keeps its per-part identity through the fan-out")

	assertPosterSourceLockFree(t, jobID, oldID)
	assertPosterSourceLockFree(t, jobID, newID)
}
