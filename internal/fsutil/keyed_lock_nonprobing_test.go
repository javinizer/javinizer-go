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

	t.Run("fallback postures freeze per resolver — mid-pass definitive caches never flip a pass", func(t *testing.T) {
		// codex P2 (PR #241 finding F2) pin: the first key on a fresh
		// resolver resolves root X by FALLBACK; definitive caches injected
		// externally mid-pass (as another live operation's probes would) must
		// NOT flip the second key of the same root — while a NEW resolver
		// (the next pass) sees the definitive fold.
		caseCalls, normCalls := countingProbeSeams(t, false, true)
		dir := t.TempDir()
		r := NewDestKeyResolver()
		upper := r.KeyNonProbing(filepath.Join(dir, "Movie.mkv"))
		lower := r.KeyNonProbing(filepath.Join(dir, "movie.mkv"))
		assert.NotEqual(t, upper, lower, "an uncached root falls back to the conservative posture")
		require.Equal(t, 0, *caseCalls, "fallback derivation probes nothing")
		require.Equal(t, 0, *normCalls)

		// Mid-pass: both legs become definitive in the process caches (the
		// stubs would fold EVERYTHING). The fallback is frozen per resolver…
		IsCaseSensitiveRoot(dir)
		IsNormalizationInsensitiveRoot(dir)
		require.Equal(t, 1, *caseCalls)
		require.Equal(t, 1, *normCalls)

		assert.Equal(t, upper, r.KeyNonProbing(filepath.Join(dir, "Movie.mkv")),
			"the frozen fallback decides the whole pass")
		assert.NotEqual(t,
			r.KeyNonProbing(filepath.Join(dir, "Movie.mkv")),
			r.KeyNonProbing(filepath.Join(dir, "movie.mkv")),
			"the mid-pass definitive cache must NOT flip this pass's fold behavior")
		assert.Equal(t, upper, r.Key(filepath.Join(dir, "Movie.mkv")),
			"even Key reuses the pass's frozen posture — one resolver, one decision")

		// …and the freeze never poisons the process-wide caches: the
		// definitive outcome above came from REAL probes (counters prove the
		// fallback published nothing), so a fresh resolver — the next pass —
		// resolves the true posture without probing again.
		r2 := NewDestKeyResolver()
		assert.Equal(t,
			r2.KeyNonProbing(filepath.Join(dir, "Movie.mkv")),
			r2.KeyNonProbing(filepath.Join(dir, "movie.mkv")),
			"a fresh resolver sees the definitive fold")
		assert.Equal(t,
			r2.KeyNonProbing(filepath.Join(dir, "caf\u00e9.mkv")),
			r2.KeyNonProbing(filepath.Join(dir, "cafe\u0301.mkv")),
			"a fresh resolver sees the definitive normalization fold")
		assert.Equal(t, 1, *caseCalls, "the process cache carries the definitive outcome — no re-probe")
		assert.Equal(t, 1, *normCalls)
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

		// codex P2 (PR #241 F2): the PARTIAL posture freezes too — the
		// missing leg becoming definitive mid-pass cannot re-derive this
		// pass's frozen overlay…
		IsNormalizationInsensitiveRoot(dir)
		require.Equal(t, 1, *normCalls)
		assert.NotEqual(t,
			r.Key(filepath.Join(dir, "caf\u00e9.mkv")),
			r.Key(filepath.Join(dir, "cafe\u0301.mkv")),
			"the pass's frozen partial posture never re-derives mid-pass")
		// …while a fresh resolver (the next pass) folds with full definitive
		// knowledge.
		r2 := NewDestKeyResolver()
		assert.Equal(t,
			r2.KeyNonProbing(filepath.Join(dir, "caf\u00e9.mkv")),
			r2.KeyNonProbing(filepath.Join(dir, "cafe\u0301.mkv")),
			"the next pass folds normalization variants with full definitive knowledge")
	})
}
