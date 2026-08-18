package fsutil

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// K4 codex finding (P2): the Windows seam previously proved only PID liveness,
// so a recycled PID would hold a destination busy forever. Windows now feeds
// the same start-time discrimination POSIX has (GetProcessTimes behind
// replacementProcessStartTimePlatform). These arms run host-independent
// through the injected probes, modeling exactly what the armed Windows seam
// reports to the classifier.

// replacementStartTimeFromUnixNano guards platform start-time stamps: the
// Windows seam forwards creation.Nanoseconds(), and a zero 1601 FILETIME (or
// any epoch-and-earlier stamp) can never describe a live marker owner, so the
// classifier must keep the documented liveness-only fallback.
func TestReplacementStartTimeFromUnixNano_K4Guard(t *testing.T) {
	nsec := time.Date(2024, 5, 6, 7, 8, 9, 123456789, time.UTC).UnixNano()
	got := replacementStartTimeFromUnixNano(nsec)
	require.NotNil(t, got)
	require.Equal(t, time.Unix(0, nsec), *got)

	require.Nil(t, replacementStartTimeFromUnixNano(0),
		"a zero FILETIME (1601 -> non-positive UnixNano) is unreadable evidence")
	require.Nil(t, replacementStartTimeFromUnixNano(-4611686018427387904),
		"pre-epoch stamps fall back to liveness-only")
}

func k4ArmWindowsProbe(t *testing.T, liveness replacementPIDLiveness, start *time.Time) {
	t.Helper()
	oldAlive := replacementProbePIDAliveAware
	oldStart := replacementProcessStartTime
	oldWindows := replacementIsWindows
	replacementProbePIDAliveAware = func(int) replacementPIDLiveness { return liveness }
	replacementProcessStartTime = func(int) *time.Time { return start }
	replacementIsWindows = true
	t.Cleanup(func() {
		replacementProbePIDAliveAware = oldAlive
		replacementProcessStartTime = oldStart
		replacementIsWindows = oldWindows
	})
}

func k4WriteMarker(t *testing.T, fs afero.Fs, dest string, pid int, created time.Time) []byte {
	t.Helper()
	raw := []byte(fmt.Sprintf("pid=%d,time=%d", pid, created.UnixNano()))
	require.NoError(t, afero.WriteFile(fs, ReplacementBusyPath(dest), raw, 0o600))
	return raw
}

func TestAcquireReplacementBusyK4_WindowsStartTimeArms(t *testing.T) {
	ownerPID := os.Getpid() + 10000

	t.Run("owner from the same tradition stays busy", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		dest := "/out/k4-win-owner/poster.jpg"
		require.NoError(t, fs.MkdirAll("/out/k4-win-owner", 0o755))
		markerTime := time.Now()
		marker := k4WriteMarker(t, fs, dest, ownerPID, markerTime)
		start := markerTime.Add(-time.Second) // process predates its marker: it IS the owner
		k4ArmWindowsProbe(t, replacementPIDAlive, &start)

		_, err := AcquireReplacementBusy(fs, dest)
		require.ErrorIs(t, err, ErrReplacementBusy, "blocked: a live owner whose start predates the marker")
		content, err := afero.ReadFile(fs, ReplacementBusyPath(dest))
		require.NoError(t, err)
		require.Equal(t, marker, content, "a blocked acquire never disturbs the owner's marker")
	})

	t.Run("owner started exactly at the marker is not reuse", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		dest := "/out/k4-win-boundary/poster.jpg"
		require.NoError(t, fs.MkdirAll("/out/k4-win-boundary", 0o755))
		markerTime := time.Now()
		k4WriteMarker(t, fs, dest, ownerPID, markerTime)
		start := markerTime // After() is strict; the boundary stays the owner's
		k4ArmWindowsProbe(t, replacementPIDAlive, &start)

		_, err := AcquireReplacementBusy(fs, dest)
		require.ErrorIs(t, err, ErrReplacementBusy, "blocked: only a strictly-later start proves reuse")
	})

	t.Run("owner started after the marker is a recycled PID", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		dest := "/out/k4-win-reuse/poster.jpg"
		require.NoError(t, fs.MkdirAll("/out/k4-win-reuse", 0o755))
		markerTime := time.Now().Add(-time.Minute)
		k4WriteMarker(t, fs, dest, ownerPID, markerTime)
		start := markerTime.Add(time.Second) // PID outlived the marker: recycled
		k4ArmWindowsProbe(t, replacementPIDAlive, &start)

		release, err := AcquireReplacementBusy(fs, dest)
		require.NoError(t, err, "allowed: the recycling process is not the marker's owner")
		release()
		_, err = fs.Stat(ReplacementBusyPath(dest))
		require.ErrorIs(t, err, os.ErrNotExist, "the recycled owner's takeover and release complete cleanly")
	})

	t.Run("unobtainable start time keeps the liveness-only fallback", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		dest := "/out/k4-win-fallback/poster.jpg"
		require.NoError(t, fs.MkdirAll("/out/k4-win-fallback", 0o755))
		markerTime := time.Now().Add(-replacementBusyStaleAge - time.Minute)
		k4WriteMarker(t, fs, dest, ownerPID, markerTime)
		// GetProcessTimes denied (e.g. a protected process): the positive
		// liveness proof still wins, even past the stale age.
		k4ArmWindowsProbe(t, replacementPIDAlive, nil)

		_, err := AcquireReplacementBusy(fs, dest)
		require.ErrorIs(t, err, ErrReplacementBusy,
			"blocked: liveness-only fallback when the start time is unreadable")
	})

	t.Run("unprobeable Windows owner stays non-expiring", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		dest := "/out/k4-win-unprobeable/poster.jpg"
		require.NoError(t, fs.MkdirAll("/out/k4-win-unprobeable", 0o755))
		markerTime := time.Now().Add(-replacementBusyStaleAge - time.Minute)
		marker := k4WriteMarker(t, fs, dest, ownerPID, markerTime)
		// Access denied on OpenProcess: undecidable, and Windows deliberately
		// does not expire an unprobeable owner by age.
		k4ArmWindowsProbe(t, replacementPIDUnprobeable, nil)

		_, err := AcquireReplacementBusy(fs, dest)
		require.ErrorIs(t, err, ErrReplacementBusy)
		content, err := afero.ReadFile(fs, ReplacementBusyPath(dest))
		require.NoError(t, err)
		require.Equal(t, marker, content)
	})
}
