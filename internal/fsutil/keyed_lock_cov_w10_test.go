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
		stat:   os.Stat,
		rename: probeRenameNoReplace,
		remove: os.Remove,
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
// stat legs without depending on the old fixed basename. Wave-39 (codex P2,
// PR#215): the readDir enumeration fallback is GONE — it necessarily matched
// the probe's OWN name via EqualFold, permanently caching a case-INSENSITIVE
// verdict on a sensitive volume; an indeterminate alternate stat now fails
// closed (uncached error), exactly like the normalization probe.
func TestProbeCaseSensitive_CoversStatAndEnumerationPaths(t *testing.T) {
	root := t.TempDir()
	base := w10RealProbeOps()
	// Wave-39 (bound cleanup): scripted stat answers must ONLY shape the
	// verdict's alternate-spelling lookup — the bound cleanup's own stat
	// re-proofs of the created probe and its scratch sibling need the real
	// on-disk object. The double dispatches by which name is asked about.
	var opened []string
	base.openFile = func(name string, flag int, perm os.FileMode) (caseProbeFile, error) {
		opened = append(opened, name)
		return os.OpenFile(name, flag, perm)
	}
	statWith := func(alternate func(string) (os.FileInfo, error)) func(string) (os.FileInfo, error) {
		return func(name string) (os.FileInfo, error) {
			cur := opened[len(opened)-1]
			if name == cur || name == cur+probeCleanupScratchSuffix {
				return os.Stat(name)
			}
			return alternate(name)
		}
	}
	notFound := func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }

	// Wave-38 (finding F5): an alternate spelling that stats but addresses a
	// DIFFERENT object than the created probe (here: the root directory
	// itself) is NOT case evidence — only an identity match against the
	// open handle's snapshot proves insensitivity.
	base.stat = statWith(func(string) (os.FileInfo, error) { return os.Stat(root) })
	got, err := probeCaseSensitive(base, root)
	require.NoError(t, err)
	require.True(t, got, "an alternate spelling naming a different object stays sensitive")

	base.stat = statWith(notFound)
	got, err = probeCaseSensitive(base, root)
	require.NoError(t, err)
	require.True(t, got, "a missing alternate spelling is sensitive")

	// Wave-39: an indeterminate alternate lookup is undecidable — it fails
	// closed with the stat error (uncached) instead of enumerating, which
	// could only ever "find" the probe's own name and fold wrongly.
	statErr := errors.New("stat indeterminate")
	base.stat = statWith(func(string) (os.FileInfo, error) { return nil, statErr })
	got, err = probeCaseSensitive(base, root)
	require.ErrorIs(t, err, statErr)
	require.False(t, got, "an indeterminate alternate lookup fails closed — no enumeration fallback")

	// The undecidable root is NOT cached: the very next derivation re-probes
	// and, once the transient failure clears, folds correctly.
	base.stat = statWith(notFound)
	got, err = probeCaseSensitive(base, root)
	require.NoError(t, err)
	require.True(t, got, "the re-probed root answers from fresh evidence")

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

	// Wave-58: a nil-identity claim (created==nil) is no longer unlinked at
	// all, so a wedged remove is UNREACHABLE — the leg simply observes the
	// verdict; the codex-directed posture never deletes an occupant it
	// cannot prove is ours.
	_ = errors.New("cleanup failed") // retained for the historical shape
	ops = caseProbeOps{
		openFile: func(string, int, os.FileMode) (caseProbeFile, error) { return w10ProbeFile{}, nil },
		stat:     func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
		remove:   func(string) error { t.Fatal("unreachable: w58 never removes an unproven claim"); return nil },
	}
	got, err = probeCaseSensitive(ops, root)
	require.NoError(t, err)
	require.True(t, got, "the alternate was never found at the path — case-sensitive verdict stands")
}
