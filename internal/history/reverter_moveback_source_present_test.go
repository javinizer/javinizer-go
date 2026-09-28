package history

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type statDenyFS struct {
	afero.Fs
	path string
	err  error
}

func (f *statDenyFS) Stat(name string) (os.FileInfo, error) {
	if name == f.path {
		return nil, f.err
	}
	return f.Fs.Stat(name)
}

func sha256HexOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// codex P1 (PRRT_kwDORn9KaM6m3ujI): a move-mode MoveBack entry whose ORIGINAL
// still exists belongs to a move whose source was never consumed (an exit
// between the pending intent commit and the source removal) or whose source
// reappeared afterwards. The rename-back must never run — on POSIX rename
// would REPLACE those bytes. The hash-pinned published copy, when a pin
// survived alongside the pending arm, is deleted by the PlannedDeletes leg
// instead; an unpinned or hash-mismatched target is retained both ways.
func TestCleanupGeneratedFilesFS_MoveBackSourcePresentSuppressesRename(t *testing.T) {
	const (
		src = "/src/ABC-123-cd2.mp4"
		dst = "/dst/lib/ABC-123-cd2.mp4"
	)
	newMoveOp := func(t *testing.T, pin string) *models.BatchFileOperation {
		gf := models.GeneratedFilesJSON{MoveBack: []models.FileMove{{OriginalPath: src, NewPath: dst}}}
		if pin != "" {
			gf.PlannedDeletes = []models.DeleteEntry{{Path: dst, SHA256: pin}}
		}
		return &models.BatchFileOperation{OperationType: models.OperationTypeMove, GeneratedFiles: models.MarshalLedgerJSON(gf)}
	}
	seedTarget := func(t *testing.T, fs afero.Fs, bytes []byte) {
		require.NoError(t, fs.MkdirAll("/dst/lib", 0o777))
		require.NoError(t, afero.WriteFile(fs, dst, bytes, 0o666))
	}
	seedSource := func(t *testing.T, fs afero.Fs, bytes []byte) {
		require.NoError(t, fs.MkdirAll("/src", 0o777))
		require.NoError(t, afero.WriteFile(fs, src, bytes, 0o666))
	}
	assertFileBytes := func(t *testing.T, fs afero.Fs, path string, want []byte) {
		got, err := afero.ReadFile(fs, path)
		require.NoError(t, err, path)
		assert.Equal(t, want, got, path)
	}

	t.Run("pinned published copy deleted, foreign source retained", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		seedTarget(t, fs, []byte("part two"))
		seedSource(t, fs, []byte("user re-edit"))
		op := newMoveOp(t, sha256HexOf([]byte("part two")))
		cleanupGeneratedFilesFS(fs, op, "/dst")
		assertFileBytes(t, fs, src, []byte("user re-edit"))
		if _, err := fs.Stat(dst); !os.IsNotExist(err) {
			t.Fatalf("the pinned published copy was deleted by its pin, not renamed over the source: %v", err)
		}
	})

	t.Run("unpinned target retained with the source", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		seedTarget(t, fs, []byte("part two"))
		seedSource(t, fs, []byte("user re-edit"))
		op := newMoveOp(t, "")
		cleanupGeneratedFilesFS(fs, op, "/dst")
		assertFileBytes(t, fs, src, []byte("user re-edit"))
		assertFileBytes(t, fs, dst, []byte("part two"))
	})

	t.Run("hash-mismatched occupant retained with the source", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		seedTarget(t, fs, []byte("foreign replacement"))
		seedSource(t, fs, []byte("user re-edit"))
		op := newMoveOp(t, sha256HexOf([]byte("part two")))
		cleanupGeneratedFilesFS(fs, op, "/dst")
		assertFileBytes(t, fs, src, []byte("user re-edit"))
		assertFileBytes(t, fs, dst, []byte("foreign replacement"))
	})

	t.Run("unprovable source state suppresses the rename but keeps the pin", func(t *testing.T) {
		base := afero.NewMemMapFs()
		seedTarget(t, base, []byte("part two"))
		seedSource(t, base, []byte("user re-edit"))
		op := newMoveOp(t, sha256HexOf([]byte("part two")))
		fs := &statDenyFS{Fs: base, path: src, err: errors.New("stat denied")}
		cleanupGeneratedFilesFS(fs, op, "/dst")
		assertFileBytes(t, base, src, []byte("user re-edit"))
		if _, err := base.Stat(dst); !os.IsNotExist(err) {
			t.Fatalf("uncertainty never licenses a rename-over; the pin still consumed the published copy: %v", err)
		}
	})

	t.Run("consumed leg: source absent renames the target back", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		seedTarget(t, fs, []byte("part two"))
		op := newMoveOp(t, sha256HexOf([]byte("part two")))
		cleanupGeneratedFilesFS(fs, op, "/dst")
		assertFileBytes(t, fs, src, []byte("part two"))
		if _, err := fs.Stat(dst); !os.IsNotExist(err) {
			t.Fatalf("the consumed move restored the source and vacated the destination: %v", err)
		}
	})

	t.Run("column-shaped primary intent still defers to the primary legs", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		seedTarget(t, fs, []byte("part two"))
		seedSource(t, fs, []byte("user re-edit"))
		op := newMoveOp(t, "")
		op.OriginalPath = src
		op.NewPath = dst
		cleanupGeneratedFilesFS(fs, op, "/dst")
		assertFileBytes(t, fs, src, []byte("user re-edit"))
		assertFileBytes(t, fs, dst, []byte("part two"))
	})
}

// The suppression is a MOVE-mode leg: copy rows never rename back, and their
// legacy delete-the-installed-copy semantic is unchanged even with the source
// (retained by construction in copy mode) present.
func TestCleanupGeneratedFilesFS_CopyModeSourcePresentKeepsDeleteOnlySemantic(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/src", 0o777))
	require.NoError(t, fs.MkdirAll("/dst", 0o777))
	require.NoError(t, afero.WriteFile(fs, "/src/sub.srt", []byte("original"), 0o666))
	require.NoError(t, afero.WriteFile(fs, "/dst/sub.srt", []byte("installed copy"), 0o666))
	op := &models.BatchFileOperation{
		OperationType:  models.OperationTypeCopy,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{MoveBack: []models.FileMove{{OriginalPath: "/src/sub.srt", NewPath: "/dst/sub.srt"}}}),
	}
	cleanupGeneratedFilesFS(fs, op, "/dst")
	bytes, err := afero.ReadFile(fs, "/src/sub.srt")
	require.NoError(t, err)
	assert.Equal(t, "original", string(bytes))
	if _, statErr := fs.Stat("/dst/sub.srt"); !os.IsNotExist(statErr) {
		t.Fatalf("copy-mode legacy entries still delete the installed copy: %v", statErr)
	}
}

// Crash-replay E2E (PRRT_kwDORn9KaM6m3ujI, crash matrix row 1): the sibling
// move intent is journaled, the source removal never runs, and foreign bytes
// land on the source path before recovery. Reverting the batch must leave the
// source bytes untouched and delete the hash-pinned published copy.
func TestRevertRowSiblingIntentCrashSourcePresentDeletesPinnedCopy(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	srcDir := "/src-w161b"
	dstDir := "/dst-w161b/lib/W161B-001"
	videoSource := srcDir + "/W161B-001.mkv"
	videoTarget := dstDir + "/W161B-001.mkv"
	siblingSource := srcDir + "/W161B-001-cd2.mp4"
	siblingTarget := dstDir + "/W161B-001-cd2.mp4"
	require.NoError(t, fs.MkdirAll(dstDir, 0o777))
	require.NoError(t, fs.MkdirAll(srcDir, 0o777))
	require.NoError(t, afero.WriteFile(fs, videoTarget, []byte("video"), 0o666))
	require.NoError(t, afero.WriteFile(fs, siblingTarget, []byte("part two"), 0o666))
	// The exit landed after the intent commit but before the source removal —
	// and the source path was rewritten with foreign bytes before recovery.
	require.NoError(t, afero.WriteFile(fs, siblingSource, []byte("user re-edit"), 0o666))

	op := &models.BatchFileOperation{
		BatchJobID:    "job-w161b-source-present",
		MovieID:       "W161B-001",
		OriginalPath:  videoSource,
		NewPath:       videoTarget,
		OperationType: models.OperationTypeMove,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			PlannedDeletes: []models.DeleteEntry{{Path: siblingTarget, SHA256: sha256HexOf([]byte("part two"))}},
			MoveBack:       []models.FileMove{{OriginalPath: siblingSource, NewPath: siblingTarget}},
		}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	res, err := NewReverter(fs, repo).RevertBatch(ctx, "job-w161b-source-present")
	require.NoError(t, err)
	require.Equal(t, 1, res.Succeeded)

	srcBytes, readErr := afero.ReadFile(fs, siblingSource)
	require.NoError(t, readErr, "rollback never overwrote the surviving source")
	assert.Equal(t, "user re-edit", string(srcBytes))
	if _, statErr := fs.Stat(siblingTarget); !os.IsNotExist(statErr) {
		t.Fatalf("the pinned published copy was deleted, not moved back over the source: %v", statErr)
	}
	videoBytes, readErr := afero.ReadFile(fs, videoSource)
	require.NoError(t, readErr, "the consumed primary still renames back normally")
	assert.Equal(t, "video", string(videoBytes))

	row, findErr := repo.FindByID(ctx, op.ID)
	require.NoError(t, findErr)
	assert.Equal(t, models.RevertStatusReverted, row.RevertStatus)
}

// Crash matrix row 2: the source removal DID land after the intent commit
// (nothing reappeared at the source path). The pending arm graduates into an
// ordinary rename-back and the sibling bytes return onto their source.
func TestRevertRowSiblingIntentConsumedRenamesBack(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()

	srcDir := "/src-w161c"
	dstDir := "/dst-w161c/lib/W161C-001"
	videoSource := srcDir + "/W161C-001.mkv"
	videoTarget := dstDir + "/W161C-001.mkv"
	siblingSource := srcDir + "/W161C-001-cd2.mp4"
	siblingTarget := dstDir + "/W161C-001-cd2.mp4"
	require.NoError(t, fs.MkdirAll(dstDir, 0o777))
	require.NoError(t, fs.MkdirAll(srcDir, 0o777))
	require.NoError(t, afero.WriteFile(fs, videoTarget, []byte("video"), 0o666))
	require.NoError(t, afero.WriteFile(fs, siblingTarget, []byte("part two"), 0o666))

	op := &models.BatchFileOperation{
		BatchJobID:    "job-w161c-consumed",
		MovieID:       "W161C-001",
		OriginalPath:  videoSource,
		NewPath:       videoTarget,
		OperationType: models.OperationTypeMove,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{
			PlannedDeletes: []models.DeleteEntry{{Path: siblingTarget, SHA256: sha256HexOf([]byte("part two"))}},
			MoveBack:       []models.FileMove{{OriginalPath: siblingSource, NewPath: siblingTarget}},
		}),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	res, err := NewReverter(fs, repo).RevertBatch(ctx, "job-w161c-consumed")
	require.NoError(t, err)
	require.Equal(t, 1, res.Succeeded)

	restored, readErr := afero.ReadFile(fs, siblingSource)
	require.NoError(t, readErr, "the consumed pending intent restored the sibling onto its source")
	assert.Equal(t, "part two", string(restored))
	if _, statErr := fs.Stat(siblingTarget); !os.IsNotExist(statErr) {
		t.Fatalf("the destination moved back instead of being deleted by the surviving pin: %v", statErr)
	}
}

// Defensive probe against hand-edited or truncated journals: a move-mode
// MoveBack row missing either endpoint has no addressable source state, so the
// suppression probe skips it (`continue`) instead of stat-ing nonsense — and
// must still evaluate every well-formed row next to it.
func TestCleanupGeneratedFilesFS_MoveBackMalformedRowsLeaveRemainingRowsAlone(t *testing.T) {
	const (
		src            = "/src/ABC-123-cd2.mp4"
		dst            = "/dst/lib/ABC-123-cd2.mp4"
		consumedSrc    = "/src/ABC-123-cd3.mp4"
		consumedDst    = "/dst/lib/ABC-123-cd3.mp4"
		orphanTarget   = "/dst/lib/ABC-123-cd4.mp4"
		headlessSource = "/src/never-targeted.mp4"
	)
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/src", 0o777))
	require.NoError(t, fs.MkdirAll("/dst/lib", 0o777))
	// Row B's source is still present: the probe must mark it suppressed.
	require.NoError(t, afero.WriteFile(fs, src, []byte("user re-edit"), 0o666))
	require.NoError(t, afero.WriteFile(fs, dst, []byte("part two"), 0o666))
	// Row D's source was consumed: the ordinary rename-back must still run.
	require.NoError(t, afero.WriteFile(fs, consumedDst, []byte("part three"), 0o666))

	op := &models.BatchFileOperation{
		OperationType: models.OperationTypeMove,
		// The column pair aliases row C below: the execution loop's
		// primary-intent guard then absorbs that malformed row without ever
		// driving a rename against empty paths.
		OriginalPath: headlessSource,
		NewPath:      "",
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{MoveBack: []models.FileMove{
			{OriginalPath: "", NewPath: orphanTarget},         // A: no source endpoint
			{OriginalPath: src, NewPath: dst},                 // B: present source -> suppressed
			{OriginalPath: headlessSource, NewPath: ""},       // C: no destination endpoint
			{OriginalPath: consumedSrc, NewPath: consumedDst}, // D: consumed -> rename back
		}}),
	}
	cleanupGeneratedFilesFS(fs, op, "/dst")

	// Neither malformed row materialized anything on disk.
	for _, p := range []string{orphanTarget, headlessSource} {
		if _, err := fs.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("malformed MoveBack rows executed nothing, but %s appeared: %v", p, err)
		}
	}
	// Both continues left the well-formed rows fully processed: B stayed
	// suppressed (both sides retained byte-for-byte), D renamed back.
	srcBytes, err := afero.ReadFile(fs, src)
	require.NoError(t, err, "the probe reached the present-source row behind the first continue")
	assert.Equal(t, "user re-edit", string(srcBytes), "suppression held — the source was never renamed over")
	dstBytes, err := afero.ReadFile(fs, dst)
	require.NoError(t, err)
	assert.Equal(t, "part two", string(dstBytes), "the suppressed destination was retained unpinned")
	restored, err := afero.ReadFile(fs, consumedSrc)
	require.NoError(t, err, "the consumed row behind the second continue still renamed back")
	assert.Equal(t, "part three", string(restored))
	if _, err := fs.Stat(consumedDst); !os.IsNotExist(err) {
		t.Fatalf("the consumed row vacated its destination: %v", err)
	}
}
