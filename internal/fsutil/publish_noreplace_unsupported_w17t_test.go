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

// The typed refusal must NOT swallow link failures that merely REFUSE this
// publish (permission classes and friends): those keep the pre-wave-17
// degrade into the classified rename leg, so the virtual fallback leg stays
// exercised — a free destination is published by rename, and an occupied one
// still classifies into the typed collision.
func TestPublishNoReplaceFallbackW17T_OtherLinkErrorsStillDegradeToVirtualLeg(t *testing.T) {
	t.Run("free destination publishes via rename", func(t *testing.T) {
		stubPublishNoReplaceLinkW17T(t, syscall.EACCES)
		dir := t.TempDir()
		src := filepath.Join(dir, "staged.tmp")
		dst := filepath.Join(dir, "poster.jpg")
		require.NoError(t, os.WriteFile(src, []byte("staged"), 0o644))

		require.NoError(t, publishNoReplaceFallback(src, dst),
			"a non-unsupported link refusal keeps the classified-rename degrade")
		got, err := os.ReadFile(dst)
		require.NoError(t, err)
		require.Equal(t, "staged", string(got))
		_, err = os.Stat(src)
		require.ErrorIs(t, err, os.ErrNotExist, "the virtual leg's rename consumed the staged name")
	})

	t.Run("occupied destination still classifies as collision", func(t *testing.T) {
		stubPublishNoReplaceLinkW17T(t, syscall.EACCES)
		dir := t.TempDir()
		src := filepath.Join(dir, "staged.tmp")
		dst := filepath.Join(dir, "poster.jpg")
		require.NoError(t, os.WriteFile(src, []byte("staged"), 0o644))
		require.NoError(t, os.WriteFile(dst, []byte("racer"), 0o644))

		err := publishNoReplaceFallback(src, dst)
		require.ErrorIs(t, err, ErrPublishCollision,
			"the degraded leg still refuses occupied destinations with the collision class")
		require.NotErrorIs(t, err, ErrPublishNoReplaceUnsupported)
		got, readErr := os.ReadFile(dst)
		require.NoError(t, readErr)
		require.Equal(t, "racer", string(got), "the virtual classify leg never touches the racer")
	})
}
