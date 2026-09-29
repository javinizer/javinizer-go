//go:build !windows

package workflow

import (
	"os"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sysStatInfo injects a fabricated kernel identity into a real FileInfo's
// Sys() so the shared artifactSourceIdentity comparator can be exercised
// with both-sides-strong tuples on any POSIX host. (The Windows handle leg
// deliberately ignores injected Stat_t legs — it pins through the OS handle
// instead — so this file is POSIX-only; the Windows code path is
// compile-verified per GOOS and its decision semantics are exactly the
// comparator legs proven here.)
type sysStatInfo struct {
	os.FileInfo
	sys any
}

func (s sysStatInfo) Sys() any { return s.sys }

// Both-sides-strong is the load-bearing equivalence: an admitted capture
// carrying a volume+file identity matches only a lookup exposing the SAME
// identity; a different file index or volume — the downloader's
// size/mtime-preserving replacement shape (codex P1, the deferred-source
// identity window) — refuses even when every metadata leg is restored. A
// lookup exposing NO identity keeps the documented size+modtime fallback,
// and a weak capture (memfs posture) against a strong lookup likewise keeps
// the metadata legs: identity strength is never imputed to a side that did
// not expose it.
func TestArtifactSourceIdentityStrongTupleComparator(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/m.mp4", []byte("video payload"), 0o644))
	info, err := base.Stat("/m.mp4")
	require.NoError(t, err)

	admitted := artifactSourceIdentity{
		known:     true,
		hasDevIno: true,
		dev:       0x2A,
		ino:       0x2B,
		size:      info.Size(),
		modTime:   info.ModTime(),
	}

	same := sysStatInfo{info, &syscall.Stat_t{Dev: 0x2A, Ino: 0x2B}}
	assert.True(t, admitted.matches(base, "/m.mp4", same),
		"an untouched source with the same strong identity matches")

	foreignIndex := sysStatInfo{info, &syscall.Stat_t{Dev: 0x2A, Ino: 0x2C}}
	assert.False(t, admitted.matches(base, "/m.mp4", foreignIndex),
		"a replacement carrying a different file index refuses — restored size/mtime are not its identity")

	foreignVolume := sysStatInfo{info, &syscall.Stat_t{Dev: 0x30, Ino: 0x2B}}
	assert.False(t, admitted.matches(base, "/m.mp4", foreignVolume),
		"a replacement on another volume refuses even at an equal file index")

	assert.True(t, admitted.matches(base, "/m.mp4", info),
		"a lookup exposing no identity keeps the size+modtime fallback legs")

	weak := artifactSourceIdentity{known: true, size: info.Size(), modTime: info.ModTime()}
	assert.True(t, weak.matches(base, "/m.mp4", same),
		"a weak capture (memfs posture) against a strong lookup keeps only the metadata legs — strength is never imputed to a side that did not expose it")

	dirInfo, statErr := base.Stat("/")
	require.NoError(t, statErr)
	require.NoError(t, base.MkdirAll("/d", 0o755))
	dirInfo, statErr = base.Stat("/d")
	require.NoError(t, statErr)
	assert.False(t, admitted.matches(base, "/m.mp4", sysStatInfo{dirInfo, &syscall.Stat_t{Dev: 0x2A, Ino: 0x2B}}),
		"a non-regular directory entry never matches even with the admitted identity injected")
}
