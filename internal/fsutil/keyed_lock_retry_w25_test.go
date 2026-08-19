package fsutil

// POSTER-WRITE-HARDENING codex PR#215 wave-25 — probe ERROR outcomes must
// never be cached (finding: "Retry filesystem probes instead of caching
// uncertainty"). A transient probe failure cached process-lifetime as
// 'sensitive' permanently splits destination keys for spellings that address
// ONE file, defeating cross-chain sequence/conflict matching. The contract
// now: concurrent first-time derivations share ONE in-flight probe
// (single-flight), the error posture is returned to every sharer WITHOUT
// publishing, the cache stays EMPTY after an error, and the next derivation
// re-probes — folding correctly once the probe recovers.

import (
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// w25CaseCacheHas reports (white-box) whether root's case probe published a
// definitive cache entry.
func w25CaseCacheHas(root string) bool {
	caseSensitivityCacheMu.Lock()
	defer caseSensitivityCacheMu.Unlock()
	_, ok := caseSensitivityCache[cleanProbeRoot(root)]
	return ok
}

// w25NormCacheHas reports (white-box) whether root's normalization probe
// published a definitive cache entry.
func w25NormCacheHas(root string) bool {
	normalizationCacheMu.Lock()
	defer normalizationCacheMu.Unlock()
	_, ok := normalizationCache[cleanProbeRoot(root)]
	return ok
}

func w25SetCaseProbe(t *testing.T, probe caseSensitivityProbe) {
	t.Helper()
	previous := CaseSensitiveProbe
	CaseSensitiveProbe = probe
	ResetCaseSensitivityCache()
	t.Cleanup(func() {
		CaseSensitiveProbe = previous
		ResetCaseSensitivityCache()
	})
}

func w25SetNormProbe(t *testing.T, probe caseSensitivityProbe) {
	t.Helper()
	previous := NormalizationProbe
	NormalizationProbe = probe
	ResetNormalizationCache()
	t.Cleanup(func() {
		NormalizationProbe = previous
		ResetNormalizationCache()
	})
}

// The finding's core contract for the case probe: a first-derivation probe
// error is conservative per-call, publishes NOTHING, and the second
// derivation re-probes — folding correctly once the probe recovers.
func TestIsCaseSensitiveRootW25_ProbeErrorNotCachedThenRecovers(t *testing.T) {
	root := t.TempDir()
	upper := filepath.Join(root, "Poster.jpg")
	lower := filepath.Join(root, "poster.jpg")

	probeErr := errors.New("w25 transient probe failure")
	w25SetCaseProbe(t, func(string) (bool, error) { return true, probeErr })
	require.True(t, IsCaseSensitiveRoot(root), "probe error stays conservative (case preserved)")
	require.False(t, w25CaseCacheHas(root), "an ERROR outcome must never publish a cache entry")
	require.True(t, IsCaseSensitiveRoot(root))
	require.False(t, w25CaseCacheHas(root), "the error stays uncached on the second derivation")
	require.NotEqual(t, DestKeyForRoot(root, upper), DestKeyForRoot(root, lower),
		"while undecidable, spellings keep distinct keys")

	// The probe recovers with a positive INSENSITIVE determination: the very
	// next derivation re-probes, folds, and then serves the folded posture
	// from cache.
	CaseSensitiveProbe = func(string) (bool, error) { return false, nil }
	require.False(t, IsCaseSensitiveRoot(root), "the retried probe folds")
	require.True(t, w25CaseCacheHas(root), "the definitive recovery publishes")
	require.Equal(t, DestKeyForRoot(root, upper), DestKeyForRoot(root, lower),
		"first-probe-error → second derivation re-probes and folds correctly afterward")

	// Recovery to SENSITIVE works the same when that is the definitive answer.
	root2 := t.TempDir()
	w25SetCaseProbe(t, func(string) (bool, error) { return true, probeErr })
	require.True(t, IsCaseSensitiveRoot(root2))
	require.False(t, w25CaseCacheHas(root2))
	CaseSensitiveProbe = func(string) (bool, error) { return true, nil }
	require.True(t, IsCaseSensitiveRoot(root2), "sensitive recovery keeps case distinctions")
	require.True(t, w25CaseCacheHas(root2), "sensitive is a definitive outcome and caches")
}

// Single-flight with an ERROR outcome: every concurrent sharer gets the
// conservative posture from the ONE in-flight probe, the cache stays empty,
// and a later derivation retries the probe.
func TestIsCaseSensitiveRootW25_SingleFlightErrorSharedThenRetry(t *testing.T) {
	root := t.TempDir()
	probeErr := errors.New("w25 blocked then failing probe")
	var calls atomic.Int32
	inFlight := make(chan struct{})
	release := make(chan struct{})
	w25SetCaseProbe(t, func(string) (bool, error) {
		if calls.Add(1) == 1 {
			close(inFlight)
		}
		<-release
		return false, probeErr
	})

	const sharers = 3
	results := make([]bool, sharers)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		results[0] = IsCaseSensitiveRoot(root)
	}()
	select {
	case <-inFlight:
	case <-time.After(2 * time.Second):
		t.Fatal("the flight leader never entered the probe")
	}
	for i := 1; i < sharers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = IsCaseSensitiveRoot(root)
		}(i)
	}
	require.Never(t, func() bool { return calls.Load() != 1 },
		200*time.Millisecond, time.Millisecond, "joiners share the in-flight probe instead of re-probing")
	close(release)
	wg.Wait()

	require.Equal(t, int32(1), calls.Load(), "one probe served every concurrent caller")
	for i, got := range results {
		require.True(t, got, "sharer %d gets the conservative error posture", i)
	}
	require.False(t, w25CaseCacheHas(root), "the failed flight leaves the cache EMPTY")

	// The next derivation retries the probe rather than inheriting the error.
	CaseSensitiveProbe = func(string) (bool, error) { calls.Add(1); return false, nil }
	require.False(t, IsCaseSensitiveRoot(root), "the retry folds once the probe recovers")
	require.Equal(t, int32(2), calls.Load(), "exactly one retry probe; its definitive publish caches")
	require.True(t, w25CaseCacheHas(root))
}

// The normalization probe mirrors the case probe: error outcomes are
// returned uncached to every single-flight sharer, and the next derivation
// retries.
func TestIsNormalizationInsensitiveRootW25_ProbeErrorNotCachedThenRecovers(t *testing.T) {
	root := t.TempDir()
	w25SetCaseProbe(t, func(string) (bool, error) { return false, nil }) // force case-insensitive for folding checks
	nfc := filepath.Join(root, "caf\u00e9.jpg")                          // U+00E9 precomposed
	nfd := filepath.Join(root, "cafe\u0301.jpg")                         // e + U+0301 combining acute
	require.NotEqual(t, nfc, nfd, "fixture spellings must differ byte-wise")

	probeErr := errors.New("w25 transient normalization probe failure")
	var calls atomic.Int32
	w25SetNormProbe(t, func(string) (bool, error) { calls.Add(1); return true, probeErr })
	require.False(t, IsNormalizationInsensitiveRoot(root), "probe error stays conservative (no fold)")
	require.False(t, w25NormCacheHas(root), "an ERROR outcome must never publish a cache entry")
	require.NotEqual(t, DestKeyForRoot(root, nfc), DestKeyForRoot(root, nfd),
		"while undecidable, the NFC/NFD spellings keep distinct keys")
	require.Equal(t, int32(3), calls.Load(), "every derivation re-probed (direct + one per DestKeyForRoot)")
	require.False(t, w25NormCacheHas(root), "still no cache entry after repeated failures")

	NormalizationProbe = func(string) (bool, error) { calls.Add(1); return true, nil }
	require.True(t, IsNormalizationInsensitiveRoot(root), "the retried probe folds")
	require.True(t, w25NormCacheHas(root), "the definitive recovery publishes")
	require.Equal(t, DestKeyForRoot(root, nfc), DestKeyForRoot(root, nfd),
		"first-probe-error → second derivation re-probes and folds correctly afterward")
	require.Equal(t, int32(4), calls.Load(), "exactly one retry probe; the publish serves from cache afterwards")
	require.True(t, IsNormalizationInsensitiveRoot(root))
	require.Equal(t, int32(4), calls.Load(), "cached recovery is not re-probed")
}

// One normalization flight serves N concurrent callers; the error is shared
// uncached and the next derivation retries.
func TestIsNormalizationInsensitiveRootW25_SingleFlightErrorSharedThenRetry(t *testing.T) {
	root := t.TempDir()
	probeErr := errors.New("w25 blocked then failing norm probe")
	var calls atomic.Int32
	inFlight := make(chan struct{})
	release := make(chan struct{})
	w25SetNormProbe(t, func(string) (bool, error) {
		if calls.Add(1) == 1 {
			close(inFlight)
		}
		<-release
		return true, probeErr
	})

	results := make([]bool, 2)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		results[0] = IsNormalizationInsensitiveRoot(root)
	}()
	select {
	case <-inFlight:
	case <-time.After(2 * time.Second):
		t.Fatal("the flight leader never entered the probe")
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		results[1] = IsNormalizationInsensitiveRoot(root)
	}()
	require.Never(t, func() bool { return calls.Load() != 1 },
		200*time.Millisecond, time.Millisecond, "the joiner shares the in-flight probe")
	close(release)
	wg.Wait()
	require.Equal(t, int32(1), calls.Load(), "one probe served the racing callers")
	require.False(t, results[0])
	require.False(t, results[1], "the sharer also gets the conservative error posture")
	require.False(t, w25NormCacheHas(root), "the failed flight leaves the cache EMPTY")

	NormalizationProbe = func(string) (bool, error) { calls.Add(1); return false, nil }
	require.False(t, IsNormalizationInsensitiveRoot(root), "the retry gets the definitive sensitive answer")
	require.Equal(t, int32(2), calls.Load())
	require.True(t, w25NormCacheHas(root), "definitive answers cache even after a failed flight")
}

// A definitive flight outcome is what every sharer observes — including one
// that joined microseconds before publication (shares the flight, not the
// cache read).
func TestIsCaseSensitiveRootW25_SingleFlightSuccessShared(t *testing.T) {
	root := t.TempDir()
	var calls atomic.Int32
	inFlight := make(chan struct{})
	release := make(chan struct{})
	w25SetCaseProbe(t, func(string) (bool, error) {
		if calls.Add(1) == 1 {
			close(inFlight)
		}
		<-release
		return false, nil
	})

	const sharers = 4
	results := make([]bool, sharers)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = IsCaseSensitiveRoot(root)
		}(i)
	}
	select {
	case <-inFlight:
	case <-time.After(2 * time.Second):
		t.Fatal("no probe entered")
	}
	require.Never(t, func() bool { return calls.Load() != 1 },
		200*time.Millisecond, time.Millisecond)
	close(release)
	wg.Wait()
	require.Equal(t, int32(1), calls.Load(), "all callers shared ONE probe")
	for i, got := range results {
		require.False(t, got, "sharer %d converged on the definitive insensitive answer", i)
	}
	require.True(t, w25CaseCacheHas(root), "the definitive outcome published once")
	require.False(t, IsCaseSensitiveRoot(root), "post-publication reads hit the cache")
	require.Equal(t, int32(1), calls.Load())
}
