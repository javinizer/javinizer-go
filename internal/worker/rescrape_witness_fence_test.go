package worker

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// audit F1: an outstanding promote/crop/rekey witness must fence the rescrape
// pipeline BEFORE poster generation (byte clobber) — the returned error types
// as an admission conflict for the caller.
func TestRescrapeFencedByWitnessPreGenerate(t *testing.T) {
	store := resultstore.New(1, []string{"f1.mp4"})
	seedFamilyResult(store, "f1.mp4", "res-1", "ABC-001", "")
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	dir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-ABC-001.json"), []byte("{}"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "ABC-001.jpg"), []byte("canon"), 0o644))
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{Movie: &models.Movie{ID: "ABC-001"}, Status: scrape.StatusCompleted}}
	inputs := rescrapePhaseInputs{
		WF:        wf,
		ResultMap: store,
		Finder:    store,
		JobID:     jobID,
		Fs:        fs,
		TempDir:   "/tmp",
	}
	phase := NewRescrapePhase()
	result, err := phase.Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "ABC-001", FilePath: "f1.mp4"})
	require.Error(t, err, "rescrape must refuse while the family's witness is unresolved")
	var cfe *EditAdmissionConflictError
	require.ErrorAs(t, err, &cfe)
	assert.Nil(t, result)
	got, rerr := afero.ReadFile(fs, filepath.Join(dir, "ABC-001.jpg"))
	require.NoError(t, rerr)
	assert.Equal(t, "canon", string(got), "fenced rescrape never touches canonical bytes")
}

// audit F1: the commit leg re-probes under the family key (the TOCTOU net
// for a witness created between the pre-generate probe and the commit).
func TestRescrapeCommitLegFencedByWitness(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "CAN-1", "")
	fs := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-RS")
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-CAN-1.json"), []byte("{}"), 0o644))
	wrapped := &familyKeyedResultMap{ResultMapAccessor: store, updater: store, registry: newKeyedMutexRegistry(), fs: fs, tempDir: "/tmp", jobID: "JOB-RS"}
	cur, err := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, err)
	incoming := &resultstore.MovieResult{ResultID: "res-1", Movie: &models.Movie{ID: "INC-1"}, FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "INC-1"}}
	err = wrapped.CommitResult("/f/a.mp4", incoming, cur.Revision)
	require.Error(t, err)
	var cfe *EditAdmissionConflictError
	require.ErrorAs(t, err, &cfe)
	after, err := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, err)
	assert.Equal(t, "CAN-1", after.Movie.ID, "fenced commit leaves the row untouched")
	assert.Equal(t, cur.Revision, after.Revision)
}

// audit F1: probe-loop housekeeping — identical stored/new IDs dedupe, empty
// incoming identity skips the probe, no witness anywhere ⇒ commit proceeds.
func TestRescrapeCommitLegDedupeAndEmptyIDs(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "CAN-1", "")
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/tmp/posters/JOB-RS", 0o755))
	wrapped := &familyKeyedResultMap{ResultMapAccessor: store, updater: store, registry: newKeyedMutexRegistry(), fs: fs, tempDir: "/tmp", jobID: "JOB-RS"}
	cur, err := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, err)
	same := &resultstore.MovieResult{ResultID: "res-1", Movie: &models.Movie{ID: "CAN-1", Title: "fresh"}, FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "CAN-1"}}
	require.NoError(t, wrapped.CommitResult("/f/a.mp4", same, cur.Revision), "same-ID commit with no witness succeeds (dup probe skipped)")

	cur2, err := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, err)
	noMovie := &resultstore.MovieResult{ResultID: "res-1", Movie: nil, FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "CAN-1"}}
	require.NoError(t, wrapped.CommitResult("/f/a.mp4", noMovie, cur2.Revision), "nil incoming movie skips the probe")
}

// audit F1: the err-path rollback must not destroy canonical bytes when the
// failure IS the witness fence.
func TestWithRescrapeStatusSkipsCleanupOnFenceError(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	dir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1.jpg"), []byte("canon"), 0o644))
	lc := rescrapeLifecycle{
		inputs: rescrapePhaseInputs{Fs: fs, TempDir: "/tmp", JobID: jobID},
		lookup: &resultstore.FileLookupResult{FilePath: "/f/a.mp4", OldMovieID: "PI-1"},
	}
	fenceErr := &EditAdmissionConflictError{Message: "poster PI-1 promote witness unresolved"}
	_, err := withRescrapeStatus(lc, func() (*RescrapeResult, *resultstore.MovieResult, error) {
		return nil, &resultstore.MovieResult{Movie: &models.Movie{ID: "PI-1"}}, fenceErr
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, fenceErr))
	_, statErr := fs.Stat(filepath.Join(dir, "PI-1.jpg"))
	assert.NoError(t, statErr, "fence rejection must not delete canonical bytes")
	assert.NoError(t, fs.Remove(filepath.Join(dir, "PI-1.jpg")))
}

// audit F1: CAS-conflict cleanup deletes ONLY bytes the losing rescrape
// itself generated — never the concurrent winner's canon.
func TestWithRescrapeStatusConflictCleanupGated(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	dir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	canon := filepath.Join(dir, "PI-1.jpg")
	lc := rescrapeLifecycle{
		inputs: rescrapePhaseInputs{Fs: fs, TempDir: "/tmp", JobID: jobID},
		lookup: &resultstore.FileLookupResult{FilePath: "/f/a.mp4", OldMovieID: "PI-1"},
	}

	require.NoError(t, afero.WriteFile(fs, canon, []byte("winner-bytes"), 0o644))
	_, err := withRescrapeStatus(lc, func() (*RescrapeResult, *resultstore.MovieResult, error) {
		return &RescrapeResult{Status: models.RescrapeStatusConflict}, &resultstore.MovieResult{Movie: &models.Movie{ID: "PI-1"}}, nil
	})
	require.NoError(t, err)
	_, statErr := fs.Stat(canon)
	assert.NoError(t, statErr, "no generation ⇒ winner's bytes stand")

	_, err = withRescrapeStatus(lc, func() (*RescrapeResult, *resultstore.MovieResult, error) {
		return &RescrapeResult{Status: models.RescrapeStatusConflict}, &resultstore.MovieResult{Movie: &models.Movie{ID: "PI-1"}, OrchestrationState: models.OrchestrationState{PosterGenerated: true}}, nil
	})
	require.NoError(t, err)
	_, statErr = fs.Stat(canon)
	assert.Error(t, statErr, "self-generated bytes deleted on conflict")
}
