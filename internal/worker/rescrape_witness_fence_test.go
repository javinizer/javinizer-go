package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "ORIG-1", "")
	lc := rescrapeLifecycleShim(jobID, fs, store)

	// The failed rescrape generated bytes for an ID NO row references — those
	// are provably ours; rollback removes them.
	canon := filepath.Join(dir, "NEW-77.jpg")
	require.NoError(t, afero.WriteFile(fs, canon, []byte("ours"), 0o644))
	_, err := withRescrapeStatus(lc, func(_ *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		return nil, &resultstore.MovieResult{Movie: &models.Movie{ID: "NEW-77"}, OrchestrationState: models.OrchestrationState{PosterGenerated: true}}, errors.New("network wedged")
	})
	require.Error(t, err)
	_, statErr := fs.Stat(canon)
	assert.Error(t, statErr, "provably self-created legs removed on error rollback")

	// A failed refresh whose bytes were parked mid-flight restores the
	// committed pre-op bytes (F-R3-2a), never deletes them: row references
	// REF-1, canon pre-existed.
	refCanon := filepath.Join(dir, "REF-1.jpg")
	require.NoError(t, afero.WriteFile(fs, refCanon, []byte("orig-bytes"), 0o644))
	_, err = withRescrapeStatus(lc, func(scope *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		scope.preExistedPair = true
		scope.parked = parkCanonicalPosterPair(fs, dir, "REF-1")
		// simulate the failed generation writing new bytes over canonical;
		// production fingerprints whatever generation wrote (PosterGenerated
		// gates it, PosterError no longer suppresses it) — model the same.
		require.NoError(t, afero.WriteFile(fs, refCanon, []byte("loser-bytes"), 0o644))
		scope.genSHA = map[string]string{"REF-1.jpg": shaContentHex([]byte("loser-bytes"))}
		return nil, &resultstore.MovieResult{Movie: &models.Movie{ID: "REF-1"}, OrchestrationState: models.OrchestrationState{PosterGenerated: true}}, errors.New("cancelled mid-flight")
	})
	require.Error(t, err)
	got, rerr := afero.ReadFile(fs, refCanon)
	require.NoError(t, rerr)
	assert.Equal(t, "orig-bytes", string(got), "parked pre-op bytes restored on rollback")
}

// rescrapeLifecycleShim builds a lifecycle with the standard seams for the
// closeout tests.
func rescrapeLifecycleShim(jobID models.JobID, fs afero.Fs, store resultstore.Store) rescrapeLifecycle {
	return rescrapeLifecycle{
		inputs: rescrapePhaseInputs{Fs: fs, TempDir: "/tmp", JobID: jobID, ResultMap: store, EditLockFn: func(ids ...string) func() { return func() {} }},
		lookup: &resultstore.FileLookupResult{FilePath: "/f/a.mp4", OldMovieID: "ORIG-1"},
	}
}

// audit F-R3-3: orphan cleanup revalidates usage under the key — an ID that
// gained a row reference between commit and deletion is never swept.
func TestWithRescrapeStatusOrphanCleanupRevalidates(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	dir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	oldFull := filepath.Join(dir, "OLD-9-full.jpg")
	require.NoError(t, afero.WriteFile(fs, oldFull, []byte("x"), 0o644))

	emptyStore := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(emptyStore, "/f/a.mp4", "res-a", "NEW-9", "")
	lc := rescrapeLifecycle{
		inputs: rescrapePhaseInputs{Fs: fs, TempDir: "/tmp", JobID: jobID, ResultMap: emptyStore, EditLockFn: func(ids ...string) func() { return func() {} }},
		lookup: &resultstore.FileLookupResult{FilePath: "/f/a.mp4", OldMovieID: "OLD-9"},
	}
	outcome := &RescrapeResult{Status: models.RescrapeStatusSuccess, OrphanedMovieIDs: []string{"OLD-9"}}
	_, err := withRescrapeStatus(lc, func(_ *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		return outcome, &resultstore.MovieResult{Movie: &models.Movie{ID: "NEW-9"}}, nil
	})
	require.NoError(t, err)
	_, statErr := fs.Stat(oldFull)
	assert.Error(t, statErr, "genuinely orphaned leg removed")

	// Rewind: OLD-9 gained an owner before the sweep — must survive.
	require.NoError(t, afero.WriteFile(fs, oldFull, []byte("x"), 0o644))
	store2 := resultstore.New(2, []string{"/f/a.mp4", "/f/b.mp4"})
	seedFamilyResult(store2, "/f/a.mp4", "res-a", "NEW-9", "")
	seedFamilyResult(store2, "/f/b.mp4", "res-b", "OLD-9", "") // moved in post-commit
	lc2 := rescrapeLifecycleShimFor(store2, jobID, fs, "/f/a.mp4", "NEW-9")
	mr2 := &resultstore.MovieResult{Movie: &models.Movie{ID: "NEW-9"}}
	_, err = withRescrapeStatus(lc2, func(_ *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		return &RescrapeResult{Status: models.RescrapeStatusSuccess, OrphanedMovieIDs: []string{"OLD-9"}}, mr2, nil
	})
	require.NoError(t, err)
	_, statErr = fs.Stat(oldFull)
	assert.NoError(t, statErr, "row-referenced 'orphan' never swept")
}

func rescrapeLifecycleShimFor(store resultstore.Store, jobID models.JobID, fs afero.Fs, filePath, oldID string) rescrapeLifecycle {
	return rescrapeLifecycle{
		inputs: rescrapePhaseInputs{Fs: fs, TempDir: "/tmp", JobID: jobID, ResultMap: store, EditLockFn: func(ids ...string) func() { return func() {} }},
		lookup: &resultstore.FileLookupResult{FilePath: filePath, OldMovieID: oldID},
	}
}

// audit F-R5-1: the conflict closeout rewinds a canonical leg ONLY while it
// provably still holds THIS rescrape's generated bytes — a concurrent
// winner's committed bytes survive.
func TestWithRescrapeStatusConflictRestoreContentVerified(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	dir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "ORIG-1", "")
	lc := rescrapeLifecycleShim(jobID, fs, store)
	canon := filepath.Join(dir, "CV-1.jpg")
	require.NoError(t, afero.WriteFile(fs, canon, []byte("pre-op-C0"), 0o644))
	parked := parkCanonicalPosterPair(fs, dir, "CV-1")
	require.True(t, parked.hadCrop)
	require.NoError(t, afero.WriteFile(fs, canon, []byte("ours-gen"), 0o644))
	oursSHA := shaContentHex([]byte("ours-gen"))

	// Case A: canon still ours → restore rewinds to C0
	_, err := withRescrapeStatus(lc, func(scope *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		scope.parked = parked
		scope.genSHA = map[string]string{"CV-1.jpg": oursSHA}
		scope.preExistedPair = true
		return &RescrapeResult{Status: models.RescrapeStatusConflict}, &resultstore.MovieResult{Movie: &models.Movie{ID: "CV-1"}, OrchestrationState: models.OrchestrationState{PosterGenerated: true}}, nil
	})
	require.NoError(t, err)
	got, _ := afero.ReadFile(fs, canon)
	assert.Equal(t, "pre-op-C0", string(got), "verified restore rewinds our bytes")

	// Case B: canon now holds a winner's committed bytes → never rewind
	require.NoError(t, afero.WriteFile(fs, canon, []byte("pre-op-C0"), 0o644))
	parked2 := parkCanonicalPosterPair(fs, dir, "CV-1")
	require.NoError(t, afero.WriteFile(fs, canon, []byte("ours-gen2"), 0o644))
	require.NoError(t, afero.WriteFile(fs, canon, []byte("winner-D"), 0o644))
	_, err = withRescrapeStatus(lc, func(scope *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		scope.parked = parked2
		scope.genSHA = map[string]string{"CV-1.jpg": shaContentHex([]byte("ours-gen2"))}
		scope.preExistedPair = true
		return &RescrapeResult{Status: models.RescrapeStatusConflict}, &resultstore.MovieResult{Movie: &models.Movie{ID: "CV-1"}, OrchestrationState: models.OrchestrationState{PosterGenerated: true}}, nil
	})
	require.NoError(t, err)
	got, _ = afero.ReadFile(fs, canon)
	assert.Equal(t, "winner-D", string(got), "winner's bytes never rewound")
	_, statErr := fs.Stat(parked2.cropBak)
	assert.Error(t, statErr, "verify-skipped parked copy is DISPOSED (F-R10-2) — fences unlock")
}

// verify() rerr branch: fingerprint set but canonical missing at closeout —
// the legacy allow path runs (no rewind possible, parked copy stays for the
// sweep/reconciler).
func TestWithRescrapeStatusConflictVerifyMissingCanonAllowsLegacy(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	dir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "ORIG-1", "")
	lc := rescrapeLifecycleShim(jobID, fs, store)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "LF-1.jpg"), []byte("parked"), 0o644))
	parked := parkCanonicalPosterPair(fs, dir, "LF-1")
	require.True(t, parked.hadCrop)
	// canonical missing entirely; fingerprint claims we SAW bytes post-gen
	_, err := withRescrapeStatus(lc, func(scope *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		scope.parked = parked
		scope.genSHA = map[string]string{"LF-1.jpg": shaContentHex([]byte("some"))}
		scope.preExistedPair = true
		return &RescrapeResult{Status: models.RescrapeStatusConflict}, &resultstore.MovieResult{Movie: &models.Movie{ID: "LF-1"}, OrchestrationState: models.OrchestrationState{PosterGenerated: true}}, nil
	})
	require.NoError(t, err)
	got, rerr := afero.ReadFile(fs, filepath.Join(dir, "LF-1.jpg"))
	require.NoError(t, rerr)
	assert.Equal(t, "parked", string(got), "legacy allow when canonical unreadable")
}

// audit F-R18-1: the success-arm orphan sweep must skip an ID whose in-flight
// marker is present — a concurrent rescrape's uncommitted bytes live there.
func TestOrphanSweepSkipsInFlightMarker(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	dir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "NEW-9", "")
	lc := rescrapeLifecycleShim(jobID, fs, store)
	// in-flight sibling's bytes at the orphan ID + its park marker
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "OLD-9.jpg"), []byte("sibling-live"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "OLD-9.jpg.rsbak.a1.b2"), []byte("parked-by-sibling"), 0o644))

	outcome := &RescrapeResult{Status: models.RescrapeStatusSuccess, OrphanedMovieIDs: []string{"OLD-9"}}
	_, err := withRescrapeStatus(lc, func(_ *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		return outcome, &resultstore.MovieResult{Movie: &models.Movie{ID: "NEW-9"}}, nil
	})
	require.NoError(t, err)
	got, rerr := afero.ReadFile(fs, filepath.Join(dir, "OLD-9.jpg"))
	require.NoError(t, rerr, "in-flight sibling's bytes never swept")
	assert.Equal(t, "sibling-live", string(got))
	_, err2 := fs.Stat(filepath.Join(dir, "OLD-9.jpg.rsbak.a1.b2"))
	assert.NoError(t, err2, "park marker survives (its owner disposes it)")

	// Marker cleared, bytes become sweepable
	require.NoError(t, fs.Remove(filepath.Join(dir, "OLD-9.jpg.rsbak.a1.b2")))
	_, err = withRescrapeStatus(lc, func(_ *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		return outcome, &resultstore.MovieResult{Movie: &models.Movie{ID: "NEW-9"}}, nil
	})
	require.NoError(t, err)
	_, err3 := fs.Stat(filepath.Join(dir, "OLD-9.jpg"))
	assert.Error(t, err3, "no marker ⇒ orphan sweep fires")
}

// audit F-R4-1: Gone must NOT restore parked bytes (that resurrects exactly
// the pre-op state Gone exists to purge); parked backups are discarded.
func TestWithRescrapeStatusGonePurgesAndDiscardsParked(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	dir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "ORIG-1", "")
	lc := rescrapeLifecycleShim(jobID, fs, store)
	canon := filepath.Join(dir, "GONE-1.jpg")
	require.NoError(t, afero.WriteFile(fs, canon, []byte("pre-op"), 0o644))
	parked := parkCanonicalPosterPair(fs, dir, "GONE-1")
	require.True(t, parked.hadCrop)
	// losing op overwrote canonical with its own bytes:
	require.NoError(t, afero.WriteFile(fs, canon, []byte("gen-bytes"), 0o644))
	outcome := &RescrapeResult{Status: models.RescrapeStatusGone}
	mr := &resultstore.MovieResult{Movie: &models.Movie{ID: "GONE-1"}, OrchestrationState: models.OrchestrationState{PosterGenerated: true}}
	_, err := withRescrapeStatus(lc, func(scope *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		scope.parked = parked
		return outcome, mr, nil
	})
	require.NoError(t, err)
	_, statErr := fs.Stat(canon)
	assert.Error(t, statErr, "Gone purge stands (no parked resurrection)")
	_, parkErr := fs.Stat(parked.cropBak)
	assert.Error(t, parkErr, "parked copy discarded, never revived")
}

// audit F-R4-3: a witness-fence error skips the restore ENTIRELY — the
// witness owns those bytes; only the reconciler may arbitrate them.
func TestWithRescrapeStatusFenceErrorSkipsRestore(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	dir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "ORIG-1", "")
	lc := rescrapeLifecycleShim(jobID, fs, store)
	canon := filepath.Join(dir, "FNC-1.jpg")
	require.NoError(t, afero.WriteFile(fs, canon, []byte("witness-era"), 0o644))
	parked := parkCanonicalPosterPair(fs, dir, "FNC-1")
	require.NoError(t, afero.WriteFile(fs, canon, []byte("gen-bytes"), 0o644))
	fenceErr := &EditAdmissionConflictError{Message: "poster FNC-1 promote witness unresolved"}
	_, err := withRescrapeStatus(lc, func(scope *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		scope.parked = parked
		return nil, &resultstore.MovieResult{Movie: &models.Movie{ID: "FNC-1"}}, fenceErr
	})
	require.Error(t, err)
	got, rerr := afero.ReadFile(fs, canon)
	require.NoError(t, rerr)
	assert.Equal(t, "gen-bytes", string(got), "fence path leaves bytes untouched for the reconciler")
	_, parkErr := fs.Stat(parked.cropBak)
	assert.NoError(t, parkErr, "parked copy persists for manual repair")
}

// F-R10-2 dispose warn: a wedged Remove on the obsolete parked copy is
// logged, not fatal — the winner's bytes still stand.
func TestRescapePosterBackupDisposeWarns(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/tmp/posters/JDW", 0o755))
	require.NoError(t, afero.WriteFile(base, "/tmp/posters/JDW/CV-9.jpg", []byte("winner"), 0o644))
	requiredPath := "/tmp/posters/JDW/CV-9.jpg.rsbak.a1.b2"
	require.NoError(t, afero.WriteFile(base, requiredPath, []byte("preop"), 0o644))
	b := &rescrapePosterBackup{
		fs:      &seqRenameFailFS{Fs: base, failOn: map[int]bool{}},
		full:    "/tmp/posters/JDW/CV-9-full.jpg",
		crop:    "/tmp/posters/JDW/CV-9.jpg",
		cropBak: requiredPath,
		hadCrop: true,
	}
	b.fs = removeFailFS{Fs: base}                                  // Remove wedges → warn-only
	b.restore(func(p string) (bool, bool) { return false, false }) // canon provably not ours ⇒ dispose
	_, err := base.Stat(requiredPath)
	assert.NoError(t, err, "wedged dispose keeps the parked copy")
}

// codex P2: indeterminate canon read during closeout verify skips the rewind
// — parked copy retained for the reconciler.
func TestVerifySkipOnUnreadableCanonKeepsParked(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	dir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "ORIG-1", "")
	lc := rescrapeLifecycleShim(jobID, fs, store)
	canon := filepath.Join(dir, "UPI-1.jpg")
	require.NoError(t, afero.WriteFile(fs, canon, []byte("pre-op"), 0o644))
	parked := parkCanonicalPosterPair(fs, dir, "UPI-1")
	require.NoError(t, afero.WriteFile(fs, canon, []byte("ours-gen"), 0o644))
	// fs wrapper where ReadFile fails for the canonical leg ONLY
	wedged := &brokenWitnessFS{Fs: fs, readFailSuffix: "UPI-1.jpg"}
	lc.inputs.Fs = wedged
	_, err := withRescrapeStatus(lc, func(scope *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		scope.parked = parked
		scope.genSHA = map[string]string{"UPI-1.jpg": shaContentHex([]byte("ours-gen"))}
		scope.preExistedPair = true
		return &RescrapeResult{Status: models.RescrapeStatusConflict}, &resultstore.MovieResult{Movie: &models.Movie{ID: "UPI-1"}, OrchestrationState: models.OrchestrationState{PosterGenerated: true}}, nil
	})
	require.NoError(t, err)
	got, rerr := afero.ReadFile(fs, canon)
	require.NoError(t, rerr)
	assert.Equal(t, "ours-gen", string(got), "undecidable canon: no rewind")
	_, statErr := fs.Stat(parked.cropBak)
	assert.NoError(t, statErr, "parked copy kept for reconciliation")
}

// brokenWitnessFS fails ReadFile for one name suffix (fs stat renames untouched).
type brokenWitnessFS struct {
	afero.Fs
	readFailSuffix string
}

func (f *brokenWitnessFS) Open(n string) (afero.File, error) {
	if strings.HasSuffix(filepath.ToSlash(n), f.readFailSuffix) {
		return nil, errors.New("read wedged")
	}
	return f.Fs.Open(n)
}

// audit F-R16-1: a successful METADATA rescrape whose poster generation
// FAILED must restore the parked pre-op pair — the commit installs no new
// bytes, so the committed state's bytes must not be parked-and-discarded.
func TestWithRescrapeStatusFailedGenerationRestoresParkedPair(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	dir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "RF-1", "")
	lc := rescrapeLifecycleShim(jobID, fs, store)
	canon := filepath.Join(dir, "RF-1.jpg")
	require.NoError(t, afero.WriteFile(fs, canon, []byte("committed-orig"), 0o644))
	parked := parkCanonicalPosterPair(fs, dir, "RF-1")
	require.True(t, parked.hadCrop)
	genErr := "download wedged"
	_, err := withRescrapeStatus(lc, func(scope *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		scope.parked = parked
		scope.preExistedPair = true
		return &RescrapeResult{Status: models.RescrapeStatusSuccess}, &resultstore.MovieResult{Movie: &models.Movie{ID: "RF-1"}, OrchestrationState: models.OrchestrationState{PosterGenerated: true, PosterError: &genErr}}, nil
	})
	require.NoError(t, err)
	got, rerr := afero.ReadFile(fs, canon)
	require.NoError(t, rerr)
	assert.Equal(t, "committed-orig", string(got), "committed bytes restored on failed generation")
	_, parkErr := fs.Stat(parked.cropBak)
	assert.Error(t, parkErr, "parked copy consumed by the restore")

	// Healthy path still discards: new bytes generated cleanly.
	require.NoError(t, afero.WriteFile(fs, canon, []byte("committed-orig2"), 0o644))
	parked2 := parkCanonicalPosterPair(fs, dir, "RF-1")
	require.NoError(t, afero.WriteFile(fs, canon, []byte("fresh-bytes"), 0o644))
	_, err = withRescrapeStatus(lc, func(scope *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		scope.parked = parked2
		scope.preExistedPair = true
		return &RescrapeResult{Status: models.RescrapeStatusSuccess}, &resultstore.MovieResult{Movie: &models.Movie{ID: "RF-1"}, OrchestrationState: models.OrchestrationState{PosterGenerated: true}}, nil
	})
	require.NoError(t, err)
	got2, _ := afero.ReadFile(fs, canon)
	assert.Equal(t, "fresh-bytes", string(got2), "healthy generation: discard stands")
}

// Marker sweep's Remove wedge: warn-only continue, marker survives.
func TestMarkerSweepWedgeKeepsMarker(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".inflight-PI-W.a1.b2"), []byte("{}"), 0o644))
	cl := &TempDirCleaner{fs: removeFailFS{Fs: fs}, tempDir: "/tmp", jobRepo: nil}
	assert.Equal(t, 0, cl.reconcileParkedPosterBackups(dir))
	_, err := fs.Stat(filepath.Join(dir, ".inflight-PI-W.a1.b2"))
	assert.NoError(t, err, "wedged sweep keeps the marker for the next startup")
}

// audit F-R22-1: a stranded sentinel whose ID contains ".rsbak." sweeps at
// startup (the exclusion tests PARSE — its tail has 3 segments, not hex.hex).
func TestMarkerBranchHandlesDottedRsbakIDs(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".inflight-X.rsbak.aa.a1.b2"), []byte("{}"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "X.rsbak.aa.jpg.rsbak.a1.b2"), []byte("parked"), 0o644))
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: nil}
	healed := cl.reconcileParkedPosterBackups(dir)
	assert.Equal(t, 2, healed, "parked leg re-homed AND stranded marker swept")
	_, mErr := fs.Stat(filepath.Join(dir, ".inflight-X.rsbak.aa.a1.b2"))
	assert.Error(t, mErr, "marker swept (parked-parse rejected its tail)")
	got, err := afero.ReadFile(fs, filepath.Join(dir, "X.rsbak.aa.jpg"))
	require.NoError(t, err)
	assert.Equal(t, "parked", string(got), "parked leg re-homed")
}

// audit F-R20-2 shape coverage: hex-hex tail anchor is exact.
func TestMarkerAnchoredShapeMatrix(t *testing.T) {
	assert.True(t, markerAnchored(".inflight-x.1.2"), "1-char nonce halves are valid")
	assert.False(t, markerAnchored(".inflight-x.1"), "single segment")
	assert.False(t, markerAnchored(".inflight-x.y.z"), "non-hex")
	assert.False(t, markerAnchored(".inflight-x"))
	assert.False(t, markerAnchored(".inflight-x.1.2.extra"))
	assert.True(t, isHexLowerRun("09abcef"))
	assert.False(t, isHexLowerRun(""))
	assert.False(t, isHexLowerRun("09Ab"))
	assert.False(t, hexLowerHexTail(".inflight-x"))
	assert.True(t, hexLowerHexTail("..a1.b2"), "empty head segments still tail-parse (markerAnchored's prefix guards this path)")
	assert.False(t, hexLowerHexTail(".x.a1."))
}

// A wedged MkdirAll on the marker dir degrades to no-marker (park still runs).
func TestParkCanonicalMarkerMkdirFailure(t *testing.T) {
	fs := statFailSuffixFS{} // placeholder unused fields safe
	_ = fs
	fs2 := &mkdirFailFS{Fs: afero.NewMemMapFs()}
	require.NoError(t, fs2.MkdirAll("/tmp/base", 0o755)) // parent ok; only actual mkdir of job dir fails
	b := parkCanonicalPosterPair(fs2, "/tmp/posters/JMK", "PI-M")
	assert.Empty(t, b.markerPath, "mkdir wedge ⇒ marker headless")
	assert.False(t, b.hadCrop)
}

type mkdirFailFS struct{ afero.Fs }

func (f *mkdirFailFS) MkdirAll(p string, perm os.FileMode) error {
	if strings.Contains(p, "/posters/") {
		return errors.New("mkdir wedged")
	}
	return f.Fs.MkdirAll(p, perm)
}

// Marker write failure degrades to no-marker (never bricked parking); the op
// proceeds with legacy rsbak-only markers.
func TestParkCanonicalPosterPairMarkerWriteFailure(t *testing.T) {
	fs := &inflightFailFS{Fs: afero.NewMemMapFs()}
	require.NoError(t, fs.MkdirAll("/tmp/posters/JMW", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/tmp/posters/JMW/PI-W.jpg", []byte("x"), 0o644))
	b := parkCanonicalPosterPair(fs, "/tmp/posters/JMW", "PI-W")
	require.NotNil(t, b)
	assert.Empty(t, b.markerPath, "wedged sentinel write degrades headlessly")
	// the legs still parked fine
	_, ferr := fs.Stat("/tmp/posters/JMW/PI-W-full.jpg.rsbak.a1.b2") // unknown nonce — just confirm SOME rsbak matches
	_ = ferr
	hit, herr := rescrapeInFlightBackupPresent(fs, "/tmp/posters/JMW", "PI-W")
	require.NoError(t, herr)
	assert.True(t, hit, "rsbak marker landed")
}

type inflightFailFS struct{ afero.Fs }

func (f *inflightFailFS) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if strings.Contains(name, ".inflight-") && flag&(os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_APPEND|os.O_RDWR) != 0 {
		return nil, errors.New("sentinel write wedged")
	}
	return f.Fs.OpenFile(name, flag, perm)
}

// audit F-R19-1/2: the unconditional in-flight sentinel marks generation even
// when nothing was parked, and closeout settles it with the pair; stranded
// sentinels die at startup reconciliation.
func TestRescrapeInFlightSentinelLifecycle(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/tmp/posters/JIF", 0o755))

	// park with zero pre-existing legs still writes the sentinel
	b := parkCanonicalPosterPair(fs, "/tmp/posters/JIF", "PI-4")
	require.NotEmpty(t, b.markerPath, "marker written even when no parkable legs")
	hit, err := rescrapeInFlightBackupPresent(fs, "/tmp/posters/JIF", "PI-4")
	require.NoError(t, err)
	assert.True(t, hit, "sentinel marks in-flight even with empty park")
	_, statErr := fs.Stat(b.markerPath)
	require.NoError(t, statErr)

	// closeout settle removes it
	b.restore(nil)
	_, statErr = fs.Stat(b.markerPath)
	assert.Error(t, statErr, "restore settles the marker")

	// stranded marker dies at startup reconciliation
	b2 := parkCanonicalPosterPair(fs, "/tmp/posters/JIF", "PI-5")
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: nil}
	healed := cl.reconcileParkedPosterBackups("/tmp/posters/JIF")
	assert.GreaterOrEqual(t, healed, 1, "stranded marker swept")
	_, statErr = fs.Stat(b2.markerPath)
	assert.Error(t, statErr)
}

// audit F-R19-2: a losing closeout deletes ONLY legs whose CURRENT bytes match
// our fingerprints — a sibling's overwrite is never disposed.
func TestCloseoutDeleteGatedByFingerprint(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	dir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "ORIG-1", "")
	lc := rescrapeLifecycleShim(jobID, fs, store)

	canonFull := filepath.Join(dir, "GATE-1-full.jpg")
	canonCrop := filepath.Join(dir, "GATE-1.jpg")
	// ours: full only (handwritable)
	require.NoError(t, afero.WriteFile(fs, canonFull, []byte("our-full"), 0o644))
	// sibling wrote BOTH
	mineSHA := shaContentHex([]byte("our-full"))

	_, err := withRescrapeStatus(lc, func(scope *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		scope.genSHA = map[string]string{"GATE-1-full.jpg": mineSHA}
		scope.preExistedPair = false
		return &RescrapeResult{Status: models.RescrapeStatusConflict}, &resultstore.MovieResult{Movie: &models.Movie{ID: "GATE-1"}, OrchestrationState: models.OrchestrationState{PosterGenerated: true}}, nil
	})
	require.NoError(t, err)
	_, statDel := fs.Stat(canonFull)
	assert.Error(t, statDel, "fingerprint-matched own bytes removed by the gated delete")
	// sanity: no crop leg — never invented
	_, cerr := fs.Stat(canonCrop)
	assert.Error(t, cerr)

	// fingerprint mismatch: a sibling's bytes must stay
	require.NoError(t, afero.WriteFile(fs, canonFull, []byte("sibling-full"), 0o644))
	_, err = withRescrapeStatus(lc, func(scope *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		scope.genSHA = map[string]string{"GATE-1-full.jpg": mineSHA}
		scope.preExistedPair = false
		return &RescrapeResult{Status: models.RescrapeStatusConflict}, &resultstore.MovieResult{Movie: &models.Movie{ID: "GATE-1"}, OrchestrationState: models.OrchestrationState{PosterGenerated: true}}, nil
	})
	require.NoError(t, err)
	got2, _ := afero.ReadFile(fs, canonFull)
	assert.Equal(t, "sibling-full", string(got2), "sibling bytes never deleted by the losing closeout")
}

// audit F-R20-2: canonical files of IDs literally starting with ".inflight-"
// must never be read as in-flight sentinels; sweep must not sweep them.
func TestInflightMarkerAnchoringRejectsMisnames(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/tmp/posters/JAM", 0o755))
	// canonical file of a leading-dot ID — NOT a marker
	require.NoError(t, afero.WriteFile(fs, "/tmp/posters/JAM/.inflight-PI-7.jpg", []byte("alive"), 0o644))
	// a marker-ish name without the hex.hex tail — NOT a marker
	require.NoError(t, afero.WriteFile(fs, "/tmp/posters/JAM/.inflight-PI-7.nothex", []byte("x"), 0o644))
	assert.False(t, markerAnchored(".inflight-PI-7.jpg"))
	assert.False(t, markerAnchored(".inflight-PI-7.nothex"))
	assert.True(t, markerAnchored(".inflight-PI-7.a14.f9"))
	hit, err := rescrapeInFlightBackupPresent(fs, "/tmp/posters/JAM", "PI-7")
	require.NoError(t, err)
	assert.False(t, hit, "mis-shaped sentinel-likes must not fence")
	require.NoError(t, afero.WriteFile(fs, "/tmp/posters/JAM/.inflight-PI-8.a14.f9", nil, 0o644))
	hit2, _ := rescrapeInFlightBackupPresent(fs, "/tmp/posters/JAM", "PI-8")
	assert.True(t, hit2, "well-shaped sentinel fences")
}

// audit F-R19-1: worker fence probes the inflight sentinel.
func TestPosterWitnessFenceFencesInflightSentinel(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/JIF-X"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".inflight-PI-1.abc123.df"), []byte("{}"), 0o644))
	err := posterWitnessConflict(fs, "/tmp", "JIF-X", "PI-1")
	require.Error(t, err)
	var cfe *EditAdmissionConflictError
	require.ErrorAs(t, err, &cfe)
	assert.Contains(t, err.Error(), "in-flight rescrape")
}

// audit F-R10-2: when a closeout's content-verify refuses the rewind (canon
// holds winner bytes), the parked pre-op copy is OBSOLETE — disposed, never
// left to brick every poster admission as "in-flight rescrape".
func TestWithRescrapeStatusConflictVerifySkipDisposesParked(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	dir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "ORIG-1", "")
	lc := rescrapeLifecycleShim(jobID, fs, store)
	canon := filepath.Join(dir, "CV-2.jpg")
	require.NoError(t, afero.WriteFile(fs, canon, []byte("pre-op-C0"), 0o644))
	parked := parkCanonicalPosterPair(fs, dir, "CV-2")
	require.NoError(t, afero.WriteFile(fs, canon, []byte("ours-gen"), 0o644))
	require.NoError(t, afero.WriteFile(fs, canon, []byte("winner-committed"), 0o644))
	_, err := withRescrapeStatus(lc, func(scope *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		scope.parked = parked
		scope.genSHA = map[string]string{"CV-2.jpg": shaContentHex([]byte("ours-gen"))}
		scope.preExistedPair = true
		return &RescrapeResult{Status: models.RescrapeStatusConflict}, &resultstore.MovieResult{Movie: &models.Movie{ID: "CV-2"}, OrchestrationState: models.OrchestrationState{PosterGenerated: true}}, nil
	})
	require.NoError(t, err)
	got, _ := afero.ReadFile(fs, canon)
	assert.Equal(t, "winner-committed", string(got), "winner bytes stand")
	_, statErr := fs.Stat(parked.cropBak)
	assert.Error(t, statErr, "verify-skipped parked copy disposed — fences unlock")
	hit, _ := rescrapeInFlightBackupPresent(fs, dir, "CV-2")
	assert.False(t, hit, "no in-flight marker left behind")
}

// audit F-R10-3: an in-flight rescrape's parked marker fences EVERY
// revision-advancing edit through posterWitnessConflict.
func TestPosterWitnessFenceFencesParkedBackup(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/JOB-PK"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1.jpg.rsbak.a1.b2"), []byte("parked"), 0o644))
	err := posterWitnessConflict(fs, "/tmp", "JOB-PK", "PI-1")
	require.Error(t, err)
	var cfe *EditAdmissionConflictError
	require.ErrorAs(t, err, &cfe)
	assert.Contains(t, err.Error(), "in-flight rescrape")

	// unrelated ID unaffected
	require.NoError(t, posterWitnessConflict(fs, "/tmp", "JOB-PK", "OTHER-1"))
}

// audit F-R10-1: park + generation run UNDER the family key — the lock is
// acquired before generation starts and released after it finishes.
func TestRescrapeHoldsFamilyKeyThroughParkAndGenerate(t *testing.T) {
	store := resultstore.New(1, []string{"f1.mp4"})
	seedFamilyResult(store, "f1.mp4", "res-1", "KEY-1", "")
	var heldDuringGen bool
	var rhoacquiredAt, rhoReleasedAt time.Time
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{Movie: &models.Movie{ID: "KEY-1"}, Status: scrape.StatusCompleted}}
	gen := &spyPosterGen{
		onGenerate: func() {
			heldDuringGen = !rhoacquiredAt.IsZero() && rhoReleasedAt.IsZero()
		},
	}
	inputs := rescrapePhaseInputs{
		WF: wf, ResultMap: store, Finder: store, JobID: models.NewJobID(),
		PosterGen: gen,
		EditLockFn: func(ids ...string) func() {
			rhoacquiredAt = time.Now()
			return func() { rhoReleasedAt = time.Now() }
		},
		Fs: afero.NewMemMapFs(), TempDir: "/tmp",
	}
	phase := NewRescrapePhase()
	res, err := phase.Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "KEY-1", FilePath: "f1.mp4"})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.True(t, heldDuringGen, "generation ran while the family key was held")
	assert.False(t, rhoacquiredAt.IsZero(), "key acquired")
}

type spyPosterGen struct{ onGenerate func() }

func (s *spyPosterGen) GeneratePoster(_ context.Context, _ string, _ *models.Movie) error {
	if s.onGenerate != nil {
		s.onGenerate()
	}
	return nil
}

// audit F-R11-1: a panicking generation releases the family key — the op's
// recovered-panic path (batch rescrape recover wraps) keeps the registry
// usable and the marker sweeper reunites parked bytes at restart.
func TestRescrapePanicInHeldSectionReleasesFamilyKey(t *testing.T) {
	store := resultstore.New(1, []string{"f1.mp4"})
	seedFamilyResult(store, "f1.mp4", "res-1", "PAN-1", "")
	reg := newKeyedMutexRegistry()
	unlock := reg.Acquire("PAN-1")
	unlock()
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{Movie: &models.Movie{ID: "PAN-1"}, Status: scrape.StatusCompleted}}
	gen := &panicPosterGen{}
	inputs := rescrapePhaseInputs{
		WF: wf, ResultMap: store, Finder: store, JobID: models.NewJobID(),
		PosterGen:  gen,
		EditLockFn: func(ids ...string) func() { return reg.AcquireMany(ids) },
		Fs:         afero.NewMemMapFs(), TempDir: "/tmp",
	}
	phase := NewRescrapePhase()
	panicked := assert.Panics(t, func() {
		_, _ = phase.Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "PAN-1", FilePath: "f1.mp4"})
	}, "panic propagates to the caller-side recover")
	if !panicked {
		t.Skip("no panic observed")
	}
	selectRelease := make(chan struct{})
	go func() {
		release := reg.Acquire("PAN-1")
		release()
		close(selectRelease)
	}()
	select {
	case <-selectRelease:
	case <-time.After(3 * time.Second):
		t.Fatal("family KEY leaked: re-acquire blocked forever after in-section panic")
	}
	// The mutex is free; the parked marker may linger until the startup sweep
	// re-homes it (witness-analogous). That is by design.
}

// spy PosterGen that panics mid-generation.
type panicPosterGen struct{}

func (panicPosterGen) GeneratePoster(_ context.Context, _ string, _ *models.Movie) error {
	panic("intentional test panic")
}

// --- guard/backup matrix for poster_cleanup.go + predicate edges ---

func TestParkCanonicalPosterPairGuards(t *testing.T) {
	// nil fs / empty dir+id: no-ops, restore/discard safe
	bNil := parkCanonicalPosterPair(nil, "/x", "A-1")
	bNil.restore(nil)
	bNil.discard()
	bEmpty := parkCanonicalPosterPair(afero.NewMemMapFs(), "", "")
	bEmpty.restore(nil)
	bEmpty.discard()

	// stat error (non-ENOENT) fails CLOSED: leg marked had
	mem := afero.NewMemMapFs()
	require.NoError(t, mem.MkdirAll("/tmp/posters/J", 0o755))
	fsStatErr := statFailSuffixFS{Fs: mem, suffix: "PA-9-full.jpg"}
	bStat := parkCanonicalPosterPair(fsStatErr, "/tmp/posters/J", "PA-9")
	assert.True(t, bStat.hadFull, "stat error => treated as pre-existing")
	assert.False(t, bStat.hadCrop)

	// park rename failure: warn only, leg untouched, had stays false
	mem2 := afero.NewMemMapFs()
	require.NoError(t, mem2.MkdirAll("/tmp/posters/J2", 0o755))
	require.NoError(t, afero.WriteFile(mem2, "/tmp/posters/J2/PA-9-full.jpg", []byte("f"), 0o644))
	require.NoError(t, afero.WriteFile(mem2, "/tmp/posters/J2/PA-9.jpg", []byte("c"), 0o644))
	fsRn := &seqRenameFailFS{Fs: mem2, failOn: map[int]bool{1: true}}
	bRn := parkCanonicalPosterPair(fsRn, "/tmp/posters/J2", "PA-9")
	assert.False(t, bRn.hadFull, "failed park => not marked")
	assert.True(t, bRn.hadCrop, "second leg parked fine")
	got, _ := afero.ReadFile(mem2, "/tmp/posters/J2/PA-9-full.jpg")
	assert.Equal(t, "f", string(got), "failed park never moved the leg")

	// restore skips: !had legs and missing .rsbak
	bSkip := &rescrapePosterBackup{fs: mem2, full: "/tmp/posters/J2/ZZ-1-full.jpg", crop: "/tmp/posters/J2/ZZ-1.jpg", hadFull: true, hadCrop: false}
	bSkip.restore(nil)

	// restore rename failure warns and keeps the bak
	mem3 := afero.NewMemMapFs()
	require.NoError(t, mem3.MkdirAll("/tmp/posters/J3", 0o755))
	require.NoError(t, afero.WriteFile(mem3, "/tmp/posters/J3/PA-9-full.jpg", []byte("live"), 0o644))
	bPark := parkCanonicalPosterPair(mem3, "/tmp/posters/J3", "PA-9")
	require.True(t, bPark.hadFull)
	bPark.fs = &seqRenameFailFS{Fs: mem3, failOn: map[int]bool{1: true}}
	bPark.restore(nil)
	_, bakErr := mem3.Stat(bPark.fullBak)
	assert.NoError(t, bakErr, "failed restore leaves parked bytes for salvage")

	// discard removes parked files
	mem4 := afero.NewMemMapFs()
	require.NoError(t, mem4.MkdirAll("/tmp/posters/J4", 0o755))
	require.NoError(t, afero.WriteFile(mem4, "/tmp/posters/J4/PA-9.jpg", []byte("c"), 0o644))
	bDisc := parkCanonicalPosterPair(mem4, "/tmp/posters/J4", "PA-9")
	require.True(t, bDisc.hadCrop)
	bDisc.discard()
	_, dErr := mem4.Stat(bDisc.cropBak)
	assert.Error(t, dErr, "parked bytes discarded")

	// F-R4-4: two parks of the same ID never share a backup path; each
	// restores ITS bytes even after both parked.
	mem5 := afero.NewMemMapFs()
	require.NoError(t, mem5.MkdirAll("/tmp/posters/J5", 0o755))
	require.NoError(t, afero.WriteFile(mem5, "/tmp/posters/J5/PA-9.jpg", []byte("first"), 0o644))
	b1 := parkCanonicalPosterPair(mem5, "/tmp/posters/J5", "PA-9")
	require.NoError(t, afero.WriteFile(mem5, "/tmp/posters/J5/PA-9.jpg", []byte("second"), 0o644))
	b2 := parkCanonicalPosterPair(mem5, "/tmp/posters/J5", "PA-9")
	assert.NotEqual(t, b1.cropBak, b2.cropBak, "per-op nonce separates backups")
	b1.restore(nil)
	got1, _ := afero.ReadFile(mem5, "/tmp/posters/J5/PA-9.jpg")
	assert.Equal(t, "first", string(got1), "op1 restores ITS bytes, not op2's")
}

func TestAnyResultUsesMovieIDEdges(t *testing.T) {
	assert.False(t, anyResultUsesMovieID(nil, "X-1"))
	assert.False(t, anyResultUsesMovieID(resultstore.New(0, nil), ""))
	store := resultstore.New(1, []string{"/f/x.mp4"})
	store.UpdateFileResult("/f/x.mp4", &resultstore.MovieResult{
		ResultID: "res-x", Status: models.JobStatusCompleted,
		FileMatchInfo: models.FileMatchInfo{Path: "/f/x.mp4", MovieID: "AL-1"}, // Movie nil: alias surface only
	})
	assert.True(t, anyResultUsesMovieID(store, "AL-1"), "matcher-alias match")
	assert.False(t, anyResultUsesMovieID(store, "UNRELATED-1"))
}

func TestCleanupOwnedPosterLegsNilMovie(t *testing.T) {
	// nil / empty-ID movies are no-ops (must not panic, must not delete)
	closeoutRescrapePosterBytes(rescrapePhaseInputs{}, &rescrapeGenScope{}, nil, nil)
	closeoutRescrapePosterBytes(rescrapePhaseInputs{}, &rescrapeGenScope{}, nil, &models.Movie{})
}

// Nil entry in a snapshot must not crash the usage scan.
type nilEntryAccessor struct{ resultstore.ResultMapAccessor }

func (n nilEntryAccessor) SnapshotData() resultstore.ResultSnapshot {
	sd := n.ResultMapAccessor.SnapshotData()
	sd.Results["/f/nil.mp4"] = nil
	return sd
}

func TestAnyResultUsesMovieIDNilEntry(t *testing.T) {
	store := resultstore.New(1, []string{"/f/x.mp4"})
	seedFamilyResult(store, "/f/x.mp4", "res-x", "PI-1", "")
	wrapped := nilEntryAccessor{store}
	assert.True(t, anyResultUsesMovieID(wrapped, "PI-1"))
	assert.False(t, anyResultUsesMovieID(wrapped, "OTHER-1"))
}

// audit R1c: a foreign family's row already owns the movie ID => the
// ownership predicate refuses cleanup on that ID.
func TestWithRescrapeStatusConflictForeignOwnerKeepsBytes(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	mkStore := func(ids ...string) resultstore.Store {
		paths := make([]string, len(ids))
		for i := range ids {
			paths[i] = fmt.Sprintf("/f/%d.mp4", i)
		}
		st := resultstore.New(len(ids), paths)
		for i, id := range ids {
			seedFamilyResult(st, paths[i], fmt.Sprintf("res-%d", i), id, "")
		}
		return st
	}
	inputsOf := func(st resultstore.Store) rescrapePhaseInputs {
		return rescrapePhaseInputs{Fs: fs, TempDir: "/tmp", JobID: jobID, ResultMap: st}
	}
	mkMr := func(id string) *resultstore.MovieResult {
		return &resultstore.MovieResult{Movie: &models.Movie{ID: id}, OrchestrationState: models.OrchestrationState{PosterGenerated: true}}
	}

	// Any committed row referencing the ID (self OR sibling) => never delete.
	storeOwned := mkStore("PI-1", "OTHER-9")
	assert.False(t, rescrapeOwnsPosterLegs(inputsOf(storeOwned), &rescrapeGenScope{}, mkMr("PI-1"), "PI-1"), "row-referenced => not ours")
	storeSibling := mkStore("ORIG-1", "PI-2")
	assert.False(t, rescrapeOwnsPosterLegs(inputsOf(storeSibling), &rescrapeGenScope{}, mkMr("PI-2"), "PI-2"), "sibling-owned => not ours")

	// No row references the ID at all => the bytes are provably ours.
	storeFree := mkStore("ORIG-1", "OTHER-9")
	assert.True(t, rescrapeOwnsPosterLegs(inputsOf(storeFree), &rescrapeGenScope{}, mkMr("NEW-77"), "NEW-77"), "unreferenced self-created => deletable")

	genErr := "wedged"
	bad := mkMr("NEW-77")
	bad.PosterError = &genErr
	assert.False(t, rescrapeOwnsPosterLegs(inputsOf(storeFree), &rescrapeGenScope{}, bad, "NEW-77"), "generation error => never delete")
	assert.False(t, rescrapeOwnsPosterLegs(inputsOf(storeFree), &rescrapeGenScope{preExistedPair: true}, mkMr("NEW-77"), "NEW-77"), "pre-existing pair => never delete")
	assert.False(t, rescrapeOwnsPosterLegs(inputsOf(storeFree), &rescrapeGenScope{}, mkMr("NEW-77"), ""), "empty id => never")
	assert.False(t, rescrapeOwnsPosterLegs(inputsOf(storeFree), nil, mkMr("NEW-77"), "NEW-77"), "nil scope => never deletes")
	assert.False(t, rescrapeOwnsPosterLegs(inputsOf(storeFree), &rescrapeGenScope{}, nil, "NEW-77"), "nil movie result => never deletes")
	assert.False(t, rescrapeOwnsPosterLegs(inputsOf(storeFree), &rescrapeGenScope{}, &resultstore.MovieResult{}, "NEW-77"), "not generated => never deletes")
}
