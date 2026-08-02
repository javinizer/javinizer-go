package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// idRekeyFixture builds a jobEditorImpl with a REAL poster manager over an
// in-memory fs (both poster assets present at the OLD movie key), a single
// completed result for oldID carrying a temp preview URL filed under that
// key, and provenance whose "dmm" source contributed a DIFFERENT movie ID.
// The returned fs and old-key paths let the test assert the cache migration
// byte-for-byte.
func idRekeyFixture(t *testing.T, jobID, oldID, newID string) (*jobEditorImpl, *resultstore.ResultTracker, afero.Fs, string, string, string) {
	t.Helper()
	filePath := "/source/" + oldID + ".mp4"
	tracker := resultstore.New(1, []string{filePath})
	tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
		ResultID:      "res-idrekey",
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: oldID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{
			ID:    oldID,
			Title: "Old Title",
			Poster: models.PosterState{
				PosterURL:        "https://old.invalid/poster.jpg",
				CroppedPosterURL: "/api/v1/temp/posters/" + jobID + "/" + oldID + ".jpg?v=111",
			},
		},
	})
	tracker.SetProvenance(filePath, &resultstore.ProvenanceData{
		FieldSources:   map[string]string{"id": "r18dev"},
		ScraperResults: []*models.ScraperResult{{Source: "dmm", ID: newID}, {Source: "r18dev", ID: oldID}},
	})

	fs := afero.NewMemMapFs()
	gen := poster.NewScrapePosterGenerator(poster.NewPosterManager(fs, "/temp", nil), "", "")
	oldFull := "/temp/posters/" + jobID + "/" + oldID + "-full.jpg"
	oldPreview := "/temp/posters/" + jobID + "/" + oldID + ".jpg"
	require.NoError(t, afero.WriteFile(fs, oldFull, []byte("old-full-bytes"), 0o644))
	require.NoError(t, afero.WriteFile(fs, oldPreview, []byte("old-preview-bytes"), 0o644))

	je := &jobEditorImpl{store: tracker, jobID: jobID, posterGen: gen}
	return je, tracker.(*resultstore.ResultTracker), fs, filePath, oldFull, oldPreview
}

var errIDRekeyPersist = errors.New("job repository unavailable")

func fileContents(t *testing.T, fs afero.Fs, path string) (string, bool) {
	t.Helper()
	data, err := afero.ReadFile(fs, path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// TestApplyFieldOverride_IDRekeyMigratesPosterAssets pins P3-6: an "id"
// override adopts the selected source's movie ID, and the cached poster
// assets must FOLLOW the key — under BOTH keys' poster-source locks (here
// the destination sorts BEFORE the origin, exercising the lexical
// release/re-acquire path) — instead of being orphaned at the old key.
// The persisted preview URL is re-pointed to the new key too.
func TestApplyFieldOverride_IDRekeyMigratesPosterAssets(t *testing.T) {
	const (
		jobID = "job-idrekey"
		oldID = "ORIG-ID9"
		newID = "DMM-NEW1" // sorts BEFORE the origin: the swap lock path
	)
	je, tracker, fs, filePath, oldFull, oldPreview := idRekeyFixture(t, jobID, oldID, newID)

	updated, _, err := je.ApplyFieldOverride(context.Background(), "res-idrekey", "id", "dmm")
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, newID, updated.Movie.ID, "the override adopted the source's movie ID")
	assert.Equal(t, "/api/v1/temp/posters/"+jobID+"/"+newID+".jpg?v=111",
		updated.Movie.Poster.CroppedPosterURL, "the preview URL follows the new key")

	// Old key freed, new key has the bytes.
	full, ok := fileContents(t, fs, "/temp/posters/"+jobID+"/"+newID+"-full.jpg")
	require.True(t, ok, "the full-size asset must exist at the new key")
	assert.Equal(t, "old-full-bytes", full)
	preview, ok := fileContents(t, fs, "/temp/posters/"+jobID+"/"+newID+".jpg")
	require.True(t, ok)
	assert.Equal(t, "old-preview-bytes", preview)
	_, ok = fileContents(t, fs, oldFull)
	assert.False(t, ok, "the old key's full-size asset is gone (no orphan)")
	_, ok = fileContents(t, fs, oldPreview)
	assert.False(t, ok, "the old key's preview is gone (no orphan)")

	// The store was re-indexed to the new ID.
	assert.Equal(t, newID, tracker.GetCurrentMovieID(filePath))
	assertPosterSourceLockFree(t, jobID, oldID)
	assertPosterSourceLockFree(t, jobID, newID)
}

// TestApplyFieldOverride_IDRekeyPersistFailureMovesAssetsBack pins the
// compensation half of P3-6: the envelope persist runs under the same held
// (origin, destination) lock pair, and its failure reverts the movie AND
// moves the assets back to the old key before the locks release.
func TestApplyFieldOverride_IDRekeyPersistFailureMovesAssetsBack(t *testing.T) {
	const (
		jobID = "job-idrekey2"
		oldID = "AAA-ORIG"
		newID = "ZZZ-NEW2" // sorts AFTER the origin: the stack-on-top lock path
	)
	je, tracker, fs, filePath, oldFull, oldPreview := idRekeyFixture(t, jobID, oldID, newID)
	je.persistEnvelope = func() error { return errIDRekeyPersist }

	_, _, err := je.ApplyFieldOverride(context.Background(), "res-idrekey", "id", "dmm")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEnvelopePersist)
	assert.ErrorIs(t, err, errIDRekeyPersist)

	// Memory reverted to the pre-override movie…
	restored, getErr := tracker.GetMovieResult(filePath)
	require.NoError(t, getErr)
	require.NotNil(t, restored.Movie)
	assert.Equal(t, oldID, restored.Movie.ID)
	assert.Equal(t, "/api/v1/temp/posters/"+jobID+"/"+oldID+".jpg?v=111",
		restored.Movie.Poster.CroppedPosterURL, "the preview URL reverts with the movie")

	// …and the assets moved BACK to the old key; the new key is freed.
	full, ok := fileContents(t, fs, oldFull)
	require.True(t, ok, "the full-size asset must be back at the old key")
	assert.Equal(t, "old-full-bytes", full)
	preview, ok := fileContents(t, fs, oldPreview)
	require.True(t, ok)
	assert.Equal(t, "old-preview-bytes", preview)
	_, ok = fileContents(t, fs, "/temp/posters/"+jobID+"/"+newID+"-full.jpg")
	assert.False(t, ok, "the new key must not keep the moved assets after compensation")
	_, ok = fileContents(t, fs, "/temp/posters/"+jobID+"/"+newID+".jpg")
	assert.False(t, ok)

	assertPosterSourceLockFree(t, jobID, oldID)
	assertPosterSourceLockFree(t, jobID, newID)
}

// TestApplyFieldOverride_IDRekeyWithoutAssetMoverDegradesToStateOnly covers
// the degrade: a generator without the move capability (test-stub style)
// leaves the re-key state-only — the override still applies and the preview
// URL is re-pointed, exactly the "no assets to move" contract.
func TestApplyFieldOverride_IDRekeyWithoutAssetMoverDegradesToStateOnly(t *testing.T) {
	filePath := "/source/ORIG-ID8.mp4"
	tracker := resultstore.New(1, []string{filePath})
	tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
		ResultID:      "res-idstub",
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "ORIG-ID8"},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: "ORIG-ID8", Title: "Old", Poster: models.PosterState{
			CroppedPosterURL: "/api/v1/temp/posters/job-stub/ORIG-ID8.jpg?v=7",
		}},
	})
	tracker.SetProvenance(filePath, &resultstore.ProvenanceData{
		FieldSources:   map[string]string{"id": "r18dev"},
		ScraperResults: []*models.ScraperResult{{Source: "dmm", ID: "DMM-STUB"}},
	})
	je := &jobEditorImpl{store: tracker, jobID: "job-stub", posterGen: &recordingPosterGen{}}

	updated, _, err := je.ApplyFieldOverride(context.Background(), "res-idstub", "id", "dmm")
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, "DMM-STUB", updated.Movie.ID)
	assert.Equal(t, "/api/v1/temp/posters/job-stub/DMM-STUB.jpg?v=7",
		updated.Movie.Poster.CroppedPosterURL)
	assert.Equal(t, "DMM-STUB", tracker.GetCurrentMovieID(filePath))
	assertPosterSourceLockFree(t, "job-stub", "ORIG-ID8")
	assertPosterSourceLockFree(t, "job-stub", "DMM-STUB")
}

// TestRewritePosterIDInPreviewURL pins the URL rewrite helper: only the file
// name segment under the old key is re-pointed; unrelated or empty URLs are
// untouched.
func TestRewritePosterIDInPreviewURL(t *testing.T) {
	assert.Equal(t,
		"/api/v1/temp/posters/job-1/NEW-1.jpg?v=42",
		rewritePosterIDInPreviewURL("/api/v1/temp/posters/job-1/OLD-1.jpg?v=42", "OLD-1", "NEW-1"))
	assert.Equal(t, "", rewritePosterIDInPreviewURL("", "OLD-1", "NEW-1"))
	assert.Equal(t, "https://cdn.example/x.jpg", rewritePosterIDInPreviewURL("https://cdn.example/x.jpg", "OLD-1", "NEW-1"),
		"a URL not filed under the old key is untouched")
	assert.Equal(t,
		"/api/v1/temp/posters/job-1/OLD-1.jpg?v=42",
		rewritePosterIDInPreviewURL("/api/v1/temp/posters/job-1/OLD-1.jpg?v=42", "OLD-1", ""),
		"an empty new ID leaves the URL alone")
	assert.Equal(t,
		"/api/v1/temp/posters/job-1/OLD-1-thumb.jpg",
		rewritePosterIDInPreviewURL("/api/v1/temp/posters/job-1/OLD-1-thumb.jpg", "OLD-1", "NEW-1"),
		"a prefix look-alike filename is not rewritten — only the exact {id}.jpg segment")
}

// moverStubGen is a poster generator with the move capability whose
// MovePosterAssets records (from,to) pairs and fails the failAt-th call
// (1-based, 0 = never) — used to drive the id-rekey forward-move and
// move-back compensation legs deterministically.
type moverStubGen struct {
	recordingPosterGen
	calls   [][2]string
	failAt  int
	failErr error
}

func (g *moverStubGen) MovePosterAssets(_, fromID, toID string) error {
	g.calls = append(g.calls, [2]string{fromID, toID})
	if g.failAt > 0 && g.failAt == len(g.calls) {
		return g.failErr
	}
	return nil
}

// TestApplyFieldOverride_IDRekeyMoveFailureRejectsOverride pins the forward
// half of P3-6: when the asset migration itself fails, the override is
// REJECTED before any part is persisted — the stored movie, the preview URL,
// and the store indexing stay at the old key.
func TestApplyFieldOverride_IDRekeyMoveFailureRejectsOverride(t *testing.T) {
	const (
		jobID = "job-idmovefail"
		oldID = "ORIG-ID7"
		newID = "DMM-NEW7"
	)
	je, tracker, fs, filePath, oldFull, _ := idRekeyFixture(t, jobID, oldID, newID)
	gen := &moverStubGen{failAt: 1, failErr: errors.New("fs jammed")}
	je.posterGen = gen

	_, _, err := je.ApplyFieldOverride(context.Background(), "res-idrekey", "id", "dmm")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "migrate poster assets to re-keyed movie "+newID)
	assert.Contains(t, err.Error(), "fs jammed")
	assert.Equal(t, [][2]string{{oldID, newID}}, gen.calls,
		"only the forward move was attempted — the override aborted before persisting")

	stored, getErr := tracker.GetMovieResult(filePath)
	require.NoError(t, getErr)
	require.NotNil(t, stored.Movie)
	assert.Equal(t, oldID, stored.Movie.ID, "the stored movie is untouched when the migration rejects")
	assert.Equal(t, oldID, tracker.GetCurrentMovieID(filePath))
	_, ok := fileContents(t, fs, oldFull)
	assert.True(t, ok, "the fs assets were untouched (the failing stub mover wrote nothing)")
	assertPosterSourceLockFree(t, jobID, oldID)
	assertPosterSourceLockFree(t, jobID, newID)
}

// TestApplyFieldOverride_IDRekeyFanoutFailureMovesAssetsBack pins the
// fan-out-failure compensation half: a part persist failure AFTER the
// successful forward move moves the assets back and, when the move-back
// itself fails, says so on the surfaced error.
func TestApplyFieldOverride_IDRekeyFanoutFailureMovesAssetsBack(t *testing.T) {
	const (
		jobID = "job-idfanoutfail"
		oldID = "ORIG-ID6"
		newID = "DMM-NEW6"
	)
	newFixture := func(t *testing.T) (*jobEditorImpl, *resultstore.ResultTracker, string) {
		je, tracker, _, filePath, _, _ := idRekeyFixture(t, jobID, oldID, newID)
		je.store = &overrideFailStore{
			Store: tracker,
			failUpdate: map[string]func(m *models.Movie) error{
				filePath: func(m *models.Movie) error { return errors.New("disk full") },
			},
		}
		return je, tracker, filePath
	}

	t.Run("move-back succeeds", func(t *testing.T) {
		je, tracker, filePath := newFixture(t)
		gen := &moverStubGen{} // never fails
		je.posterGen = gen

		_, _, err := je.ApplyFieldOverride(context.Background(), "res-idrekey", "id", "dmm")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "persist field override: disk full")
		assert.NotContains(t, err.Error(), "move-back")
		assert.Equal(t, [][2]string{{oldID, newID}, {newID, oldID}}, gen.calls,
			"the compensation moves the assets back to the origin key")
		assert.Equal(t, oldID, tracker.GetCurrentMovieID(filePath))
	})

	t.Run("move-back failure is surfaced", func(t *testing.T) {
		je, _, _ := newFixture(t)
		gen := &moverStubGen{failAt: 2, failErr: errors.New("restore jammed")}
		je.posterGen = gen

		_, _, err := je.ApplyFieldOverride(context.Background(), "res-idrekey", "id", "dmm")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "persist field override: disk full")
		assert.Contains(t, err.Error(), "poster asset move-back failed: restore jammed",
			"a failed move-back rides along instead of being swallowed")
		assert.Equal(t, [][2]string{{oldID, newID}, {newID, oldID}}, gen.calls)
	})
}

// TestApplyFieldOverride_PersistFailureRevertErrorAndNilProvenance pins two
// compensation branches: a part whose revert ITSELF fails reports the revert
// error with the persist error, and a part with NO prior provenance is
// skipped during the provenance restore (provenance cannot be unset).
func TestApplyFieldOverride_PersistFailureRevertErrorAndNilProvenance(t *testing.T) {
	const (
		jobID   = "job-reverrfail"
		movieID = "REV-001"
		part1   = "rev1.mp4"
		part2   = "rev2.mp4"
	)
	tracker := resultstore.New(1, []string{part1, part2})
	for i, fp := range []string{part1, part2} {
		resultID := "res-rev"
		if i == 1 {
			resultID = "res-rev-2"
		}
		tracker.UpdateFileResult(fp, &resultstore.MovieResult{
			ResultID:      resultID,
			FileMatchInfo: models.FileMatchInfo{Path: fp, MovieID: movieID},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: movieID, Maker: "OrigMaker"},
		})
	}
	// Only part1 carries provenance; part2 has NONE — its compensation
	// restore must hit the nil-provenance skip branch.
	tracker.SetProvenance(part1, &resultstore.ProvenanceData{
		FieldSources:   map[string]string{"maker": "r18dev"},
		ScraperResults: []*models.ScraperResult{{Source: "dmm", Maker: "DMMMaker"}, {Source: "r18dev", Maker: "R18Maker"}},
	})
	store := &overrideFailStore{
		Store: tracker,
		failUpdate: map[string]func(m *models.Movie) error{
			// Fail part2's REVERT (the compensation half): the fan-out write
			// carries the overridden maker, the revert restores "OrigMaker".
			part2: func(m *models.Movie) error {
				if m.Maker != "DMMMaker" {
					return errors.New("revert exploded")
				}
				return nil
			},
		},
	}
	je := &jobEditorImpl{
		store:           store,
		jobID:           jobID,
		persistEnvelope: func() error { return errIDRekeyPersist },
	}

	_, _, err := je.ApplyFieldOverride(context.Background(), "res-rev", "maker", "dmm")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEnvelopePersist)
	assert.Contains(t, err.Error(), "override revert failed",
		"the revert failure must ride along with the persist error")
	assert.Contains(t, err.Error(), "revert of part "+part2)

	// Part1 still reverted fully (no short-circuit), part2 is stranded on the
	// override (surfaced via the error above) — and provenance restore ran for
	// part1 only.
	r1, getErr := tracker.GetMovieResult(part1)
	require.NoError(t, getErr)
	assert.Equal(t, "OrigMaker", r1.Movie.Maker)
	prov := tracker.GetProvenance(part1)
	require.NotNil(t, prov)
	assert.Equal(t, "r18dev", prov.FieldSources["maker"], "part1's attribution is restored")
	part2Prov := tracker.GetProvenance(part2)
	require.NotNil(t, part2Prov)
	assert.Equal(t, "dmm", part2Prov.FieldSources["maker"],
		"part2 never had provenance — the nil-snapshot branch skips it, so the fan-out stays (provenance cannot be unset)")
	assertPosterSourceLockFree(t, jobID, movieID)
}

// TestApplyFieldOverride_PersistFailureMoveBackError pins the last asset leg
// of the persist-failure compensation: a failed move-back joins the surfaced
// error while the state/provenance reverts still complete.
func TestApplyFieldOverride_PersistFailureMoveBackError(t *testing.T) {
	const (
		jobID = "job-idmovebackfail"
		oldID = "ORIG-ID5"
		newID = "DMM-NEW5"
	)
	je, tracker, _, filePath, _, _ := idRekeyFixture(t, jobID, oldID, newID)
	gen := &moverStubGen{failAt: 2, failErr: errors.New("restore jammed")}
	je.posterGen = gen
	je.persistEnvelope = func() error { return errIDRekeyPersist }

	_, _, err := je.ApplyFieldOverride(context.Background(), "res-idrekey", "id", "dmm")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEnvelopePersist)
	assert.Contains(t, err.Error(), "override revert failed")
	assert.Contains(t, err.Error(), "poster asset move-back failed: restore jammed")
	assert.Equal(t, [][2]string{{oldID, newID}, {newID, oldID}}, gen.calls)

	restored, getErr := tracker.GetMovieResult(filePath)
	require.NoError(t, getErr)
	assert.Equal(t, oldID, restored.Movie.ID, "memory still reverts when the move-back fails")
}
