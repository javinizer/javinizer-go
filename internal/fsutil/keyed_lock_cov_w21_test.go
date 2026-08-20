package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProbeCaseSensitiveW21_CollisionRetriesAndStatsCreatedProbe(t *testing.T) {
	root := t.TempDir()
	var opened []string
	// Wave-39 (bound cleanup): after the verdict the take-aside cleanup also
	// stats the CREATE-PATH and its scratch sibling — the double answers the
	// real lookup for names the probe owns and the scripted ENOENT only for
	// the alternate spelling the verdict actually asks about.
	ops := caseProbeOps{
		openFile: func(name string, flag int, perm os.FileMode) (caseProbeFile, error) {
			if flag&os.O_CREATE == 0 {
				// Wave-34 bind leg: O_RDONLY re-open of the verified scratch — the
				// real renamed object answers; it is not a create attempt (the
				// stat legs below assert exactly two created names).
				return os.OpenFile(name, flag, perm)
			}
			opened = append(opened, name)
			if len(opened) == 1 {
				// Model another process winning the first O_EXCL name. This is a
				// collision, not filesystem case evidence.
				return nil, os.ErrExist
			}
			return os.OpenFile(name, flag, perm)
		},
		stat: func(name string) (os.FileInfo, error) {
			require.Len(t, opened, 2)
			if name == opened[1] || name == opened[1]+probeCleanupScratchSuffix {
				return os.Stat(name)
			}
			require.Equal(t, filepath.Join(root, strings.ToUpper(filepath.Base(opened[1]))), name)
			// The alternate spelling is absent, so the created second probe
			// proves this injected root is case-sensitive.
			return nil, os.ErrNotExist
		},
		rename: probeRenameNoReplace,
		remove: func(name string) error {
			require.Equal(t, opened[1]+probeCleanupScratchSuffix, name,
				"cleanup must unlink only the verified probe's scratch name")
			return os.Remove(name)
		},
	}

	got, err := probeCaseSensitive(ops, root)
	require.NoError(t, err)
	require.True(t, got, "EEXIST must not be cached/interpreted as insensitive")
	require.Len(t, opened, 2)
	require.NotEqual(t, opened[0], opened[1], "retry must draw a fresh process-unique name")
}

func TestCaseProbeNameW21_HasProcessEntropyAndIsUnique(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	prefix := ".javinizer_case_probe_" + strconv.Itoa(os.Getpid()) + "_"
	for i := 0; i < 100; i++ {
		name := caseProbeName()
		require.True(t, strings.HasPrefix(name, prefix), "probe name must carry process identity: %q", name)
		_, duplicate := seen[name]
		require.False(t, duplicate, "probe name repeated at iteration %d", i)
		seen[name] = struct{}{}
	}
}

func TestCaseProbeNameW21_EntropyFailureUsesNonSensitiveFallback(t *testing.T) {
	previous := caseProbeRandReader
	t.Cleanup(func() { caseProbeRandReader = previous })

	caseProbeRandReader = strings.NewReader("")
	fallbackName := caseProbeName()
	require.True(t, strings.HasPrefix(fallbackName, ".javinizer_case_probe_"+strconv.Itoa(os.Getpid())+"_"))

	caseProbeRandReader = nil
	nilReaderName := caseProbeName()
	require.NotEqual(t, fallbackName, nilReaderName)
}

func TestProbeCaseSensitiveW21_ExistRetryIsBounded(t *testing.T) {
	var opened []string
	ops := caseProbeOps{
		openFile: func(name string, _ int, _ os.FileMode) (caseProbeFile, error) {
			opened = append(opened, name)
			return nil, os.ErrExist
		},
		stat: func(string) (os.FileInfo, error) {
			t.Fatal("stat must not run when no probe was created")
			return nil, nil
		},
		remove: func(string) error {
			t.Fatal("cleanup must not remove a path that was never created")
			return nil
		},
	}

	got, err := probeCaseSensitive(ops, t.TempDir())
	require.ErrorIs(t, err, os.ErrExist)
	require.False(t, got, "exhausted collisions fail closed")
	require.Len(t, opened, caseProbeMaxAttempts)
	for i := 1; i < len(opened); i++ {
		require.NotEqual(t, opened[i-1], opened[i], "each collision retry must use fresh entropy")
	}
}

func TestProbeCaseSensitiveW21_NonCollisionOpenFailureDoesNotRetry(t *testing.T) {
	openErr := errors.New("read-only probe root")
	calls := 0
	ops := caseProbeOps{
		openFile: func(string, int, os.FileMode) (caseProbeFile, error) {
			calls++
			return nil, openErr
		},
	}

	got, err := probeCaseSensitive(ops, t.TempDir())
	require.ErrorIs(t, err, openErr)
	require.False(t, got)
	require.Equal(t, 1, calls, "permission/I/O errors retain the existing fail-closed path")
}

func TestDestinationProbeRootW21_ReadOnlyAncestorStillUsesStatSelection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only permission semantics are POSIX-specific")
	}
	root := t.TempDir()
	require.NoError(t, os.Chmod(root, 0o500))
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })

	// Root selection remains the pre-existing stat-only fallback: a readable
	// but read-only existing ancestor is selected, and only its later O_EXCL
	// failure determines the fail-closed result.
	require.Equal(t, cleanProbeRoot(root), destinationProbeRoot(filepath.Join(root, "missing", "poster.jpg")))
}
