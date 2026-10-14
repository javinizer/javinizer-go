//go:build darwin || freebsd || netbsd

package history

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// This fixture must run natively: a synthetic Stat_t cannot prove that the
// platform's real file.Stat supplies the Ctimespec mutation witness.
func TestPrimaryUndoBSDNativeMutationWitness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "native-witness")
	require.NoError(t, os.WriteFile(path, []byte("synthetic fixture"), 0600))
	file, err := os.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })

	info, err := file.Stat()
	require.NoError(t, err)
	stat, ok := info.Sys().(*syscall.Stat_t)
	require.True(t, ok, "native file.Stat must expose *syscall.Stat_t")
	require.NotNil(t, stat)
	want := primaryUndoChangeWitness{
		Seconds:     int64(stat.Ctimespec.Sec),
		Nanoseconds: int64(stat.Ctimespec.Nsec),
	}
	require.NotEqual(t, primaryUndoChangeWitness{}, want, "fixture must supply an actual native change time")
	got, err := primaryUndoObserveChange(file)
	require.NoError(t, err)
	require.Equal(t, want, got, "witness must preserve both native Ctimespec fields")
}

func TestPrimaryUndoBSDMutationWitnessStatError(t *testing.T) {
	sentinel := errors.New("synthetic native Stat failure")
	got, err := primaryUndoObserveChange(&errPinnedStatFile{err: sentinel})
	require.ErrorIs(t, err, sentinel)
	require.Equal(t, primaryUndoChangeWitness{}, got)
}

type primaryUndoBSDSysInfo struct {
	os.FileInfo
	sys any
}

func (i primaryUndoBSDSysInfo) Sys() any { return i.sys }

func TestPrimaryUndoBSDMutationWitnessRefusesNonNativeStat(t *testing.T) {
	for _, tc := range []struct {
		name string
		sys  any
	}{
		{name: "nonnative Sys", sys: struct{}{}},
		{name: "typed nil native Stat", sys: (*syscall.Stat_t)(nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := w25FakeStatFile(nil, primaryUndoBSDSysInfo{sys: tc.sys})
			got, err := primaryUndoObserveChange(file)
			require.ErrorContains(t, err, "primary Undo requires a native mutation witness")
			require.Equal(t, primaryUndoChangeWitness{}, got)
		})
	}
}
