package fsutil

// POSTER-WRITE-HARDENING wave-38 (codex P2, PR#215 finding F5) — both
// filesystem-semantics probes bind their verdict to the created object's
// identity captured from the OPEN handle BEFORE Close. The pre-wave-38
// create-path re-stat addressed whatever a watcher left AT that name after
// close — a renamed-away probe plus renamed-in successor would masquerade as
// probe evidence and a failed lookup degraded into a permanently cached
// (non-)insensitive verdict. Stat failure fails closed like close failure.

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// w38ProbeInfo is the shared fake FileInfo whose pointer identity models a
// shared inode under the probeSameFile test seam. The unused field keeps
// the struct non-zero-sized: otherwise two distinct allocations collapse
// onto runtime.zerobase and pointer equality can no longer tell a shared
// inode from a successor.
type w38ProbeInfo struct{ marker byte }

func (*w38ProbeInfo) Name() string       { return "probe" }
func (*w38ProbeInfo) Size() int64        { return 0 }
func (*w38ProbeInfo) Mode() os.FileMode  { return 0o600 }
func (*w38ProbeInfo) ModTime() time.Time { return time.Time{} }
func (*w38ProbeInfo) IsDir() bool        { return false }
func (*w38ProbeInfo) Sys() any           { return nil }

// w38StatProbeFile scripts the probe handle's Stat/Close outcomes; the
// create ops still perform the real O_EXCL create so cleanup stays
// name-honest.
type w38StatProbeFile struct {
	info     os.FileInfo
	statErr  error
	closeErr error
}

func (f w38StatProbeFile) Stat() (os.FileInfo, error) { return f.info, f.statErr }
func (f w38StatProbeFile) Close() error               { return f.closeErr }

// A failed handle Stat on the case probe: fail closed (error surfaces,
// conservative posture), created probe still cleaned up.
func TestProbeCaseSensitiveW38_HandleStatFailureFailsClosed(t *testing.T) {
	statErr := errors.New("w38 handle stat wedged")
	removed := 0
	ops := caseProbeOps{
		openFile: func(name string, _ int, _ os.FileMode) (caseProbeFile, error) {
			real, err := os.Create(name)
			if err != nil {
				return nil, err
			}
			// Close the real create's handle BEFORE handing back the scripted
			// fake — a leaked handle would keep Windows from unlinking the
			// probe at cleanup time ("file being used by another process").
			if err := real.Close(); err != nil {
				return nil, err
			}
			return w38StatProbeFile{statErr: statErr}, nil
		},
		stat: func(string) (os.FileInfo, error) {
			t.Fatal("no verdict runs on a failed identity capture")
			return nil, nil
		},
		remove: func(name string) error {
			removed++
			return os.Remove(name)
		},
	}
	got, err := probeCaseSensitive(ops, t.TempDir())
	require.ErrorIs(t, err, statErr)
	require.False(t, got)
	require.Zero(t, removed, "w58: unproven identity never pathname-unlinks — the O_EXCL claim may now be foreign")
}

// The normalization probe twin: failed handle Stat fails closed the same way.
func TestProbeNormalizationInsensitiveW38_HandleStatFailureFailsClosed(t *testing.T) {
	statErr := errors.New("w38 handle stat wedged")
	removed := 0
	ops := caseProbeOps{
		openFile: func(name string, _ int, _ os.FileMode) (caseProbeFile, error) {
			real, err := os.Create(name)
			if err != nil {
				return nil, err
			}
			withClosedW39(t, real)
			return w38StatProbeFile{statErr: statErr}, nil
		},
		stat: func(string) (os.FileInfo, error) {
			t.Fatal("no verdict runs on a failed identity capture")
			return nil, nil
		},
		remove: func(name string) error {
			removed++
			return os.Remove(name)
		},
	}
	got, err := probeNormalizationInsensitive(ops, t.TempDir())
	require.ErrorIs(t, err, statErr)
	require.False(t, got)
	require.Zero(t, removed, "w58: unproven identity never pathname-unlinks")
}

// withClosedW39 closes the real on-disk create behind a scripted probe
// handle BEFORE the fake rides it: the wave-39 bound cleanup unlinks the
// probe for real, and a leaked handle blocks that unlink outright on
// Windows ("file being used by another process" — CI test-vs-platform).
func withClosedW39(t *testing.T, f *os.File) {
	t.Helper()
	require.NoError(t, f.Close())
}

// The identity snapshot outlives close + rename-away: a watcher renames the
// probe and plants a lookalike at the create-path after close; the verdict
// must ride the HANDLE's captured identity, so the successor at the mutable
// create-path is irrelevant — the alternate name naming THE created object
// still proves insensitivity.
func TestProbeCaseSensitiveW38_VerdictRidesHandleIdentity(t *testing.T) {
	sentinel := &w38ProbeInfo{}
	ops := caseProbeOps{
		openFile: func(name string, _ int, _ os.FileMode) (caseProbeFile, error) {
			real, err := os.Create(name)
			if err != nil {
				return nil, err
			}
			withClosedW39(t, real)
			return w38StatProbeFile{info: sentinel}, nil
		},
		// The alternate spelling answers THE created object (same fake
		// identity): insensitive regardless of what the create-path now
		// holds (the watcher already renamed it away — no stat of it runs).
		stat:   func(string) (os.FileInfo, error) { return sentinel, nil },
		rename: probeRenameNoReplace,
		remove: os.Remove,
	}
	prev := probeSameFile
	probeSameFile = func(a, b os.FileInfo) bool { return a == b }
	t.Cleanup(func() { probeSameFile = prev })

	got, err := probeCaseSensitive(ops, t.TempDir())
	require.NoError(t, err)
	require.False(t, got, "alternate spelling == created object proves insensitivity via the handle snapshot")
}

// Normalization twin: same handle-bound verdict.
func TestProbeNormalizationInsensitiveW38_VerdictRidesHandleIdentity(t *testing.T) {
	sentinel := &w38ProbeInfo{}
	var opened []string
	ops := caseProbeOps{
		openFile: func(name string, _ int, _ os.FileMode) (caseProbeFile, error) {
			opened = append(opened, name)
			real, err := os.Create(name)
			if err != nil {
				return nil, err
			}
			withClosedW39(t, real)
			return w38StatProbeFile{info: sentinel}, nil
		},
		stat:   func(string) (os.FileInfo, error) { return sentinel, nil },
		rename: probeRenameNoReplace,
		remove: os.Remove,
	}
	prev := probeSameFile
	probeSameFile = func(a, b os.FileInfo) bool { return a == b }
	t.Cleanup(func() { probeSameFile = prev })

	got, err := probeNormalizationInsensitive(ops, t.TempDir())
	require.NoError(t, err)
	require.True(t, got)
	require.Contains(t, opened[0], "_a\u0308", "the NFD-form probe name was created")
}

// A DISTINCT alternate object is never evidence — via the handle snapshot
// the verdict keeps spellings byte-distinct (the w17B tables pin the same
// on their seeded fakes; these keep the w38 regression name-addressable).
func TestProbeCaseSensitiveW38_DistinctAlternateStaysSensitive(t *testing.T) {
	created := &w38ProbeInfo{}
	var opened []string
	ops := caseProbeOps{
		openFile: func(name string, _ int, _ os.FileMode) (caseProbeFile, error) {
			opened = append(opened, name)
			real, err := os.Create(name)
			if err != nil {
				return nil, err
			}
			withClosedW39(t, real)
			return w38StatProbeFile{info: created}, nil
		},
		// The ALTERNATE lookup answers a distinct object; the wave-39 bound
		// cleanup's stat of the probe's own names answers THE created one.
		stat: func(name string) (os.FileInfo, error) {
			if name == opened[0] || name == opened[0]+probeCleanupScratchSuffix {
				return created, nil
			}
			return &w38ProbeInfo{}, nil
		},
		rename: probeRenameNoReplace,
		remove: os.Remove,
	}
	prev := probeSameFile
	probeSameFile = func(a, b os.FileInfo) bool { return a == b }
	t.Cleanup(func() { probeSameFile = prev })

	got, err := probeCaseSensitive(ops, t.TempDir())
	require.NoError(t, err)
	require.True(t, got, "distinct alternate objects keep the root case-sensitive")
}
