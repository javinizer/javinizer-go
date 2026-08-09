package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
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
		scope.parked = parkCanonicalPosterPair(fs, dir, "REF-1", 0)
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
	parked := parkCanonicalPosterPair(fs, dir, "CV-1", 0)
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
	parked2 := parkCanonicalPosterPair(fs, dir, "CV-1", 0)
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
	parked := parkCanonicalPosterPair(fs, dir, "LF-1", 0)
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
	parked := parkCanonicalPosterPair(fs, dir, "GONE-1", 0)
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
	parked := parkCanonicalPosterPair(fs, dir, "FNC-1", 0)
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
	parked := parkCanonicalPosterPair(fs, dir, "UPI-1", 0)
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
	parked := parkCanonicalPosterPair(fs, dir, "RF-1", 0)
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
	parked2 := parkCanonicalPosterPair(fs, dir, "RF-1", 0)
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
	assert.Equal(t, 0, cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir))
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
	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 2, healed, "parked leg re-homed AND stranded marker swept")
	_, mErr := fs.Stat(filepath.Join(dir, ".inflight-X.rsbak.aa.a1.b2"))
	assert.Error(t, mErr, "marker swept (parked-parse rejected its tail)")
	got, err := afero.ReadFile(fs, filepath.Join(dir, "X.rsbak.aa.jpg"))
	require.NoError(t, err)
	assert.Equal(t, "parked", string(got), "parked leg re-homed")
}

// codex P1: unsafe scraper-derived IDs never flow into orphan-sweep paths.
func TestOrphanedPosterPathsSkipsUnsafeIDs(t *testing.T) {
	ids := []string{"../victim", "safe-1", "with/slash"}
	out := OrphanedPosterPaths(ids, "NEW-1", "/tmp", models.NewJobID(), nil)
	assert.Len(t, out, 2, "only the safe ID survives path construction")
	for _, p := range out {
		assert.Contains(t, p, "safe-1")
		assert.NotContains(t, p, "victim")
	}
	// case-folded equal IDs never sweep on case-insensitive filesystems
	assert.Empty(t, OrphanedPosterPaths([]string{"sAmE"}, "SAME", "/tmp", models.NewJobID(), nil))
}

// codex P2 (case): probes fold candidate spellings — a marker parked under
// the scraper's case variant reaches the same canonical file on
// case-insensitive filesystems.

func TestInFlightProbesFoldCaseSentinels(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/JCV-W"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".inflight-abc-1.a1.b2"), []byte("{}"), 0o644))
	hit, err := rescrapeInFlightBackupPresent(fs, dir, "ABC-1")
	require.NoError(t, err)
	assert.True(t, hit, "variant-case sentinel fences the canonical probe")
}
func TestInFlightProbesFoldCase(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/JCF"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "abc-1.jpg.rsbak.a1.b2"), []byte("parked"), 0o644))
	hit, err := rescrapeInFlightBackupPresent(fs, dir, "ABC-1")
	require.NoError(t, err)
	assert.True(t, hit, "folded probe catches the variant-case marker")
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
	b := parkCanonicalPosterPair(fs2, "/tmp/posters/JMK", "PI-M", 0)
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
	b := parkCanonicalPosterPair(fs, "/tmp/posters/JMW", "PI-W", 0)
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
	b := parkCanonicalPosterPair(fs, "/tmp/posters/JIF", "PI-4", 0)
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
	b2 := parkCanonicalPosterPair(fs, "/tmp/posters/JIF", "PI-5", 0)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: nil}
	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", "/tmp/posters/JIF")
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
	parked := parkCanonicalPosterPair(fs, dir, "CV-2", 0)
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
// Patch-coverage: the own-leg grand delete READ-fail arm (a recorded leg that
// vanished before closeout) is a silent skip, never an error.
func TestCloseoutOwnLegSweepReadFailSkips(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	dir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "ORIG-A", "")
	lc := rescrapeLifecycleShim(jobID, fs, store)

	_, err := withRescrapeStatus(lc, func(scope *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		scope.genSHA = map[string]string{"SWPR-9.jpg": "deadbeef"}
		scope.preExistedPair = false
		return &RescrapeResult{Status: models.RescrapeStatusConflict}, &resultstore.MovieResult{Movie: &models.Movie{ID: "SWPR-9"}, OrchestrationState: models.OrchestrationState{PosterGenerated: true}}, nil
	})
	require.NoError(t, err)
	entries, rdErr := afero.ReadDir(fs, dir)
	require.NoError(t, rdErr)
	assert.Empty(t, entries, "vanished leg: nothing deleted, nothing created")
}

// Patch-coverage: a matching-fingerprint leg whose delete wedges stays in
// place (warn-only), the closeout never destroys unverifiable bytes.
func TestCloseoutOwnLegSweepRemoveFailWarns(t *testing.T) {
	mem := afero.NewMemMapFs()
	jobID := models.NewJobID()
	dir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, mem.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(dir, "SWPD-9-full.jpg"), []byte("mine-full"), 0o644))
	fs := selectiveFailRemoveFS{Fs: mem, failSuffix: "-full.jpg"}
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "ORIG-B", "")
	lc := rescrapeLifecycleShim(jobID, fs, store)

	_, err := withRescrapeStatus(lc, func(scope *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		scope.genSHA = map[string]string{"SWPD-9-full.jpg": shaContentHex([]byte("mine-full"))}
		scope.preExistedPair = false
		return &RescrapeResult{Status: models.RescrapeStatusConflict}, &resultstore.MovieResult{Movie: &models.Movie{ID: "SWPD-9"}, OrchestrationState: models.OrchestrationState{PosterGenerated: true}}, nil
	})
	require.NoError(t, err)
	got, rdErr := afero.ReadFile(mem, filepath.Join(dir, "SWPD-9-full.jpg"))
	require.NoError(t, rdErr)
	assert.Equal(t, "mine-full", string(got), "delete failure keeps the leg in place")
}

// Patch-coverage: a transitional (non-terminal, non-success) outcome status
// restores the parked pre-op pair verbatim — no content arbitration yet.
func TestWithRescrapeStatusUnusualStatusRestoresParked(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	dir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "ORIG-C", "")
	lc := rescrapeLifecycleShim(jobID, fs, store)
	canon := filepath.Join(dir, "PEND-1.jpg")
	require.NoError(t, afero.WriteFile(fs, canon, []byte("pre-op"), 0o644))
	parked := parkCanonicalPosterPair(fs, dir, "PEND-1", 0)
	require.True(t, parked.hadCrop)
	require.NoError(t, afero.WriteFile(fs, canon, []byte("gen-bytes"), 0o644))

	outcome := &RescrapeResult{Status: "pending"}
	mr := &resultstore.MovieResult{Movie: &models.Movie{ID: "PEND-1"}}
	_, err := withRescrapeStatus(lc, func(scope *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		scope.parked = parked
		return outcome, mr, nil
	})
	require.NoError(t, err)
	got, rdErr := afero.ReadFile(fs, canon)
	require.NoError(t, rdErr)
	assert.Equal(t, "pre-op", string(got), "transitional status rewinds to pre-op bytes")
}

type openFailDirFS struct {
	afero.Fs
	dir string
}

func (f openFailDirFS) Open(name string) (afero.File, error) {
	if filepath.ToSlash(name) == filepath.ToSlash(f.dir) {
		return nil, errors.New("open wedged")
	}
	return f.Fs.Open(name)
}

// Patch-coverage: an UNREADABLE poster dir during the orphan marker probe is
// undecidable — the orphan is kept, never swept.
func TestOrphanSweepProbeErrorKeepsOrphan(t *testing.T) {
	mem := afero.NewMemMapFs()
	jobID := models.NewJobID()
	dir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, mem.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(dir, "OLD-9.jpg"), []byte("orphan-bytes"), 0o644))
	fs := openFailDirFS{Fs: mem, dir: dir}
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "NEW-9", "")
	lc := rescrapeLifecycle{
		inputs: rescrapePhaseInputs{Fs: fs, TempDir: "/tmp", JobID: jobID, ResultMap: store, EditLockFn: func(ids ...string) func() { return func() {} }},
		lookup: &resultstore.FileLookupResult{FilePath: "/f/a.mp4", OldMovieID: "OLD-9"},
	}
	outcome := &RescrapeResult{Status: models.RescrapeStatusSuccess, OrphanedMovieIDs: []string{"OLD-9"}}
	_, err := withRescrapeStatus(lc, func(_ *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		return outcome, &resultstore.MovieResult{Movie: &models.Movie{ID: "NEW-9"}}, nil
	})
	require.NoError(t, err)
	got, rdErr := afero.ReadFile(mem, filepath.Join(dir, "OLD-9.jpg"))
	require.NoError(t, rdErr)
	assert.Equal(t, "orphan-bytes", string(got), "undecidable probe keeps the orphan")
}

// Patch-coverage: with no EditLockFn configured the orphan sweep still runs
// end-to-end on the default no-op release — sweeping works lock-free.
func TestOrphanSweepDefaultReleaseWhenNoEditLock(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	dir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "OLD-7.jpg"), []byte("orphan"), 0o644))
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "NEW-7", "")
	lc := rescrapeLifecycle{
		inputs: rescrapePhaseInputs{Fs: fs, TempDir: "/tmp", JobID: jobID, ResultMap: store}, // no EditLockFn
		lookup: &resultstore.FileLookupResult{FilePath: "/f/a.mp4", OldMovieID: "OLD-7"},
	}
	outcome := &RescrapeResult{Status: models.RescrapeStatusSuccess, OrphanedMovieIDs: []string{"OLD-7"}}
	_, err := withRescrapeStatus(lc, func(_ *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		return outcome, &resultstore.MovieResult{Movie: &models.Movie{ID: "NEW-7"}}, nil
	})
	require.NoError(t, err)
	_, statErr := fs.Stat(filepath.Join(dir, "OLD-7.jpg"))
	assert.Error(t, statErr, "orphan swept on the default release path")
}

// Patch-coverage: generation success captures content fingerprints of BOTH
// canonical legs for the closeout's restore arbitration.
func TestRescrapeGenerationFingerprintCapture(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	pdir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, fs.MkdirAll(pdir, 0o755))
	store := resultstore.New(1, []string{"f1.mp4"})
	seedFamilyResult(store, "f1.mp4", "res-1", "GEN-9", "")
	gen := &spyPosterGen{onGenerate: func() {
		_ = afero.WriteFile(fs, filepath.Join(pdir, "GEN-9-full.jpg"), []byte("gen-full"), 0o644)
		_ = afero.WriteFile(fs, filepath.Join(pdir, "GEN-9.jpg"), []byte("gen-crop"), 0o644)
	}}
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{Movie: &models.Movie{ID: "GEN-9"}, Status: scrape.StatusCompleted}}
	inputs := rescrapePhaseInputs{
		WF: wf, ResultMap: store, Finder: store, JobID: jobID,
		PosterGen:  gen,
		EditLockFn: func(ids ...string) func() { return func() {} },
		Fs:         fs, TempDir: "/tmp",
	}
	phase := NewRescrapePhase()
	res, err := phase.Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "GEN-9", FilePath: "f1.mp4"})
	require.NoError(t, err)
	require.NotNil(t, res)
	full, ferr := afero.ReadFile(fs, filepath.Join(pdir, "GEN-9-full.jpg"))
	require.NoError(t, ferr)
	assert.Equal(t, "gen-full", string(full), "generated full leg survives success closeout")
	crop, cerr := afero.ReadFile(fs, filepath.Join(pdir, "GEN-9.jpg"))
	require.NoError(t, cerr)
	assert.Equal(t, "gen-crop", string(crop), "generated crop leg survives success closeout")
}

// codex cloud P1: a successful op whose generation parked bytes keeps the
// whole trinity past return until the caller finalizes (post-envelope-persist).
func TestRescrapeSuccessDefersTrinityUntilFinalize(t *testing.T) {
	base := afero.NewMemMapFs()
	jobID := models.NewJobID()
	dir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "DF-1-full.jpg"), []byte("old-full"), 0o644))
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "DF-1.jpg"), []byte("old-crop"), 0o644))
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "ORIG-D", "")
	b := parkCanonicalPosterPair(base, dir, "DF-1", 1)
	require.NoError(t, b.parkErr)
	lc := rescrapeLifecycleShim(jobID, base, store)

	outcome, err := withRescrapeStatus(lc, func(scope *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		scope.parked = b
		if wErr := afero.WriteFile(base, filepath.Join(dir, "DF-1.jpg"), []byte("new-crop"), 0o644); wErr != nil {
			return nil, nil, wErr
		}
		return &RescrapeResult{Status: models.RescrapeStatusSuccess}, &resultstore.MovieResult{Movie: &models.Movie{ID: "DF-1"}, OrchestrationState: models.OrchestrationState{PosterGenerated: true}}, nil
	})
	require.NoError(t, err)
	require.NotNil(t, outcome)
	require.NotNil(t, outcome.PosterRecovery, "recovery handle must ride the outcome until the caller finalizes")
	// Trinity retained pre-finalize:
	for _, n := range []string{b.markerPath, b.commitPath, b.cropBak} {
		_, sErr := base.Stat(n)
		assert.NoError(t, sErr, "%s retained until the durable envelope lands", filepath.Base(n))
	}
	got, rerr := afero.ReadFile(base, filepath.Join(dir, "DF-1.jpg"))
	require.NoError(t, rerr)
	assert.Equal(t, "new-crop", string(got), "generated bytes stay — the op won")
	outcome.PosterRecovery.Finalize()
	for _, n := range []string{b.markerPath, b.commitPath, b.cropBak} {
		_, sErr := base.Stat(n)
		assert.Error(t, sErr, "%s finalized away post-persist", filepath.Base(n))
	}
	outcome.PosterRecovery.Finalize() // idempotent
}

// codex cloud P1 closeout seam: a commit-token write failure is warn-only// codex cloud P1 closeout seam: a commit-token write failure is warn-only
// — the durable commit stands and the backup survives for startup arbitration.
func TestRescrapeCloseoutCommitTokenWriteFailWarnOnly(t *testing.T) {
	base := afero.NewMemMapFs()
	jobID := models.NewJobID()
	dir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "CT-1-full.jpg"), []byte("cf"), 0o644))
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "CT-1.jpg"), []byte("cc"), 0o644))
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "ORIG-C", "")

	b := parkCanonicalPosterPair(base, dir, "CT-1", 3)
	require.NoError(t, b.parkErr)
	wedge := createWedgeFS{Fs: base, contains: ".commit-"}
	b.fs = wedge
	lc := rescrapeLifecycleShim(jobID, wedge, store)

	outcome, err := withRescrapeStatus(lc, func(scope *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		scope.parked = b
		return &RescrapeResult{Status: models.RescrapeStatusSuccess}, &resultstore.MovieResult{Movie: &models.Movie{ID: "CT-1"}, OrchestrationState: models.OrchestrationState{PosterGenerated: true}}, nil
	})
	require.NoError(t, err, "token write failure never fails the committed op")
	require.NotNil(t, outcome, "successful outcome rides back")
	require.NotNil(t, outcome.PosterRecovery, "teardown deferred to the persist boundary")
	_, bErr := base.Stat(filepath.Join(dir, "CT-1.jpg.rsbak."+b.nonce))
	assert.NoError(t, bErr, "backup retained while the envelope write has not landed")
	outcome.PosterRecovery.Finalize()
	_, bErr2 := base.Stat(filepath.Join(dir, "CT-1.jpg.rsbak."+b.nonce))
	assert.Error(t, bErr2, "finalize (post-persist) discards the backup")
}

type flipErrCtx struct {
	context.Context
	errs int64
	when int64
	err  error
}

func (c *flipErrCtx) Err() error {
	calls := atomic.AddInt64(&c.errs, 1)
	if calls >= c.when {
		return c.err
	}
	return c.Context.Err()
}

// The re-check after poster generation: a cancelled context surfaces as the
// op's error instead of a silent commit.
func TestRescrapeCancelledPostGenerationCtxError(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	pdir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, fs.MkdirAll(pdir, 0o755))
	store := resultstore.New(1, []string{"f1.mp4"})
	seedFamilyResult(store, "f1.mp4", "res-1", "CX-9", "")
	gen := &spyPosterGen{onGenerate: func() {
		_ = afero.WriteFile(fs, filepath.Join(pdir, "CX-9-full.jpg"), []byte("cf"), 0o644)
	}}
	ctx := &flipErrCtx{Context: context.Background(), when: 2, err: context.Canceled} // 1st Err() = ScrapeSingle's funnel; 2nd = the fn-level guard
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{Movie: &models.Movie{ID: "CX-9"}, Status: scrape.StatusCompleted}}
	inputs := rescrapePhaseInputs{
		WF: wf, ResultMap: store, Finder: store, JobID: jobID,
		PosterGen:  gen,
		EditLockFn: func(ids ...string) func() { return func() {} },
		Fs:         fs, TempDir: "/tmp",
	}
	phase := NewRescrapePhase()
	_, err := phase.Rescrape(ctx, inputs, RescrapeCmd{MovieID: "CX-9", FilePath: "f1.mp4"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled), "post-generation ctx re-check surfaces cancellation")
}

// codex cloud P2: a live closeout whose restore leaves a wedged leg must// codex cloud P2: a live closeout whose restore leaves a wedged leg must
// keep the in-flight marker too — it is the ONLY startup-arbitration record
// pairing those bytes back to their op.
func TestRestoreRetainsMarkerWhileBackupLegsRemain(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/posters/J-RL"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "RL-1.jpg"), []byte("gen"), 0o644))
	b := parkCanonicalPosterPair(base, dir, "RL-1", 9)
	require.NoError(t, b.parkErr)
	b.fs = &seqRenameFailFS{Fs: base, failOn: map[int]bool{1: true}}
	b.restore(nil)
	_, err := base.Stat(filepath.Join(dir, "RL-1.jpg.rsbak."+b.nonce))
	assert.NoError(t, err, "restore wedged — parked leg stranding stands")
	_, mErr := base.Stat(b.markerPath)
	assert.NoError(t, mErr, "marker retained until every leg settles")
}

// Legacy guard: a success whose parked accessor carries no fs (older fenced
// constructions) still teardowns immediately — the deferral handle needs it.
func TestRescrapeSuccessNilFsParkedDiscardsLegacy(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "ORIG-L", "")
	lc := rescrapeLifecycleShim(models.NewJobID(), afero.NewMemMapFs(), store)
	outcome, err := withRescrapeStatus(lc, func(scope *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		scope.parked = &rescrapePosterBackup{} // fs nil → legacy immediate teardown arm
		return &RescrapeResult{Status: models.RescrapeStatusSuccess}, &resultstore.MovieResult{Movie: &models.Movie{ID: "LG-1"}}, nil
	})
	require.NoError(t, err)
	require.NotNil(t, outcome)
	assert.Nil(t, outcome.PosterRecovery, "no fs ⇒ nothing deferrable exists")
}

// codex cloud P1: live finalization sweeps the winner's commit token when
// nothing contestable pends, but RETAINS it while a same-base competitor's
// .rsbak leg still needs attestation (startup would otherwise keep-both
// forever + fence edits).
// codex cloud P2 (@306): the rival-scan error is undecidable — the winner's
// token must survive so the rival's pending backup remains attributable.
func TestDiscardScanUndecidableKeepsWinnerToken(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/posters/J-SC"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	b := parkCanonicalPosterPair(base, dir, "SC-1", 2)
	require.NoError(t, b.parkErr)
	require.NoError(t, afero.WriteFile(base, b.commitPath, []byte(`{"poster_id":"SC-1","crop_sha":"x"}`), 0o644))
	b.fs = openExactFailFS{Fs: base, path: dir}
	b.discard()
	_, tErr := base.Stat(b.commitPath)
	assert.NoError(t, tErr, "scan undecidable ⇒ token retained")
	_, mErr := base.Stat(b.markerPath)
	assert.NoError(t, mErr, "marker retained with it (fused)")
}

func TestDiscardKeepsWinnerTokenWhileCompetitorBackupPends(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/J-RET"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	b := parkCanonicalPosterPair(fs, dir, "RET-1", 3)
	require.NoError(t, b.parkErr)
	// a rival op's parked leg for the same poster, still pending on disk:
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "RET-1.jpg.rsbak.a9.c9"), []byte("rival"), 0o644))
	require.NoError(t, afero.WriteFile(fs, b.commitPath, []byte(`{"poster_id":"RET-1"}`), 0o644))

	b.discard()
	_, tErr := fs.Stat(b.commitPath)
	assert.NoError(t, tErr, "winner token retained while the rival's backup pends")
	_, mErr := fs.Stat(b.markerPath)
	assert.NoError(t, mErr, "marker follows the token — fused retention while rivals pend")

	// Rival settled: next discard sweeps the token too.
	require.NoError(t, fs.Remove(filepath.Join(dir, "RET-1.jpg.rsbak.a9.c9")))
	require.NoError(t, afero.WriteFile(fs, b.commitPath, []byte(`{"poster_id":"RET-1"}`), 0o644))
	b.discard()
	_, t2Err := fs.Stat(b.commitPath)
	assert.Error(t, t2Err, "nothing pending ⇒ token swept at finalization")
} // discard/restore on accessors without paths run the no-op guard arms.
// A backup possessing only a marker (no commit token written) sweeps it on
// discard — the fused token-parallel retention cannot apply (nothing to prove).
func TestDiscardMarkerOnlyBackupSweeps(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/J-MO"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	mp := filepath.Join(dir, ".inflight-MO-1.a1.b2")
	require.NoError(t, afero.WriteFile(fs, mp, nil, 0o644))
	b := &rescrapePosterBackup{fs: fs, markerPath: mp}
	b2 := &rescrapePosterBackup{fs: fs, commitPath: filepath.Join(dir, ".commit-MO-1.a1.b2"), markerPath: mp}
	b.discard() // marker-only: commitPath empty → marker sweeps
	b2.discard()
	_, err := fs.Stat(mp)
	assert.Error(t, err, "marker swept when no token evidence exists")
}

func TestRescrapePosterBackupEmptyPathGuards(t *testing.T) {
	(&rescrapePosterBackup{fs: afero.NewMemMapFs()}).discard()
	// markerPath set but both bak paths empty → the restore retention loop
	// hits the empty-leg continue before concluding "no legs remain".
	(&rescrapePosterBackup{fs: afero.NewMemMapFs(), markerPath: "/t/marker"}).restore(nil)
}

// codex cloud P1 (case-fold probes) — worker-side promote-pending scan must
// fence case-variant spellings exactly like exact ones.
func TestPromoteWitnessPending_FoldMatters(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/JPF"
	require.NoError(t, fs.MkdirAll(dir, 0o755))

	// payload with variant spelling:
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-abx-1.json"),
		[]byte(`{"poster_id":"abx-1","url":"https://x","result_id":"res-1"}`), 0o644))
	hit, err := promoteWitnessPendingCore(fs, dir, "ABX-1")
	require.NoError(t, err)
	assert.True(t, hit, "case-variant pending witness fences")

	// legacy contentless payload under a variant NAME fences via folded name:
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-abx-2.json"), []byte("{}"), 0o644))
	hit2, err := promoteWitnessPendingCore(fs, dir, "ABX-2")
	require.NoError(t, err)
	assert.True(t, hit2)

	// unrelated spelling: no fence:
	hit3, err := promoteWitnessPendingCore(fs, dir, "ZZZ-9")
	require.NoError(t, err)
	assert.False(t, hit3)

	// missing dir ⇒ no fence:
	hit4, err := promoteWitnessPendingCore(fs, "/tmp/posters/NOPE", "ABX-1")
	require.NoError(t, err)
	assert.False(t, hit4)
}

// And the conflict entry point still surfaces a typed admission conflict.
func TestPosterWitnessConflictCore_FoldFencesAdmission(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/JPA"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-abx-8.json"),
		[]byte(`{"poster_id":"abx-8","url":"https://x","result_id":"res-1"}`), 0o644))
	err := posterWitnessConflictCore(fs, "/tmp", "JPA", "ABX-8")
	require.Error(t, err)
	var cfe *EditAdmissionConflictError
	require.ErrorAs(t, err, &cfe)
}

// codex cloud P2: a wedged backup-leg removal keeps the marker AND token —
// deleting provenance while bytes persist strands the leg permanently and
// fences the family until manual cleanup.
func TestDiscardSkipsMarkerTokenWhenBackupRemoveWedges(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/posters/J-DW"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	b := parkCanonicalPosterPair(base, dir, "DW-1", 2)
	require.NoError(t, b.parkErr)
	require.NoError(t, afero.WriteFile(base, b.commitPath, []byte(`{"poster_id":"DW-1"}`), 0o644))
	// bake actual backup files so the wedged removals are observable
	require.NoError(t, afero.WriteFile(base, b.fullBak, []byte("old-full"), 0o644))
	require.NoError(t, afero.WriteFile(base, b.cropBak, []byte("old-crop"), 0o644))
	b.fs = removeExactFailFS{Fs: base, name: filepath.ToSlash(b.fullBak)}
	b.discard()
	_, mErr := base.Stat(b.markerPath)
	assert.NoError(t, mErr, "marker retained — parked full leg unsettled")
	_, tErr := base.Stat(b.commitPath)
	assert.NoError(t, tErr, "token retained alongside")
}

type readWedgeFS struct {
	afero.Fs
	suffix string
}

func (f readWedgeFS) Open(name string) (afero.File, error) {
	if strings.HasSuffix(filepath.ToSlash(name), f.suffix) {
		return nil, errors.New("read wedged")
	}
	return f.Fs.Open(name)
}

// codex cloud P2: a transient fingerprint read failure aborts the op and
// restores the parked pair WHILE the key is held — the canonical never keeps
// this op's uncommitted generation bytes, and the parked committed bytes are
// never discarded against a silent fingerprint gap.
func TestRescrapeFingerprintReadFailAbortsAndRestores(t *testing.T) {
	base := afero.NewMemMapFs()
	jobID := models.NewJobID()
	pdir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, base.MkdirAll(pdir, 0o755))
	require.NoError(t, afero.WriteFile(base, filepath.Join(pdir, "FPR-1-full.jpg"), []byte("committed-full"), 0o644))
	require.NoError(t, afero.WriteFile(base, filepath.Join(pdir, "FPR-1.jpg"), []byte("committed-crop"), 0o644))
	store := resultstore.New(1, []string{"f1.mp4"})
	seedFamilyResult(store, "f1.mp4", "res-1", "FPR-1", "")
	gen := &spyPosterGen{onGenerate: func() {
		_ = afero.WriteFile(base, filepath.Join(pdir, "FPR-1-full.jpg"), []byte("gen-full"), 0o644)
		_ = afero.WriteFile(base, filepath.Join(pdir, "FPR-1.jpg"), []byte("gen-crop"), 0o644)
	}}
	fs := readWedgeFS{Fs: base, suffix: "FPR-1.jpg"}
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{Movie: &models.Movie{ID: "FPR-1"}, Status: scrape.StatusCompleted}}
	inputs := rescrapePhaseInputs{
		WF: wf, ResultMap: store, Finder: store, JobID: jobID,
		PosterGen:  gen,
		EditLockFn: func(ids ...string) func() { return func() {} },
		Fs:         fs, TempDir: "/tmp",
	}
	phase := NewRescrapePhase()
	_, err := phase.Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "FPR-1", FilePath: "f1.mp4"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "poster fingerprint capture")
	full, ferr := afero.ReadFile(base, filepath.Join(pdir, "FPR-1-full.jpg"))
	require.NoError(t, ferr)
	assert.Equal(t, "committed-full", string(full), "this op's generation rewound under the key")
	crop, cerr := afero.ReadFile(base, filepath.Join(pdir, "FPR-1.jpg"))
	require.NoError(t, cerr)
	assert.Equal(t, "committed-crop", string(crop), "unfingerprinted leg rewound, never discarded")
}

type midScrapeEditWorkflow struct {
	*stubRescrapeWorkflow
	onScrape func()
}

func (w *midScrapeEditWorkflow) Scrape(ctx context.Context, cmd scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	if w.onScrape != nil {
		w.onScrape()
	}
	return w.stubRescrapeWorkflow.Scrape(ctx, cmd)
}

// codex cloud P1: the sentinel's provenance binds to the POST-scrape baseline
// read under the family key — an edit landing mid-scrape advances the durable
// revision and must NOT later mark a crashed generation as "committed".
func TestRescrapeSentinelBindsPostScrapeRevision(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	pdir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, fs.MkdirAll(pdir, 0o755))
	store := resultstore.New(1, []string{"f1.mp4"})
	seedFamilyResult(store, "f1.mp4", "res-1", "PR-1", "")
	pre, preErr := store.GetMovieResult("f1.mp4")
	require.NoError(t, preErr)
	require.Equal(t, uint64(1), pre.Revision, "pre-scrape baseline fixture")
	require.NoError(t, afero.WriteFile(fs, filepath.Join(pdir, "PR-1-full.jpg"), []byte("oldfull"), 0o644))

	var sentinelPrev uint64
	gen := &spyPosterGen{onGenerate: func() {
		entries, _ := afero.ReadDir(fs, pdir)
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), ".inflight-") {
				continue
			}
			if data, rerr := afero.ReadFile(fs, filepath.Join(pdir, e.Name())); rerr == nil {
				var meta inFlightMeta
				if json.Unmarshal(data, &meta) == nil {
					sentinelPrev = meta.PrevRevision
				}
			}
		}
	}}
	wf := &midScrapeEditWorkflow{
		stubRescrapeWorkflow: &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{Movie: &models.Movie{ID: "PR-1"}, Status: scrape.StatusCompleted}},
		onScrape: func() {
			// a concurrent (already-concluded) edit landing DURING the scrape:
			// legal (no key held yet), advances the durable revision pre-park.
			cur, uerr := store.GetMovieResult("f1.mp4")
			if uerr == nil && cur != nil {
				clone := cur.Clone()
				clone.Movie.Title = "Edited Mid-Scrape"
				store.UpdateFileResult("f1.mp4", clone)
			}
		},
	}
	inputs := rescrapePhaseInputs{
		WF: wf, ResultMap: store, Finder: store, JobID: jobID,
		PosterGen:  gen,
		EditLockFn: func(ids ...string) func() { return func() {} },
		Fs:         fs, TempDir: "/tmp",
	}
	phase := NewRescrapePhase()
	res, err := phase.Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "PR-1", FilePath: "f1.mp4"})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, uint64(2), sentinelPrev, "provenance binds the post-scrape (keyed) baseline, not the stale capture")
}

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

type createWedgeFS struct {
	afero.Fs
	contains string
}

func (f createWedgeFS) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if flag&os.O_CREATE != 0 && strings.Contains(name, f.contains) {
		return nil, errors.New("create wedged")
	}
	return f.Fs.OpenFile(name, flag, perm)
}

type mkdirWedgeFS struct{ afero.Fs }

func (mkdirWedgeFS) MkdirAll(string, os.FileMode) error { return errors.New("mkdirall wedged") }

// codex cloud P2: an un-writable in-flight sentinel must refuse generation —
// otherwise crop/download admission sees neither marker nor parked leg
// between key release and CAS commit.
func TestParkCanonicalPosterPairMarkerWriteFailRefusesGeneration(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/tmp/posters/JM", 0o755))
	fs := createWedgeFS{Fs: base, contains: ".inflight-"}
	b := parkCanonicalPosterPair(fs, "/tmp/posters/JM", "MK-1", 0)
	assert.Empty(t, b.markerPath)
	require.Error(t, b.parkErr, "marker write failure aborts generation")
	assert.Contains(t, b.parkErr.Error(), "in-flight marker write")
}

// codex cloud P2 companion: when the poster dir itself cannot be created no
// sentinel and no park are possible — refuse rather than run unfenced.
func TestParkCanonicalPosterPairMkdirFailRefusesGeneration(t *testing.T) {
	b := parkCanonicalPosterPair(mkdirWedgeFS{Fs: afero.NewMemMapFs()}, "/tmp/posters/JMD", "MK-2", 0)
	require.Error(t, b.parkErr)
	assert.Contains(t, b.parkErr.Error(), "poster backup dir")
	assert.Empty(t, b.markerPath)
}

func TestParkCanonicalPosterPairGuards(t *testing.T) {
	// nil fs / empty dir+id: no-ops, restore/discard safe
	bNil := parkCanonicalPosterPair(nil, "/x", "A-1", 0)
	bNil.restore(nil)
	bNil.discard()
	bEmpty := parkCanonicalPosterPair(afero.NewMemMapFs(), "", "", 0)
	bEmpty.restore(nil)
	bEmpty.discard()

	// stat error (non-ENOENT) fails CLOSED: leg marked had
	mem := afero.NewMemMapFs()
	require.NoError(t, mem.MkdirAll("/tmp/posters/J", 0o755))
	fsStatErr := statFailSuffixFS{Fs: mem, suffix: "PA-9-full.jpg"}
	bStat := parkCanonicalPosterPair(fsStatErr, "/tmp/posters/J", "PA-9", 0)
	assert.True(t, bStat.hadFull, "stat error => treated as pre-existing")
	assert.Error(t, bStat.parkErr, "stat error => refuse generation, no overwrite-without-restore window (local codex review P1)")
	assert.False(t, bStat.hadCrop)

	// park rename failure: warn only, leg untouched, had stays false
	mem2 := afero.NewMemMapFs()
	require.NoError(t, mem2.MkdirAll("/tmp/posters/J2", 0o755))
	require.NoError(t, afero.WriteFile(mem2, "/tmp/posters/J2/PA-9-full.jpg", []byte("f"), 0o644))
	require.NoError(t, afero.WriteFile(mem2, "/tmp/posters/J2/PA-9.jpg", []byte("c"), 0o644))
	fsRn := &seqRenameFailFS{Fs: mem2, failOn: map[int]bool{1: true}}
	bRn := parkCanonicalPosterPair(fsRn, "/tmp/posters/J2", "PA-9", 0)
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
	bPark := parkCanonicalPosterPair(mem3, "/tmp/posters/J3", "PA-9", 0)
	require.True(t, bPark.hadFull)
	bPark.fs = &seqRenameFailFS{Fs: mem3, failOn: map[int]bool{1: true}}
	bPark.restore(nil)
	_, bakErr := mem3.Stat(bPark.fullBak)
	assert.NoError(t, bakErr, "failed restore leaves parked bytes for salvage")

	// discard removes parked files
	mem4 := afero.NewMemMapFs()
	require.NoError(t, mem4.MkdirAll("/tmp/posters/J4", 0o755))
	require.NoError(t, afero.WriteFile(mem4, "/tmp/posters/J4/PA-9.jpg", []byte("c"), 0o644))
	bDisc := parkCanonicalPosterPair(mem4, "/tmp/posters/J4", "PA-9", 0)
	require.True(t, bDisc.hadCrop)
	bDisc.discard()
	_, dErr := mem4.Stat(bDisc.cropBak)
	assert.Error(t, dErr, "parked bytes discarded")

	// F-R4-4: two parks of the same ID never share a backup path; each
	// restores ITS bytes even after both parked.
	mem5 := afero.NewMemMapFs()
	require.NoError(t, mem5.MkdirAll("/tmp/posters/J5", 0o755))
	require.NoError(t, afero.WriteFile(mem5, "/tmp/posters/J5/PA-9.jpg", []byte("first"), 0o644))
	b1 := parkCanonicalPosterPair(mem5, "/tmp/posters/J5", "PA-9", 0)
	require.NoError(t, afero.WriteFile(mem5, "/tmp/posters/J5/PA-9.jpg", []byte("second"), 0o644))
	b2 := parkCanonicalPosterPair(mem5, "/tmp/posters/J5", "PA-9", 0)
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
