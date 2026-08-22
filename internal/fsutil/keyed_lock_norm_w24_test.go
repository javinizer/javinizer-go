package fsutil

// POSTER-WRITE-HARDENING codex PR#215 wave-24 (P2) — destination keys must
// fold Unicode NORMALIZATION on normalization-insensitive roots: APFS/HFS+
// address one file by its NFC and NFD spellings alike, so a journaled NFD
// spelling and a queried NFC spelling must derive ONE key — two buckets for
// one physical file corrupts sequence reuse and destination-conflict checks
// (the same class wave-20's final-sigma collapse fixed for CASE). The probe
// mirrors the case probe: write an NFD-form temp name, stat its NFC spelling
// (success = insensitive), cache per root, fail closed on error. Folding is
// NFC canonicalization IN ADDITION to the wave-21 per-rune ToUpper, applied
// ToUpper-first-then-NFC so the finished key is always canonical.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode"

	"github.com/stretchr/testify/require"
	"golang.org/x/text/unicode/norm"
)

// w24SetNormProbe forces the normalization-probe posture and resets the
// per-root cache around it (mirrors w13SetProbe for the case probe).
func w24SetNormProbe(t *testing.T, probe caseSensitivityProbe) {
	t.Helper()
	previous := NormalizationProbe
	NormalizationProbe = probe
	ResetNormalizationCache()
	t.Cleanup(func() {
		NormalizationProbe = previous
		ResetNormalizationCache()
	})
}

func w24ForceInsensitiveCase(t *testing.T) {
	t.Helper()
	w13SetProbe(t, func(string) (bool, error) { return false, nil })
}

// The finding itself: under a positive insensitivity determination the NFC
// and NFD spellings of one name collapse onto ONE key; case folding keeps
// composing with it (mixed-case NFD ≡ precomposed-uppercase NFC).
func TestDestKeyW24_NFCAndNFDFoldTogetherOnlyUnderInsensitiveNorm(t *testing.T) {
	w24ForceInsensitiveCase(t)
	w24SetNormProbe(t, func(string) (bool, error) { return true, nil })
	root := t.TempDir()

	nfc := filepath.Join(root, "caf\u00e9.jpg")  // U+00E9 precomposed
	nfd := filepath.Join(root, "cafe\u0301.jpg") // e + U+0301 combining acute
	require.NotEqual(t, nfc, nfd, "fixture spellings must differ byte-wise")
	require.Equal(t, DestKeyForRoot(root, nfc), DestKeyForRoot(root, nfd),
		"the two spellings of one file share ONE key on a normalization-insensitive root")

	// Case + normalization compose: mixed-case NFD equals precomposed-upper NFC.
	mixedNFD := filepath.Join(root, "Cafe\u0301.jpg")
	upperNFC := filepath.Join(root, "CAF\u00c9.JPG")
	require.Equal(t, DestKeyForRoot(root, upperNFC), DestKeyForRoot(root, mixedNFD),
		"ToUpper + NFC compose — case and normalization folds stack")

	// DestKey (root derived from the path) resolves the same probe root.
	require.Equal(t, DestKey(nfd), DestKeyForRoot(root, nfc),
		"the implicit-root form lands in the same probe-root bucket")
}

// Conservative postures: a sensitive determination OR a probe error keeps the
// byte-distinct spellings on DISTINCT keys — folding on a guess would alias
// files that coexist on the volume.
func TestDestKeyW24_NFCAndNFDStayDistinctUnderSensitiveNormAndProbeError(t *testing.T) {
	w24ForceInsensitiveCase(t)
	root := t.TempDir()
	nfc := filepath.Join(root, "caf\u00e9.jpg")
	nfd := filepath.Join(root, "cafe\u0301.jpg")

	w24SetNormProbe(t, func(string) (bool, error) { return false, nil })
	require.False(t, IsNormalizationInsensitiveRoot(root))
	require.NotEqual(t, DestKeyForRoot(root, nfc), DestKeyForRoot(root, nfd),
		"normalization-sensitive root: spellings stay byte-distinct")

	probeErr := errors.New("w24 normalization probe undecidable")
	var errCalls atomic.Int32
	w24SetNormProbe(t, func(string) (bool, error) { errCalls.Add(1); return true, probeErr })
	require.False(t, IsNormalizationInsensitiveRoot(root),
		"a probe error is undecidable — conservative no-fold posture")
	require.NotEqual(t, DestKeyForRoot(root, nfc), DestKeyForRoot(root, nfd),
		"probe failure never folds normalization on a guess")
	require.False(t, IsNormalizationInsensitiveRoot(root),
		"wave-25: the error outcome is NOT cached — the undecidable root re-probes")
	require.Equal(t, int32(4), errCalls.Load(),
		"every derivation re-probes while the root stays undecidable (two direct + one per DestKeyForRoot)")

	// After recovery the retried probe folds correctly — the finding's
	// first-error → re-probe → fold contract for the normalization probe.
	var okCalls atomic.Int32
	NormalizationProbe = func(string) (bool, error) { okCalls.Add(1); return true, nil }
	ResetNormalizationCache()
	require.True(t, IsNormalizationInsensitiveRoot(root), "the retried probe folds after the transient failure clears")
	require.Equal(t, DestKeyForRoot(root, nfc), DestKeyForRoot(root, nfd),
		"first-probe-error → second derivation re-probes and folds correctly afterward")
	require.Equal(t, int32(1), okCalls.Load(), "the recovered definitive outcome is served from cache afterwards")
}

// Case-sensitive roots still fold normalization ALONE: the fold is gated on
// the normalization probe, independent of the case posture.
func TestDestKeyW24_NormalizationFoldsIndependentlyOfCasePosture(t *testing.T) {
	w13SetProbe(t, func(string) (bool, error) { return true, nil }) // case-SENSITIVE
	w24SetNormProbe(t, func(string) (bool, error) { return true, nil })
	root := t.TempDir()

	require.Equal(t,
		DestKeyForRoot(root, filepath.Join(root, "caf\u00e9.jpg")),
		DestKeyForRoot(root, filepath.Join(root, "cafe\u0301.jpg")),
		"case-sensitive but normalization-insensitive: spellings fold without case folding")
	require.NotEqual(t,
		DestKeyForRoot(root, filepath.Join(root, "Caf\u00e9.jpg")),
		DestKeyForRoot(root, filepath.Join(root, "caf\u00e9.jpg")),
		"case distinctions survive on the case-sensitive leg")

	// Both sensitive: nothing folds at all.
	w24SetNormProbe(t, func(string) (bool, error) { return false, nil })
	require.NotEqual(t,
		DestKeyForRoot(root, filepath.Join(root, "caf\u00e9.jpg")),
		DestKeyForRoot(root, filepath.Join(root, "cafe\u0301.jpg")),
		"normalization-sensitive roots never fold normalization")
}

// Ordering pin: the fold runs per-rune ToUpper FIRST and NFC canonical
// composition LAST. A combining sequence that becomes composable only AFTER
// the uppercase mapping (LATIN SMALL LETTER LONG S + COMBINING DOT ABOVE →
// S + dot → Ṡ) must land on the precomposed spelling's key — an
// NFC-first-then-ToUpper ordering would leave the key permanently
// DECOMPOSED and split the bucket.
func TestDestKeyW24_ToUpperRunsBeforeNFCCanonicalization(t *testing.T) {
	w24ForceInsensitiveCase(t)
	w24SetNormProbe(t, func(string) (bool, error) { return true, nil })
	root := t.TempDir()

	longS := filepath.Join(root, "\u017f\u0307.jpg") // U+017F + U+0307
	precomposed := filepath.Join(root, "\u1e60.jpg") // U+1E60, S WITH DOT ABOVE

	k1, k2 := DestKeyForRoot(root, longS), DestKeyForRoot(root, precomposed)
	require.Equal(t, k1, k2,
		"ToUpper(\u017f+dot) → S+dot → NFC composes it onto the precomposed Ṡ key")

	// Shape pin against the reference pipeline: per-rune ToUpper, then NFC.
	expected := norm.NFC.String(strings.Map(unicode.ToUpper, normalizeDestPath(longS)))
	require.Equal(t, filepath.ToSlash(expected), filepath.ToSlash(k1),
		"the fold is exactly strings.Map(unicode.ToUpper, …) followed by norm.NFC")
	require.Equal(t, filepath.ToSlash(expected), filepath.ToSlash(k2))
}

// The nil-probe seam is the forced-insensitive test posture (mirrors
// CaseSensitiveProbe): result is folded without any filesystem access.
func TestIsNormalizationInsensitiveRootW24_NilProbeForcesInsensitive(t *testing.T) {
	w24SetNormProbe(t, nil)
	require.True(t, IsNormalizationInsensitiveRoot(t.TempDir()),
		"nil probe seam = deliberate forced-insensitive posture")
}

// Probe results publish once per root: a cached posture is not re-probed.
func TestIsNormalizationInsensitiveRootW24_ResultCachedPerRoot(t *testing.T) {
	root := t.TempDir()
	var calls atomic.Int32
	w24SetNormProbe(t, func(string) (bool, error) { calls.Add(1); return true, nil })

	require.True(t, IsNormalizationInsensitiveRoot(root))
	require.True(t, IsNormalizationInsensitiveRoot(root))
	require.Equal(t, int32(1), calls.Load(), "one probe per root per process")
}

// Racing first probes on one root converge on a single published posture
// (wave-25 single-flight): exactly ONE probe runs; the racer JOINS the
// in-flight probe and adopts its outcome; the definitive publish is then
// served from cache.
func TestIsNormalizationInsensitiveRootW24_ConcurrentFirstProbesPublishOnePosture(t *testing.T) {
	root := t.TempDir()
	var entered atomic.Int32
	probeInFlight := make(chan struct{})
	releaseProbe := make(chan struct{})
	w24SetNormProbe(t, func(string) (bool, error) {
		if entered.Add(1) == 1 {
			close(probeInFlight)
		}
		<-releaseProbe
		return true, nil
	})

	var wg sync.WaitGroup
	results := make([]bool, 2)
	wg.Add(1)
	go func() {
		defer wg.Done()
		results[0] = IsNormalizationInsensitiveRoot(root)
	}()
	require.Eventually(t, func() bool { return entered.Load() == 1 },
		2*time.Second, time.Millisecond, "the flight leader must be probing")
	wg.Add(1)
	go func() {
		defer wg.Done()
		results[1] = IsNormalizationInsensitiveRoot(root)
	}()
	require.Never(t, func() bool { return entered.Load() != 1 },
		200*time.Millisecond, time.Millisecond, "no second probe while the first is in flight")
	close(releaseProbe)
	wg.Wait()
	require.Equal(t, int32(1), entered.Load(),
		"racing first-time callers share ONE probe (single-flight)")
	require.True(t, results[0])
	require.True(t, results[1], "the flight sharer converges on the leader's posture")
	require.True(t, IsNormalizationInsensitiveRoot(root),
		"the definitive outcome is adopted from cache afterwards")
	require.Equal(t, int32(1), entered.Load(), "a cached root is not re-probed after publication")
}

// Probe leg suite, injected ops (mirrors the wave-21 case-probe suite): the
// create/stat/remove choreography is host-independent and deterministic.
func TestProbeNormalizationInsensitiveW24_StatSuccessMeansInsensitive(t *testing.T) {
	root := t.TempDir()
	var opened, removed []string
	// Wave-38 (finding F5): the created identity rides the OPEN handle —
	// model handle stat and alternate lookup answering THE SAME fake object.
	sentinel := &w38ProbeInfo{}
	ops := caseProbeOps{
		openFile: func(name string, flag int, perm os.FileMode) (caseProbeFile, error) {
			if flag&os.O_CREATE == 0 {
				// Wave-34 bind leg: the verified scratch re-opens O_RDONLY and
				// re-proves THE created identity by descriptor — answer with the
				// same scripted identity without counting as a create attempt.
				return w38StatProbeFile{info: sentinel}, nil
			}
			opened = append(opened, name)
			real, err := os.OpenFile(name, flag, perm)
			if err != nil {
				return nil, err
			}
			// The real create leaves something name-honest for cleanup to act
			// on; its handle must CLOSE before the scripted fake replaces it —
			// a leaked handle keeps Windows from unlinking the probe at all
			// ("file being used by another process", CI test-vs-platform).
			if err := real.Close(); err != nil {
				return nil, err
			}
			return w38StatProbeFile{info: sentinel}, nil
		},
		stat:   func(string) (os.FileInfo, error) { return sentinel, nil },
		rename: probeRenameNoReplace,
		remove: func(name string) error {
			removed = append(removed, name)
			return os.Remove(name)
		},
	}

	// w31 binding: model the "same object" proof through the seam.
	prev := probeSameFile
	probeSameFile = func(a, b os.FileInfo) bool { return a == b }
	t.Cleanup(func() { probeSameFile = prev })

	got, err := probeNormalizationInsensitive(ops, root)
	require.NoError(t, err)
	require.True(t, got, "NFC visible for the NFD-created probe = insensitive")
	require.Len(t, opened, 1)
	require.Equal(t, []string{opened[0] + probeCleanupScratchSuffix}, removed,
		"the bound cleanup unlinks only the verified probe's scratch name (wave-39)")
	require.Contains(t, opened[0], "_a\u0308", "the probe name is created in NFD form")
}

func TestProbeNormalizationInsensitiveW24_ENOENTMeansSensitive(t *testing.T) {
	root := t.TempDir()
	var opened []string
	ops := caseProbeOps{
		openFile: func(name string, flag int, perm os.FileMode) (caseProbeFile, error) {
			opened = append(opened, name)
			return os.OpenFile(name, flag, perm)
		},
		// The scripted ENOENT answers ONLY the alternate (NFC) lookup; the
		// wave-39 bound cleanup's own stat/re-stat of the created probe and
		// its scratch sibling must see the real object (host-independent on
		// case- or normalization-insensitive volumes).
		stat: func(name string) (os.FileInfo, error) {
			if name == opened[len(opened)-1] || name == opened[len(opened)-1]+probeCleanupScratchSuffix {
				return os.Stat(name)
			}
			return nil, os.ErrNotExist
		},
		rename: probeRenameNoReplace,
		remove: os.Remove,
	}
	got, err := probeNormalizationInsensitive(ops, root)
	require.NoError(t, err)
	require.False(t, got, "byte-distinct normalization forms = sensitive, no error")
	entries, rdErr := os.ReadDir(root)
	require.NoError(t, rdErr)
	require.Empty(t, entries, "the probe cleans up after itself on every verdict")
}

func TestProbeNormalizationInsensitiveW24_IndeterminateStatFailsClosed(t *testing.T) {
	statErr := errors.New("w24 indeterminate stat")
	removed := 0
	ops := caseProbeOps{
		openFile: func(name string, flag int, perm os.FileMode) (caseProbeFile, error) {
			return os.OpenFile(name, flag, perm)
		},
		stat: func(string) (os.FileInfo, error) { return nil, statErr },
		remove: func(name string) error {
			removed++
			return os.Remove(name)
		},
	}
	got, err := probeNormalizationInsensitive(ops, t.TempDir())
	require.ErrorIs(t, err, statErr)
	require.False(t, got)
	require.Zero(t, removed, "wave-39: with the lookup indeterminate the bound cleanup has no identity proof to unlink against — the probe is left for the next retry rather than unlinked by a bare pathname")
}

func TestProbeNormalizationInsensitiveW24_OpenErrorPropagates(t *testing.T) {
	openErr := errors.New("w24 open failure")
	ops := caseProbeOps{
		openFile: func(string, int, os.FileMode) (caseProbeFile, error) { return nil, openErr },
		stat:     func(string) (os.FileInfo, error) { t.Fatal("unused"); return nil, nil },
		remove:   func(string) error { t.Fatal("nothing created"); return nil },
	}
	got, err := probeNormalizationInsensitive(ops, t.TempDir())
	require.ErrorIs(t, err, openErr)
	require.False(t, got)
}

func TestProbeNormalizationInsensitiveW24_CollisionRetriesWithFreshName(t *testing.T) {
	root := t.TempDir()
	var opened []string
	ops := caseProbeOps{
		openFile: func(name string, flag int, perm os.FileMode) (caseProbeFile, error) {
			if flag&os.O_CREATE == 0 {
				// Wave-34 bind leg: O_RDONLY re-open of the verified scratch — the
				// real renamed object answers; it is not a create attempt.
				return os.OpenFile(name, flag, perm)
			}
			opened = append(opened, name)
			if len(opened) == 1 {
				return nil, os.ErrExist
			}
			return os.OpenFile(name, flag, perm)
		},
		// Only the alternate lookup belongs to the probe's case evidence;
		// the bound cleanup's stat legs see the real created probe.
		stat: func(name string) (os.FileInfo, error) {
			if name == opened[len(opened)-1] || name == opened[len(opened)-1]+probeCleanupScratchSuffix {
				return os.Stat(name)
			}
			return nil, os.ErrNotExist
		},
		rename: probeRenameNoReplace,
		remove: os.Remove,
	}
	got, err := probeNormalizationInsensitive(ops, root)
	require.NoError(t, err)
	require.False(t, got)
	require.Len(t, opened, 2)
	require.NotEqual(t, opened[0], opened[1], "EEXIST retry draws a fresh process-unique name")
}

type w24CloseErrProbeFile struct{ err error }

func (f w24CloseErrProbeFile) Close() error { return f.err }

// Stat satisfies the wave-38 probe-handle identity channel; a failed close
// is asserted before any verdict comparison, so a nil identity suffices.
func (w24CloseErrProbeFile) Stat() (os.FileInfo, error) { return nil, nil }

func TestProbeNormalizationInsensitiveW24_CloseErrorPropagates(t *testing.T) {
	closeErr := errors.New("w24 close failure")
	removed := 0
	ops := caseProbeOps{
		openFile: func(string, int, os.FileMode) (caseProbeFile, error) { return w24CloseErrProbeFile{closeErr}, nil },
		stat:     func(string) (os.FileInfo, error) { t.Fatal("unused on the close-failure leg"); return nil, nil },
		remove:   func(string) error { removed++; return nil },
	}
	got, err := probeNormalizationInsensitive(ops, t.TempDir())
	require.ErrorIs(t, err, closeErr)
	require.False(t, got)
	// Wave-58: no cleanup ever runs for a claim whose identity was never
	// captured (Close-after-failed-create drops it, Unlink always proves
	// the inode first) — the nil-identity retain leg keeps it in place.
	require.Zero(t, removed, "unproven identity never pathname-unlinks")
}

func TestProbeNormalizationInsensitiveW24_CleanupErrorPropagates(t *testing.T) {
	removeErr := errors.New("w24 remove failure")
	var opened []string
	ops := caseProbeOps{
		openFile: func(name string, flag int, perm os.FileMode) (caseProbeFile, error) {
			opened = append(opened, name)
			return os.OpenFile(name, flag, perm)
		},
		// The scripted ENOENT must NOT cover the bound cleanup's own probes:
		// the take-aside rename and verified unlink need the real object.
		stat: func(name string) (os.FileInfo, error) {
			if name == opened[0] || name == opened[0]+probeCleanupScratchSuffix {
				return os.Stat(name)
			}
			return nil, os.ErrNotExist
		},
		rename: probeRenameNoReplace,
		remove: func(string) error { return removeErr },
	}
	got, err := probeNormalizationInsensitive(ops, t.TempDir())
	require.ErrorIs(t, err, removeErr)
	require.False(t, got)
}

// Host observation contract: wherever the suite runs, the REAL probe agrees
// with a manual NFD-create/NFC-stat experiment against the same root.
func TestIsNormalizationInsensitiveRootW24_RealProbeMatchesObservation(t *testing.T) {
	ResetNormalizationCache()
	t.Cleanup(ResetNormalizationCache)
	root := t.TempDir()

	nfd := filepath.Join(root, "w24observe_a\u0308.jpg")
	require.NoError(t, os.WriteFile(nfd, []byte("x"), 0o600))
	_, statErr := os.Lstat(filepath.Join(root, "w24observe_\u00e4.jpg"))
	expected := statErr == nil

	require.Equal(t, expected, IsNormalizationInsensitiveRoot(root),
		"the probe verdict tracks the filesystem's observable alias behavior")
}
