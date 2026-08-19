//go:build !windows

package fsutil

// POSTER-WRITE-HARDENING codex PR#215 wave-17 (P2) — "preserve no-replace
// semantics when hard links are unsupported": the pre-wave-17 chain
// renameat2(ENOSYS/EINVAL/EOPNOTSUPP) → hard-link → Stat-then-Rename degrade
// was NOT atomic — a foreign writer occupying the destination inside the
// classify→rename window was silently overwritten, which quietly collapsed
// every caller's collision guarantee on FAT/exFAT-style volumes (exactly the
// volumes whose link(2) answers EPERM/ENOTSUP). The fallback now refuses
// with the typed ErrPublishNoReplaceUnsupported when hard links fail with an
// unsupported-class error, and callers map that onto the same conservative
// leg as a collision.
//
// Like the wave-16 suite, these tests drive publishNoReplaceFallback DIRECTLY
// on any POSIX host — the link failure classes cannot be coerced out of a
// host kernel on demand, so the publishNoReplaceLink seam replays them; the
// full renameat2-degrade → link-unsupported → typed-refusal chain is pinned
// end-to-end on the Linux host in publish_noreplace_unsupported_w17t_linux_test.go.

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func stubPublishNoReplaceLinkW17T(t *testing.T, err error) {
	t.Helper()
	prev := publishNoReplaceLink
	publishNoReplaceLink = func(string, string) error { return err }
	t.Cleanup(func() { publishNoReplaceLink = prev })
}

// Every unsupported-class link(2) refusal (Linux FAT/exFAT answer EPERM,
// Darwin answers ENOTSUP, an unimplemented syscall answers ENOSYS...) now
// surfaces as the typed ErrPublishNoReplaceUnsupported —WITH the underlying
// errno still unwrap-reachable — and is NEVER the collision class: nothing
// is occupied, the primitive is simply inexpressible. Neither side moves.
func TestPublishNoReplaceFallbackW17T_UnsupportedLinkClassesRefuseTyped(t *testing.T) {
	for _, errno := range []error{syscall.EPERM, syscall.ENOSYS, syscall.EOPNOTSUPP, syscall.ENOTSUP} {
		t.Run(errno.Error(), func(t *testing.T) {
			stubPublishNoReplaceLinkW17T(t, errno)
			dir := t.TempDir()
			src := filepath.Join(dir, "staged.tmp")
			dst := filepath.Join(dir, "poster.jpg")
			require.NoError(t, os.WriteFile(src, []byte("staged"), 0o644))

			err := publishNoReplaceFallback(src, dst)
			require.ErrorIs(t, err, ErrPublishNoReplaceUnsupported,
				"hard-link-unsupported volumes refuse with the typed class")
			require.ErrorIs(t, err, errno, "the underlying errno stays unwrap-reachable")
			require.NotErrorIs(t, err, ErrPublishCollision,
				"an inexpressible primitive is not an occupied destination")
			got, readErr := os.ReadFile(src)
			require.NoError(t, readErr)
			require.Equal(t, "staged", string(got), "the caller's staged file survives for its kept posture")
			_, statErr := os.Stat(dst)
			require.ErrorIs(t, statErr, os.ErrNotExist, "nothing is published")
		})
	}
}

// Wave-29 (codex P2, PR#215) — "refuse all unsafe hard-link fallback
// failures": ANY link(2) failure that is NEITHER an occupied destination
// (EEXIST → the collision class) NOR an unsupported-class volume answer must
// NEVER degrade into the non-atomic classify-then-rename virtual leg on an
// OsFs — that window is exactly what the fallback exists to close. EMLINK
// (the codex callout), EACCES, EIO, and friends now surface the original
// errno wrapped behind the typed ErrPublishNoReplaceLinkFailed, neither side
// moves, and the class reads as neither a PublishRefusal (the name may be
// absent AND may be occupied — nothing is proven either way) nor a
// PublishCompleted (nothing was installed).
func TestPublishNoReplaceFallbackW29_UnsafeLinkFailuresRefuseTyped(t *testing.T) {
	for _, errno := range []error{syscall.EMLINK, syscall.EACCES, syscall.EIO} {
		t.Run(errno.Error(), func(t *testing.T) {
			stubPublishNoReplaceLinkW17T(t, errno)
			dir := t.TempDir()
			src := filepath.Join(dir, "staged.tmp")
			dst := filepath.Join(dir, "poster.jpg")
			require.NoError(t, os.WriteFile(src, []byte("staged"), 0o644))

			err := publishNoReplaceFallback(src, dst)
			require.ErrorIs(t, err, ErrPublishNoReplaceLinkFailed,
				"wave-29: a non-unsupported link refusal is the typed link-failure class, never a degrade")
			require.ErrorIs(t, err, errno, "the original link error stays unwrap-reachable")
			require.NotErrorIs(t, err, ErrPublishCollision)
			require.NotErrorIs(t, err, ErrPublishNoReplaceUnsupported)
			require.NotErrorIs(t, err, ErrPublishCompleted)
			require.False(t, PublishRefusal(err), "unproven name-ownership is not a refusal class")
			require.False(t, PublishCompleted(err), "nothing was installed")
			got, readErr := os.ReadFile(src)
			require.NoError(t, readErr)
			require.Equal(t, "staged", string(got), "the caller's staged file survives for its kept posture")
			_, statErr := os.Stat(dst)
			require.ErrorIs(t, statErr, os.ErrNotExist, "NOTHING is published — no virtual-leg rename")
		})
	}

	t.Run("an occupied destination is untouched by the typed refusal", func(t *testing.T) {
		stubPublishNoReplaceLinkW17T(t, syscall.EMLINK)
		dir := t.TempDir()
		src := filepath.Join(dir, "staged.tmp")
		dst := filepath.Join(dir, "poster.jpg")
		require.NoError(t, os.WriteFile(src, []byte("staged"), 0o644))
		require.NoError(t, os.WriteFile(dst, []byte("racer"), 0o644))

		err := publishNoReplaceFallback(src, dst)
		require.ErrorIs(t, err, ErrPublishNoReplaceLinkFailed)
		got, readErr := os.ReadFile(dst)
		require.NoError(t, readErr)
		require.Equal(t, "racer", string(got),
			"the pre-wave-29 degrade would have clobbered these bytes in the classify→rename window")
	})
}
