package fsutil

// POSTER-WRITE-HARDENING codex PR#215 w12 (P2): the token-mismatch recovery
// in replacementBusyReturnTakeover reserves the marker path with a 0-byte
// O_EXCL placeholder, then renames the owned takeover file ONTO that
// placeholder. Windows OsFs rename (MoveFileW) refuses an existing
// destination, so the restore failed and left a malformed zero-byte .dlbusy
// marker behind — an ownerless file that no claimant can reclaim, permanently
// blocking overwrites and reverts for that destination. The restore leg now
// routes through ReplaceFile (OsFs → MoveFileExW with
// MOVEFILE_REPLACE_EXISTING; POSIX remains an atomic rename).

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w12TakeoverContentSwapFs makes the post-takeover readback observe bytes
// that differ from the token the stale-marker inspection saw — the
// deterministic form of W28's channel-orchestrated interleave — by rewriting
// the takeover file's content once, right after the claimant's
// marker→takeover rename. The recovery leg then moves the REAL takeover file
// (the swapped bytes) back over its restore placeholder.
type w12TakeoverContentSwapFs struct {
	afero.Fs
	markerPath string
	swapped    []byte
	fired      atomic.Bool
}

func (f *w12TakeoverContentSwapFs) Rename(oldpath, newpath string) error {
	if err := f.Fs.Rename(oldpath, newpath); err != nil {
		return err
	}
	if oldpath == f.markerPath && strings.HasPrefix(newpath, f.markerPath+".takeover-") && f.fired.CompareAndSwap(false, true) {
		if werr := afero.WriteFile(f.Fs, newpath, f.swapped, 0o600); werr != nil {
			return werr
		}
	}
	return nil
}

// Acquire-level recovery on the host: a token mismatch must restore the real
// marker content ONTO the reserved placeholder (replacing it), report busy,
// and leave a well-formed, reclaimable marker — never a stranded 0-byte file.
func TestReplacementBusyW12_TokenMismatchRecoveryReplacesReservedPlaceholder(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/out/w12-mismatch"
	dest := dir + "/poster.jpg"
	markerPath := ReplacementBusyPath(dest)
	require.NoError(t, base.MkdirAll(dir, 0o755))

	original := []byte(fmt.Sprintf("pid=%d,time=%d", 999999999, time.Now().Add(-time.Hour).UnixNano()))
	swapped := []byte(fmt.Sprintf("pid=%d,time=%d", 999999998, time.Now().Add(-time.Hour).UnixNano()))
	require.NoError(t, afero.WriteFile(base, markerPath, original, 0o600))
	setW28DeadProbe(t)

	fs := &w12TakeoverContentSwapFs{Fs: base, markerPath: markerPath, swapped: swapped}
	release, err := AcquireReplacementBusy(fs, dest)
	require.ErrorIs(t, err, ErrReplacementBusy, "a mismatched takeover must back down")
	require.Nil(t, release)
	require.True(t, fs.fired.Load(), "the takeover swap was exercised")

	restored, readErr := afero.ReadFile(base, markerPath)
	require.NoError(t, readErr)
	require.Equal(t, swapped, restored,
		"the recovery installed the takeover's real bytes over the placeholder — no malformed zero-byte marker")
	_, _, parseOK := parseReplacementBusyToken(string(restored))
	require.True(t, parseOK, "the restored marker stays a well-formed Javinizer token, reclaimable by later claimants")
	requireW28NoRecoveryArtifacts(t, base, dir)

	// The restored marker must not permanently block the destination: its
	// dead-PID token is reclaimable and a fresh owner can claim and release it.
	release, err = AcquireReplacementBusy(fs, dest)
	require.NoError(t, err, "the recovered marker is reclaimable — no permanent busy block")
	require.NotNil(t, release)
	release()
	_, statErr := base.Stat(markerPath)
	require.ErrorIs(t, statErr, os.ErrNotExist)
	requireW28NoRecoveryArtifacts(t, base, dir)
}

// Direct unit on the restore helper's free-path leg: the takeover bytes land
// where the helper itself reserved the 0-byte O_EXCL placeholder moments
// earlier — the byte-exact content assertion proves the takeover replaced the
// placeholder rather than the placeholder surviving as a stranded empty
// marker. (The Acquire-level test above covers the same leg end-to-end. POSIX
// host: ReplaceFile is rename; the native Windows OsFs MoveFileEx leg is
// covered by TestReplaceFileWindows_AtomicReplace.)
func TestReplacementBusyW12_ReturnTakeoverRestoresOverReservedPath(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/out/w12-return"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	path := ReplacementBusyPath(dir + "/poster.jpg")
	takeover := path + ".takeover-w12"
	content := []byte("pid=123,time=456")
	require.NoError(t, afero.WriteFile(base, takeover, content, 0o600))

	require.NoError(t, replacementBusyReturnTakeover(base, path, takeover, content, w28TakeoverIdentity(t, base, takeover)))
	restored, readErr := afero.ReadFile(base, path)
	require.NoError(t, readErr)
	require.Equal(t, content, restored,
		"the takeover bytes replaced the helper-reserved placeholder in place (never an empty marker)")
	_, statErr := base.Stat(takeover)
	require.ErrorIs(t, statErr, os.ErrNotExist, "the owned successor is consumed by the restore")
	requireW28NoRecoveryArtifacts(t, base, dir)
}
