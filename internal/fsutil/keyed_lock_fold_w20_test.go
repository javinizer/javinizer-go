package fsutil

// POSTER-WRITE-HARDENING codex PR#215 wave-20 (P2), refined by wave-21 —
// "fold the case-equivalent spellings of one insensitive-root file onto one
// destination key, WITHOUT aliasing filesystem-distinct names": the
// insensitive DestKey form used to run strings.ToLower, a per-rune SIMPLE
// case mapping that never unifies GREEK SMALL LETTER FINAL SIGMA (ς) with σ
// even though both uppercase to Σ and are case-equivalent on insensitive
// filesystems. Equivalent journal spellings (`…/στ.jpg` vs `…/ΣΤ.jpg`)
// produced different keys and stayed invisible to the exact matcher —
// sequence reuse, missed conflicts. Wave-20 swapped in cases.Fold (full
// Unicode folding), which over-corrected: its one-to-many table entries
// (ß→ss, ﬃ→ffi) aliased NTFS-DISTINCT filenames into one bucket. Wave-21
// (codex P2) settles on the per-rune simple uppercase mapping
// (destKeyInsensitive — strings.Map over unicode.ToUpper): final-sigma
// pairs still unify (ς→Σ alongside σ→Σ), while multi-char folds stay
// distinct exactly as the filesystem treats them. These tests pin the
// equivalence table (final-sigma pairs included), the case-SENSITIVE leg's
// byte identity, and ASCII ToUpper parity. The filesystem-DISTINCTION pins
// (Straße≢Strasse, ﬃ≢FFI) live in keyed_lock_fold_w21_test.go.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// w20FoldTable lists spelling groups that MUST collapse to ONE destination
// key on an insensitive root. Every group member uppercases rune-for-rune to
// the same string while simple LOWERING sees some of them differently — the
// exact hole the wave-20 finding reported. Only per-rune equivalences belong
// here: one-to-many expansions (ß→ss, ligatures) are pinned DISTINCT in the
// wave-21 file.
var w20FoldTable = [][]string{
	// The reported case: lowercase sigma, uppercase, and mixed forms —
	// plus the final sigma (ς) variant that ToLower kept distinct.
	{"στ.jpg", "ΣΤ.jpg", "Στ.jpg", "ςτ.jpg", "στ.jpg"},
	// Plain ASCII keeps folding (ToUpper parity with the wave-8 shapes).
	{"POSTER.JPG", "poster.jpg", "PoStEr.JpG"},
	// Cased non-ASCII accented letters (wave-16 fallback companions).
	{"ÄÖ.jpg", "äö.jpg", "äÖ.jpg", "Äö.jpg"},
}

// Under an insensitive root every spelling inside one group shares ONE key,
// and different groups stay distinct.
func TestDestKeyW20_FullUnicodeCaseFoldingEquivalence(t *testing.T) {
	k4SetSeams(t, false, false) // POSIX separators, forced-insensitive probe
	root := t.TempDir()

	seenKeys := make(map[string]string) // key → first spelling that produced it
	for gi, group := range w20FoldTable {
		var groupKey string
		for si, spelling := range group {
			key := DestKeyForRoot(root, filepath.Join(root, spelling))
			if si == 0 {
				groupKey = key
				continue
			}
			require.Equal(t, groupKey, key,
				"group %d: %q must fold onto %q's key", gi, spelling, group[0])
		}
		if prior, ok := seenKeys[groupKey]; ok {
			t.Fatalf("groups %d (%q) and %q collided — distinct filenames must not share a journal key", gi, prior, group[0])
		}
		seenKeys[groupKey] = group[0]
	}
	require.Len(t, seenKeys, len(w20FoldTable), "every group keeps its own bucket")

	// DestKey (the probing form) agrees with DestKeyForRoot for the reported
	// final-sigma pair.
	require.Equal(t, DestKey(filepath.Join(root, "στ.jpg")), DestKey(filepath.Join(root, "ΣΤ.jpg")),
		"journal grouping resolves the reported spellings to one chain")
}

// The case-SENSITIVE leg stays byte-identical to the platform-aware
// normalization — no folding whatsoever touches it, so byte-distinct
// spellings (case variants AND final-sigma variants) keep distinct keys.
func TestDestKeyW20_SensitiveLegByteIdentity(t *testing.T) {
	k4SetSeams(t, false, true) // forced case-sensitive probe
	root := t.TempDir()

	for _, spelling := range []string{"στ.jpg", "ςτ.jpg", "ΣΤ.jpg", "poster.jpg", "POSTER.JPG", "中/foo.jpg"} {
		p := filepath.Join(root, spelling)
		require.Equal(t, normalizeDestPath(p), DestKeyForRoot(root, p),
			"sensitive leg is normalizeDestPath byte-for-byte (no folding)")
	}
	keys := map[string]string{}
	for _, spelling := range []string{"στ.jpg", "ΣΤ.jpg", "Στ.jpg", "ςτ.jpg"} {
		key := DestKeyForRoot(root, filepath.Join(root, spelling))
		if prior, ok := keys[key]; ok {
			t.Fatalf("sensitive root folded %q onto %q — byte-distinct spellings must stay distinct", spelling, prior)
		}
		keys[key] = spelling
	}
}

// Insensitive ASCII folds are byte-identical to strings.ToUpper — the
// wave-21 per-rune mapping changes nothing for the wave-8 pattern-family
// destinations (the SQL LIKE prefilters consume raw spellings and fold
// ASCII in the database itself, so only this in-process exact-matcher shape
// matters).
func TestDestKeyW20_ASCIIInsensitiveParityWithToUpper(t *testing.T) {
	k4SetSeams(t, false, false)
	root := t.TempDir()

	for _, spelling := range []string{"Poster.jpg", "MOVIE-001.MKV", "a/B/c.PNG"} {
		p := filepath.Join(root, spelling)
		require.Equal(t, filepath.ToSlash(strings.ToUpper(normalizeDestPath(p))),
			filepath.ToSlash(DestKeyForRoot(root, p)),
			"ASCII insensitive fold is ToUpper-identical")
	}
}

// The keyed-lock acquisition fold must at least match the journal key's
// final-sigma equivalence — case variants contending on one mutex relied on
// ToUpper folding, which already maps both sigma forms to Σ; this pins the
// parity so a future fold change cannot split journal bucket and lock.
func TestDestKeyW20_KeyedLockFinalSigmaContention(t *testing.T) {
	k4SetSeams(t, false, false)
	root := t.TempDir()
	a := filepath.Join(root, "ΣΤ.jpg")
	b := filepath.Join(root, "ςτ.jpg")
	require.Equal(t, foldKeyedLock(a), foldKeyedLock(b),
		"final-sigma case variants contend on the same mutex")
	require.Equal(t, DestKeyForRoot(root, a), DestKeyForRoot(root, b),
		"the journal bucket agrees")
}
