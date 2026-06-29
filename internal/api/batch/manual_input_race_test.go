package batch

import (
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveManualInputOverride_ConcurrentReadSafety asserts the manual-input
// override resolution is safe under the race detector when many goroutines
// resolve the same shared inputs concurrently. resolveManualInputOverride is
// the propagation step on the batch scrape path (usecases.go); it reads
// submittedFiles, manualInputs, and fileMatchInfo and builds a fresh override
// map. Inputs are read-only across the call, so concurrent callers must not
// race on the shared maps/slices and must each observe identical, deterministic
// results. The discovered-sibling propagation case (the new manual-input path)
// is included so the race detector exercises the full build path, not just the
// early-return shortcuts.
func TestResolveManualInputOverride_ConcurrentReadSafety(t *testing.T) {
	t.Parallel()

	submitted := []string{"/d/ABC-001-pt1.mp4"}
	manualInputs := map[string]string{"/d/ABC-001-pt1.mp4": "IPX-999"}
	fileMatchInfo := map[string]models.FileMatchInfo{
		"/d/ABC-001-pt1.mp4": fmiFor("/d/ABC-001-pt1.mp4", "ABC-001", 1),
		"/d/ABC-001-pt2.mp4": fmiFor("/d/ABC-001-pt2.mp4", "ABC-001", 2),
	}
	allFiles := []string{"/d/ABC-001-pt1.mp4", "/d/ABC-001-pt2.mp4"}

	want, err := resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)
	require.NoError(t, err)
	require.Equal(t, "IPX-999", want["/d/ABC-001-pt1.mp4"])
	require.Equal(t, "IPX-999", want["/d/ABC-001-pt2.mp4"], "precondition: discovered sibling inherits submitter input")

	const goroutines = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				got, gerr := resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)
				if gerr != nil {
					t.Errorf("concurrent resolve failed: %v", gerr)
					return
				}
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
// concurrent resolution never mutates the shared input maps/slices: a caller
// must treat its inputs as read-only. This guards the contract the usecase
// relies on (the same manualInputs/fileMatchInfo feed into the job's
// FileMatchInfo and RawInputOverride) — if a concurrent resolver mutated them,
// a later caller or the scrape phase would observe corrupted data.
func TestResolveManualInputOverride_ConcurrentSharedInputs_NoMutation(t *testing.T) {
	t.Parallel()

	submitted := []string{"/d/ABC-001-pt1.mp4"}
	manualInputs := map[string]string{"/d/ABC-001-pt1.mp4": "IPX-999"}
	fileMatchInfo := map[string]models.FileMatchInfo{
		"/d/ABC-001-pt1.mp4": fmiFor("/d/ABC-001-pt1.mp4", "ABC-001", 1),
		"/d/ABC-001-pt2.mp4": fmiFor("/d/ABC-001-pt2.mp4", "ABC-001", 2),
	}
	allFiles := []string{"/d/ABC-001-pt1.mp4", "/d/ABC-001-pt2.mp4"}

	manualSnapshot := map[string]string{"/d/ABC-001-pt1.mp4": "IPX-999"}
	fmiSnapshot := map[string]models.FileMatchInfo{
		"/d/ABC-001-pt1.mp4": fmiFor("/d/ABC-001-pt1.mp4", "ABC-001", 1),
		"/d/ABC-001-pt2.mp4": fmiFor("/d/ABC-001-pt2.mp4", "ABC-001", 2),
	}

	const goroutines = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				_, _ = resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, manualSnapshot, manualInputs, "shared manualInputs must not be mutated by concurrent resolvers")
	assert.Equal(t, fmiSnapshot, fileMatchInfo, "shared fileMatchInfo must not be mutated by concurrent resolvers")
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
