package worker

import (
	"context"
	"errors"
	"fmt"
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
		// simulate the failed generation writing new bytes over canonical
		require.NoError(t, afero.WriteFile(fs, refCanon, []byte("loser-bytes"), 0o644))
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

// --- guard/backup matrix for poster_cleanup.go + predicate edges ---

func TestParkCanonicalPosterPairGuards(t *testing.T) {
	// nil fs / empty dir+id: no-ops, restore/discard safe
	bNil := parkCanonicalPosterPair(nil, "/x", "A-1")
	bNil.restore()
	bNil.discard()
	bEmpty := parkCanonicalPosterPair(afero.NewMemMapFs(), "", "")
	bEmpty.restore()
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
	bSkip.restore()

	// restore rename failure warns and keeps the bak
	mem3 := afero.NewMemMapFs()
	require.NoError(t, mem3.MkdirAll("/tmp/posters/J3", 0o755))
	require.NoError(t, afero.WriteFile(mem3, "/tmp/posters/J3/PA-9-full.jpg", []byte("live"), 0o644))
	bPark := parkCanonicalPosterPair(mem3, "/tmp/posters/J3", "PA-9")
	require.True(t, bPark.hadFull)
	bPark.fs = &seqRenameFailFS{Fs: mem3, failOn: map[int]bool{1: true}}
	bPark.restore()
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
	b1.restore()
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
