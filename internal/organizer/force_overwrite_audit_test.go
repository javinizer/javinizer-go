package organizer

// Force-overwrite audit crumb: an overwrite-AUTHORIZED organize move/copy leg
// that replaces a bytes-bearing destination must carry the
// "overwrite authorized: replaced existing destination <path>" warning on its
// OrganizeResult so the audit surfaces downstream (worker history metadata,
// API eventlog resolver, CLI console print) keep the replacement visible.
// Absent destinations, self/same-inode no-ops, non-authorized runs, and the
// authorized intra-batch duplicate skip keep their pre-existing behavior:
// no crumb, and the dup skip never double-warns.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
)

// forceAuditOrganizer builds the organize-mode organizer over a fresh
// MemMapFs — the duplex fixture convention of dupBatchFixture: sources under
// /in, destinations compute as /dest/<ID>/<ID>.mkv.
func forceAuditOrganizer(t *testing.T) (*Organizer, afero.Fs) {
	t.Helper()
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}
	org := NewOrganizer(fs, cfg, nil, nil)
	require.NoError(t, fs.MkdirAll("/in", 0o755))
	return org, fs
}

// forceAuditCmd builds the single-file organize command onto the /dest
// library; force maps to the CLI's -f overwrite authorization.
func forceAuditCmd(path string, force, move bool) OrganizeCmd {
	return OrganizeCmd{
		Match:       models.FileMatchInfo{MovieID: "ABC-123", Path: path, Name: filepath.Base(path), Extension: filepath.Ext(path)},
		Movie:       &models.Movie{ID: "ABC-123"},
		DestDir:     "/dest",
		MoveFiles:   move,
		LinkMode:    LinkModeNone,
		ForceUpdate: force,
	}
}

// plantOccupiedDest pre-seeds the computed destination with resident bytes —
// the foreign occupant a -f run must visibly replace.
func plantOccupiedDest(t *testing.T, fs afero.Fs) string {
	t.Helper()
	const dst = "/dest/ABC-123/ABC-123.mkv"
	require.NoError(t, fs.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, afero.WriteFile(fs, dst, []byte("resident-bytes"), 0o644))
	return dst
}

func TestForceOverwriteAudit_MoveLeg(t *testing.T) {
	t.Run("force move onto occupied dest warns and replaces", func(t *testing.T) {
		org, fs := forceAuditOrganizer(t)
		require.NoError(t, afero.WriteFile(fs, "/in/A.mkv", []byte("winner-bytes"), 0o644))
		dst := plantOccupiedDest(t, fs)

		result, err := org.Organize(context.Background(), forceAuditCmd("/in/A.mkv", true, true))
		require.NoError(t, err)
		require.True(t, result.Moved, "the authorized move executed")
		require.Len(t, result.Warnings, 1, "exactly one audit crumb — the resident bytes' replacement")
		assert.Equal(t, authorizedOverwriteWarning(dst), result.Warnings[0])
		assert.Contains(t, filepath.ToSlash(result.Warnings[0]),
			"overwrite authorized: replaced existing destination /dest/ABC-123/ABC-123.mkv")

		// Journal evidence: the destination now carries the winner's bytes and
		// the source path is gone (the move actually replaced the occupant).
		content, readErr := afero.ReadFile(fs, dst)
		require.NoError(t, readErr)
		assert.Equal(t, []byte("winner-bytes"), content, "resident bytes were replaced")
		srcExists, statErr := afero.Exists(fs, "/in/A.mkv")
		require.NoError(t, statErr)
		assert.False(t, srcExists, "the winner's source moved")
	})

	t.Run("force move onto vacant dest stays silent", func(t *testing.T) {
		org, fs := forceAuditOrganizer(t)
		require.NoError(t, afero.WriteFile(fs, "/in/A.mkv", []byte("winner-bytes"), 0o644))

		result, err := org.Organize(context.Background(), forceAuditCmd("/in/A.mkv", true, true))
		require.NoError(t, err)
		require.True(t, result.Moved)
		assert.Empty(t, result.Warnings, "no occupant existed at plan time — no crumb")
		content, readErr := afero.ReadFile(fs, "/dest/ABC-123/ABC-123.mkv")
		require.NoError(t, readErr)
		assert.Equal(t, []byte("winner-bytes"), content)
	})

	t.Run("non-force onto occupied dest conflicts exactly as before", func(t *testing.T) {
		org, fs := forceAuditOrganizer(t)
		require.NoError(t, afero.WriteFile(fs, "/in/A.mkv", []byte("winner-bytes"), 0o644))
		dst := plantOccupiedDest(t, fs)

		_, err := org.Organize(context.Background(), forceAuditCmd("/in/A.mkv", false, true))
		require.Error(t, err, "an occupied destination still blocks an unauthorized run")
		assert.Contains(t, err.Error(), "organization validation failed")
		assert.Contains(t, filepath.ToSlash(err.Error()), dst)

		// Neither side changed: the refusal replaced nothing.
		content, readErr := afero.ReadFile(fs, dst)
		require.NoError(t, readErr)
		assert.Equal(t, []byte("resident-bytes"), content, "the occupant's bytes are intact")
		srcContent, readErr := afero.ReadFile(fs, "/in/A.mkv")
		require.NoError(t, readErr)
		assert.Equal(t, []byte("winner-bytes"), srcContent, "the source stayed put")
	})
}

func TestForceOverwriteAudit_CopyLeg(t *testing.T) {
	t.Run("force copy onto occupied dest warns and replaces", func(t *testing.T) {
		org, fs := forceAuditOrganizer(t)
		require.NoError(t, afero.WriteFile(fs, "/in/A.mkv", []byte("winner-bytes"), 0o644))
		dst := plantOccupiedDest(t, fs)

		result, err := org.Organize(context.Background(), forceAuditCmd("/in/A.mkv", true, false))
		require.NoError(t, err)
		require.True(t, result.Moved, "the authorized copy executed")
		require.Len(t, result.Warnings, 1, "exactly one audit crumb")
		assert.Equal(t, authorizedOverwriteWarning(dst), result.Warnings[0])

		content, readErr := afero.ReadFile(fs, dst)
		require.NoError(t, readErr)
		assert.Equal(t, []byte("winner-bytes"), content, "resident bytes were replaced")
		srcExists, statErr := afero.Exists(fs, "/in/A.mkv")
		require.NoError(t, statErr)
		assert.True(t, srcExists, "a copy retains its source")
	})

	t.Run("force copy onto vacant dest stays silent", func(t *testing.T) {
		org, fs := forceAuditOrganizer(t)
		require.NoError(t, afero.WriteFile(fs, "/in/A.mkv", []byte("winner-bytes"), 0o644))

		result, err := org.Organize(context.Background(), forceAuditCmd("/in/A.mkv", true, false))
		require.NoError(t, err)
		require.True(t, result.Moved)
		assert.Empty(t, result.Warnings, "no occupant existed at plan time — no crumb")
	})

	t.Run("non-force copy onto occupied dest conflicts exactly as before", func(t *testing.T) {
		org, fs := forceAuditOrganizer(t)
		require.NoError(t, afero.WriteFile(fs, "/in/A.mkv", []byte("winner-bytes"), 0o644))
		dst := plantOccupiedDest(t, fs)

		_, err := org.Organize(context.Background(), forceAuditCmd("/in/A.mkv", false, false))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "organization validation failed")
		content, readErr := afero.ReadFile(fs, dst)
		require.NoError(t, readErr)
		assert.Equal(t, []byte("resident-bytes"), content, "the occupant's bytes are intact")
	})
}

// Same-inode alias no-op (requirement: NO crumb when nothing is replaced):
// source and destination are hardlink aliases of one inode — even under -f
// the plan-time classifier reports no occupation and execution no-ops.
func TestForceOverwriteAudit_SameInodeNoOp_Silent(t *testing.T) {
	dir := t.TempDir()
	fs := afero.NewOsFs()
	src := filepath.Join(dir, "in", "ABC-123.mkv")
	dst := filepath.Join(dir, "dest", "ABC-123", "ABC-123.mkv")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("shared-bytes"), 0o644))
	require.NoError(t, os.Link(src, dst))

	cfg := &Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}
	org := NewOrganizer(fs, cfg, nil, nil)

	result, err := org.Organize(context.Background(), OrganizeCmd{
		Match:       models.FileMatchInfo{MovieID: "ABC-123", Path: src, Name: "ABC-123.mkv", Extension: ".mkv"},
		Movie:       &models.Movie{ID: "ABC-123"},
		DestDir:     filepath.Join(dir, "dest"),
		MoveFiles:   true,
		ForceUpdate: true,
	})
	require.NoError(t, err)
	assert.Empty(t, result.Warnings, "a same-inode no-op replaced nothing — no crumb")
	content, readErr := os.ReadFile(dst)
	require.NoError(t, readErr)
	assert.Equal(t, []byte("shared-bytes"), content)
	srcExists, statErr := afero.Exists(fs, src)
	require.NoError(t, statErr)
	assert.True(t, srcExists, "the no-op never removed the source")
}

// The authorized intra-batch duplicate skip carries ONLY its demotion
// warning — even when its plan ALSO saw a bytes-bearing occupant on the
// claimed destination, the skipped loser must never double-warn with the
// overwrite crumb (it executed nothing and replaced nothing).
func TestForceOverwriteAudit_DuplicateSkip_NoDoubleWarn(t *testing.T) {
	forceCasePosture(t, true)
	org, fs := forceAuditOrganizer(t)
	require.NoError(t, afero.WriteFile(fs, "/in/A.mkv", []byte("a-bytes"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/in/B.mkv", []byte("b-bytes"), 0o644))
	dst := plantOccupiedDest(t, fs)
	tracker := NewDuplicateTracker(false)

	// Winner A force-organizes FIRST onto the occupied destination: the crumb
	// fires there (it really replaced the resident bytes).
	winner, err := org.Organize(context.Background(), OrganizeCmd{
		Match:            models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/A.mkv", Name: "A.mkv", Extension: ".mkv"},
		Movie:            &models.Movie{ID: "ABC-123"},
		DestDir:          "/dest",
		MoveFiles:        true,
		ForceUpdate:      true,
		DuplicateTracker: tracker,
	})
	require.NoError(t, err)
	require.Len(t, winner.Warnings, 1)
	assert.Equal(t, authorizedOverwriteWarning(dst), winner.Warnings[0],
		"the winner replaced the pre-seeded occupant")
	assert.True(t, winner.Moved)
	assert.False(t, winner.DuplicateSkipped)

	// Loser B: batch duplicate AND (at plan time) an occupied destination —
	// both warning signals exist, but the skip demotes to the dup warning
	// alone; the overwrite crumb belongs to whoever moved bytes (winner A).
	loser, err := org.Organize(context.Background(), OrganizeCmd{
		Match:            models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/B.mkv", Name: "B.mkv", Extension: ".mkv"},
		Movie:            &models.Movie{ID: "ABC-123"},
		DestDir:          "/dest",
		MoveFiles:        true,
		ForceUpdate:      true,
		DuplicateTracker: tracker,
	})
	require.NoError(t, err)
	assert.True(t, loser.DuplicateSkipped)
	require.Len(t, loser.Warnings, 1, "exactly one warning — the dup demotion, never doubled")
	assert.Contains(t, loser.Warnings[0], "duplicate destination within batch")
	assert.NotContains(t, loser.Warnings[0], "overwrite authorized: replaced existing destination",
		"the skipped loser replaced nothing — no overwrite crumb")

	// The destination carries exactly the winner's bytes.
	content, readErr := afero.ReadFile(fs, dst)
	require.NoError(t, readErr)
	assert.Equal(t, []byte("a-bytes"), content)
}
