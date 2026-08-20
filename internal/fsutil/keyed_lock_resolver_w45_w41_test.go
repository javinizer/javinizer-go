package fsutil

// POSTER-WRITE-HARDENING wave-45 (codex P2, PR#215 finding F2) — the
// DestKeyResolver freezes each root's case/normalization postures ONCE per
// grouping invocation: a transient probe failure is deliberately never cached
// (wave-25), so per-entry DestKey calls could flip posture MID-JOURNAL and
// split one file's case-variant cousins across destination-key buckets.

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// The seam mutation is process-global — these tests never run parallel.
func TestDestKeyResolverW45_OneProbePerRootPerResolver(t *testing.T) {
	root := t.TempDir()
	lower := filepath.Join(root, "poster.jpg")
	upper := filepath.Join(root, "POSTER.JPG")

	prevCase, prevNorm := CaseSensitiveProbe, NormalizationProbe
	t.Cleanup(func() {
		CaseSensitiveProbe, NormalizationProbe = prevCase, prevNorm
		ResetCaseSensitivityCache()
		ResetNormalizationCache()
	})
	ResetCaseSensitivityCache()
	ResetNormalizationCache()

	var caseCalls, normCalls int
	CaseSensitiveProbe = func(string) (bool, error) { caseCalls++; return true, nil }
	NormalizationProbe = func(string) (bool, error) { normCalls++; return false, nil }

	r := NewDestKeyResolver()
	k1 := r.Key(lower)
	k2 := r.Key(upper)
	require.Equal(t, 1, caseCalls, "the root's case posture probes exactly once for the whole resolver")
	require.Equal(t, 1, normCalls, "the root's normalization posture probes exactly once")
	require.NotEqual(t, k1, k2, "case-sensitive posture preserves case distinctions")
	require.Equal(t, k1, r.Key(lower), "repeat keys derive from the frozen posture")

	// A fresh resolver re-resolves (definitive outcomes ride the process
	// cache; the freeze horizon is the invocation, never longer-lived).
	r2 := NewDestKeyResolver()
	require.Equal(t, k1, r2.Key(lower))
	require.Equal(t, 1, caseCalls, "the definitive posture rides the process cache into the next resolver")
}

// The wave-45 failure mixture: first probe errors transiently (uncached,
// conservative case-preserving), the retry succeeds INSENSITIVE. One resolver
// sees ONE posture for the whole journal.
func TestDestKeyResolverW45_TransientFailureFrozenForWholeResolver(t *testing.T) {
	root := t.TempDir()
	lower := filepath.Join(root, "poster.jpg")
	upper := filepath.Join(root, "Poster.jpg")

	prevCase := CaseSensitiveProbe
	t.Cleanup(func() {
		CaseSensitiveProbe = prevCase
		ResetCaseSensitivityCache()
	})
	ResetCaseSensitivityCache()

	caseCalls := 0
	CaseSensitiveProbe = func(string) (bool, error) {
		caseCalls++
		if caseCalls == 1 {
			return false, errors.New("transient probe outage")
		}
		return false, nil // definitive INSENSITIVE on recovery
	}

	r1 := NewDestKeyResolver()
	k1 := r1.Key(lower)
	k2 := r1.Key(upper)
	require.Equal(t, 1, caseCalls, "one probe per root per invocation — even an uncached error outcome freezes")
	require.NotEqual(t, k1, k2,
		"the conservative frozen posture keeps the cousins in distinct buckets for the WHOLE invocation — no mixed-posture split")

	r2 := NewDestKeyResolver()
	require.Equal(t, r2.Key(lower), r2.Key(upper),
		"the recovered definitive posture folds the cousins under one bucket within its own invocation")
	require.Equal(t, 2, caseCalls)
}

// Resolver keys agree byte-for-byte with the equivalent per-call DestKey
// resolution on every posture leg the host exercises.
func TestDestKeyResolverW45_MatchesDestKeyPerCall(t *testing.T) {
	root := t.TempDir()
	r := NewDestKeyResolver()
	for _, name := range []string{
		filepath.Join(root, "poster.jpg"),
		filepath.Join(root, "Poster.JPG"),
		filepath.Join(root, "strasse.jpg"),
		filepath.Join(root, "café.jpg"),
		filepath.Join(root, " spaced .jpg"),
		filepath.Join(root, "sub", "dir", "Trailer.MP4"),
	} {
		require.Equal(t, DestKey(name), r.Key(name), "path %q", name)
	}
}
