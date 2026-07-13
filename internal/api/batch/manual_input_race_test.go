package batch

import (
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func copyFMI(m map[string]models.FileMatchInfo) map[string]models.FileMatchInfo {
	out := make(map[string]models.FileMatchInfo, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// TestResolveManualInputOverride_ConcurrentReadSafety asserts the manual-input
// override resolution is safe under the race detector when many goroutines
// resolve equivalent inputs concurrently. resolveManualInputOverride mutates
// fileMatchInfo in place (Layer 1: override the grouping key), so each
// goroutine resolves against its own copy of fileMatchInfo — the production
// caller invokes it once per scrape, never concurrently on a shared map.
// The result map is per-call, and manualInputs is read-only across the call,
// so concurrent callers each observe identical, deterministic results. The
// discovered-sibling propagation case (the new manual-input path) is included
// so the race detector exercises the full build path, not just the
// early-return shortcuts.
func TestResolveManualInputOverride_ConcurrentReadSafety(t *testing.T) {
	t.Parallel()

	submitted := []string{"/d/ABC-001-pt1.mp4"}
	manualInputs := map[string]string{"/d/ABC-001-pt1.mp4": "IPX-999"}
	baseFMI := map[string]models.FileMatchInfo{
		"/d/ABC-001-pt1.mp4": fmiFor("/d/ABC-001-pt1.mp4", "ABC-001", 1),
		"/d/ABC-001-pt2.mp4": fmiFor("/d/ABC-001-pt2.mp4", "ABC-001", 2),
	}
	allFiles := []string{"/d/ABC-001-pt1.mp4", "/d/ABC-001-pt2.mp4"}

	wantResolved := resolveManualInputOverride(submitted, manualInputs, copyFMI(baseFMI), allFiles)
	want := wantResolved.overrides
	require.Equal(t, "IPX-999", want["/d/ABC-001-pt1.mp4"])
	require.Equal(t, "IPX-999", want["/d/ABC-001-pt2.mp4"], "precondition: discovered sibling inherits submitter input")

	const goroutines = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				gotResolved := resolveManualInputOverride(submitted, manualInputs, copyFMI(baseFMI), allFiles)
				got := gotResolved.overrides
				if !mapsEqual(got, want) {
					t.Errorf("concurrent resolve diverged: got %v want %v", got, want)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// TestValidateAndSanitizeManualInputs_ConcurrentReadSafety asserts the
// manual-input sanitization+validation layer (the 400 gate on the batch create
// path, lifecycle.go) is race-free and deterministic when many goroutines
// validate the same shared inputs concurrently. Plain IDs (no scheme) are used
// so validation passes without a scraper registry, isolating the manual-input
// data path from external deps.
func TestValidateAndSanitizeManualInputs_ConcurrentReadSafety(t *testing.T) {
	t.Parallel()

	files := []string{"/d/ABC-001-pt1.mp4", "/d/ABC-001-pt2.mp4", "/b/SSIS-002.mp4"}
	rawInputs := map[string]string{
		"/d/ABC-001-pt1.mp4": "  IPX-999  ",
		"/b/SSIS-002.mp4":    "SSIS-002",
	}

	want, err := validateAndSanitizeManualInputs(rawInputs, files, nil)
	require.NoError(t, err)
	require.Len(t, want, 2)

	const goroutines = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				got, gerr := validateAndSanitizeManualInputs(rawInputs, files, nil)
				if gerr != nil {
					t.Errorf("concurrent validate failed: %v", gerr)
					return
				}
				if !mapsEqual(got, want) {
					t.Errorf("concurrent validate diverged: got %v want %v", got, want)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// TestResolveManualInputOverride_ConcurrentSharedInputs_NoMutation asserts that
// concurrent resolution never mutates the shared read-only input map
// manualInputs: a caller must treat manualInputs as read-only. fileMatchInfo
// is now an in/out parameter (Layer 1 overrides its grouping key), so each
// goroutine resolves against its own copy and the production caller invokes
// the resolver once per scrape, never concurrently on a shared map. This
// guards the contract the usecase relies on (the same manualInputs feed into
// the job's RawInputOverride) — if a concurrent resolver mutated it, a later
// caller or the scrape phase would observe corrupted data.
func TestResolveManualInputOverride_ConcurrentSharedInputs_NoMutation(t *testing.T) {
	t.Parallel()

	submitted := []string{"/d/ABC-001-pt1.mp4"}
	manualInputs := map[string]string{"/d/ABC-001-pt1.mp4": "IPX-999"}
	baseFMI := map[string]models.FileMatchInfo{
		"/d/ABC-001-pt1.mp4": fmiFor("/d/ABC-001-pt1.mp4", "ABC-001", 1),
		"/d/ABC-001-pt2.mp4": fmiFor("/d/ABC-001-pt2.mp4", "ABC-001", 2),
	}
	allFiles := []string{"/d/ABC-001-pt1.mp4", "/d/ABC-001-pt2.mp4"}

	manualSnapshot := map[string]string{"/d/ABC-001-pt1.mp4": "IPX-999"}

	const goroutines = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				_ = resolveManualInputOverride(submitted, manualInputs, copyFMI(baseFMI), allFiles)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, manualSnapshot, manualInputs, "shared manualInputs must not be mutated by concurrent resolvers")
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}
