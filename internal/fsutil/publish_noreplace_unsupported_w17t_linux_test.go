//go:build linux

package fsutil

// POSTER-WRITE-HARDENING codex PR#215 wave-17 (P2) — Linux end-to-end pin for
// "preserve no-replace semantics when hard links are unsupported": when the
// kernel cannot express renameat2(RENAME_NOREPLACE) (ENOSYS / EINVAL /
// EOPNOTSUPP degrade) AND the volume cannot express hard links (link(2)
// EPERM/ENOSYS/EOPNOTSUPP/ENOTSUP), PublishNoReplace on a real OsFs must
// refuse with the typed ErrPublishNoReplaceUnsupported end-to-end — never
// degrade into replacing semantics. The stubs replay both kernel responses
// through the existing seams (stubRenameNoReplaceKernelW16 for the wave-16
// renameat2 seam, the publishNoReplaceLink seam for the link refusal);
// publishNoReplaceRemove is left alone so a hypothetical publish would still
// clean up for real.

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func stubPublishNoReplaceLinkW17L(t *testing.T, err error) {
	t.Helper()
	prev := publishNoReplaceLink
	publishNoReplaceLink = func(string, string) error { return err }
	t.Cleanup(func() { publishNoReplaceLink = prev })
}

func TestPublishNoReplaceLinuxW17_UnsupportedKernelAndLinksRefuseEndToEnd(t *testing.T) {
	cases := []struct {
		name   string
		kernel error
		link   error
	}{
		{"enosys kernel + eperm links (FAT/exFAT posture)", syscall.ENOSYS, syscall.EPERM},
		{"einval kernel + enotsup links", syscall.EINVAL, syscall.ENOTSUP},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stubRenameNoReplaceKernelW16(t, tc.kernel)
			stubPublishNoReplaceLinkW17L(t, tc.link)
			dir := t.TempDir()
			src := filepath.Join(dir, "staged.tmp")
			dst := filepath.Join(dir, "poster.jpg")
			require.NoError(t, os.WriteFile(src, []byte("staged"), 0o644))

			err := PublishNoReplace(afero.NewOsFs(), src, dst)
			require.ErrorIs(t, err, ErrPublishNoReplaceUnsupported,
				"both primitives unsupported: the publish refuses end-to-end, never a replacing rename")
			require.ErrorIs(t, err, tc.link)
			require.NotErrorIs(t, err, ErrPublishCollision)
			got, readErr := os.ReadFile(src)
			require.NoError(t, readErr)
			require.Equal(t, "staged", string(got), "the caller's armed/staged bytes survive untouched")
			_, statErr := os.Stat(dst)
			require.ErrorIs(t, statErr, os.ErrNotExist, "nothing is published")
		})
	}
}
