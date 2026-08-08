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
	_, err := withRescrapeStatus(lc, func(_ *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
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
	_, err := withRescrapeStatus(lc, func(_ *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		return &RescrapeResult{Status: models.RescrapeStatusConflict}, &resultstore.MovieResult{Movie: &models.Movie{ID: "PI-1"}}, nil
	})
	require.NoError(t, err)
	_, statErr := fs.Stat(canon)
	assert.NoError(t, statErr, "no generation ⇒ winner's bytes stand")

	// audit R1: even SELF-generated bytes survive when the pair pre-existed —
	// the success path that overwrote it cannot be undone by this op.
	_, err = withRescrapeStatus(lc, func(scope *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		scope.preExistedPair = true
		return &RescrapeResult{Status: models.RescrapeStatusConflict}, &resultstore.MovieResult{Movie: &models.Movie{ID: "PI-1"}, OrchestrationState: models.OrchestrationState{PosterGenerated: true}}, nil
	})
	require.NoError(t, err)
	_, statErr = fs.Stat(canon)
	assert.NoError(t, statErr, "pre-existing pair survives even when this op 'generated' over it")

	// audit R1a: generation ERROR (bytes may be absent/partial) never deletes.
	_, err = withRescrapeStatus(lc, func(_ *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		genErr := "download wedged"
		return &RescrapeResult{Status: models.RescrapeStatusConflict}, &resultstore.MovieResult{Movie: &models.Movie{ID: "PI-1"}, OrchestrationState: models.OrchestrationState{PosterGenerated: true, PosterError: &genErr}}, nil
	})
	require.NoError(t, err)
	_, statErr = fs.Stat(canon)
	assert.NoError(t, statErr, "failed generation ⇒ canon kept")

	// Legit self-created pair: fresh legs, successful generation ⇒ cleanup.
	_, err = withRescrapeStatus(lc, func(_ *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		return &RescrapeResult{Status: models.RescrapeStatusConflict}, &resultstore.MovieResult{Movie: &models.Movie{ID: "PI-1"}, OrchestrationState: models.OrchestrationState{PosterGenerated: true}}, nil
	})
	require.NoError(t, err)
	_, statErr = fs.Stat(canon)
	assert.Error(t, statErr, "self-created pair deleted on conflict")
}

// audit R1: on a REAL error (network/scrape) with provably self-created legs,
// the rollback removes only those legs.
func TestWithRescrapeStatusErrPathDeletesOnlyOwned(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	dir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	canon := filepath.Join(dir, "PI-1.jpg")
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "PI-1", "")
	lc := rescrapeLifecycle{
		inputs: rescrapePhaseInputs{Fs: fs, TempDir: "/tmp", JobID: jobID, ResultMap: store},
		lookup: &resultstore.FileLookupResult{FilePath: "/f/a.mp4", OldMovieID: "PI-1"},
	}
	require.NoError(t, afero.WriteFile(fs, canon, []byte("self"), 0o644))
	_, err := withRescrapeStatus(lc, func(_ *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		return nil, &resultstore.MovieResult{Movie: &models.Movie{ID: "PI-1"}, OrchestrationState: models.OrchestrationState{PosterGenerated: true}}, errors.New("network wedged")
	})
	require.Error(t, err)
	_, statErr := fs.Stat(canon)
	assert.Error(t, statErr, "owned self-created leg removed on error rollback")

	require.NoError(t, afero.WriteFile(fs, canon, []byte("keep"), 0o644))
	_, err = withRescrapeStatus(lc, func(scope *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		scope.preExistedPair = true
		return nil, &resultstore.MovieResult{Movie: &models.Movie{ID: "PI-1"}, OrchestrationState: models.OrchestrationState{PosterGenerated: true}}, errors.New("network wedged")
	})
	require.Error(t, err)
	_, statErr = fs.Stat(canon)
	assert.NoError(t, statErr, "pre-existing leg kept on error rollback")
}

// audit R1c: a foreign family's row already owns the movie ID => the
// ownership predicate refuses cleanup on that ID.
func TestWithRescrapeStatusConflictForeignOwnerKeepsBytes(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	store := resultstore.New(2, []string{"/f/a.mp4", "/f/b.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "PI-1", "")
	seedFamilyResult(store, "/f/b.mp4", "res-b", "PI-1", "") // sibling owns the ID too
	inputs := rescrapePhaseInputs{Fs: fs, TempDir: "/tmp", JobID: jobID, ResultMap: store}
	lookup := &resultstore.FileLookupResult{FilePath: "/f/a.mp4", OldMovieID: "PI-1"}
	mr := &resultstore.MovieResult{Movie: &models.Movie{ID: "PI-1"}, OrchestrationState: models.OrchestrationState{PosterGenerated: true}}
	assert.False(t, rescrapeOwnsPosterLegs(inputs, lookup, &rescrapeGenScope{}, mr, "PI-1"), "sibling-owned ID => not ours to delete")

	store2 := resultstore.New(2, []string{"/f/a.mp4", "/f/b.mp4"})
	seedFamilyResult(store2, "/f/a.mp4", "res-a", "PI-7", "")
	seedFamilyResult(store2, "/f/b.mp4", "res-b", "PI-8", "")
	inputs2 := rescrapePhaseInputs{Fs: fs, TempDir: "/tmp", JobID: jobID, ResultMap: store2}
	lookup2 := &resultstore.FileLookupResult{FilePath: "/f/a.mp4", OldMovieID: "PI-7"}
	mr2 := &resultstore.MovieResult{Movie: &models.Movie{ID: "PI-7"}, OrchestrationState: models.OrchestrationState{PosterGenerated: true}}
	assert.True(t, rescrapeOwnsPosterLegs(inputs2, lookup2, &rescrapeGenScope{}, mr2, "PI-7"), "self-created + sole owner => deletable")
	genErr := "wedged"
	mr2.PosterError = &genErr
	assert.False(t, rescrapeOwnsPosterLegs(inputs2, lookup2, &rescrapeGenScope{}, mr2, "PI-7"), "generation error => never delete")
	assert.False(t, rescrapeOwnsPosterLegs(inputs2, lookup2, &rescrapeGenScope{preExistedPair: true}, mr, "PI-7"), "pre-existing pair => never delete")
	assert.False(t, rescrapeOwnsPosterLegs(inputs2, lookup2, &rescrapeGenScope{}, mr, ""), "empty id => never")
	assert.False(t, rescrapeOwnsPosterLegs(inputs2, lookup2, nil, mr, "PI-7"), "nil scope => never deletes")
	assert.False(t, rescrapeOwnsPosterLegs(inputs2, lookup2, &rescrapeGenScope{}, nil, "PI-7"), "nil movie result => never deletes")
	assert.False(t, rescrapeOwnsPosterLegs(inputs2, lookup2, &rescrapeGenScope{}, &resultstore.MovieResult{}, "PI-7"), "not generated => never deletes")
}
