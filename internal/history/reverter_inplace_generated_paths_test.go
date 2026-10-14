package history

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

func TestRecoverInPlaceMoveAnchorOnlyUsesConsumedSourceIntents(t *testing.T) {
	for _, state := range []string{"directory-landed", "scratch-live", "source-retained", "invalid-ledger", "no-live-intent"} {
		t.Run(state, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			root := t.TempDir()
			original := filepath.Join(root, "incoming", "video.mkv")
			scratch := filepath.Join(root, "scratch", "video.mkv")
			landed := filepath.Join(root, "renamed", "video.mkv")
			for _, dir := range []string{filepath.Dir(original), filepath.Dir(scratch), filepath.Dir(landed)} {
				require.NoError(t, fs.MkdirAll(dir, 0o755))
			}
			ledger := models.GeneratedFilesJSON{MoveBack: []models.FileMove{{OriginalPath: "unrelated", NewPath: landed}, {OriginalPath: original}, {OriginalPath: original, NewPath: filepath.Dir(landed)}, {OriginalPath: original, NewPath: landed}}}
			op := &models.BatchFileOperation{OriginalPath: original, NewPath: scratch, OriginalDirPath: filepath.Dir(original), InPlaceRenamed: true, OperationType: models.OperationTypeMove, GeneratedFiles: models.MarshalLedgerJSON(ledger)}
			switch state {
			case "directory-landed":
				require.NoError(t, afero.WriteFile(fs, landed, []byte("owned"), 0o644))
			case "scratch-live":
				require.NoError(t, afero.WriteFile(fs, scratch, []byte("owned"), 0o644))
			case "source-retained":
				require.NoError(t, afero.WriteFile(fs, original, []byte("retained"), 0o644))
				require.NoError(t, afero.WriteFile(fs, landed, []byte("unexecuted destination"), 0o644))
			case "invalid-ledger":
				op.GeneratedFiles = "{"
			}
			NewReverter(fs, nil).recoverInPlaceMoveAnchor(op)
			if state == "directory-landed" {
				require.Equal(t, landed, op.NewPath)
			} else {
				require.Equal(t, scratch, op.NewPath)
			}
		})
	}
}

func TestCleanupInPlaceGeneratedPathsFollowRestoredDirectory(t *testing.T) {
	fs := afero.NewMemMapFs()
	root := t.TempDir()
	current, original := filepath.Join(root, "renamed"), filepath.Join(root, "incoming")
	require.NoError(t, fs.MkdirAll(original, 0o755))
	outside := filepath.Join(root, "outside.nfo")
	for path, body := range map[string]string{filepath.Join(original, "video-original.mkv"): "video", filepath.Join(original, "video.nfo"): "generated", filepath.Join(original, "pinned.jpg"): "pinned", filepath.Join(original, "video.en.srt"): "subtitle", outside: "outside generated"} {
		require.NoError(t, afero.WriteFile(fs, path, []byte(body), 0o644))
	}
	digest := sha256.Sum256([]byte("pinned"))
	ledger := models.GeneratedFilesJSON{
		Delete:         []string{"", filepath.Join(current, "video.nfo"), outside},
		PlannedDeletes: []models.DeleteEntry{{Path: filepath.Join(current, "pinned.jpg"), SHA256: hex.EncodeToString(digest[:])}},
		MoveBack:       []models.FileMove{{OriginalPath: filepath.Join(original, "video-original.en.srt"), NewPath: filepath.Join(current, "video.en.srt")}},
	}
	op := &models.BatchFileOperation{OperationType: models.OperationTypeMove, OriginalPath: filepath.Join(original, "video-original.mkv"), NewPath: filepath.Join(current, "video.mkv"), OriginalDirPath: original, InPlaceRenamed: true, GeneratedFiles: models.MarshalLedgerJSON(ledger)}
	cleanupGeneratedFilesFS(fs, op, root)
	for _, path := range []string{filepath.Join(original, "video.nfo"), filepath.Join(original, "pinned.jpg"), filepath.Join(original, "video.en.srt"), outside} {
		exists, err := afero.Exists(fs, path)
		require.NoError(t, err)
		require.False(t, exists, path)
	}
	for _, path := range []string{op.OriginalPath, filepath.Join(original, "video-original.en.srt")} {
		exists, err := afero.Exists(fs, path)
		require.NoError(t, err)
		require.True(t, exists, path)
	}
}
