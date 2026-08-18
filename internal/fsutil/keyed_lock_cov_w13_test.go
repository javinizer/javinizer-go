package fsutil

import (
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func w13SetProbe(t *testing.T, probe caseSensitivityProbe) {
	t.Helper()
	previous := CaseSensitiveProbe
	CaseSensitiveProbe = probe
	ResetCaseSensitivityCache()
	t.Cleanup(func() {
		CaseSensitiveProbe = previous
		ResetCaseSensitivityCache()
	})
}

// Codex P2: when the filesystem probe ERRORS, the case-fold decision is
// undecided — folding '/A/x' and '/a/x' onto one key could alias byte-distinct
// files on a case-SENSITIVE volume. The conservative fallback keeps case
// distinctions; only a positive insensitivity determination folds.
func TestDestKeyW13_ProbeErrorPreservesCaseDistinctions(t *testing.T) {
	root := t.TempDir()
	upper := filepath.Join(root, "A", "x.jpg")
	lower := filepath.Join(root, "a", "x.jpg")

	probeErr := errors.New("probe undecidable")
	w13SetProbe(t, func(string) (bool, error) { return false, probeErr })
	require.True(t, IsCaseSensitiveRoot(root),
		"an undecidable probe must report case-preserving posture, not guessed-insensitive")
	require.NotEqual(t, DestKeyForRoot(root, upper), DestKeyForRoot(root, lower),
		"probe failure keeps '/A/x' and '/a/x' on DISTINCT keys")

	// A positive insensitivity determination is the ONLY fold unlock.
	w13SetProbe(t, func(string) (bool, error) { return false, nil })
	require.False(t, IsCaseSensitiveRoot(root))
	require.Equal(t, DestKeyForRoot(root, upper), DestKeyForRoot(root, lower),
		"insensitive determination folds the same spellings into one key")
}

// Codex P2: the cache mutex must not be held across the filesystem probe —
// one root's slow probe must not serialize every other root's key derivation.
func TestIsCaseSensitiveRootW13_SlowProbeDoesNotSerializeOtherRoots(t *testing.T) {
	blockedRoot := t.TempDir()
	freeRoot := t.TempDir()
	release := make(chan struct{})
	var blockedCalls atomic.Int32
	w13SetProbe(t, func(root string) (bool, error) {
		if root == cleanProbeRoot(blockedRoot) {
			blockedCalls.Add(1)
			<-release
			return true, nil
		}
		return false, nil
	})

	blockedDone := make(chan bool, 1)
	go func() { blockedDone <- IsCaseSensitiveRoot(blockedRoot) }()
	require.Eventually(t, func() bool { return blockedCalls.Load() == 1 },
		2*time.Second, time.Millisecond, "the blocked root's probe must be in flight")

	freeDone := make(chan bool, 1)
	go func() { freeDone <- IsCaseSensitiveRoot(freeRoot) }()
	select {
	case got := <-freeDone:
		require.False(t, got)
	case <-time.After(2 * time.Second):
		t.Fatal("a second root serialized behind another root's in-flight probe (mutex held across filesystem IO)")
	}

	close(release)
	select {
	case got := <-blockedDone:
		require.True(t, got)
	case <-time.After(2 * time.Second):
		t.Fatal("the blocked probe never finished after release")
	}
}

// Racing FIRST acquisitions on one root: each caller probes at most once, no
// deadlock, and the speculative-cache revalidation publishes exactly one
// posture that both callers adopt.
func TestIsCaseSensitiveRootW13_ConcurrentFirstProbesPublishOnePosture(t *testing.T) {
	root := t.TempDir()
	var entered atomic.Int32
	bothEntered := make(chan struct{})
	w13SetProbe(t, func(string) (bool, error) {
		if entered.Add(1) == 2 {
			close(bothEntered)
		}
		<-bothEntered
		return true, nil
	})

	var wg sync.WaitGroup
	results := make([]bool, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = IsCaseSensitiveRoot(root)
		}(i)
	}
	wg.Wait()
	require.Equal(t, int32(2), entered.Load(),
		"both first-time callers probed in parallel (no mutex serialization), exactly once each")
	require.True(t, results[0])
	require.True(t, results[1], "racing probes converge on one posture")
	require.True(t, IsCaseSensitiveRoot(root),
		"the loser of the speculative publish adopts the cached entry instead of re-probing")
	require.Equal(t, int32(2), entered.Load(), "a cached root is not re-probed after publication")
}
