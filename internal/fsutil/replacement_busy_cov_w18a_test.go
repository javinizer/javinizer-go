package fsutil

import (
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestAcquireReplacementBusyW18A_WindowsLiveMarkerIgnoresAge(t *testing.T) {
	fs := afero.NewMemMapFs()
	dest := "/out/w18a-live/poster.jpg"
	require.NoError(t, fs.MkdirAll("/out/w18a-live", 0o755))
	pid := os.Getpid() + 1000
	probedPID := 0
	setReplacementW18AProbe(t, func(gotPID int) replacementPIDLiveness {
		probedPID = gotPID
		return replacementPIDAlive
	})
	writeW14ABusyToken(t, fs, dest, pid, time.Now().Add(-replacementBusyStaleAge-time.Minute))
	before, err := afero.ReadFile(fs, ReplacementBusyPath(dest))
	require.NoError(t, err)

	_, err = AcquireReplacementBusy(fs, dest)
	require.ErrorIs(t, err, ErrReplacementBusy, "a live Windows owner stays busy past the old age threshold")
	require.Equal(t, pid, probedPID)
	after, err := afero.ReadFile(fs, ReplacementBusyPath(dest))
	require.NoError(t, err)
	require.Equal(t, before, after, "a live marker must not be removed")
}

func TestAcquireReplacementBusyW18A_WindowsDeadMarkerReclaimsRegardlessOfAge(t *testing.T) {
	fs := afero.NewMemMapFs()
	dest := "/out/w18a-dead/poster.jpg"
	require.NoError(t, fs.MkdirAll("/out/w18a-dead", 0o755))
	pid := os.Getpid() + 1001
	probedPID := 0
	setReplacementW18AProbe(t, func(gotPID int) replacementPIDLiveness {
		probedPID = gotPID
		return replacementPIDDead
	})
	writeW14ABusyToken(t, fs, dest, pid, time.Now())

	release, err := AcquireReplacementBusy(fs, dest)
	require.NoError(t, err, "a well-formed marker whose PID is dead is reclaimable")
	require.Equal(t, pid, probedPID)
	release()
	_, err = fs.Stat(ReplacementBusyPath(dest))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestAcquireReplacementBusyW18A_WindowsUnprobeableMarkerIsRetained(t *testing.T) {
	fs := afero.NewMemMapFs()
	dest := "/out/w18a-unprobeable/poster.jpg"
	require.NoError(t, fs.MkdirAll("/out/w18a-unprobeable", 0o755))
	setReplacementW18AProbe(t, func(int) replacementPIDLiveness {
		return replacementPIDUnprobeable
	})
	writeW14ABusyToken(t, fs, dest, os.Getpid()+1002, time.Now().Add(-replacementBusyStaleAge-time.Minute))
	before, err := afero.ReadFile(fs, ReplacementBusyPath(dest))
	require.NoError(t, err)

	_, err = AcquireReplacementBusy(fs, dest)
	require.ErrorIs(t, err, ErrReplacementBusy, "an unprobeable Windows owner is retained")
	after, err := afero.ReadFile(fs, ReplacementBusyPath(dest))
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestAcquireReplacementBusyW18A_WindowsMalformedOldMarkerRetainsOwnershipUnknown(t *testing.T) {
	fs := afero.NewMemMapFs()
	dest := "/out/w18a-malformed/poster.jpg"
	path := ReplacementBusyPath(dest)
	require.NoError(t, fs.MkdirAll("/out/w18a-malformed", 0o755))
	setReplacementW18AProbe(t, func(int) replacementPIDLiveness {
		t.Fatal("malformed markers must not invoke PID probing")
		return replacementPIDAlive
	})
	require.NoError(t, afero.WriteFile(fs, path, []byte("user-owned bytes"), 0o644))
	old := time.Now().Add(-replacementBusyStaleAge - time.Minute)
	require.NoError(t, fs.Chtimes(path, old, old))

	_, err := AcquireReplacementBusy(fs, dest)
	require.ErrorIs(t, err, ErrReplacementBusy, "an old malformed marker remains retained")
	content, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	require.Equal(t, "user-owned bytes", string(content))
}

func TestAcquireReplacementBusyW18A_POSIXProbeStillDistinguishesLiveAndDead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX Signal(0) seam")
	}
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/out/w18a-posix", 0o755))
	oldWindows := replacementIsWindows
	replacementIsWindows = false
	t.Cleanup(func() { replacementIsWindows = oldWindows })

	liveDest := "/out/w18a-posix/live.jpg"
	writeW14ABusyToken(t, fs, liveDest, os.Getppid(), time.Now())
	_, err := AcquireReplacementBusy(fs, liveDest)
	require.ErrorIs(t, err, ErrReplacementBusy, "POSIX keeps a live foreign owner busy")

	deadDest := "/out/w18a-posix/dead.jpg"
	writeW14ABusyToken(t, fs, deadDest, os.Getpid()+1003, time.Now())
	release, err := AcquireReplacementBusy(fs, deadDest)
	require.NoError(t, err, "POSIX reclaims a dead foreign owner")
	release()
}

func setReplacementW18AProbe(t *testing.T, probe func(int) replacementPIDLiveness) {
	t.Helper()
	oldWindows := replacementIsWindows
	oldProbe := replacementProbePIDAliveAware
	replacementIsWindows = true
	replacementProbePIDAliveAware = probe
	t.Cleanup(func() {
		replacementIsWindows = oldWindows
		replacementProbePIDAliveAware = oldProbe
	})
}
