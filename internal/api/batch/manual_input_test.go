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

	resolved := resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)
	result := resolved.overrides

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

	resolved := resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)
	result := resolved.overrides

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

	resolved := resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)
	result := resolved.overrides

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

	resolved := resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)
	result := resolved.overrides

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

	resolved := resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)
	result := resolved.overrides

	assert.Equal(t, "IPX-111", result["/d/ABC-001-pt1.mp4"], "submitted file keeps its own input")
	assert.Equal(t, "IPX-222", result["/d/ABC-001-pt2.mp4"], "submitted file keeps its own input")
	_, hasSibling := result["/d/ABC-001-pt3.mp4"]
	assert.False(t, hasSibling, "discovered sibling for an ambiguous MovieID is NOT propagated")
}

func TestResolveManualInputOverride_PreservesMultipartForSameManualID(t *testing.T) {
	submitted := []string{"/d/ABC-001-pt1.mp4", "/d/ABC-001-pt2.mp4"}
	manualInputs := map[string]string{
		"/d/ABC-001-pt1.mp4": "ABC-001",
		"/d/ABC-001-pt2.mp4": "ABC-001",
	}
	fileMatchInfo := map[string]models.FileMatchInfo{
		"/d/ABC-001-pt1.mp4": fmiFor("/d/ABC-001-pt1.mp4", "ABC-001", 1),
		"/d/ABC-001-pt2.mp4": fmiFor("/d/ABC-001-pt2.mp4", "ABC-001", 2),
	}
	allFiles := submitted

	resolved := resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)
	result := resolved.overrides

	assert.Equal(t, "ABC-001", result["/d/ABC-001-pt1.mp4"], "manual input preserved in result map")
	assert.Equal(t, "ABC-001", result["/d/ABC-001-pt2.mp4"], "manual input preserved in result map")
	assert.Equal(t, "ABC-001", fileMatchInfo["/d/ABC-001-pt1.mp4"].MovieID, "MovieID matches the manual input")
	assert.Equal(t, "ABC-001", fileMatchInfo["/d/ABC-001-pt2.mp4"].MovieID, "MovieID matches the manual input")
	assert.True(t, fileMatchInfo["/d/ABC-001-pt1.mp4"].IsMultiPart, "IsMultiPart preserved when manual ID matches matcher ID")
	assert.True(t, fileMatchInfo["/d/ABC-001-pt2.mp4"].IsMultiPart, "IsMultiPart preserved when manual ID matches matcher ID")
	assert.Equal(t, 1, fileMatchInfo["/d/ABC-001-pt1.mp4"].PartNumber, "PartNumber preserved for genuine multi-part")
	assert.Equal(t, 2, fileMatchInfo["/d/ABC-001-pt2.mp4"].PartNumber, "PartNumber preserved for genuine multi-part")
}

func TestResolveManualInputOverride_PreservesMultipartForPropagatedSibling(t *testing.T) {
	// Matcher groups pt1+pt2 under ABC-001. User submits only pt1 with
	// manual input IPX-999. Propagation gives pt2 the same input.
	// Both parts should keep multipart metadata (not a split — same input).
	submitted := []string{"/d/ABC-001-pt1.mp4"}
	manualInputs := map[string]string{"/d/ABC-001-pt1.mp4": "IPX-999"}
	fileMatchInfo := map[string]models.FileMatchInfo{
		"/d/ABC-001-pt1.mp4": fmiFor("/d/ABC-001-pt1.mp4", "ABC-001", 1),
		"/d/ABC-001-pt2.mp4": fmiFor("/d/ABC-001-pt2.mp4", "ABC-001", 2),
	}
	allFiles := []string{"/d/ABC-001-pt1.mp4", "/d/ABC-001-pt2.mp4"}

	resolved := resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)
	result := resolved.overrides

	assert.Equal(t, "IPX-999", result["/d/ABC-001-pt1.mp4"], "submitter keeps its input")
	assert.Equal(t, "IPX-999", result["/d/ABC-001-pt2.mp4"], "sibling inherits the propagated input")
	assert.Equal(t, "IPX-999", fileMatchInfo["/d/ABC-001-pt1.mp4"].MovieID, "MovieID overridden")
	assert.Equal(t, "IPX-999", fileMatchInfo["/d/ABC-001-pt2.mp4"].MovieID, "MovieID overridden")
	assert.True(t, fileMatchInfo["/d/ABC-001-pt1.mp4"].IsMultiPart, "multipart preserved for non-ambiguous group")
	assert.True(t, fileMatchInfo["/d/ABC-001-pt2.mp4"].IsMultiPart, "multipart preserved for propagated sibling")
	assert.Equal(t, 1, fileMatchInfo["/d/ABC-001-pt1.mp4"].PartNumber, "PartNumber preserved")
	assert.Equal(t, 2, fileMatchInfo["/d/ABC-001-pt2.mp4"].PartNumber, "PartNumber preserved")
}

func TestResolveManualInputOverride_PreservesMultipartForSameIDSubsetInAmbiguousGroup(t *testing.T) {
	// pt1 and pt2 both set to IPX-111; pt3 set to IPX-222.
	// pt1/pt2 share the same input → keep multipart metadata.
	// pt3 is unique → loses multipart metadata.
	submitted := []string{"/d/ABC-001-pt1.mp4", "/d/ABC-001-pt2.mp4", "/d/ABC-001-pt3.mp4"}
	manualInputs := map[string]string{
		"/d/ABC-001-pt1.mp4": "IPX-111",
		"/d/ABC-001-pt2.mp4": "IPX-111",
		"/d/ABC-001-pt3.mp4": "IPX-222",
	}
	fileMatchInfo := map[string]models.FileMatchInfo{
		"/d/ABC-001-pt1.mp4": fmiFor("/d/ABC-001-pt1.mp4", "ABC-001", 1),
		"/d/ABC-001-pt2.mp4": fmiFor("/d/ABC-001-pt2.mp4", "ABC-001", 2),
		"/d/ABC-001-pt3.mp4": fmiFor("/d/ABC-001-pt3.mp4", "ABC-001", 3),
	}
	allFiles := submitted

	resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)

	assert.True(t, fileMatchInfo["/d/ABC-001-pt1.mp4"].IsMultiPart, "pt1 keeps multipart (shares IPX-111 with pt2)")
	assert.True(t, fileMatchInfo["/d/ABC-001-pt2.mp4"].IsMultiPart, "pt2 keeps multipart (shares IPX-111 with pt1)")
	assert.False(t, fileMatchInfo["/d/ABC-001-pt3.mp4"].IsMultiPart, "pt3 loses multipart (unique IPX-222)")
	assert.Equal(t, 1, fileMatchInfo["/d/ABC-001-pt1.mp4"].PartNumber, "pt1 PartNumber preserved")
	assert.Equal(t, 2, fileMatchInfo["/d/ABC-001-pt2.mp4"].PartNumber, "pt2 PartNumber preserved")
	assert.Equal(t, "IPX-111", fileMatchInfo["/d/ABC-001-pt1.mp4"].MovieID, "pt1 MovieID overridden")
	assert.Equal(t, "IPX-111", fileMatchInfo["/d/ABC-001-pt2.mp4"].MovieID, "pt2 MovieID overridden")
	assert.Equal(t, "IPX-222", fileMatchInfo["/d/ABC-001-pt3.mp4"].MovieID, "pt3 MovieID overridden")
}

func TestResolveManualInputOverride_RedactsURLInMovieID(t *testing.T) {
	submitted := []string{"/d/video.mp4"}
	rawURL := "https://example.com/video?token=secret"
	manualInputs := map[string]string{
		"/d/video.mp4": rawURL,
	}
	fileMatchInfo := map[string]models.FileMatchInfo{
		"/d/video.mp4": fmiFor("/d/video.mp4", "matcher-id", 1),
	}
	allFiles := submitted

	resolved := resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)
	result := resolved.overrides

	assert.Equal(t, rawURL, result["/d/video.mp4"], "RawInputOverride (result map) keeps the raw URL for the scraper")
	assert.NotContains(t, fileMatchInfo["/d/video.mp4"].MovieID, "token=secret", "MovieID grouping key has query token redacted")
	assert.NotContains(t, fileMatchInfo["/d/video.mp4"].MovieID, "secret", "MovieID grouping key does not leak the token value")
	assert.Equal(t, "https://example.com/video", fileMatchInfo["/d/video.mp4"].MovieID, "MovieID is the redacted URL (scheme+host+path only)")
	// Single file with a single manual input is NOT ambiguous, so multipart
	// metadata is preserved even though the redacted manual ID differs from the
	// matcher ID (a genuine single-part correction, not a split).
	assert.True(t, fileMatchInfo["/d/video.mp4"].IsMultiPart, "IsMultiPart preserved for non-ambiguous single-file manual input")
}

func TestResolveManualInputOverride_CoversDefensiveGuards(t *testing.T) {
	// Cover the defensive guard branches in resolveManualInputOverride.
	// Each sub-test targets a specific continue branch.

	t.Run("manual input for non-submitted file is skipped", func(t *testing.T) {
		// A manual input whose path is NOT in submittedFiles should be
		// skipped in the movieInput loop (line 47: !submitted[path]).
		submitted := []string{"/d/ABC-001-pt1.mp4"}
		manualInputs := map[string]string{
			"/d/ABC-001-pt1.mp4": "IPX-999",
			"/d/unsubmitted.mp4": "IPX-888",
		}
		fileMatchInfo := map[string]models.FileMatchInfo{
			"/d/ABC-001-pt1.mp4": fmiFor("/d/ABC-001-pt1.mp4", "ABC-001", 1),
			"/d/unsubmitted.mp4": fmiFor("/d/unsubmitted.mp4", "ABC-001", 2),
		}
		allFiles := []string{"/d/ABC-001-pt1.mp4", "/d/unsubmitted.mp4"}

		resolved := resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)
		result := resolved.overrides

		assert.Equal(t, "IPX-999", result["/d/ABC-001-pt1.mp4"])
		// The unsubmitted file's input (IPX-888) was seeded into result and is
		// NOT overwritten by propagation (it already has an entry).
		assert.Equal(t, "IPX-888", result["/d/unsubmitted.mp4"], "unsubmitted file keeps its own explicit input, not overwritten by propagation")
	})

	t.Run("submitted file with no fileMatchInfo entry is skipped", func(t *testing.T) {
		// A submitted file with a manual input but no fileMatchInfo entry
		// hits the !ok branch (line 51: !ok || fmi.MovieID == "").
		submitted := []string{"/d/missing.mp4"}
		manualInputs := map[string]string{"/d/missing.mp4": "IPX-999"}
		fileMatchInfo := map[string]models.FileMatchInfo{} // no entry for missing.mp4
		allFiles := submitted

		resolved := resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)
		result := resolved.overrides

		assert.Equal(t, "IPX-999", result["/d/missing.mp4"], "input still seeded into result even without fileMatchInfo")
		// fileMatchInfo should now have the override applied (the override loop
		// adds an entry since the input is non-empty, but !ok skips it — verify
		// no entry was created)
		_, hasFMI := fileMatchInfo["/d/missing.mp4"]
		assert.False(t, hasFMI, "no fileMatchInfo entry created when original was absent")
	})

	t.Run("submitted file with empty MovieID in fileMatchInfo is skipped", func(t *testing.T) {
		// A submitted file whose fileMatchInfo has an empty MovieID
		// hits the fmi.MovieID == "" branch (line 51).
		submitted := []string{"/d/empty-id.mp4"}
		manualInputs := map[string]string{"/d/empty-id.mp4": "IPX-999"}
		fileMatchInfo := map[string]models.FileMatchInfo{
			"/d/empty-id.mp4": {Path: "/d/empty-id.mp4", Name: "empty-id.mp4", MovieID: ""},
		}
		allFiles := submitted

		resolved := resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)
		result := resolved.overrides

		assert.Equal(t, "IPX-999", result["/d/empty-id.mp4"])
	})

	t.Run("discovered sibling already in result is skipped", func(t *testing.T) {
		// A discovered sibling that already has a result entry hits the
		// `has` branch (line 73: _, has := result[path]).
		submitted := []string{"/d/ABC-001-pt1.mp4"}
		manualInputs := map[string]string{
			"/d/ABC-001-pt1.mp4": "IPX-999",
			"/d/ABC-001-pt2.mp4": "IPX-888", // pt2 is a discovered sibling but also has an explicit input
		}
		fileMatchInfo := map[string]models.FileMatchInfo{
			"/d/ABC-001-pt1.mp4": fmiFor("/d/ABC-001-pt1.mp4", "ABC-001", 1),
			"/d/ABC-001-pt2.mp4": fmiFor("/d/ABC-001-pt2.mp4", "ABC-001", 2),
		}
		allFiles := []string{"/d/ABC-001-pt1.mp4", "/d/ABC-001-pt2.mp4"}

		resolved := resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)
		result := resolved.overrides

		assert.Equal(t, "IPX-888", result["/d/ABC-001-pt2.mp4"], "explicit input preserved, not overwritten by propagation")
	})

	t.Run("discovered sibling with no fileMatchInfo is skipped", func(t *testing.T) {
		// A discovered sibling that has no fileMatchInfo entry hits the
		// !ok branch (line 77: !ok || fmi.MovieID == "").
		submitted := []string{"/d/ABC-001-pt1.mp4"}
		manualInputs := map[string]string{"/d/ABC-001-pt1.mp4": "IPX-999"}
		fileMatchInfo := map[string]models.FileMatchInfo{
			"/d/ABC-001-pt1.mp4": fmiFor("/d/ABC-001-pt1.mp4", "ABC-001", 1),
			// pt2 has no fileMatchInfo entry
		}
		allFiles := []string{"/d/ABC-001-pt1.mp4", "/d/ABC-001-pt2.mp4"}

		resolved := resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)
		result := resolved.overrides

		_, hasSibling := result["/d/ABC-001-pt2.mp4"]
		assert.False(t, hasSibling, "sibling with no fileMatchInfo is not propagated")
	})

	t.Run("manual input with whitespace-only string is skipped", func(t *testing.T) {
		// A manual input that is all whitespace hits the trimmed == ""
		// branch (line 94).
		submitted := []string{"/d/ABC-001-pt1.mp4"}
		manualInputs := map[string]string{"/d/ABC-001-pt1.mp4": "   "}
		fileMatchInfo := map[string]models.FileMatchInfo{
			"/d/ABC-001-pt1.mp4": fmiFor("/d/ABC-001-pt1.mp4", "ABC-001", 1),
		}
		allFiles := submitted

		resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)

		assert.Equal(t, "ABC-001", fileMatchInfo["/d/ABC-001-pt1.mp4"].MovieID, "whitespace-only input does not override MovieID")
		assert.True(t, fileMatchInfo["/d/ABC-001-pt1.mp4"].IsMultiPart, "multipart metadata preserved for whitespace-only input")
	})
}

func TestResolveManualInputOverride_NormalizesURLKeysForAmbiguity(t *testing.T) {
	// Two files in the same matcher group given URLs that share a path but
	// differ in query token. With raw-input keying the inputs are distinct,
	// so the group IS ambiguous: each file has a unique input and both lose
	// multipart metadata. The grouping key (MovieID) is still the redacted
	// URL, so no token leaks.
	submitted := []string{"/d/ABC-001-pt1.mp4", "/d/ABC-001-pt2.mp4"}
	manualInputs := map[string]string{
		"/d/ABC-001-pt1.mp4": "https://example.com/video?token=secret1",
		"/d/ABC-001-pt2.mp4": "https://example.com/video?token=secret2",
	}
	fileMatchInfo := map[string]models.FileMatchInfo{
		"/d/ABC-001-pt1.mp4": fmiFor("/d/ABC-001-pt1.mp4", "ABC-001", 1),
		"/d/ABC-001-pt2.mp4": fmiFor("/d/ABC-001-pt2.mp4", "ABC-001", 2),
	}
	allFiles := submitted

	resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)

	// Distinct raw inputs → ambiguous group → both files lose multipart metadata.
	assert.False(t, fileMatchInfo["/d/ABC-001-pt1.mp4"].IsMultiPart, "pt1 loses multipart (distinct raw URL from pt2)")
	assert.False(t, fileMatchInfo["/d/ABC-001-pt2.mp4"].IsMultiPart, "pt2 loses multipart (distinct raw URL from pt1)")
	assert.Equal(t, "https://example.com/video", fileMatchInfo["/d/ABC-001-pt1.mp4"].MovieID, "pt1 MovieID is redacted URL")
	assert.Equal(t, "https://example.com/video", fileMatchInfo["/d/ABC-001-pt2.mp4"].MovieID, "pt2 MovieID is redacted URL")
	assert.Equal(t, 0, fileMatchInfo["/d/ABC-001-pt1.mp4"].PartNumber, "pt1 PartNumber reset (split)")
	assert.Equal(t, 0, fileMatchInfo["/d/ABC-001-pt2.mp4"].PartNumber, "pt2 PartNumber reset (split)")
}

func TestResolveManualInputOverride_QueryBasedURLsSplitCorrectly(t *testing.T) {
	// Two files in the same matcher group use manual URLs whose movie ID
	// lives in the query string. Redaction would collapse them to the same
	// URL, but they are distinct inputs and should split.
	submitted := []string{"/d/ABC-001-pt1.mp4", "/d/ABC-001-pt2.mp4"}
	manualInputs := map[string]string{
		"/d/ABC-001-pt1.mp4": "https://www.javlibrary.com/vl_searchbyid.php?keyword=IPX-111",
		"/d/ABC-001-pt2.mp4": "https://www.javlibrary.com/vl_searchbyid.php?keyword=IPX-222",
	}
	fileMatchInfo := map[string]models.FileMatchInfo{
		"/d/ABC-001-pt1.mp4": fmiFor("/d/ABC-001-pt1.mp4", "ABC-001", 1),
		"/d/ABC-001-pt2.mp4": fmiFor("/d/ABC-001-pt2.mp4", "ABC-001", 2),
	}
	allFiles := submitted

	resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)

	// Both redact to the same URL, but the raw inputs are distinct, so the
	// group IS ambiguous and both files should lose multipart metadata.
	assert.False(t, fileMatchInfo["/d/ABC-001-pt1.mp4"].IsMultiPart, "pt1 loses multipart (distinct raw URL from pt2)")
	assert.False(t, fileMatchInfo["/d/ABC-001-pt2.mp4"].IsMultiPart, "pt2 loses multipart (distinct raw URL from pt1)")
}

func TestResolveManualInputOverride_DeterministicURLPropagation(t *testing.T) {
	// Two submitted files in the same matcher group share the same manual URL,
	// so the group is non-ambiguous. A discovered sibling should deterministically
	// receive that shared raw URL regardless of map iteration order.
	submitted := []string{"/d/ABC-001-pt1.mp4", "/d/ABC-001-pt2.mp4"}
	manualInputs := map[string]string{
		"/d/ABC-001-pt1.mp4": "https://example.com/video?token=aaaa",
		"/d/ABC-001-pt2.mp4": "https://example.com/video?token=aaaa",
	}
	fileMatchInfo := map[string]models.FileMatchInfo{
		"/d/ABC-001-pt1.mp4": fmiFor("/d/ABC-001-pt1.mp4", "ABC-001", 1),
		"/d/ABC-001-pt2.mp4": fmiFor("/d/ABC-001-pt2.mp4", "ABC-001", 2),
		"/d/ABC-001-pt3.mp4": fmiFor("/d/ABC-001-pt3.mp4", "ABC-001", 3),
	}
	allFiles := []string{"/d/ABC-001-pt1.mp4", "/d/ABC-001-pt2.mp4", "/d/ABC-001-pt3.mp4"}

	// Run multiple times to verify determinism (map iteration order varies)
	for i := 0; i < 50; i++ {
		fmiCopy := copyFMI(fileMatchInfo)
		resolved := resolveManualInputOverride(submitted, manualInputs, fmiCopy, allFiles)
		result := resolved.overrides
		// The sibling should always receive the shared raw URL
		assert.Equal(t, "https://example.com/video?token=aaaa", result["/d/ABC-001-pt3.mp4"],
			"sibling gets the shared raw URL (deterministic)")
	}
}

func TestResolveManualInputOverride_ExcludesAmbiguousSiblingFromAllFiles(t *testing.T) {
	// pt1→IPX-111, pt2→IPX-222 (ambiguous), pt3 is auto-discovered.
	// pt3 should be excluded from allFiles since the group is ambiguous.
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

	resolved := resolveManualInputOverride(submitted, manualInputs, fileMatchInfo, allFiles)

	assert.Contains(t, resolved.allFiles, "/d/ABC-001-pt1.mp4", "submitted pt1 included")
	assert.Contains(t, resolved.allFiles, "/d/ABC-001-pt2.mp4", "submitted pt2 included")
	assert.NotContains(t, resolved.allFiles, "/d/ABC-001-pt3.mp4", "discovered pt3 excluded from ambiguous group")
}
