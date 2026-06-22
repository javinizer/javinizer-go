package history

import (
	"encoding/json"
	"testing"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func determineOperationType(moveFiles bool, linkMode organizer.LinkMode, isUpdateMode bool) models.OperationTypeEnum {
	if isUpdateMode {
		return models.OperationTypeUpdate
	}
	if !moveFiles && linkMode == organizer.LinkModeHard {
		return models.OperationTypeHardlink
	}
	if !moveFiles && linkMode == organizer.LinkModeSoft {
		return models.OperationTypeSymlink
	}
	if !moveFiles {
		return models.OperationTypeCopy
	}
	return models.OperationTypeMove
}

func buildGeneratedFilesJSON(nfoPath string, subtitleMoves []models.SubtitleMove, downloadPaths []string) string {
	gf := models.GeneratedFilesJSON{}

	deleteList := make([]string, 0, 1+len(downloadPaths))
	if nfoPath != "" {
		deleteList = append(deleteList, nfoPath)
	}
	deleteList = append(deleteList, downloadPaths...)
	if len(deleteList) > 0 {
		gf.Delete = deleteList
	}

	if len(subtitleMoves) > 0 {
		moveBackList := make([]models.FileMove, 0, len(subtitleMoves))
		for _, sr := range subtitleMoves {
			if sr.Moved && sr.OriginalPath != "" && sr.NewPath != "" {
				moveBackList = append(moveBackList, models.FileMove{
					OriginalPath: sr.OriginalPath,
					NewPath:      sr.NewPath,
				})
			}
		}
		if len(moveBackList) > 0 {
			gf.MoveBack = moveBackList
		}
	}

	if len(gf.Delete) == 0 && len(gf.MoveBack) == 0 {
		return ""
	}

	data, err := json.Marshal(gf)
	if err != nil {
		logging.Warnf("Failed to marshal models.GeneratedFilesJSON: %v (attempting partial recovery)", err)
		data, err = json.Marshal(models.GeneratedFilesJSON{Delete: gf.Delete})
		if err != nil {
			logging.Warnf("Failed to marshal partial models.GeneratedFilesJSON: %v", err)
			return ""
		}
	}
	return string(data)
}

// --- determineOperationType tests ---

func TestDetermineOperationType_Move(t *testing.T) {
	assert.Equal(t, models.OperationTypeMove, determineOperationType(true, "", false))
	assert.Equal(t, models.OperationTypeCopy, determineOperationType(false, "", false))
	assert.Equal(t, models.OperationTypeHardlink, determineOperationType(false, "hard", false))
	assert.Equal(t, models.OperationTypeSymlink, determineOperationType(false, "soft", false))
	assert.Equal(t, models.OperationTypeUpdate, determineOperationType(false, "", true))
	assert.Equal(t, models.OperationTypeUpdate, determineOperationType(true, "", true))
}

// --- buildGeneratedFilesJSON tests ---

func TestBuildGeneratedFilesJSON_WithNFOAndDownloadsAndSubtitles(t *testing.T) {
	nfoPath := "/dst/ABC-123.nfo"
	downloadPaths := []string{"/dst/poster.jpg", "/dst/fanart.jpg"}
	subtitles := []models.SubtitleMove{
		{OriginalPath: "/src/sub.srt", NewPath: "/dst/sub.srt", Moved: true},
	}

	result := buildGeneratedFilesJSON(nfoPath, subtitles, downloadPaths)
	assert.NotEmpty(t, result, "should produce JSON when files are present")

	var gf models.GeneratedFilesJSON
	err := json.Unmarshal([]byte(result), &gf)
	require.NoError(t, err)

	assert.Equal(t, []string{"/dst/ABC-123.nfo", "/dst/poster.jpg", "/dst/fanart.jpg"}, gf.Delete)
	assert.Len(t, gf.MoveBack, 1)
	assert.Equal(t, "/src/sub.srt", gf.MoveBack[0].OriginalPath)
	assert.Equal(t, "/dst/sub.srt", gf.MoveBack[0].NewPath)
}

func TestBuildGeneratedFilesJSON_EmptyReturnsEmptyString(t *testing.T) {
	result := buildGeneratedFilesJSON("", nil, nil)
	assert.Equal(t, "", result)
}
