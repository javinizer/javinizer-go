package fsutil

// POSTER-WRITE-HARDENING codex PR#215 wave-21 (P2) — "preserve
// filesystem-distinct names in destination keys": wave-20's full Unicode
// case fold (cases.Fold) aliased NTFS-DISTINCT names — its one-to-many
// expansions (ß→ss under the full fold table, so Straße ≡ Strasse; ﬃ→ffi)
// merged names that coexist as SEPARATE files on NTFS/APFS, whose upcase
// tables map per code unit and never expand. Two physical files then shared
// one replacement chain under one journal key, and
// restoreReplacementJournal would restore every entry to the first-recorded
// spelling — overwriting one file with another's backups. The insensitive
// form now runs the per-rune simple uppercase mapping (strings.Map over
// unicode.ToUpper in destKeyInsensitive), which satisfies BOTH findings:
// wave-20's final-sigma rescue (ς→Σ, σ→Σ, Σ→Σ — identical images) survives,
// while ß and ligatures keep their byte identities (no expansion, so no
// aliasing). These tests pin the distinctions and the unified sigma table —
// and parity with the keyed-lock acquisition fold, which took the ToUpper
// shape from the start.

import (
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/require"
)

// w21DistinctPairs lists cross-GROUP pairs that MUST NOT collapse onto one
// destination key under an insensitive root even though full Unicode case
// folding merges them: every pair names files that can coexist in one
// directory on NTFS/APFS (their upcase tables map per code unit and never
// expand ß→ss or ligatures to letter sequences).
var w21DistinctPairs = [][2]string{
	// The reported case: ß never expands to ss under a per-rune mapping, so
	// Straße and Strasse are distinct files on-disk and stay distinct in
	// the journal.
	{"Straße.jpg", "Strasse.jpg"},
	{"Straße.jpg", "STRASSE.jpg"},
	// Ligature expansions are not filename equivalences either.
	{"ﬃ.jpg", "FFI.jpg"},
	{"ﬃ.jpg", "ffi.jpg"},
}

// True per-rune case variants of one name still unify — only the one-to-many
// expansions were removed. (ß has no single-rune uppercase: unicode.ToUpper
// maps it to itself, exactly like the filesystem upcase tables.)
var w21UnifiedGroups = [][]string{
	{"Straße.jpg", "straße.jpg", "STRAßE.JPG"},
	{"Strasse.jpg", "STRASSE.JPG"},
}

// Every distinction pair keeps separate replacement chains, and the true
// case variants keep sharing one — under the insensitive posture.
func TestDestKeyW21_FullFoldExpansionsStayDistinct(t *testing.T) {
	k4SetSeams(t, false, false) // POSIX separators, forced-insensitive probe
	root := t.TempDir()

	for _, group := range w21UnifiedGroups {
		var groupKey string
		for i, spelling := range group {
			key := DestKeyForRoot(root, filepath.Join(root, spelling))
			if i == 0 {
				groupKey = key
				continue
			}
			require.Equal(t, groupKey, key,
				"true case variants %q and %q still share one chain", group[0], spelling)
		}
	}
	for _, pair := range w21DistinctPairs {
		k1 := DestKeyForRoot(root, filepath.Join(root, pair[0]))
		k2 := DestKeyForRoot(root, filepath.Join(root, pair[1]))
		require.NotEqual(t, k1, k2,
			"%q and %q are filesystem-distinct names — full folding must not merge their chains", pair[0], pair[1])
	}

	// The keyed-lock acquisition fold must split exactly where the journal
	// bucket splits — a lock shared by filesystem-distinct names is safe,
	// but a journal bucket merging them is the corruption vector; pin both.
	a := filepath.Join(root, "Straße.jpg")
	b := filepath.Join(root, "Strasse.jpg")
	require.NotEqual(t, DestKeyForRoot(root, a), DestKeyForRoot(root, b),
		"Straße and Strasse keep separate replacement chains")
	require.NotEqual(t, foldKeyedLock(a), foldKeyedLock(b),
		"the per-destination lock split matches the journal bucket split")
}

// The wave-20 rescue survives the mapping swap: final sigma ς, plain σ, and
// capital Σ still unify — through unicode.ToUpper's identical images — and
// the insensitive fold IS the per-rune ToUpper mapping, rune for rune.
func TestDestKeyW21_SigmaUnifiedViaPerRuneToUpper(t *testing.T) {
	k4SetSeams(t, false, false)
	root := t.TempDir()

	group := []string{"στ.jpg", "ΣΤ.jpg", "Στ.jpg", "ςτ.jpg"}
	var groupKey string
	for i, spelling := range group {
		p := filepath.Join(root, spelling)
		key := DestKeyForRoot(root, p)
		require.Equal(t, filepath.ToSlash(strings.Map(unicode.ToUpper, normalizeDestPath(p))),
			filepath.ToSlash(key), "the insensitive fold is exactly per-rune ToUpper")
		if i == 0 {
			groupKey = key
			continue
		}
		require.Equal(t, groupKey, key, "final-sigma variants stay unified after the wave-21 swap")
	}
}

// The insensitive fold IS the per-rune reference mapping — pin the exact
// shape against strings.Map(unicode.ToUpper, …) so no full-fold table can
// silently sneak back in.
func TestDestKeyW21_InsensitiveFormIsExactlyPerRuneToUpper(t *testing.T) {
	k4SetSeams(t, false, false)
	root := t.TempDir()

	for _, spelling := range []string{"Straße.jpg", "ﬃ.jpg", "στ.jpg", "poster.jpg", "ÄÖ.jpg"} {
		p := filepath.Join(root, spelling)
		require.Equal(t, filepath.ToSlash(strings.Map(unicode.ToUpper, normalizeDestPath(p))),
			filepath.ToSlash(DestKeyForRoot(root, p)),
			"the insensitive key of %q is exactly per-rune ToUpper", spelling)
	}
}
