package batch

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func fmiFor(path, movieID string, part int) models.FileMatchInfo {
	return models.FileMatchInfo{Path: path, Name: path, MovieID: movieID, IsMultiPart: true, PartNumber: part}
}

func TestResolveManualInputOverride_AllowsConflictingInputsForSameMovieID(t *testing.T) {
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

	result := resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)

	assert.Equal(t, "IPX-111", result["/d/ABC-001-pt1.mp4"], "each submitted file keeps its own manual input")
	assert.Equal(t, "IPX-222", result["/d/ABC-001-pt2.mp4"], "each submitted file keeps its own manual input")
	assert.Equal(t, "IPX-111", fileMatchInfo["/d/ABC-001-pt1.mp4"].MovieID, "fileMatchInfo MovieID overridden with the manual input")
	assert.Equal(t, "IPX-222", fileMatchInfo["/d/ABC-001-pt2.mp4"].MovieID, "fileMatchInfo MovieID overridden with the manual input")
	assert.False(t, fileMatchInfo["/d/ABC-001-pt1.mp4"].IsMultiPart, "IsMultiPart cleared for split files")
	assert.False(t, fileMatchInfo["/d/ABC-001-pt2.mp4"].IsMultiPart, "IsMultiPart cleared for split files")
}

func TestResolveManualInputOverride_DoesNotOverwriteCoSubmittedSibling(t *testing.T) {
	submitted := []string{"/d/ABC-001-pt1.mp4", "/d/ABC-001-pt2.mp4"}
	manualInputs := map[string]string{"/d/ABC-001-pt1.mp4": "IPX-999"}
	fileMatchInfo := map[string]models.FileMatchInfo{
		"/d/ABC-001-pt1.mp4": fmiFor("/d/ABC-001-pt1.mp4", "ABC-001", 1),
		"/d/ABC-001-pt2.mp4": fmiFor("/d/ABC-001-pt2.mp4", "ABC-001", 2),
	}
	allFiles := submitted

	result := resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)

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

	result := resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)

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

	result := resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)

	assert.Len(t, result, 1, "only the manually-input row gets an override; the auto row stays auto (no map entry)")
	assert.Equal(t, "ABC-123", result["/a/SSIS-001.mp4"], "manual row scrapes as its explicit input")
	_, hasAuto := result["/b/SSIS-002.mp4"]
	assert.False(t, hasAuto, "auto row (no input, no matching sibling) is absent from the override map — buildScrapeCmd auto-IDs it from the matcher")
}

func TestResolveManualInputOverride_OverridesGroupingKey(t *testing.T) {
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

	resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)

	assert.Equal(t, "IPX-111", fileMatchInfo["/d/ABC-001-pt1.mp4"].MovieID, "MovieID replaced with manual input")
	assert.Equal(t, "IPX-222", fileMatchInfo["/d/ABC-001-pt2.mp4"].MovieID, "MovieID replaced with manual input")
	assert.False(t, fileMatchInfo["/d/ABC-001-pt1.mp4"].IsMultiPart, "IsMultiPart cleared")
	assert.False(t, fileMatchInfo["/d/ABC-001-pt2.mp4"].IsMultiPart, "IsMultiPart cleared")
	assert.Equal(t, 0, fileMatchInfo["/d/ABC-001-pt1.mp4"].PartNumber, "PartNumber reset")
	assert.Equal(t, 0, fileMatchInfo["/d/ABC-001-pt2.mp4"].PartNumber, "PartNumber reset")
	assert.Equal(t, "", fileMatchInfo["/d/ABC-001-pt1.mp4"].PartSuffix, "PartSuffix reset")
	assert.Equal(t, "", fileMatchInfo["/d/ABC-001-pt2.mp4"].PartSuffix, "PartSuffix reset")
}

func TestResolveManualInputOverride_AmbiguousMovieSkipsPropagation(t *testing.T) {
	submitted := []string{"/d/ABC-001-pt1.mp4", "/d/ABC-001-pt2.mp4"}
	manualInputs := map[string]string{
		"/d/ABC-001-pt1.mp4": "IPX-111",
		"/d/ABC-001-pt2.mp4": "IPX-222",
	}
	fileMatchInfo := map[string]models.FileMatchInfo{
		"/d/ABC-001-pt1.mp4": fmiFor("/d/ABC-001-pt1.mp4", "ABC-001", 1),
		"/d/ABC-001-pt2.mp4": fmiFor("/d/ABC-001-pt2.mp4", "ABC-001", 2),
		"/d/ABC-001-pt3.mp4": fmiFor("/d/ABC-001-pt3.mp4", "ABC-001", 3),
	}
	allFiles := []string{"/d/ABC-001-pt1.mp4", "/d/ABC-001-pt2.mp4", "/d/ABC-001-pt3.mp4"}

	result := resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)

	assert.Equal(t, "IPX-111", result["/d/ABC-001-pt1.mp4"], "submitted file keeps its own input")
	assert.Equal(t, "IPX-222", result["/d/ABC-001-pt2.mp4"], "submitted file keeps its own input")
	_, hasSibling := result["/d/ABC-001-pt3.mp4"]
	assert.False(t, hasSibling, "discovered sibling for an ambiguous MovieID is NOT propagated")
}
