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
// completed result for oldID carrying temp preview URLs filed under that
// key (CroppedPosterURL AND OriginalCroppedPosterURL — the poster reset
// flow reads the latter), and provenance whose "dmm" source contributed a
// DIFFERENT movie ID. The returned fs and old-key paths let the test
// assert the cache migration byte-for-byte.
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
				PosterURL:                "https://old.invalid/poster.jpg",
				CroppedPosterURL:         "/api/v1/temp/posters/" + jobID + "/" + oldID + ".jpg?v=111",
				OriginalCroppedPosterURL: "/api/v1/temp/posters/" + jobID + "/" + oldID + ".jpg?v=orig7",
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
	assert.Equal(t, "/api/v1/temp/posters/"+jobID+"/"+newID+".jpg?v=orig7",
		updated.Movie.Poster.OriginalCroppedPosterURL,
		"the ORIGINAL preview URL follows the new key too — staling it would 404 the poster reset flow")

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
	assert.Equal(t, "/api/v1/temp/posters/"+jobID+"/"+oldID+".jpg?v=orig7",
		restored.Movie.Poster.OriginalCroppedPosterURL, "the original preview URL reverts too")

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
// name segment under the old key of a RELATIVE /api/v1/temp/posters/ URL is
// re-pointed; unrelated or empty URLs are untouched. F3: the match is
// anchored to the temp-preview prefix, so a scraper-provided URL that merely
// ENDS with {oldID}.jpg is never rewritten (the reset flow would otherwise
// lose the remote source).
func TestRewritePosterIDInPreviewURL(t *testing.T) {
	assert.Equal(t,
		"/api/v1/temp/posters/job-1/NEW-1.jpg?v=42",
		RewritePosterIDInPreviewURL("/api/v1/temp/posters/job-1/OLD-1.jpg?v=42", "OLD-1", "NEW-1"))
	assert.Equal(t, "", RewritePosterIDInPreviewURL("", "OLD-1", "NEW-1"))
	assert.Equal(t, "https://cdn.example/x.jpg", RewritePosterIDInPreviewURL("https://cdn.example/x.jpg", "OLD-1", "NEW-1"),
		"a URL not filed under the old key is untouched")
	assert.Equal(t,
		"/api/v1/temp/posters/job-1/OLD-1.jpg?v=42",
		RewritePosterIDInPreviewURL("/api/v1/temp/posters/job-1/OLD-1.jpg?v=42", "OLD-1", ""),
		"an empty new ID leaves the URL alone")
	assert.Equal(t,
		"/api/v1/temp/posters/job-1/OLD-1-thumb.jpg",
		RewritePosterIDInPreviewURL("/api/v1/temp/posters/job-1/OLD-1-thumb.jpg", "OLD-1", "NEW-1"),
		"a prefix look-alike filename is not rewritten — only the exact {id}.jpg segment")
	// F3 anchoring: the match must require the /api/v1/temp/posters/ prefix on
	// the path portion of a relative URL.
	assert.Equal(t,
		"https://pics.example.co/st/OLD-1.jpg",
		RewritePosterIDInPreviewURL("https://pics.example.co/st/OLD-1.jpg", "OLD-1", "NEW-1"),
		"a scraper URL that merely ends with {oldID}.jpg is remote content, not a cache key — untouched")
	assert.Equal(t,
		"https://mirror.example/api/v1/temp/posters/job-1/OLD-1.jpg",
		RewritePosterIDInPreviewURL("https://mirror.example/api/v1/temp/posters/job-1/OLD-1.jpg", "OLD-1", "NEW-1"),
		"an absolute URL embedding the temp prefix under another host is untouched")
	assert.Equal(t,
		"/images/OLD-1.jpg",
		RewritePosterIDInPreviewURL("/images/OLD-1.jpg", "OLD-1", "NEW-1"),
		"a relative URL outside the temp preview namespace is untouched")
	assert.Equal(t,
		"/api/v1/temp/posters/job-1/x.jpg?v=/OLD-1.jpg",
		RewritePosterIDInPreviewURL("/api/v1/temp/posters/job-1/x.jpg?v=/OLD-1.jpg", "OLD-1", "NEW-1"),
		"a look-alike segment in the query string is never consulted")
}

// moverStubGen is a poster generator with the move AND snapshot
// capabilities. MovePosterAssets records (from,to) pairs and fails
// deterministically: failAt fails exactly the failAt-th call (1-based, 0 =
// never); failFrom fails every call from failFrom onward. Snapshots succeed
// (no assets held) unless snapshotErr names the movieID whose snapshot
// fails; restores are counted and can fail via restoreErr. Used to drive the
// id-rekey forward-move, the immediate snapshot-based partial-move reversal,
// and the move-back compensation legs deterministically.
type moverStubGen struct {
	recordingPosterGen
	calls       [][2]string
	snapshotErr map[string]error
	restores    int
	restoreErr  error
	failAt      int
	failFrom    int
	failErr     error
}

func (g *moverStubGen) MovePosterAssets(_, fromID, toID string) error {
	g.calls = append(g.calls, [2]string{fromID, toID})
	if g.failFrom > 0 && len(g.calls) >= g.failFrom {
		return g.failErr
	}
	if g.failAt > 0 && g.failAt == len(g.calls) {
		return g.failErr
	}
	return nil
}

// SnapshotPosterAssets satisfies posterAssetSnapshooter so the migration
// preflights; a nil snapshot mirrors a generator whose manager holds no
// assets for the key.
func (g *moverStubGen) SnapshotPosterAssets(_, movieID string) (*poster.AssetsSnapshot, error) {
	if err, ok := g.snapshotErr[movieID]; ok {
		return nil, err
	}
	return nil, nil
}

// RestorePosterAssets records the immediate reversal (two restores per
// failed forward move: destination first, then origin) and can fail it.
func (g *moverStubGen) RestorePosterAssets(_ *poster.AssetsSnapshot) error {
	g.restores++
	return g.restoreErr
}

// moverOnlyStubGen has the move capability WITHOUT the snapshot capability:
// MigratePosterCacheAssets cannot preflight it, so a forward failure must be
// left in place and reported, never reversed via the hazardous opposite
// re-key.
type moverOnlyStubGen struct {
	recordingPosterGen
	calls   [][2]string
	failErr error
}

func (g *moverOnlyStubGen) MovePosterAssets(_, fromID, toID string) error {
	g.calls = append(g.calls, [2]string{fromID, toID})
	if g.failErr != nil && len(g.calls) == 1 {
		return g.failErr
	}
	return nil
}

// TestApplyFieldOverride_IDRekeyMoveFailureRejectsOverride pins the forward
// half of P3-6 together with the audit-5 F1 reversal contract: when the
// asset migration fails, the override is REJECTED before any part is
// persisted — the stored movie, the preview URL, and the store indexing
// stay at the old key — and the possibly-partial move (MoveAssets joins
// per-leg errors instead of short-circuiting) is reversed IMMEDIATELY by
// replaying BOTH keys' pre-move snapshots (destination first, then origin),
// never by a hazardous opposite re-key.
func TestApplyFieldOverride_IDRekeyMoveFailureRejectsOverride(t *testing.T) {
	const (
		jobID = "job-idmovefail"
		oldID = "ORIG-ID7"
		newID = "DMM-NEW7"
	)

	t.Run("partial move legs reversed immediately via snapshots", func(t *testing.T) {
		je, tracker, fs, filePath, oldFull, _ := idRekeyFixture(t, jobID, oldID, newID)
		gen := &moverStubGen{failAt: 1, failErr: errors.New("fs jammed")}
		je.posterGen = gen

		_, _, err := je.ApplyFieldOverride(context.Background(), "res-idrekey", "id", "dmm")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "migrate poster assets to re-keyed movie "+newID)
		assert.Contains(t, err.Error(), "fs jammed")
		assert.NotContains(t, err.Error(), "partial move reversal failed",
			"the immediate reversal succeeded, so no reversal failure rides along")
		assert.Equal(t, [][2]string{{oldID, newID}}, gen.calls,
			"F1: the forward failure is compensated WITHOUT an opposite re-key call")
		assert.Equal(t, 2, gen.restores,
			"the reversal replays the destination snapshot, then the origin one")

		stored, getErr := tracker.GetMovieResult(filePath)
		require.NoError(t, getErr)
		require.NotNil(t, stored.Movie)
		assert.Equal(t, oldID, stored.Movie.ID, "the stored movie is untouched when the migration rejects")
		assert.Equal(t, oldID, tracker.GetCurrentMovieID(filePath))
		_, ok := fileContents(t, fs, oldFull)
		assert.True(t, ok, "the fs assets were untouched (the failing stub mover wrote nothing)")
		assertPosterSourceLockFree(t, jobID, oldID)
		assertPosterSourceLockFree(t, jobID, newID)
	})

	t.Run("reversal failure is joined onto the error", func(t *testing.T) {
		je, tracker, _, filePath, _, _ := idRekeyFixture(t, jobID, oldID, newID)
		gen := &moverStubGen{failAt: 1, failErr: errors.New("fs jammed"), restoreErr: errors.New("restore jammed")}
		je.posterGen = gen

		_, _, err := je.ApplyFieldOverride(context.Background(), "res-idrekey", "id", "dmm")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "migrate poster assets to re-keyed movie "+newID)
		assert.Contains(t, err.Error(), "partial move reversal failed",
			"a failed immediate reversal must surface, not be swallowed")
		assert.Contains(t, err.Error(), "restore jammed")
		assert.Equal(t, [][2]string{{oldID, newID}}, gen.calls)
		assert.Equal(t, 2, gen.restores, "both snapshot restores are attempted even when they fail")

		stored, getErr := tracker.GetMovieResult(filePath)
		require.NoError(t, getErr)
		assert.Equal(t, oldID, stored.Movie.ID, "the stored movie is untouched either way")
		assertPosterSourceLockFree(t, jobID, oldID)
		assertPosterSourceLockFree(t, jobID, newID)
	})

	t.Run("snapshot failure fails closed before the move", func(t *testing.T) {
		t.Run("origin", func(t *testing.T) {
			je, tracker, fs, filePath, oldFull, oldPreview := idRekeyFixture(t, jobID, oldID, newID)
			gen := &moverStubGen{snapshotErr: map[string]error{oldID: errors.New("snapshot jammed")}}
			je.posterGen = gen

			_, _, err := je.ApplyFieldOverride(context.Background(), "res-idrekey", "id", "dmm")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "snapshot origin poster assets before re-key")
			assert.Contains(t, err.Error(), "snapshot jammed")
			assert.Empty(t, gen.calls,
				"a move against an un-capturable pre-state has no honest reversal — it must not start")

			stored, getErr := tracker.GetMovieResult(filePath)
			require.NoError(t, getErr)
			assert.Equal(t, oldID, stored.Movie.ID, "the stored movie is untouched when the preflight rejects")
			assert.Equal(t, oldID, tracker.GetCurrentMovieID(filePath))
			full, ok := fileContents(t, fs, oldFull)
			require.True(t, ok)
			assert.Equal(t, "old-full-bytes", full)
			preview, ok := fileContents(t, fs, oldPreview)
			require.True(t, ok)
			assert.Equal(t, "old-preview-bytes", preview)
			assertPosterSourceLockFree(t, jobID, oldID)
			assertPosterSourceLockFree(t, jobID, newID)
		})

		t.Run("destination", func(t *testing.T) {
			je, _, fs, _, oldFull, oldPreview := idRekeyFixture(t, jobID, oldID, newID)
			gen := &moverStubGen{snapshotErr: map[string]error{newID: errors.New("snapshot jammed")}}
			je.posterGen = gen

			_, _, err := je.ApplyFieldOverride(context.Background(), "res-idrekey", "id", "dmm")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "snapshot destination poster assets before re-key")
			assert.Contains(t, err.Error(), "snapshot jammed")
			assert.Empty(t, gen.calls, "the move must not start without a destination pre-state")

			full, ok := fileContents(t, fs, oldFull)
			require.True(t, ok)
			assert.Equal(t, "old-full-bytes", full)
			preview, ok := fileContents(t, fs, oldPreview)
			require.True(t, ok)
			assert.Equal(t, "old-preview-bytes", preview)
			assertPosterSourceLockFree(t, jobID, oldID)
			assertPosterSourceLockFree(t, jobID, newID)
		})
	})

	t.Run("mover without snapshot capability is not reversed unsafely", func(t *testing.T) {
		je, tracker, _, filePath, _, _ := idRekeyFixture(t, jobID, oldID, newID)
		gen := &moverOnlyStubGen{failErr: errors.New("fs jammed")}
		je.posterGen = gen

		_, _, err := je.ApplyFieldOverride(context.Background(), "res-idrekey", "id", "dmm")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "migrate poster assets to re-keyed movie "+newID)
		assert.Contains(t, err.Error(), "no asset snapshot capability",
			"without pre-move snapshots a possibly partial move is reported, not reverse-re-keyed")
		assert.Equal(t, [][2]string{{oldID, newID}}, gen.calls,
			"the forward move ran once and NO hazardous opposite re-key was attempted")

		stored, getErr := tracker.GetMovieResult(filePath)
		require.NoError(t, getErr)
		assert.Equal(t, oldID, stored.Movie.ID, "the stored movie is untouched")
		assertPosterSourceLockFree(t, jobID, oldID)
		assertPosterSourceLockFree(t, jobID, newID)
	})
}

// TestApplyFieldOverride_IDRekeyFanoutFailureMovesAssetsBack pins the
// fan-out-failure compensation half: a part persist failure AFTER the
// successful forward move rewinds the assets and, when the rewind itself
// fails, says so on the surfaced error. With a snapshot-capable generator
// the rewind is a pure snapshot replay (destination, then origin — no
// opposite re-key call), per audit-6 F-new.
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
		assert.Equal(t, [][2]string{{oldID, newID}}, gen.calls,
			"F-new: the compensation is a snapshot replay — NO opposite re-key call")
		assert.Equal(t, 2, gen.restores,
			"both pre-move snapshots replay: destination first, then origin")
		assert.Equal(t, oldID, tracker.GetCurrentMovieID(filePath))
	})

	t.Run("move-back failure is surfaced", func(t *testing.T) {
		je, _, _ := newFixture(t)
		gen := &moverStubGen{restoreErr: errors.New("restore jammed")}
		je.posterGen = gen

		_, _, err := je.ApplyFieldOverride(context.Background(), "res-idrekey", "id", "dmm")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "persist field override: disk full")
		assert.Contains(t, err.Error(), "poster asset move-back failed: restore jammed",
			"a failed snapshot replay rides along instead of being swallowed")
		assert.Equal(t, [][2]string{{oldID, newID}}, gen.calls)
		assert.Equal(t, 2, gen.restores, "both snapshot restores are attempted even when they fail")
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
// of the persist-failure compensation: a failed snapshot replay joins the
// surfaced error while the state/provenance reverts still complete.
func TestApplyFieldOverride_PersistFailureMoveBackError(t *testing.T) {
	const (
		jobID = "job-idmovebackfail"
		oldID = "ORIG-ID5"
		newID = "DMM-NEW5"
	)
	je, tracker, _, filePath, _, _ := idRekeyFixture(t, jobID, oldID, newID)
	gen := &moverStubGen{restoreErr: errors.New("restore jammed")}
	je.posterGen = gen
	je.persistEnvelope = func() error { return errIDRekeyPersist }

	_, _, err := je.ApplyFieldOverride(context.Background(), "res-idrekey", "id", "dmm")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEnvelopePersist)
	assert.Contains(t, err.Error(), "override revert failed")
	assert.Contains(t, err.Error(), "poster asset move-back failed: restore jammed")
	assert.Equal(t, [][2]string{{oldID, newID}}, gen.calls,
		"F-new: the compensation replays snapshots — no opposite re-key")
	assert.Equal(t, 2, gen.restores)

	restored, getErr := tracker.GetMovieResult(filePath)
	require.NoError(t, getErr)
	assert.Equal(t, oldID, restored.Movie.ID, "memory still reverts when the move-back fails")
}

// TestApplyFieldOverride_IDRekeyPlanFailureMovesAssetsBack pins F1(a): when
// the multipart fan-out planner fails AFTER the id-rekey asset migration
// already completed, the assets must be moved BACK to the origin key before
// the override rejects — otherwise they are stranded at the destination key
// while the persisted state still resolves the old one (and a destination
// key owned by another result would serve this movie's images to its
// crop/preview lookups). No stored-state combination makes an id-rekey
// merge fail naturally (every part merges against the same provenance
// envelope the selected part already validated), so the planner failure is
// injected through the planOverrideFn seam.
func TestApplyFieldOverride_IDRekeyPlanFailureMovesAssetsBack(t *testing.T) {
	const (
		jobID = "job-idplanfail"
		oldID = "ORIG-ID4"
		newID = "DMM-NEW4"
	)
	planErr := errors.New("planner exploded")

	t.Run("assets return to origin, destination key left empty", func(t *testing.T) {
		je, tracker, fs, filePath, oldFull, oldPreview := idRekeyFixture(t, jobID, oldID, newID)
		je.planOverrideFn = func(_ []string, _ *models.Movie, _ *resultstore.ProvenanceData, _, _ string) ([]overridePartWrite, error) {
			return nil, planErr
		}

		_, _, err := je.ApplyFieldOverride(context.Background(), "res-idrekey", "id", "dmm")
		require.Error(t, err)
		assert.ErrorIs(t, err, planErr)
		assert.NotContains(t, err.Error(), "move-back",
			"the move-back succeeded, so no reversal failure rides along")

		// Origin restored byte-for-byte; the destination key holds NOTHING —
		// nothing may remain filed under the new key when state stays at A.
		full, ok := fileContents(t, fs, oldFull)
		require.True(t, ok, "the full-size asset must be back at the old key")
		assert.Equal(t, "old-full-bytes", full)
		preview, ok := fileContents(t, fs, oldPreview)
		require.True(t, ok)
		assert.Equal(t, "old-preview-bytes", preview)
		_, ok = fileContents(t, fs, "/temp/posters/"+jobID+"/"+newID+"-full.jpg")
		assert.False(t, ok, "the new key must not keep stranded assets after the rejected override")
		_, ok = fileContents(t, fs, "/temp/posters/"+jobID+"/"+newID+".jpg")
		assert.False(t, ok)

		// The persisted state never moved: still the old key, old preview URLs.
		stored, getErr := tracker.GetMovieResult(filePath)
		require.NoError(t, getErr)
		require.NotNil(t, stored.Movie)
		assert.Equal(t, oldID, stored.Movie.ID)
		assert.Equal(t, "/api/v1/temp/posters/"+jobID+"/"+oldID+".jpg?v=111",
			stored.Movie.Poster.CroppedPosterURL)
		assert.Equal(t, oldID, tracker.GetCurrentMovieID(filePath))
		assertPosterSourceLockFree(t, jobID, oldID)
		assertPosterSourceLockFree(t, jobID, newID)
	})

	t.Run("move-back failure is surfaced with the plan error", func(t *testing.T) {
		je, _, _, _, _, _ := idRekeyFixture(t, jobID, oldID, newID)
		gen := &moverStubGen{restoreErr: errors.New("restore jammed")}
		je.posterGen = gen
		je.planOverrideFn = func(_ []string, _ *models.Movie, _ *resultstore.ProvenanceData, _, _ string) ([]overridePartWrite, error) {
			return nil, planErr
		}

		_, _, err := je.ApplyFieldOverride(context.Background(), "res-idrekey", "id", "dmm")
		require.Error(t, err)
		assert.ErrorIs(t, err, planErr)
		assert.Contains(t, err.Error(), "poster asset move-back failed: restore jammed",
			"a failed plan-failure snapshot replay must surface, not be swallowed")
		assert.Equal(t, [][2]string{{oldID, newID}}, gen.calls,
			"F-new: the compensation replays snapshots — no opposite re-key")
		assert.Equal(t, 2, gen.restores)
		assertPosterSourceLockFree(t, jobID, oldID)
		assertPosterSourceLockFree(t, jobID, newID)
	})
}
