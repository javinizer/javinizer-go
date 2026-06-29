package batch

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fmiFor(path, movieID string, part int) models.FileMatchInfo {
	return models.FileMatchInfo{Path: path, Name: path, MovieID: movieID, IsMultiPart: true, PartNumber: part}
}

func TestResolveManualInputOverride_RejectsConflictingInputsForSameMovieID(t *testing.T) {
	submitted := []string{"/d/ABC-001-pt1.mp4", "/d/ABC-001-pt2.mp4"}
	manualInputs := map[string]string{
		"/d/ABC-001-pt1.mp4": "IPX-111",
		"/d/ABC-001-pt2.mp4": "IPX-222",
	}
	fileMatchInfo := map[string]models.FileMatchInfo{
		"/d/ABC-001-pt1.mp4": fmiFor("/d/ABC-001-pt1.mp4", "ABC-001", 1),
		"/d/ABC-001-pt2.mp4": fmiFor("/d/ABC-001-pt2.mp4", "ABC-001", 2),
	}
	allFiles := submitted

	_, err := resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)

	require.Error(t, err, "two submitted files sharing a matcher MovieID with conflicting inputs must be rejected (non-deterministic scrape)")
	assert.Contains(t, err.Error(), "conflicting manual inputs")
}

func TestResolveManualInputOverride_DoesNotOverwriteCoSubmittedSibling(t *testing.T) {
	submitted := []string{"/d/ABC-001-pt1.mp4", "/d/ABC-001-pt2.mp4"}
	manualInputs := map[string]string{"/d/ABC-001-pt1.mp4": "IPX-999"} // only pt1 has an input
	fileMatchInfo := map[string]models.FileMatchInfo{
		"/d/ABC-001-pt1.mp4": fmiFor("/d/ABC-001-pt1.mp4", "ABC-001", 1),
		"/d/ABC-001-pt2.mp4": fmiFor("/d/ABC-001-pt2.mp4", "ABC-001", 2),
	}
	allFiles := submitted // no discovered siblings

	result, err := resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)

	require.NoError(t, err)
	assert.Equal(t, "IPX-999", result["/d/ABC-001-pt1.mp4"], "submitter keeps its own input")
	_, has := result["/d/ABC-001-pt2.mp4"]
	assert.False(t, has, "co-submitted pt2 does NOT inherit pt1's input — it stays auto-ID (its own row's choice)")
}

func TestResolveManualInputOverride_PropagatesToDiscoveredSibling(t *testing.T) {
	submitted := []string{"/d/ABC-001-pt1.mp4"}
	manualInputs := map[string]string{"/d/ABC-001-pt1.mp4": "IPX-999"}
	fileMatchInfo := map[string]models.FileMatchInfo{
		"/d/ABC-001-pt1.mp4": fmiFor("/d/ABC-001-pt1.mp4", "ABC-001", 1),
		"/d/ABC-001-pt2.mp4": fmiFor("/d/ABC-001-pt2.mp4", "ABC-001", 2),
	}
	allFiles := []string{"/d/ABC-001-pt1.mp4", "/d/ABC-001-pt2.mp4"}

	result, err := resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)

	require.NoError(t, err)
	assert.Equal(t, "IPX-999", result["/d/ABC-001-pt1.mp4"], "submitter keeps its own input")
	assert.Equal(t, "IPX-999", result["/d/ABC-001-pt2.mp4"], "discovered sibling inherits the submitter's input (same matcher MovieID)")
}

func TestResolveManualInputOverride_MixedBatchManualAndAuto(t *testing.T) {
	submitted := []string{"/a/SSIS-001.mp4", "/b/SSIS-002.mp4"}
	manualInputs := map[string]string{"/a/SSIS-001.mp4": "ABC-123"}
	fileMatchInfo := map[string]models.FileMatchInfo{
		"/a/SSIS-001.mp4": fmiFor("/a/SSIS-001.mp4", "SSIS-001", 1),
		"/b/SSIS-002.mp4": fmiFor("/b/SSIS-002.mp4", "SSIS-002", 1),
	}
	allFiles := submitted

	result, err := resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)

	require.NoError(t, err)
	assert.Len(t, result, 1, "only the manually-input row gets an override; the auto row stays auto (no map entry)")
	assert.Equal(t, "ABC-123", result["/a/SSIS-001.mp4"], "manual row scrapes as its explicit input")
	_, hasAuto := result["/b/SSIS-002.mp4"]
	assert.False(t, hasAuto, "auto row (no input, no matching sibling) is absent from the override map — buildScrapeCmd auto-IDs it from the matcher")
}
