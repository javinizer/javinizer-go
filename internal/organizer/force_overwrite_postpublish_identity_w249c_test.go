package organizer

// PR #249 codex P2 follow-up — the retained force-overwrite crumb keys on the
// publish's displacement answer ALONE (movePublishDestReplaced), never on a
// single error class. The EXDEV staged publish of an authorized move can
// LAND (displacing the resident) and then fail POST-publish with an identity
// class that deliberately does NOT wrap fsutil.ErrPublishCompleted —
// fsutil.ErrPublishStagedIdentityBreak (a post-publish reverify the
// destination lookup could not satisfy / the destination no longer provably
// names the staged inode) and fsutil.ErrPublishStagedExhausted (a staged-name
// substitution outlasted the bounded republish budget; always joined with
// the identity-break class). The pre-fix caller-side predicate
// (fsutil.PublishCompleted(mErr) && replaced) returned the displacement
// WITHOUT the warning for exactly this shape and re-opened the
// hidden-overwrite class PR #249 closed.
//
// These tuples cannot be produced through an fs-level wedge by construction:
// the bound staged publish's post-publish identity machinery runs only
// against the REAL *afero.OsFs (fsutil's osStagingHandle gate), while the
// EXDEV routing a move into that staged publish can only be wedged through a
// VIRTUAL wrapper fs — which flips the identity gate off. fsutil pins the
// real production of every (replaced=true, identity-class) tuple itself
// against the OsFs (move_destreplaced_union_w249b_test.go, wedging its own
// publishStagedBoundDestLstat seam); this file replays that documented tuple
// contract through the moveFileDestReplaced test seam at the exact call edge
// — for the normal organize move lane and BOTH in-place legs — with the
// landed publish's byte state replayed honestly (resident displaced, source
// NEVER consumed: the identity classes keep the caller's backup, #224
// keep-both). The predicate under test (foldMovePublishCrumb) stays OUTSIDE
// the seam, so the replay exercises the production crumb keying itself —
// running these pins against the pre-fix (PublishCompleted-gated) predicate
// fails them.
//
// Every pin asserts the RESULT shape (the w249 partial-publish lineage's
// discipline):
//
//   - the FAILED result carries the crumb in Warnings (the console,
//     eventlog, and worker history surfaces all read that slice);
//   - the error carries the identity class and NOT
//     fsutil.ErrPublishCompleted — the pre-fix gate keyed exactly on that
//     class, so the old predicate provably drops this result's crumb;
//   - Moved stays false and DuplicateSkipped untouched: the failure
//     accumulates a SINGLE failed-and-retained journal row — never a
//     completed move/replace row alongside it;
//   - the compensation axis stays DISTINCT from the evidence axis: without
//     publish-completed typing the destination name is not provably this
//     operation's own object, so the claim does NOT settle (PrePublication
//     stands) — while the audit crumb still discloses that a landed publish
//     destroyed resident bytes.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
)

// identityBreakReplayError mirrors fsutil's indeterminate post-publish
// reverify shape verbatim (publish_staged_bound_unix.go): the lookup could
// not prove which object the destination names — wrapping the identity-break
// sentinel and NEVER ErrPublishCompleted.
func identityBreakReplayError(dst string) error {
	return fmt.Errorf("post-publish publish reverify of %s: %w: %w", dst,
		&os.PathError{Op: "lstat", Path: dst, Err: syscall.EIO},
		fsutil.ErrPublishStagedIdentityBreak)
}

// republishExhaustedReplayError mirrors fsutil's bounded-republish refusal
// verbatim (publish_staged_bound_unix.go): staged-name substitution outlasted
// the budget, always joined with the identity-break class, NEVER
// ErrPublishCompleted.
func republishExhaustedReplayError(dst string) error {
	return fmt.Errorf("%w after %d attempts for %s: %w",
		fsutil.ErrPublishStagedExhausted, fsutil.PublishStagedBoundAttempts, dst,
		fsutil.ErrPublishStagedIdentityBreak)
}

// replayMovePostPublishFailure arms the moveFileDestReplaced seam for one
// (src, dst) pair and replays fsutil's failed-with-displacement contract at
// that exact call edge, byte-honestly:
//
//   - holds == nil: the destination is left carrying THIS operation's staged
//     bytes (the published object itself stood at the destination when the
//     reverify went indeterminate — the identity-break shape);
//   - holds != nil: the destination is left carrying a preserved post-publish
//     occupant (the wave-38 lineage never unverified-deletes a mismatch
//     occupant — the exhaustion shape; the RESIDENT was still displaced by an
//     earlier landed attempt);
//   - the source is NEVER consumed (the identity classes keep the caller's
//     backup — #224 keep-both);
//   - replaced answers fsutil's unioned displacement evidence exactly as the
//     real verb does (true only when a landed publish leg displaced resident
//     bytes);
//   - classError supplies the typed identity-class error in its production
//     wrap shape.
//
// Every unrelated (src, dst) pair delegates to the real verb. The returned
// flag proves the seam fired at the intended publish.
func replayMovePostPublishFailure(t *testing.T, src, dst string, holds []byte, replaced bool, classError func(dst string) error) *atomic.Bool {
	t.Helper()
	prev := moveFileDestReplaced
	var fired atomic.Bool
	moveFileDestReplaced = func(fs afero.Fs, gotSrc, gotDst string) (bool, error) {
		if filepath.Clean(gotSrc) != filepath.Clean(src) || filepath.Clean(gotDst) != filepath.Clean(dst) {
			return prev(fs, gotSrc, gotDst)
		}
		fired.Store(true)
		landed := holds
		if landed == nil {
			staged, rerr := afero.ReadFile(fs, gotSrc)
			require.NoError(t, rerr, "the replay stages from the source exactly like the real verb")
			landed = staged
		}
		require.NoError(t, afero.WriteFile(fs, gotDst, landed, 0o644),
			"replay the landed publish's byte state over the destination — the resident is displaced")
		return replaced, classError(gotDst)
	}
	t.Cleanup(func() { moveFileDestReplaced = prev })
	return &fired
}

// assertFailedWithDisplacementCrumb pins the failed-move result contract
// shared by every post-publish identity pin (see the file header): crumb on
// the FAILED result, identity-class typing with NO publish-completed
// wrapping, single failed-and-retained journal row accumulation (never
// double-journaled as a clean move), and the compensation axis kept distinct
// (no claim settlement — PrePublication stands).
func assertFailedWithDisplacementCrumb(t *testing.T, result *OrganizeResult, err error, dst string, classes ...error) {
	t.Helper()
	require.Error(t, err)
	for _, class := range classes {
		assert.True(t, errors.Is(err, class), "the typed identity class stays unwrap-reachable: %v", class)
	}
	assert.False(t, errors.Is(err, fsutil.ErrPublishCompleted),
		"the identity classes never wrap publish-completed — the pre-fix crumb gate keyed exactly on that class and dropped this crumb")
	require.NotNil(t, result)
	assert.False(t, result.Moved,
		"the source is retained — this failure journals ONCE as the failed-and-retained row, never as a clean move/replace alongside it")
	assert.False(t, result.DuplicateSkipped)
	assert.True(t, result.PrePublication,
		"the compensation axis stays DISTINCT from the evidence axis: without publish-completed typing the destination name is not provably this operation's own object, so the claim does not settle")
	assert.Equal(t, filepath.ToSlash(dst), filepath.ToSlash(result.NewPath),
		"the failed event still names the destination the displacement happened against")
	require.Len(t, result.Warnings, 1,
		"the displaced resident bytes keep their audit crumb on the FAILED result — the regression this wave closes")
	assert.Equal(t, authorizedOverwriteWarning(dst), filepath.ToSlash(result.Warnings[0]))
}

// postPublishMoveLaneFixture names one move lane's replay fixture.
type postPublishMoveLaneFixture struct {
	name  string
	setup func(t *testing.T) (org *Organizer, fs afero.Fs, src, dst string, cmd OrganizeCmd)
}

// postPublishOrganizeMoveLane seeds the organize-mode duplex fixture (the
// w249 lineage's convention: sources under /in, destinations compute
// /dest/<ID>/<ID>.mkv).
func postPublishOrganizeMoveLane(t *testing.T) (*Organizer, afero.Fs, string, string, OrganizeCmd) {
	t.Helper()
	base := afero.NewMemMapFs()
	const (
		src = "/in/A.mkv"
		dst = "/dest/ABC-123/ABC-123.mkv"
	)
	require.NoError(t, base.MkdirAll("/in", 0o755))
	require.NoError(t, afero.WriteFile(base, src, []byte("winner-bytes"), 0o644))
	require.NoError(t, base.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, afero.WriteFile(base, dst, []byte("resident-bytes"), 0o644))
	org := NewOrganizer(base, &Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}, nil, nil)
	return org, base, src, dst, forceAuditCmd(src, true, true)
}

// postPublishInPlaceMoveLane seeds the in-place non-rename move lane's
// fixture (FileFormat diverges the filename inside the source's own
// directory, so the plan takes the plain file-move leg).
func postPublishInPlaceMoveLane(t *testing.T) (*Organizer, afero.Fs, string, string, OrganizeCmd) {
	t.Helper()
	base := afero.NewMemMapFs()
	const (
		src = "/pool/mixed/ABC-100 ownA.mkv"
		dst = "/pool/mixed/ABC-100 vidfile.mkv"
	)
	require.NoError(t, base.MkdirAll("/pool/mixed", 0o755))
	require.NoError(t, afero.WriteFile(base, src, []byte("a-bytes"), 0o644))
	require.NoError(t, afero.WriteFile(base, "/pool/mixed/DEF-999 other.mkv", []byte("other-tenant"), 0o644))
	require.NoError(t, afero.WriteFile(base, dst, []byte("resident-bytes"), 0o644))
	org := NewOrganizer(base, &Config{
		FolderFormat: "<ID>", FileFormat: "<ID> vidfile",
		RenameFile: true, OperationMode: operationmode.OperationModeInPlace,
	}, nil, ipfMatcher(t))
	cmd := OrganizeCmd{
		Match:       models.FileMatchInfo{MovieID: "ABC-100", Path: src, Name: "ABC-100 ownA.mkv", Extension: ".mkv"},
		Movie:       &models.Movie{ID: "ABC-100"},
		DestDir:     "/dest",
		MoveFiles:   true,
		ForceUpdate: true,
	}
	return org, base, src, dst, cmd
}

// postPublishNoRenameFolderLane seeds the in-place-norenamefolder lane's
// fixture (file rename only, directly in the pooled directory).
func postPublishNoRenameFolderLane(t *testing.T) (*Organizer, afero.Fs, string, string, OrganizeCmd) {
	t.Helper()
	base := afero.NewMemMapFs()
	const (
		src = "/pool/ABC-100 ownA.mkv"
		dst = "/pool/ABC-100 vidfile.mkv"
	)
	require.NoError(t, base.MkdirAll("/pool", 0o755))
	require.NoError(t, afero.WriteFile(base, src, []byte("a-bytes"), 0o644))
	require.NoError(t, afero.WriteFile(base, dst, []byte("resident-bytes"), 0o644))
	org := NewOrganizer(base, &Config{
		FolderFormat: "<ID>", FileFormat: "<ID> vidfile",
		RenameFile: true, OperationMode: operationmode.OperationModeInPlaceNoRenameFolder,
	}, nil, nil)
	cmd := OrganizeCmd{
		Match:       models.FileMatchInfo{MovieID: "ABC-100", Path: src, Name: "ABC-100 ownA.mkv", Extension: ".mkv"},
		Movie:       &models.Movie{ID: "ABC-100"},
		DestDir:     "/dest",
		MoveFiles:   true,
		ForceUpdate: true,
	}
	return org, base, src, dst, cmd
}

func TestForceOverwriteAudit_PostPublishFailureCrumb_MoveLanes(t *testing.T) {
	lanes := []postPublishMoveLaneFixture{
		{name: "organize move lane", setup: postPublishOrganizeMoveLane},
		{name: "in-place non-rename move lane", setup: postPublishInPlaceMoveLane},
		{name: "in-place-norenamefolder lane", setup: postPublishNoRenameFolderLane},
	}
	for _, lane := range lanes {
		t.Run(lane.name+"/identity-break: FAILED result carries the crumb", func(t *testing.T) {
			org, fs, src, dst, cmd := lane.setup(t)
			fired := replayMovePostPublishFailure(t, src, dst, nil, true, identityBreakReplayError)

			result, err := org.Organize(context.Background(), cmd)
			require.True(t, fired.Load(), "the seam fired at the move publish %s -> %s", src, dst)
			assertFailedWithDisplacementCrumb(t, result, err, dst, fsutil.ErrPublishStagedIdentityBreak)

			content, derr := afero.ReadFile(fs, dst)
			require.NoError(t, derr, "the publish landed — the destination carries this operation's bytes")
			retained, serr := afero.ReadFile(fs, src)
			require.NoError(t, serr, "the source is preserved byte-intact (#224 keep-both)")
			assert.Equal(t, retained, content,
				"the destination now names the SOURCE's bytes — the resident was really displaced by the publish the identity break post-dates")
			assert.NotEqual(t, "resident-bytes", string(content))
		})

		t.Run(lane.name+"/republish exhaustion: FAILED result carries the crumb", func(t *testing.T) {
			org, fs, src, dst, cmd := lane.setup(t)
			const plant = "foreign-plant"
			fired := replayMovePostPublishFailure(t, src, dst, []byte(plant), true, republishExhaustedReplayError)

			result, err := org.Organize(context.Background(), cmd)
			require.True(t, fired.Load(), "the seam fired at the move publish %s -> %s", src, dst)
			assertFailedWithDisplacementCrumb(t, result, err, dst,
				fsutil.ErrPublishStagedExhausted, fsutil.ErrPublishStagedIdentityBreak)

			content, derr := afero.ReadFile(fs, dst)
			require.NoError(t, derr)
			assert.Equal(t, []byte(plant), content,
				"the post-publish occupant is preserved byte-intact (never unverified-deleted); the crumb discloses the RESIDENT's displacement regardless")
			_, serr := afero.ReadFile(fs, src)
			require.NoError(t, serr, "the source is preserved (#224 keep-both — the identity classes consume nothing)")
		})
	}
}

func TestForceOverwriteAudit_PostPublishIdentityBreak_NoDisplacementStaysSilent(t *testing.T) {
	t.Run("organize move lane: publish landed into a VACANCY — indeterminate reverify, nothing displaced, no crumb", func(t *testing.T) {
		base := afero.NewMemMapFs()
		const (
			src = "/in/A.mkv"
			dst = "/dest/ABC-123/ABC-123.mkv"
		)
		require.NoError(t, base.MkdirAll("/in", 0o755))
		require.NoError(t, afero.WriteFile(base, src, []byte("winner-bytes"), 0o644))
		require.NoError(t, base.MkdirAll(filepath.Dir(dst), 0o755))
		org := NewOrganizer(base, &Config{
			FolderFormat:  "<ID>",
			FileFormat:    "<ID>",
			RenameFile:    true,
			OperationMode: operationmode.OperationModeOrganize,
		}, nil, nil)
		fired := replayMovePostPublishFailure(t, src, dst, nil, false, identityBreakReplayError)

		result, err := org.Organize(context.Background(), forceAuditCmd(src, true, true))
		require.True(t, fired.Load(), "the seam fired at the move publish")
		require.Error(t, err)
		assert.True(t, errors.Is(err, fsutil.ErrPublishStagedIdentityBreak))
		assert.False(t, errors.Is(err, fsutil.ErrPublishCompleted))
		require.NotNil(t, result)
		assert.False(t, result.Moved)
		assert.True(t, result.PrePublication,
			"no publish-completed typing — the compensation axis keeps its distinct non-settling posture")
		assert.Empty(t, result.Warnings,
			"nothing was displaced — the crumb never fires on unconfirmed evidence, whatever the error class")

		content, derr := afero.ReadFile(base, dst)
		require.NoError(t, derr, "the publish landed into the vacancy")
		assert.Equal(t, []byte("winner-bytes"), content)
		retained, serr := afero.ReadFile(base, src)
		require.NoError(t, serr, "the source is preserved byte-intact (#224 keep-both)")
		assert.Equal(t, []byte("winner-bytes"), retained)
	})
}
