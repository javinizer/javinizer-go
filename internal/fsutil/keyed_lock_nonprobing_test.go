package fsutil

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingProbeSeams installs definitive probe stubs that count every probe,
// resets both process caches, and restores everything afterwards. Landing the
// counters at zero proves a derivation never touched the filesystem.
func countingProbeSeams(t *testing.T, caseSensitive, normInsensitive bool) (caseCalls, normCalls *int) {
	t.Helper()
	ResetCaseSensitivityCache()
	ResetNormalizationCache()
	cc, nc := 0, 0
	prevCase, prevNorm := CaseSensitiveProbe, NormalizationProbe
	CaseSensitiveProbe = func(string) (bool, error) { cc++; return caseSensitive, nil }
	NormalizationProbe = func(string) (bool, error) { nc++; return normInsensitive, nil }
	t.Cleanup(func() {
		CaseSensitiveProbe = prevCase
		NormalizationProbe = prevNorm
		ResetCaseSensitivityCache()
		ResetNormalizationCache()
	})
	return &cc, &nc
}

func TestDestKeyResolver_KeyNonProbing(t *testing.T) {
	t.Run("uncached roots fall back to the conservative posture without probing", func(t *testing.T) {
		// Stubs would fold EVERYTHING if wrongly probed — distinct keys prove
		// the fallback stayed distinction-preserving.
		caseCalls, normCalls := countingProbeSeams(t, false, true)
		dir := t.TempDir()
		r := NewDestKeyResolver()
		upper := r.KeyNonProbing(filepath.Join(dir, "Movie.mkv"))
		lower := r.KeyNonProbing(filepath.Join(dir, "movie.mkv"))
		assert.NotEqual(t, upper, lower, "unknown case posture preserves case distinctions")
		nfc := r.KeyNonProbing(filepath.Join(dir, "caf\u00e9.mkv"))
		nfd := r.KeyNonProbing(filepath.Join(dir, "cafe\u0301.mkv"))
		assert.NotEqual(t, nfc, nfd, "unknown normalization posture preserves normalization distinctions")
		assert.Equal(t, 0, *caseCalls, "no case probe")
		assert.Equal(t, 0, *normCalls, "no normalization probe")
	})

	t.Run("fallback postures are never frozen into the resolver", func(t *testing.T) {
		caseCalls, normCalls := countingProbeSeams(t, false, true)
		dir := t.TempDir()
		r := NewDestKeyResolver()
		before := r.KeyNonProbing(filepath.Join(dir, "Movie.mkv"))
		require.Equal(t, 0, *caseCalls)

		// Both legs acquire definitive cached postures (as an earlier live
		// probe would have published); the same resolver must re-derive from
		// the cache instead of clinging to the fallback.
		IsCaseSensitiveRoot(dir)
		IsNormalizationInsensitiveRoot(dir)
		require.Equal(t, 1, *caseCalls)
		require.Equal(t, 1, *normCalls)

		foldedUpper := r.Key(filepath.Join(dir, "Movie.mkv"))
		foldedLower := r.Key(filepath.Join(dir, "movie.mkv"))
		assert.Equal(t, 1, *caseCalls, "Key reuses definitive caches without re-probing")
		assert.Equal(t, 1, *normCalls)
		assert.Equal(t, foldedUpper, foldedLower, "post-fill derivation folds case variants again")
		assert.NotEqual(t, before, foldedUpper, "the fallback posture was not frozen")
	})

	t.Run("fully cached roots derive the definitive posture with zero probes and freeze it", func(t *testing.T) {
		caseCalls, normCalls := countingProbeSeams(t, false, true)
		dir := t.TempDir()
		IsCaseSensitiveRoot(dir)
		IsNormalizationInsensitiveRoot(dir)
		require.Equal(t, 1, *caseCalls)
		require.Equal(t, 1, *normCalls)

		r := NewDestKeyResolver()
		key := r.KeyNonProbing(filepath.Join(dir, "Movie.mkv"))
		assert.Equal(t, 1, *caseCalls, "no new case probe")
		assert.Equal(t, 1, *normCalls, "no new normalization probe")
		assert.Equal(t, r.KeyNonProbing(filepath.Join(dir, "movie.mkv")), key,
			"cached-insensitive postures fold case variants")
		assert.Equal(t, r.KeyNonProbing(filepath.Join(dir, "caf\u00e9.mkv")), r.KeyNonProbing(filepath.Join(dir, "cafe\u0301.mkv")),
			"cached normalization-insensitivity folds NFC/NFD variants")
		// Postures frozen from definitive caches: the frozen hit-leg serves
		// every later key of the root without any cache dependence.
		assert.Equal(t, r.Key(filepath.Join(dir, "MOVIE.mkv")), key, "frozen posture matches Key derivation")
		assert.Equal(t, 1, *caseCalls)
		assert.Equal(t, 1, *normCalls)
	})

	t.Run("partially cached roots overlay the known leg on the conservative fallback", func(t *testing.T) {
		caseCalls, normCalls := countingProbeSeams(t, false, true)
		dir := t.TempDir()
		IsCaseSensitiveRoot(dir) // case leg definitive-insensitive; norm leg stays unknown
		require.Equal(t, 1, *caseCalls)
		require.Equal(t, 0, *normCalls)

		r := NewDestKeyResolver()
		assert.Equal(t,
			r.KeyNonProbing(filepath.Join(dir, "Movie.mkv")),
			r.KeyNonProbing(filepath.Join(dir, "movie.mkv")),
			"the cached case-insensitive leg folds case variants")
		assert.NotEqual(t,
			r.KeyNonProbing(filepath.Join(dir, "caf\u00e9.mkv")),
			r.KeyNonProbing(filepath.Join(dir, "cafe\u0301.mkv")),
			"the unknown normalization leg conservatively keeps NFD/NFC distinct")
		assert.Equal(t, 0, *normCalls, "the unknown leg is never probed")

		// Partially-known derivations freeze nothing: once the missing leg is
		// cached definitively, Key re-derives the true full posture.
		IsNormalizationInsensitiveRoot(dir)
		require.Equal(t, 1, *normCalls)
		assert.Equal(t,
			r.Key(filepath.Join(dir, "caf\u00e9.mkv")),
			r.Key(filepath.Join(dir, "cafe\u0301.mkv")),
			"post-fill derivation folds normalization variants — no frozen partial posture")
	})
}
