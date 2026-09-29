package workflow

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// codex P2 (PRRT_kwDORn9KaM6nBUq8) crash-replay E2E: a LinkModeSoft deferred
// publication that exits between ExecuteOrganizePlan's link creation and
// ReconcileDeleteIntents must recover through the SYMLINK-AWARE pin — the
// superseded content-hash pin provably never fired (the planned-delete leg
// retains every non-regular entry), orphaning the link and blocking the
// retry's no-replace publish. Journaled through the production recorder in
// the production shape, recovered by the production reverter.
func TestPendingSoftLinkPrimaryIntentCrashReplayRemovesInstalledLink(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "pending-softlink-crash", "")
	fs, root, source, subtitle, _, unrelated, match := pr260FencedFiles(t, "pending-softlink-crash")
	dest := filepath.Join(root, "library")
	repo := database.NewBatchFileOperationRepository(db)
	log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "pending-softlink-crash", fs, nil, nil, nil)
	opID, err := log.Begin(context.Background(), ApplyCmd{Movie: &movie, Match: match, DestPath: dest, Organize: OrganizeOptions{LinkMode: organizer.LinkModeSoft}})
	require.NoError(t, err)

	finalDir := filepath.Join(dest, "movie")
	videoTarget := filepath.Join(finalDir, filepath.Base(source))
	require.NoError(t, fs.MkdirAll(finalDir, 0o755))

	// The production pin shape: the exact payload the symlink leg installs.
	pinTarget, pinErr := organizer.SymlinkLinkTarget(source)
	require.NoError(t, pinErr)
	require.NoError(t, log.RecordDeleteIntent(context.Background(), opID, []models.DeleteEntry{{Path: videoTarget, LinkTarget: pinTarget}}))

	// Execute's link already landed; the exit lands before the reconcile.
	if linkErr := os.Symlink(source, videoTarget); linkErr != nil {
		t.Skipf("symlink creation unsupported here: %v", linkErr)
	}
	require.NoError(t, db.Model(&models.BatchFileOperation{}).Where("id = ?", mustParseOpID(t, opID)).Update("new_path", videoTarget).Error)

	res, revErr := history.NewReverter(fs, repo).RevertBatch(t.Context(), "pending-softlink-crash")
	require.NoError(t, revErr)
	require.Equal(t, 1, res.Succeeded)

	_, statErr := os.Lstat(videoTarget)
	assert.True(t, os.IsNotExist(statErr), "the orphaned install is consumed — a retry's no-replace link publish sees a vacant name (the finding's exact failure)")
	assert.FileExists(t, source, "the link TARGET (the user's source) is never the pin's removal subject")

	persisted, findErr := repo.FindByID(context.Background(), mustParseOpID(t, opID))
	require.NoError(t, findErr)
	assert.Equal(t, models.RevertStatusReverted, persisted.RevertStatus)
	for _, path := range []string{subtitle, unrelated} {
		exists, existsErr := afero.Exists(fs, path)
		require.NoError(t, existsErr)
		require.True(t, exists, path)
	}
}

// The retention side of the same recovery: occupants that are NOT this
// apply's pinned link must survive untouched — a foreign link to elsewhere,
// and a regular file (the m5HF7 partition seen from the link side).
func TestPendingSoftLinkPrimaryIntentCrashReplayRetainsForeignOccupants(t *testing.T) {
	for _, tc := range []struct {
		name  string
		plant func(t *testing.T, fs afero.Fs, root, videoTarget, source string)
		check func(t *testing.T, videoTarget string)
	}{
		{
			name: "foreign link to another object",
			plant: func(t *testing.T, fs afero.Fs, root, videoTarget, source string) {
				t.Helper()
				foreign := filepath.Join(root, "foreign-source.mkv")
				require.NoError(t, afero.WriteFile(fs, foreign, []byte("foreign video"), 0o644))
				if err := os.Symlink(foreign, videoTarget); err != nil {
					t.Skipf("symlink creation unsupported here: %v", err)
				}
			},
			check: func(t *testing.T, videoTarget string) {
				got, err := os.Readlink(videoTarget)
				require.NoError(t, err, "a link whose payload differs from the pin is foreign — retained")
				assert.Contains(t, got, "foreign-source.mkv")
			},
		},
		{
			name: "regular file occupant",
			plant: func(t *testing.T, fs afero.Fs, root, videoTarget, source string) {
				t.Helper()
				require.NoError(t, afero.WriteFile(fs, videoTarget, []byte("renamed-in foreign file"), 0o644))
			},
			check: func(t *testing.T, videoTarget string) {
				got, err := afero.ReadFile(afero.NewOsFs(), videoTarget)
				require.NoError(t, err, "a regular occupant is outside the link install shape — retained (m5HF7 partition)")
				assert.Equal(t, "renamed-in foreign file", string(got))
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, _ := pr260ArtifactDB(t)
			movie := pr260FencedMovie(t, db, "pending-softlink-foreign", "")
			fs, root, source, _, _, _, match := pr260FencedFiles(t, "pending-softlink-foreign")
			dest := filepath.Join(root, "library")
			repo := database.NewBatchFileOperationRepository(db)
			log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "pending-softlink-foreign", fs, nil, nil, nil)
			opID, err := log.Begin(context.Background(), ApplyCmd{Movie: &movie, Match: match, DestPath: dest, Organize: OrganizeOptions{LinkMode: organizer.LinkModeSoft}})
			require.NoError(t, err)

			finalDir := filepath.Join(dest, "movie")
			videoTarget := filepath.Join(finalDir, filepath.Base(source))
			require.NoError(t, fs.MkdirAll(finalDir, 0o755))
			pinTarget, pinErr := organizer.SymlinkLinkTarget(source)
			require.NoError(t, pinErr)
			require.NoError(t, log.RecordDeleteIntent(context.Background(), opID, []models.DeleteEntry{{Path: videoTarget, LinkTarget: pinTarget}}))

			tc.plant(t, fs, root, videoTarget, source)
			require.NoError(t, db.Model(&models.BatchFileOperation{}).Where("id = ?", mustParseOpID(t, opID)).Update("new_path", videoTarget).Error)

			res, revErr := history.NewReverter(fs, repo).RevertBatch(t.Context(), "pending-softlink-foreign")
			require.NoError(t, revErr)
			require.Equal(t, 1, res.Succeeded)
			tc.check(t, videoTarget)
		})
	}
}

// codex P2 (PRRT_kwDORn9KaM6nBUrF) crash replay, hard-link lane: the
// identity-tuple pin authenticates the linked object — the SAME object the
// admitted source names — without any content pass, and the revert consumes
// the linked name while the source's bytes survive.
func TestPendingHardLinkPrimaryIntentCrashReplayRemovesLinkedName(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "pending-hardlink-crash", "")
	fs, root, source, subtitle, _, unrelated, match := pr260FencedFiles(t, "pending-hardlink-crash")
	dest := filepath.Join(root, "library")
	repo := database.NewBatchFileOperationRepository(db)
	log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "pending-hardlink-crash", fs, nil, nil, nil)
	opID, err := log.Begin(context.Background(), ApplyCmd{Movie: &movie, Match: match, DestPath: dest, Organize: OrganizeOptions{LinkMode: organizer.LinkModeHard}})
	require.NoError(t, err)

	finalDir := filepath.Join(dest, "movie")
	videoTarget := filepath.Join(finalDir, filepath.Base(source))
	require.NoError(t, fs.MkdirAll(finalDir, 0o755))

	srcInfo, statErr := fs.Stat(source)
	require.NoError(t, statErr)
	dev, ino, identityOK := fsutil.BoundObjectIdentity(fs, source, srcInfo)
	require.True(t, identityOK, "OsFs carries the kernel identity")
	entry := models.DeleteEntry{Path: videoTarget, IdentityStrong: true, IdentityDev: dev, IdentityIno: ino, IdentitySize: srcInfo.Size(), IdentityModUnix: srcInfo.ModTime().Unix()}
	require.NoError(t, log.RecordDeleteIntent(context.Background(), opID, []models.DeleteEntry{entry}))

	require.NoError(t, os.Link(source, videoTarget))
	require.NoError(t, db.Model(&models.BatchFileOperation{}).Where("id = ?", mustParseOpID(t, opID)).Update("new_path", videoTarget).Error)

	res, revErr := history.NewReverter(fs, repo).RevertBatch(t.Context(), "pending-hardlink-crash")
	require.NoError(t, revErr)
	require.Equal(t, 1, res.Succeeded)
	_, lStatErr := os.Lstat(videoTarget)
	assert.True(t, os.IsNotExist(lStatErr), "the linked install is consumed")
	assert.FileExists(t, source, "the source's bytes survive — unlinking a name never frees the shared inode")

	persisted, findErr := repo.FindByID(context.Background(), mustParseOpID(t, opID))
	require.NoError(t, findErr)
	assert.Equal(t, models.RevertStatusReverted, persisted.RevertStatus)
	for _, path := range []string{subtitle, unrelated} {
		exists, existsErr := afero.Exists(fs, path)
		require.NoError(t, existsErr)
		require.True(t, exists, path)
	}
}

// codex P2 (PRRT_kwDORn9KaM6nBUrF) crash replay, copy lane: the interrupted
// row may hold EITHER interim shape — the head+tail partial pin (crash
// between publish and seal) or the sealed full hash (crash between seal and
// reconcile) — and each must consume exactly the published bytes while
// retaining a same-size foreign occupant.
func TestPendingCopyPrimaryIntentCrashReplayInterimAndSealed(t *testing.T) {
	for _, sealedSeal := range []bool{false, true} {
		name := "interim partial pin"
		if sealedSeal {
			name = "sealed full-hash pin"
		}
		t.Run(name, func(t *testing.T) {
			db, _ := pr260ArtifactDB(t)
			movie := pr260FencedMovie(t, db, "pending-copy-crash", "")
			fs, root, source, _, _, _, match := pr260FencedFiles(t, "pending-copy-crash")
			dest := filepath.Join(root, "library")
			repo := database.NewBatchFileOperationRepository(db)
			log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "pending-copy-crash", fs, nil, nil, nil)
			opID, err := log.Begin(context.Background(), ApplyCmd{Movie: &movie, Match: match, DestPath: dest})
			require.NoError(t, err)

			finalDir := filepath.Join(dest, "movie")
			videoTarget := filepath.Join(finalDir, filepath.Base(source))
			require.NoError(t, fs.MkdirAll(finalDir, 0o755))

			// The production intent path journals the interim partial pin, the
			// publish streams (copy the source, which IS the verbatim payload),
			// and — in the sealed shape — the seal upgrades the pin with the
			// teed digest.
			info, partial, pErr := fsutil.PartialCopyDigest(fs, source)
			require.NoError(t, pErr)
			require.NoError(t, log.RecordDeleteIntent(context.Background(), opID, []models.DeleteEntry{{Path: videoTarget, CopySize: info.Size(), CopyPartialSHA256: partial}}))
			require.NoError(t, afero.WriteFile(fs, videoTarget, mustRead(t, fs, source), 0o644))
			if sealedSeal {
				full, digestErr := artifactDigest(fs, source)
				require.NoError(t, digestErr)
				require.NoError(t, log.FinalizeDeleteIntentCopyDigest(context.Background(), opID, videoTarget, full),
					"the seal lands through the production journal channel")
			}
			require.NoError(t, db.Model(&models.BatchFileOperation{}).Where("id = ?", mustParseOpID(t, opID)).Update("new_path", videoTarget).Error)

			res, revErr := history.NewReverter(fs, repo).RevertBatch(t.Context(), "pending-copy-crash")
			require.NoError(t, revErr)
			require.Equal(t, 1, res.Succeeded)
			exists, existsErr := afero.Exists(fs, videoTarget)
			require.NoError(t, existsErr)
			assert.False(t, exists, "the interrupted row's published copy is consumed under both pin shapes")
			assert.FileExists(t, source, "copy mode never consumed the source")
		})
	}

	t.Run("interim pin retains a same-size foreign occupant", func(t *testing.T) {
		db, _ := pr260ArtifactDB(t)
		movie := pr260FencedMovie(t, db, "pending-copy-foreign", "")
		fs, root, source, _, _, _, match := pr260FencedFiles(t, "pending-copy-foreign")
		dest := filepath.Join(root, "library")
		repo := database.NewBatchFileOperationRepository(db)
		log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "pending-copy-foreign", fs, nil, nil, nil)
		opID, err := log.Begin(context.Background(), ApplyCmd{Movie: &movie, Match: match, DestPath: dest})
		require.NoError(t, err)

		finalDir := filepath.Join(dest, "movie")
		videoTarget := filepath.Join(finalDir, filepath.Base(source))
		require.NoError(t, fs.MkdirAll(finalDir, 0o755))

		info, partial, pErr := fsutil.PartialCopyDigest(fs, source)
		require.NoError(t, pErr)
		require.NoError(t, log.RecordDeleteIntent(context.Background(), opID, []models.DeleteEntry{{Path: videoTarget, CopySize: info.Size(), CopyPartialSHA256: partial}}))

		// A foreign occupant of the SAME SIZE but diverging head content lands
		// where the pin expected the published copy.
		foreign := make([]byte, info.Size())
		for i := range foreign {
			foreign[i] = 'F'
		}
		require.NoError(t, afero.WriteFile(fs, videoTarget, foreign, 0o644))
		require.NoError(t, db.Model(&models.BatchFileOperation{}).Where("id = ?", mustParseOpID(t, opID)).Update("new_path", videoTarget).Error)

		res, revErr := history.NewReverter(fs, repo).RevertBatch(t.Context(), "pending-copy-foreign")
		require.NoError(t, revErr)
		require.Equal(t, 1, res.Succeeded)
		got, readErr := afero.ReadFile(fs, videoTarget)
		require.NoError(t, readErr, "the interim proof refused and the foreign bytes stay byte-intact")
		assert.Equal(t, foreign, got)
	})
}

func mustRead(t *testing.T, fs afero.Fs, path string) []byte {
	t.Helper()
	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	return data
}
