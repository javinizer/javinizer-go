package workflow

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
)

// codex P2 (PRRT_kwDORn9KaM6nHyl5): an epoch-dated source must journal its
// hard-link pin with presence explicit — the identity_pinned marker, never
// the zero mtime as a sentinel — on BOTH admission reading paths (the real
// filesystem's strong tuple and the in-memory filesystem's metadata-only
// weak posture), and the pin must decode as present at every recovery seam.
func TestDeferredHardLinkPrimaryPinEpochMtimeCarriesPresenceMarker(t *testing.T) {
	epoch := time.Unix(0, 0)
	roundTrip := func(t *testing.T, entry models.DeleteEntry) models.DeleteEntry {
		t.Helper()
		gf, err := models.ParseGeneratedFiles(models.MarshalLedgerJSON(models.GeneratedFilesJSON{PlannedDeletes: []models.DeleteEntry{entry}}))
		require.NoError(t, err, "the production ledger serialization round-trips")
		require.Len(t, gf.PlannedDeletes, 1)
		return gf.PlannedDeletes[0]
	}

	t.Run("real filesystem strong tuple", func(t *testing.T) {
		fs := afero.NewOsFs()
		source := filepath.Join(t.TempDir(), "restored.mkv")
		require.NoError(t, afero.WriteFile(fs, source, []byte("restored video"), 0o644))
		require.NoError(t, os.Chtimes(source, epoch, epoch))
		srcInfo, err := fs.Stat(source)
		require.NoError(t, err)
		require.Zero(t, srcInfo.ModTime().Unix(), "the freshly restored file reads as epoch-dated")

		stage := &artifactStage{fs: fs, sourcePath: source}
		stage.original.Organize.LinkMode = organizer.LinkModeHard
		stage.sourceIdentity = captureArtifactSourceIdentity(fs, source, srcInfo)
		target := filepath.Join(t.TempDir(), "restored.mkv")
		entry, err := stage.deferredPrimaryDeleteEntry(&organizer.OrganizePlan{SourcePath: source, TargetPath: target})
		require.NoError(t, err)
		assert.True(t, entry.IdentityPinned, "the pin carries the explicit presence marker — never the timestamp as a sentinel")
		assert.True(t, entry.IdentityStrong, "OsFs pins strongly")
		assert.Zero(t, entry.IdentityModUnix, "the epoch tuple serializes as zero — the sentinel shape that orphaned recovery")
		assert.Greater(t, entry.IdentityDev+entry.IdentityIno, uint64(0), "the kernel pair materialized")

		decoded := roundTrip(t, entry)
		assert.True(t, decoded.HasIdentityPin(), "the journal decode recovers the pin's presence off the marker")
		assert.Equal(t, entry, decoded, "the round-trip is byte-faithful")
	})

	t.Run("in-memory weak metadata legs", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/restored.mkv", []byte("restored video"), 0o644))
		require.NoError(t, fs.Chtimes("/restored.mkv", epoch, epoch))
		srcInfo, err := fs.Stat("/restored.mkv")
		require.NoError(t, err)
		require.Zero(t, srcInfo.ModTime().Unix())

		stage := &artifactStage{fs: fs, sourcePath: "/restored.mkv"}
		stage.original.Organize.LinkMode = organizer.LinkModeHard
		stage.sourceIdentity = captureArtifactSourceIdentity(fs, "/restored.mkv", srcInfo)
		require.False(t, stage.sourceIdentity.hasDevIno, "memfs keeps only the metadata legs — the weak posture the epoch sentinel could never express")
		entry, err := stage.deferredPrimaryDeleteEntry(&organizer.OrganizePlan{SourcePath: "/restored.mkv", TargetPath: "/out/restored.mkv"})
		require.NoError(t, err)
		assert.True(t, entry.IdentityPinned)
		assert.False(t, entry.IdentityStrong)
		assert.Zero(t, entry.IdentityModUnix)
		assert.True(t, entry.HasIdentityPin(), "a weak epoch pin is now expressible — pre-marker code lost this shape outright")

		decoded := roundTrip(t, entry)
		assert.True(t, decoded.HasIdentityPin())
		assert.Equal(t, entry, decoded)
	})
}

// codex P2 (PRRT_kwDORn9KaM6nHyl5) crash-replay E2E on the finding's exact
// window: a LinkModeHard deferred publication of a freshly restored
// (epoch-mtime) source exits after the link landed but before the reconcile.
// The pin comes from the PRODUCTION producer and rides the production ledger
// and reverter; recovery must consume the linked name (no orphaned
// destination to block the retry's no-replace publish), never a silent
// retention, and the admitted source's bytes survive.
func TestPendingHardLinkPrimaryIntentCrashReplayEpochMtimeRemovesLinkedName(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "pending-hardlink-epoch", "")
	fs, root, source, subtitle, _, unrelated, match := pr260FencedFiles(t, "pending-hardlink-epoch")
	dest := filepath.Join(root, "library")
	repo := database.NewBatchFileOperationRepository(db)
	log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "pending-hardlink-epoch", fs, nil, nil, nil)
	opID, err := log.Begin(context.Background(), ApplyCmd{Movie: &movie, Match: match, DestPath: dest, Organize: OrganizeOptions{LinkMode: organizer.LinkModeHard}})
	require.NoError(t, err)

	// The freshly restored/normalized media file this finding is about.
	epoch := time.Unix(0, 0)
	require.NoError(t, os.Chtimes(source, epoch, epoch))
	srcInfo, statErr := fs.Stat(source)
	require.NoError(t, statErr)
	require.Zero(t, srcInfo.ModTime().Unix(), "the admission tuple's mtime leg is the epoch")

	finalDir := filepath.Join(dest, "movie")
	videoTarget := filepath.Join(finalDir, filepath.Base(source))
	require.NoError(t, fs.MkdirAll(finalDir, 0o755))

	// The pin is produced by the production producer (the code path the
	// finding cites, internal/workflow/artifact_staging.go) over the admitted
	// epoch-dated identity.
	stage := &artifactStage{fs: fs, sourcePath: source}
	stage.original.Organize.LinkMode = organizer.LinkModeHard
	stage.sourceIdentity = captureArtifactSourceIdentity(fs, source, srcInfo)
	entry, pinErr := stage.deferredPrimaryDeleteEntry(&organizer.OrganizePlan{SourcePath: source, TargetPath: videoTarget})
	require.NoError(t, pinErr)
	require.True(t, entry.IdentityPinned, "presence rides the explicit marker")
	require.Zero(t, entry.IdentityModUnix, "the epoch tuple journals as all-zero")
	require.NoError(t, log.RecordDeleteIntent(context.Background(), opID, []models.DeleteEntry{entry}))

	// The fault-injected crash window: the hard link installed, the exit
	// lands before ReconcileDeleteIntents could consume the pin.
	require.NoError(t, os.Link(source, videoTarget))
	require.NoError(t, db.Model(&models.BatchFileOperation{}).Where("id = ?", mustParseOpID(t, opID)).Update("new_path", videoTarget).Error)

	res, revErr := history.NewReverter(fs, repo).RevertBatch(t.Context(), "pending-hardlink-epoch")
	require.NoError(t, revErr)
	require.Equal(t, 1, res.Succeeded)

	_, lStatErr := os.Lstat(videoTarget)
	require.True(t, os.IsNotExist(lStatErr), "no orphaned destination — the epoch pin consumed the linked name rather than silently retaining it")
	assert.FileExists(t, source, "the admitted source's bytes survive — unlinking a name never frees the shared inode")

	// The retry's no-replace link publish now sees a vacant name (the
	// finding's exact failure was the retained orphan blocking it) — the
	// publisher re-creates the final directory, then links unconditionally.
	require.NoError(t, os.MkdirAll(finalDir, 0o755))
	require.NoError(t, os.Link(source, videoTarget), "the retried publication lands where recovery left a vacant name")
	require.NoError(t, os.Remove(videoTarget))

	persisted, findErr := repo.FindByID(context.Background(), mustParseOpID(t, opID))
	require.NoError(t, findErr)
	assert.Equal(t, models.RevertStatusReverted, persisted.RevertStatus)
	for _, path := range []string{subtitle, unrelated} {
		exists, existsErr := afero.Exists(fs, path)
		require.NoError(t, existsErr)
		require.True(t, exists, path)
	}
}
