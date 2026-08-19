package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

type w10ProbeFile struct {
	err error
}

func (f w10ProbeFile) Close() error { return f.err }

// Stat satisfies the wave-38 probe-handle identity channel; close-failure
// legs never reach a verdict comparison, so a nil identity suffices.
func (w10ProbeFile) Stat() (os.FileInfo, error) { return nil, nil }

func w10RealProbeOps() caseProbeOps {
	return caseProbeOps{
		openFile: func(name string, flag int, perm os.FileMode) (caseProbeFile, error) {
			return os.OpenFile(name, flag, perm)
		},
		stat:    os.Stat,
		readDir: os.ReadDir,
		remove:  os.Remove,
	}
}

func TestDestKey_CaseProbeCacheAndConditionalFolding(t *testing.T) {
	previous := CaseSensitiveProbe
	t.Cleanup(func() {
		CaseSensitiveProbe = previous
		ResetCaseSensitivityCache()
	})

	sensitiveRoot := t.TempDir()
	sensitiveCalls := 0
	CaseSensitiveProbe = func(root string) (bool, error) {
		sensitiveCalls++
		require.Equal(t, cleanProbeRoot(sensitiveRoot), root)
		return true, nil
	}
	ResetCaseSensitivityCache()
	require.NotEqual(t,
		DestKeyForRoot(sensitiveRoot, filepath.Join(sensitiveRoot, "Poster.jpg")),
		DestKeyForRoot(sensitiveRoot, filepath.Join(sensitiveRoot, "poster.jpg")),
		"case-sensitive roots retain case")
	require.Equal(t, 1, sensitiveCalls, "a root is probed once and then served from cache")
	require.Equal(t, cleanProbeRoot(sensitiveRoot), destinationProbeRoot(filepath.Join(sensitiveRoot, "missing", "poster.jpg")),
		"missing destination paths probe their nearest existing ancestor")

	insensitiveRoot := t.TempDir()
	CaseSensitiveProbe = func(string) (bool, error) { return false, nil }
	ResetCaseSensitivityCache()
	require.Equal(t,
		DestKeyForRoot(insensitiveRoot, filepath.Join(insensitiveRoot, "Poster.jpg")),
		DestKeyForRoot(insensitiveRoot, filepath.Join(insensitiveRoot, "poster.jpg")),
		"insensitive roots preserve folded matching")
}

func TestIsCaseSensitiveRoot_ProbeFailureRetriesAndNilFallbackCaches(t *testing.T) {
	previous := CaseSensitiveProbe
	t.Cleanup(func() {
		CaseSensitiveProbe = previous
		ResetCaseSensitivityCache()
	})

	root := t.TempDir()
	calls := 0
	probeErr := errors.New("unwritable")
	CaseSensitiveProbe = func(string) (bool, error) {
		calls++
		return true, probeErr
	}
	ResetCaseSensitivityCache()
	// Codex P2: a probe ERROR leaves the case decision undecidable, so the
	// conservative posture preserves case distinctions instead of folding on a
	// guess; folding requires a positive insensitivity determination.
	require.True(t, IsCaseSensitiveRoot(root), "probe failure preserves case distinctions")
	// Wave-25 (codex P3 PR#215): the error outcome is NOT cached — caching a
	// transient probe failure would permanently split keys for spellings that
	// address ONE file. Every derivation while the root stays broken probes
	// anew, and the very next one after recovery folds correctly.
	require.True(t, IsCaseSensitiveRoot(root), "an uncached failure keeps the conservative posture per call")
	require.Equal(t, 2, calls, "probe failure is not cached — the second derivation re-probes")

	upper := filepath.Join(root, "A", "x.jpg")
	lower := filepath.Join(root, "a", "x.jpg")
	require.NotEqual(t, DestKeyForRoot(root, upper), DestKeyForRoot(root, lower),
		"first-probe-error keeps spellings distinct only while the root stays undecidable")
	CaseSensitiveProbe = func(string) (bool, error) { calls++; return false, nil }
	require.False(t, IsCaseSensitiveRoot(root), "the retried probe folds after the transient failure clears")
	require.Equal(t, DestKeyForRoot(root, upper), DestKeyForRoot(root, lower),
		"first-probe-error → second derivation re-probes and folds correctly afterward")
	require.False(t, IsCaseSensitiveRoot(root), "the recovered definitive outcome IS cached")
	require.Equal(t, 5, calls, "every undecidable derivation re-probed (two direct + one per DestKeyForRoot); the recovered definitive publish is then served from cache")

	CaseSensitiveProbe = nil
	ResetCaseSensitivityCache()
	require.False(t, IsCaseSensitiveRoot(t.TempDir()), "the nil probe seam is a deliberate forced-insensitive test posture, not a probe failure")
	require.False(t, IsCaseSensitiveRoot(""), "an empty root is normalized before probing")
}

func TestDestinationProbeRoot_StopsAtUnstatableFilesystemRoot(t *testing.T) {
	previous := probeRootStat
	t.Cleanup(func() { probeRootStat = previous })
	probeRootStat = func(string) (os.FileInfo, error) { return nil, errors.New("stat unavailable") }
	require.Equal(t, cleanProbeRoot("/"), destinationProbeRoot("/"))
}

// Probe filenames are process-unique; this test intentionally verifies the
// stat/enumeration behavior without depending on the old fixed basename.
func TestProbeCaseSensitive_CoversStatAndEnumerationPaths(t *testing.T) {
	root := t.TempDir()
	base := w10RealProbeOps()
	notFound := func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }

	// Wave-38 (finding F5): an alternate spelling that stats but addresses a
	// DIFFERENT object than the created probe (here: the root directory
	// itself) is NOT case evidence — only an identity match against the
	// open handle's snapshot proves insensitivity.
	base.stat = func(string) (os.FileInfo, error) { return os.Stat(root) }
	got, err := probeCaseSensitive(base, root)
	require.NoError(t, err)
	require.True(t, got, "an alternate spelling naming a different object stays sensitive")

	base.stat = notFound
	got, err = probeCaseSensitive(base, root)
	require.NoError(t, err)
	require.True(t, got, "a missing alternate spelling is sensitive")

	statErr := errors.New("stat indeterminate")
	base.stat = func(string) (os.FileInfo, error) { return nil, statErr }
	base.readDir = func(string) ([]os.DirEntry, error) { return nil, nil }
	got, err = probeCaseSensitive(base, root)
	require.NoError(t, err)
	require.True(t, got, "an enumeration without a folded name is sensitive")

	base.readDir = os.ReadDir
	got, err = probeCaseSensitive(base, root)
	require.NoError(t, err)
	require.False(t, got, "enumeration finds the created name when stat is indeterminate")

	readErr := errors.New("enumeration unavailable")
	base.readDir = func(string) ([]os.DirEntry, error) { return nil, readErr }
	got, err = probeCaseSensitive(base, root)
	require.ErrorIs(t, err, readErr)
	require.False(t, got, "enumeration failure fails closed")

	got, err = defaultCaseSensitiveProbe(root)
	require.NoError(t, err)
	_ = got // The host filesystem may be either case-sensitive or tolerant.

	previous := CaseSensitiveProbe
	t.Cleanup(func() {
		CaseSensitiveProbe = previous
		ResetCaseSensitivityCache()
	})
	CaseSensitiveProbe = defaultCaseSensitiveProbe
	ResetCaseSensitivityCache()
	actual := IsCaseSensitiveRoot(root)
	upper := DestKeyForRoot(root, filepath.Join(root, "Poster.jpg"))
	lower := DestKeyForRoot(root, filepath.Join(root, "poster.jpg"))
	if actual {
		require.NotEqual(t, upper, lower)
	} else {
		require.Equal(t, upper, lower)
	}
}

func TestProbeCaseSensitive_CoversOpenCloseAndCleanupFailures(t *testing.T) {
	root := t.TempDir()
	openErr := errors.New("open failed")
	ops := caseProbeOps{
		openFile: func(string, int, os.FileMode) (caseProbeFile, error) { return nil, openErr },
	}
	got, err := probeCaseSensitive(ops, root)
	require.ErrorIs(t, err, openErr)
	require.False(t, got)

	closeErr := errors.New("close failed")
	ops = caseProbeOps{
		openFile: func(string, int, os.FileMode) (caseProbeFile, error) { return w10ProbeFile{err: closeErr}, nil },
		remove:   func(string) error { return nil },
	}
	got, err = probeCaseSensitive(ops, root)
	require.ErrorIs(t, err, closeErr)
	require.False(t, got)

	cleanupErr := errors.New("cleanup failed")
	ops = caseProbeOps{
		openFile: func(string, int, os.FileMode) (caseProbeFile, error) { return w10ProbeFile{}, nil },
		stat:     func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
		remove:   func(string) error { return cleanupErr },
	}
	got, err = probeCaseSensitive(ops, root)
	require.ErrorIs(t, err, cleanupErr)
	require.False(t, got)
}
