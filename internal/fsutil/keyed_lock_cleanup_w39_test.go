package fsutil

// POSTER-WRITE-HARDENING wave-39 (codex P2, PR#215 findings C1/C2 + Windows
// CI run 32315155818 test-vs-platform repairs):
//
//   - C1: probeCaseSensitive's indeterminate alternate-stat fell through to
//     a readDir enumeration that necessarily matched the probe's OWN name
//     via EqualFold — the root was then permanently cached as
//     case-INSENSITIVE even on a sensitive volume, aliasing DestKeys for
//     byte-distinct files. The indeterminate lookup now fails closed with
//     the stat error (uncached, per the wave-25 retry contract) and the
//     enumeration leg is gone.
//   - C2: both probes' cleanup unlinked the mutable create-path by bare
//     pathname — a watcher renaming the probe away and parking a substitute
//     at that name got its own object deleted. The cleanup is now the bound
//     take-aside (boundProbeCleanup): re-prove the create-path occupant
//     against the handle-captured identity, no-replace move to a scratch
//     sibling, re-prove at the scratch, unlink ONLY the scratch;
//     mismatches/substitutes are left entirely alone.
//   - W33: scripted probe doubles must CLOSE the real on-disk create before
//     answering with their fake handle — a leaked handle blocks the cleanup
//     unlink outright on Windows ("file being used by another process").

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// w39BindIdentity installs the pointer-equality identity seam for the fake
// FileInfos the bound-cleanup legs compare (the w38 probe seam discipline).
func w39BindIdentity(t *testing.T) {
	t.Helper()
	prev := probeSameFile
	probeSameFile = func(a, b os.FileInfo) bool { return a == b }
	t.Cleanup(func() { probeSameFile = prev })
}

// boundProbeCleanup leg table, driven directly so every wedge answer is
// deterministic and host-independent.
func TestBoundProbeCleanupW39_Legs(t *testing.T) {
	w39BindIdentity(t)
	created := &w38ProbeInfo{}
	other := &w38ProbeInfo{}
	path := filepath.Join(t.TempDir(), ".javinizer_case_probe_x")
	scratch := path + probeCleanupScratchSuffix

	unusedStat := func(string) (os.FileInfo, error) { t.Fatal("unused stat"); return nil, nil }
	unusedRename := func(string, string) error { t.Fatal("unused rename"); return nil }
	unusedRemove := func(string) error { t.Fatal("unused remove"); return nil }
	// Wave-34 bind leg (bindScratchForUnlink): the verified scratch is
	// re-opened O_RDONLY and must re-prove THE created identity by
	// descriptor before the unlink runs. Legs reaching the unlink answer
	// that read-open with the same fake identity; legs wedging earlier keep
	// a nil openFile so a misrouted read-open panics instead of passing.
	bindReadOpen := func(string, int, os.FileMode) (caseProbeFile, error) {
		return w38StatProbeFile{info: created}, nil
	}

	t.Run("nil identity: nothing was provable so the name survives", func(t *testing.T) {
		// Codex P2 (w58): with NO identity capture there is nothing to
		// authenticate — the O_EXCL-derived claim may already have been
		// swapped, so the cleanup never unlinks the pathname; the foreign
		// occupant is preserved and the residual name's lease simply lapses.
		removed := 0
		ops := caseProbeOps{stat: unusedStat, rename: unusedRename, remove: func(name string) error {
			removed++
			return nil
		}}
		require.NoError(t, boundProbeCleanup(ops, path, nil))
		require.Zero(t, removed, "unproven claims are never pathname-unlinked")
	})

	t.Run("nil identity: cleanup is a no-op when unproven", func(t *testing.T) {
		// remove must never even be consulted — there is no provable claim.
		ops := caseProbeOps{stat: unusedStat, rename: unusedRename, remove: func(string) error { t.Fatal("unused"); return nil }}
		require.NoError(t, boundProbeCleanup(ops, path, nil))
	})

	t.Run("vanished create-path completes the cleanup itself", func(t *testing.T) {
		ops := caseProbeOps{
			stat:   func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
			rename: unusedRename,
			remove: unusedRemove,
		}
		require.NoError(t, boundProbeCleanup(ops, path, created))
	})

	t.Run("indeterminate create-path stat fails closed", func(t *testing.T) {
		sentinel := errors.New("w39 create-path stat wedged")
		ops := caseProbeOps{
			stat:   func(string) (os.FileInfo, error) { return nil, sentinel },
			rename: unusedRename,
			remove: unusedRemove,
		}
		require.ErrorIs(t, boundProbeCleanup(ops, path, created), sentinel)
	})

	t.Run("substitute at the create-path is left entirely alone", func(t *testing.T) {
		removed := 0
		ops := caseProbeOps{
			stat:   func(string) (os.FileInfo, error) { return other, nil },
			rename: unusedRename,
			remove: func(string) error { removed++; return nil },
		}
		err := boundProbeCleanup(ops, path, created)
		require.ErrorIs(t, err, ErrTakeAsideForeign)
		require.Zero(t, removed, "a substitute is never unlinked by the probe's bare create-path")
	})

	t.Run("take-aside rename failure surfaces wrapped", func(t *testing.T) {
		sentinel := errors.New("w39 rename wedged")
		ops := caseProbeOps{
			stat:   func(name string) (os.FileInfo, error) { require.Equal(t, path, name); return created, nil },
			rename: func(string, string) error { return sentinel },
			remove: unusedRemove,
		}
		err := boundProbeCleanup(ops, path, created)
		require.ErrorIs(t, err, sentinel)
	})

	t.Run("rename-time vanish answers completed", func(t *testing.T) {
		ops := caseProbeOps{
			stat:   func(string) (os.FileInfo, error) { return created, nil },
			rename: func(string, string) error { return os.ErrNotExist },
			remove: unusedRemove,
		}
		require.NoError(t, boundProbeCleanup(ops, path, created))
	})

	t.Run("indeterminate scratch stat fails closed", func(t *testing.T) {
		sentinel := errors.New("w39 scratch stat wedged")
		ops := caseProbeOps{
			stat: func(name string) (os.FileInfo, error) {
				if name == scratch {
					return nil, sentinel
				}
				return created, nil
			},
			rename: func(string, string) error { return nil },
			remove: unusedRemove,
		}
		require.ErrorIs(t, boundProbeCleanup(ops, path, created), sentinel)
	})

	t.Run("scratch vanished after the move completes the cleanup", func(t *testing.T) {
		ops := caseProbeOps{
			stat: func(name string) (os.FileInfo, error) {
				if name == scratch {
					return nil, os.ErrNotExist
				}
				return created, nil
			},
			rename: func(string, string) error { return nil },
			remove: unusedRemove,
		}
		require.NoError(t, boundProbeCleanup(ops, path, created))
	})

	t.Run("substitute under the take-aside is left alone at the scratch", func(t *testing.T) {
		removed := 0
		ops := caseProbeOps{
			stat: func(name string) (os.FileInfo, error) {
				if name == scratch {
					return other, nil
				}
				return created, nil
			},
			rename: func(string, string) error { return nil },
			remove: func(string) error { removed++; return nil },
		}
		err := boundProbeCleanup(ops, path, created)
		require.ErrorIs(t, err, ErrTakeAsideForeign)
		require.Zero(t, removed, "the swapped scratch occupant is never unlinked")
	})

	t.Run("verified unlink failure surfaces wrapped", func(t *testing.T) {
		sentinel := errors.New("w39 scratch remove wedged")
		ops := caseProbeOps{
			openFile: bindReadOpen,
			stat:     func(string) (os.FileInfo, error) { return created, nil },
			rename:   func(string, string) error { return nil },
			remove:   func(string) error { return sentinel },
		}
		require.ErrorIs(t, boundProbeCleanup(ops, path, created), sentinel)
	})

	t.Run("unlink answering ENOENT completes the cleanup", func(t *testing.T) {
		ops := caseProbeOps{
			openFile: bindReadOpen,
			stat:     func(string) (os.FileInfo, error) { return created, nil },
			rename:   func(string, string) error { return nil },
			remove:   func(string) error { return os.ErrNotExist },
		}
		require.NoError(t, boundProbeCleanup(ops, path, created))
	})

	t.Run("happy path renames verified and unlinks only the scratch", func(t *testing.T) {
		var renamed [][2]string
		var removed []string
		ops := caseProbeOps{
			openFile: bindReadOpen,
			stat:     func(string) (os.FileInfo, error) { return created, nil },
			rename:   func(from, to string) error { renamed = append(renamed, [2]string{from, to}); return nil },
			remove:   func(name string) error { removed = append(removed, name); return nil },
		}
		require.NoError(t, boundProbeCleanup(ops, path, created))
		require.Equal(t, [][2]string{{path, scratch}}, renamed)
		require.Equal(t, []string{scratch}, removed)
	})
}

// C1: the case probe's indeterminate alternate lookup fails closed with the
// stat error — no enumeration fallback, nothing cached (the caller-side
// wave-25 contract), and the created probe still leaves through the bound
// cleanup. Pinned end-to-end on the real filesystem.
func TestProbeCaseSensitiveW39_IndeterminateAlternateStatFailsClosed(t *testing.T) {
	statErr := errors.New("w39 indeterminate alternate lookup")
	root := t.TempDir()
	var opened []string
	ops := caseProbeOps{
		openFile: func(name string, flag int, perm os.FileMode) (caseProbeFile, error) {
			opened = append(opened, name)
			return os.OpenFile(name, flag, perm)
		},
		stat: func(name string) (os.FileInfo, error) {
			if name == opened[0] || name == opened[0]+probeCleanupScratchSuffix {
				return os.Stat(name) // the bound cleanup's own re-proofs
			}
			return nil, statErr
		},
		rename: probeRenameNoReplace,
		remove: os.Remove,
	}
	got, err := probeCaseSensitive(ops, root)
	require.ErrorIs(t, err, statErr)
	require.False(t, got, "indeterminate is undecidable — never a folded verdict")
	entries, rdErr := os.ReadDir(root)
	require.NoError(t, rdErr)
	require.Empty(t, entries, "the bound cleanup still reaps the created probe on the error leg")
}

// C2 case-probe integration: a watcher planting a substitute at the
// create-path between the capture and the cleanup is refused typed, and the
// substitute's bytes are left intact. Real filesystem end-to-end.
func TestProbeCaseSensitiveW39_SubstituteAtCreatePathRefusesUnlink(t *testing.T) {
	root := t.TempDir()
	side := filepath.Join(root, "w39-foreign-plant")
	var opened []string
	ops := caseProbeOps{
		openFile: func(name string, flag int, perm os.FileMode) (caseProbeFile, error) {
			opened = append(opened, name)
			return os.OpenFile(name, flag, perm)
		},
		stat: func(name string) (os.FileInfo, error) {
			if name == opened[0] || name == opened[0]+probeCleanupScratchSuffix {
				return os.Stat(name)
			}
			// The verdict's alternate lookup: replay the watcher — our probe
			// is renamed away and a FOREIGN object claims the create-path.
			// The substitute arrives by RENAME from a pre-created side file:
			// remove+create at one path can reuse the freed inode on CI
			// filesystems and would alias the created identity itself.
			require.NoError(t, os.WriteFile(side, []byte("w39 foreign substitute"), 0o600))
			require.NoError(t, os.Remove(opened[0]))
			require.NoError(t, os.Rename(side, opened[0]))
			return nil, os.ErrNotExist
		},
		rename: func(string, string) error { t.Fatal("no rename runs for a substitute"); return nil },
		remove: func(string) error { t.Fatal("a substitute is never unlinked"); return nil },
	}
	got, err := probeCaseSensitive(ops, root)
	require.ErrorIs(t, err, ErrTakeAsideForeign)
	require.False(t, got)
	body, rerr := os.ReadFile(opened[0])
	require.NoError(t, rerr)
	require.Equal(t, "w39 foreign substitute", string(body), "the substitute stays byte-intact at the create-path")
}

// C2 normalization-probe twin: same watcher replay on the alternate lookup.
func TestProbeNormalizationInsensitiveW39_SubstituteAtCreatePathRefusesUnlink(t *testing.T) {
	root := t.TempDir()
	side := filepath.Join(root, "w39-foreign-plant")
	var opened []string
	ops := caseProbeOps{
		openFile: func(name string, flag int, perm os.FileMode) (caseProbeFile, error) {
			opened = append(opened, name)
			return os.OpenFile(name, flag, perm)
		},
		stat: func(name string) (os.FileInfo, error) {
			if name == opened[0] || name == opened[0]+probeCleanupScratchSuffix {
				return os.Stat(name)
			}
			require.NoError(t, os.WriteFile(side, []byte("w39 foreign substitute"), 0o600))
			require.NoError(t, os.Remove(opened[0]))
			require.NoError(t, os.Rename(side, opened[0]))
			return nil, os.ErrNotExist
		},
		rename: func(string, string) error { t.Fatal("no rename runs for a substitute"); return nil },
		remove: func(string) error { t.Fatal("a substitute is never unlinked"); return nil },
	}
	got, err := probeNormalizationInsensitive(ops, root)
	require.ErrorIs(t, err, ErrTakeAsideForeign)
	require.False(t, got)
	body, rerr := os.ReadFile(opened[0])
	require.NoError(t, rerr)
	require.Equal(t, "w39 foreign substitute", string(body), "the substitute stays byte-intact at the create-path")
}
