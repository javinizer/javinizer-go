package worker

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

func TestReviewManualNameEditProtectsAgainstImportOverwrite(t *testing.T) {
	for _, thumb := range []bool{false, true} {
		t.Run(map[bool]string{false: "names", true: "names_and_thumb"}[thumb], func(t *testing.T) {
			db := newActressEditTestDB(t)
			repos := db.Repositories()
			a := models.Actress{DMMID: 81236, FirstName: "Imported"}
			require.NoError(t, repos.ActressRepo.ImportUpsert(t.Context(), &a))
			jq := NewJobStore(nil, nil, repos.MovieRepo, "", nil, nil, WithActressRepo(repos.ActressRepo))
			job := jq.CreateJobBatch([]string{"file1.mp4"})
			job.results.UpdateFileResult("file1.mp4", &resultstore.MovieResult{FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"}, Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "ABC-001", Actresses: []models.Actress{a}}})
			ej, ok := jq.GetJobForEdit(job.ID.String())
			require.True(t, ok)
			edited := a
			edited.FirstName = "Manual"
			if thumb {
				edited.ThumbURL = "https://example.test/manual.jpg"
				edited.ThumbEdited = true
			}
			require.NoError(t, ej.UpdateMovie(t.Context(), "file1.mp4", &models.Movie{ID: "ABC-001", Actresses: []models.Actress{edited}}))
			manual, err := repos.ActressRepo.FindByID(t.Context(), a.ID)
			require.NoError(t, err)
			t.Logf("review edit name=%q origin=%q", manual.FirstName, manual.Origin)
			incoming := models.Actress{DMMID: a.DMMID, FirstName: "Reimported"}
			result, err := repos.ActressRepo.ImportUpsertWithOptions(t.Context(), &incoming, database.ActressImportOptions{OverwriteNames: true})
			require.NoError(t, err)
			after, err := repos.ActressRepo.FindByID(t.Context(), a.ID)
			require.NoError(t, err)
			t.Logf("import result=%s name=%q", result.Outcome, after.FirstName)
			require.Equal(t, "Manual", after.FirstName)
		})
	}
}
