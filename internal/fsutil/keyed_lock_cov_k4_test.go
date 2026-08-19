package fsutil

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// K4 codex finding (P2): normalizeDestPath used to strings.TrimSpace its
// input, so 'poster.jpg ' (a VALID POSIX name) collided with 'poster.jpg' in
// the lock registry and every DestKey-derived journal bucket. Keys must stay
// byte-distinct for byte-distinct names; these tests pin that contract.

func k4SetSeams(t *testing.T, separators, caseSensitive bool) {
	t.Helper()
	previousSeparators := PathBackslashesAreSeparators
	previousProbe := CaseSensitiveProbe
	PathBackslashesAreSeparators = separators
	CaseSensitiveProbe = func(string) (bool, error) { return caseSensitive, nil }
	ResetCaseSensitivityCache()
	t.Cleanup(func() {
		PathBackslashesAreSeparators = previousSeparators
		CaseSensitiveProbe = previousProbe
		ResetCaseSensitivityCache()
	})
}

func TestDestKey_K4TrailingSpaceNamesStayDistinct(t *testing.T) {
	root := t.TempDir()
	plain := filepath.Join(root, "poster.jpg")
	trailing := plain + " "
	leading := filepath.Join(root, " poster.jpg")

	for _, tc := range []struct {
		name          string
		separators    bool
		caseSensitive bool
	}{
		{"posix-sensitive", false, true},
		{"posix-insensitive", false, false},
		{"windows-sensitive", true, true},
		{"windows-insensitive", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k4SetSeams(t, tc.separators, tc.caseSensitive)

			keyPlain := DestKeyForRoot(root, plain)
			keyTrailing := DestKeyForRoot(root, trailing)
			keyLeading := DestKeyForRoot(root, leading)
			require.NotEqual(t, keyPlain, keyTrailing, "whitespace is a filename character, never folded")
			require.NotEqual(t, keyPlain, keyLeading)
			// Wave-21: the insensitive leg uppercases (per-rune simple mapping),
			// so the survival shape is case-posture-dependent; the SPACE is the
			// pinned char either way.
			want := "poster.jpg "
			if !tc.caseSensitive {
				want = strings.ToUpper(want)
			}
			require.True(t, strings.HasSuffix(keyTrailing, want),
				"the trailing-space spelling must survive canonicalization: %q", keyTrailing)

			// Lock-key derivation shares normalizeDestPath and must inherit the
			// same byte-distinctness even though it still folds case.
			require.NotEqual(t, foldKeyedLock(plain), foldKeyedLock(trailing),
				"distinct filename shapes must not share one registry mutex")
			require.NotEqual(t, foldKeyedLock(""), foldKeyedLock(" "),
				"empty and whitespace-only keys stay distinct ('' still cleans to \".\")")

			// The intentional folds are untouched: case only, plus separators
			// exclusively under the Windows seam.
			require.Equal(t, foldKeyedLock(plain), foldKeyedLock(strings.ToUpper(plain)),
				"case folding remains")
			if tc.caseSensitive {
				require.NotEqual(t, DestKeyForRoot(root, plain), DestKeyForRoot(root, strings.ToUpper(plain)),
					"sensitive roots keep case spellings distinct")
			} else {
				require.Equal(t, DestKeyForRoot(root, plain), DestKeyForRoot(root, strings.ToUpper(plain)),
					"insensitive roots keep folding case")
			}
		})
	}
}

// The probe root is part of the key trail (root -> case posture -> bucket), so
// whitespace-distinct roots must neither share a cache entry nor redirect the
// probe to a differently-named directory.
func TestIsCaseSensitiveRoot_K4WhitespaceRootsStayByteDistinct(t *testing.T) {
	previousProbe := CaseSensitiveProbe
	t.Cleanup(func() {
		CaseSensitiveProbe = previousProbe
		ResetCaseSensitivityCache()
	})

	var mu sync.Mutex
	var probed []string
	CaseSensitiveProbe = func(root string) (bool, error) {
		mu.Lock()
		probed = append(probed, root)
		mu.Unlock()
		return true, nil
	}
	ResetCaseSensitivityCache()

	plain := filepath.Join(t.TempDir(), "probe")
	spaced := plain + " "
	require.True(t, IsCaseSensitiveRoot(plain))
	require.True(t, IsCaseSensitiveRoot(spaced))
	cleanPlain := cleanProbeRoot(plain)
	cleanSpaced := cleanProbeRoot(spaced)
	if runtime.GOOS == "windows" {
		// Byte-distinct probe roots are a POSIX-only contract: Win32
		// GetFullPathName (beneath filepath.Abs/syscall.FullPath) strips a
		// trailing space at the OS normalization layer before the probe seam
		// ever sees it (Windows CI job 95712522465), and WinFS cannot form a
		// trailing-space directory at all. Both spellings therefore legitimately
		// fold onto the plain root's entry. What Windows must still prove:
		// neither spelling errors, the collapse is total, and the folded
		// sibling is served from the one cache entry without a second probe.
		require.Equal(t, cleanPlain, cleanSpaced, "Win32 full-path normalization folds the trailing-space root")
		require.Equal(t, []string{cleanPlain}, probed, "the folded spelling is not probed in its own right")
		require.True(t, IsCaseSensitiveRoot(plain))
		require.True(t, IsCaseSensitiveRoot(spaced))
		require.Len(t, probed, 1, "repeat queries stay on the single folded cache entry")
		return
	}
	require.NotEqual(t, cleanPlain, cleanSpaced, "probe cache keys keep whitespace-distinct roots distinct")
	require.True(t, strings.HasSuffix(cleanSpaced, "probe "), "the real spelling is probed, not its trimmed alias")
	require.Equal(t, []string{cleanPlain, cleanSpaced}, probed, "each distinct root is probed once in its own right")

	// A repeat query is served from the byte-distinct cache entry; it must not
	// re-probe or migrate onto the trimmed spelling's entry.
	require.True(t, IsCaseSensitiveRoot(plain))
	require.True(t, IsCaseSensitiveRoot(spaced))
	require.Len(t, probed, 2)
}

// End-to-end through the fsutil plumbing that history/database consumers key
// on: journal grouping buckets, destination locks, and .dlbusy takeover all
// keep 'poster.jpg' and 'poster.jpg ' disjoint for the full lifecycle.
func TestWhitespace_K4TakeoverSweepAndGroupingStayDisjoint(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/out/k4-whitespace"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	destA := dir + "/poster.jpg"
	destB := destA + " "

	oldProbe := replacementProbePIDAliveAware
	oldWindows := replacementIsWindows
	replacementProbePIDAliveAware = func(int) replacementPIDLiveness { return replacementPIDDead }
	replacementIsWindows = false
	t.Cleanup(func() {
		replacementProbePIDAliveAware = oldProbe
		replacementIsWindows = oldWindows
	})

	for _, tc := range []struct {
		name          string
		separators    bool
		caseSensitive bool
	}{
		{"posix-sensitive", false, true},
		{"posix-insensitive", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k4SetSeams(t, tc.separators, tc.caseSensitive)

			// Journal-chain grouping mirrors internal/history's byDest loop:
			// two recorded spellings of A collapse to one chain while B — the
			// whitespace sibling — keeps its own.
			groups := make(map[string][]string)
			for _, d := range []string{destA, destA, destB} {
				key := DestKey(d)
				groups[key] = append(groups[key], d)
			}
			require.Len(t, groups, 2, "poster.jpg and 'poster.jpg ' form disjoint journal chains")
			require.Len(t, groups[DestKey(destA)], 2)
			require.Len(t, groups[DestKey(destB)], 1)
		})
	}

	// Destination locks: byte-distinct names never share a mutex, while the
	// case fold for the same name still contends.
	t.Run("locks", func(t *testing.T) {
		registry := NewKeyedLockRegistry()
		releaseA := registry.Acquire(destA)

		acquiredB := make(chan func(), 1)
		go func() { acquiredB <- registry.Acquire(destB) }()
		select {
		case releaseB := <-acquiredB:
			releaseB()
		case <-time.After(2 * time.Second):
			t.Fatal("the whitespace sibling must not wait on the plain name's mutex")
		}

		var folded atomic.Bool
		done := make(chan struct{})
		go func() {
			registry.Acquire(strings.ToUpper(destA))()
			folded.Store(true)
			close(done)
		}()
		time.Sleep(30 * time.Millisecond)
		require.False(t, folded.Load(), "case-variant of the held name still contends (fold preserved)")
		releaseA()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("case-variant acquisition never unblocked")
		}
	})

	// Busy-marker takeover: .dlbusy paths are byte-distinct, and reclaiming
	// one stale marker must never touch the sibling's bytes.
	t.Run("takeover", func(t *testing.T) {
		require.NotEqual(t, ReplacementBusyPath(destA), ReplacementBusyPath(destB))
		staleB := []byte("pid=424242,time=1000")
		require.NoError(t, afero.WriteFile(base, ReplacementBusyPath(destB), staleB, 0o600))

		releaseA, err := AcquireReplacementBusy(base, destA)
		require.NoError(t, err)
		contentB, err := afero.ReadFile(base, ReplacementBusyPath(destB))
		require.NoError(t, err)
		require.Equal(t, staleB, contentB, "A's acquisition leaves B's marker bytes untouched")
		releaseA()

		releaseB, err := AcquireReplacementBusy(base, destB)
		require.NoError(t, err, "B's stale marker is reclaimed on its own key")
		releaseB()
		_, err = base.Stat(ReplacementBusyPath(destA))
		require.ErrorIs(t, err, os.ErrNotExist)
		_, err = base.Stat(ReplacementBusyPath(destB))
		require.ErrorIs(t, err, os.ErrNotExist)
	})
}
