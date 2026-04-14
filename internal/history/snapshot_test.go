package history

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ReadNFOSnapshot tests ---

func TestReadNFOSnapshot_ReadsExistingNFO(t *testing.T) {
	fs := afero.NewMemMapFs()
	sourceDir := "/src/ABC-123"
	require.NoError(t, fs.MkdirAll(sourceDir, 0777))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(sourceDir, "ABC-123.nfo"), []byte("<nfo>content</nfo>"), 0666))

	result := ReadNFOSnapshot(fs, filepath.Join(sourceDir, "ABC-123.nfo"))
	assert.Equal(t, "<nfo>content</nfo>", result.Content)
	assert.NotEmpty(t, result.FoundPath)
}

func TestReadNFOSnapshot_TriesMultiplePaths(t *testing.T) {
	fs := afero.NewMemMapFs()
	sourceDir := "/src/ABC-123"
	require.NoError(t, fs.MkdirAll(sourceDir, 0777))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(sourceDir, "ABC-123 - Title.nfo"), []byte("<nfo>custom</nfo>"), 0666))

	result := ReadNFOSnapshot(fs,
		filepath.Join(sourceDir, "ABC-123 - Title.nfo"),
		filepath.Join(sourceDir, "ABC-123.nfo"),
	)
	assert.Equal(t, "<nfo>custom</nfo>", result.Content)
	assert.Contains(t, result.FoundPath, "ABC-123 - Title.nfo")
}

func TestReadNFOSnapshot_ReturnsEmptyWhenNoNFO(t *testing.T) {
	fs := afero.NewMemMapFs()
	sourceDir := "/src/ABC-123"
	require.NoError(t, fs.MkdirAll(sourceDir, 0777))

	result := ReadNFOSnapshot(fs, filepath.Join(sourceDir, "ABC-123.nfo"))
	assert.Equal(t, "", result.Content)
	assert.Equal(t, "", result.FoundPath)
}

func TestReadNFOSnapshot_FoundPathIsCanonical(t *testing.T) {
	fs := afero.NewMemMapFs()
	sourceDir := "/src/ABC-123"
	require.NoError(t, fs.MkdirAll(sourceDir, 0777))
	nfoPath := filepath.Join(sourceDir, "ABC-123.nfo")
	require.NoError(t, afero.WriteFile(fs, nfoPath, []byte("<nfo/>"), 0666))

	result := ReadNFOSnapshot(fs, nfoPath)
	assert.Equal(t, "<nfo/>", result.Content)
	canonical, _ := filepath.Abs(filepath.Clean(nfoPath))
	assert.Equal(t, canonical, result.FoundPath)
}

// --- DetermineOperationType tests ---

func TestDetermineOperationType_Move(t *testing.T) {
	assert.Equal(t, models.OperationTypeMove, DetermineOperationType(true, organizer.LinkModeNone, false))
}

func TestDetermineOperationType_Copy(t *testing.T) {
	assert.Equal(t, models.OperationTypeCopy, DetermineOperationType(false, organizer.LinkModeNone, false))
}

func TestDetermineOperationType_Hardlink(t *testing.T) {
	assert.Equal(t, models.OperationTypeHardlink, DetermineOperationType(false, organizer.LinkModeHard, false))
}

func TestDetermineOperationType_Symlink(t *testing.T) {
	assert.Equal(t, models.OperationTypeSymlink, DetermineOperationType(false, organizer.LinkModeSoft, false))
}

func TestDetermineOperationType_Update(t *testing.T) {
	assert.Equal(t, models.OperationTypeUpdate, DetermineOperationType(false, organizer.LinkModeNone, true))
	assert.Equal(t, models.OperationTypeUpdate, DetermineOperationType(true, organizer.LinkModeNone, true))
}

// --- NewPreOrganizeRecord tests ---

func TestNewPreOrganizeRecord_CreatesRecordWithAllFields(t *testing.T) {
	record := NewPreOrganizeRecord(
		"batch-1", "ABC-123", "/src/file.mp4",
		"<nfo>snapshot</nfo>", "/dst/ABC-123.nfo", "/src", models.OperationTypeMove, false,
	)

	assert.Equal(t, "batch-1", record.BatchJobID)
	assert.Equal(t, "ABC-123", record.MovieID)
	assert.Equal(t, "/src/file.mp4", record.OriginalPath)
	assert.Equal(t, "", record.NewPath) // filled after organize
	assert.Equal(t, models.OperationTypeMove, record.OperationType)
	assert.Equal(t, "<nfo>snapshot</nfo>", record.NFOSnapshot)
	assert.Equal(t, "/dst/ABC-123.nfo", record.NFOPath)
	assert.Equal(t, "", record.GeneratedFiles) // filled after post-organize
	assert.Equal(t, models.RevertStatusApplied, record.RevertStatus)
	assert.Equal(t, false, record.InPlaceRenamed)
	assert.Equal(t, "/src", record.OriginalDirPath)
}

func TestNewPreOrganizeRecord_UpdateMode(t *testing.T) {
	record := NewPreOrganizeRecord(
		"batch-2", "DEF-456", "/src/def.mkv",
		"", "/src/DEF-456.nfo", "/src", models.OperationTypeUpdate, false,
	)

	assert.Equal(t, models.OperationTypeUpdate, record.OperationType)
	assert.Equal(t, "", record.NFOSnapshot)
}

// --- BuildGeneratedFilesJSON tests ---

func TestBuildGeneratedFilesJSON_WithNFOAndDownloadsAndSubtitles(t *testing.T) {
	nfoPath := "/dst/ABC-123.nfo"
	downloadPaths := []string{"/dst/poster.jpg", "/dst/fanart.jpg"}
	subtitles := []organizer.SubtitleResult{
		{OriginalPath: "/src/sub.srt", NewPath: "/dst/sub.srt", Moved: true, Error: nil},
	}

	result := BuildGeneratedFilesJSON(nfoPath, subtitles, downloadPaths)
	assert.NotEmpty(t, result, "should produce JSON when files are present")

	var gf GeneratedFilesJSON
	err := json.Unmarshal([]byte(result), &gf)
	require.NoError(t, err)

	assert.Equal(t, []string{"/dst/ABC-123.nfo", "/dst/poster.jpg", "/dst/fanart.jpg"}, gf.Delete)
	assert.Len(t, gf.MoveBack, 1)
	assert.Equal(t, "/src/sub.srt", gf.MoveBack[0].OriginalPath)
	assert.Equal(t, "/dst/sub.srt", gf.MoveBack[0].NewPath)
}

func TestBuildGeneratedFilesJSON_EmptyReturnsEmptyString(t *testing.T) {
	result := BuildGeneratedFilesJSON("", nil, nil)
	assert.Equal(t, "", result)
}

func TestBuildGeneratedFilesJSON_NFOOnly(t *testing.T) {
	result := BuildGeneratedFilesJSON("/dst/ABC-123.nfo", nil, nil)
	assert.NotEmpty(t, result)

	var gf GeneratedFilesJSON
	err := json.Unmarshal([]byte(result), &gf)
	require.NoError(t, err)

	assert.Equal(t, []string{"/dst/ABC-123.nfo"}, gf.Delete)
	assert.Empty(t, gf.MoveBack)
}

func TestBuildGeneratedFilesJSON_SkipsNonMovedSubtitles(t *testing.T) {
	subtitles := []organizer.SubtitleResult{
		{OriginalPath: "/src/sub1.srt", NewPath: "/dst/sub1.srt", Moved: true},
		{OriginalPath: "/src/sub2.srt", NewPath: "", Moved: false}, // not moved, should be skipped
	}

	result := BuildGeneratedFilesJSON("", subtitles, nil)
	assert.NotEmpty(t, result)

	var gf GeneratedFilesJSON
	err := json.Unmarshal([]byte(result), &gf)
	require.NoError(t, err)

	assert.Empty(t, gf.Delete)
	assert.Len(t, gf.MoveBack, 1) // only the moved subtitle
	assert.Equal(t, "/src/sub1.srt", gf.MoveBack[0].OriginalPath)
}

// --- UpdatePostOrganize tests ---

func TestUpdatePostOrganize_UpdatesAllFields(t *testing.T) {
	op := NewPreOrganizeRecord(
		"batch-1", "ABC-123", "/src/file.mp4",
		"<nfo/>", "/dst/ABC-123.nfo", "/src", models.OperationTypeMove, false,
	)

	UpdatePostOrganize(op, "/dst/file.mp4", false, "/src", `{"delete":["/dst/ABC-123.nfo"]}`)

	assert.Equal(t, "/dst/file.mp4", op.NewPath)
	assert.Equal(t, false, op.InPlaceRenamed)
	assert.Equal(t, "/src", op.OriginalDirPath)
	assert.Equal(t, `{"delete":["/dst/ABC-123.nfo"]}`, op.GeneratedFiles)
	assert.Equal(t, "/dst/ABC-123.nfo", op.NFOPath) // preserved from NewPreOrganizeRecord
}

func TestUpdatePostOrganize_InPlaceRenamed(t *testing.T) {
	op := NewPreOrganizeRecord(
		"batch-1", "ABC-123", "/src/ABC-123/file.mp4",
		"<nfo/>", "/src/ABC-123/ABC-123.nfo", "/src/ABC-123", models.OperationTypeMove, false,
	)

	UpdatePostOrganize(op, "/src/Studio - Title/file.mp4", true, "/src/ABC-123", "")

	assert.Equal(t, "/src/Studio - Title/file.mp4", op.NewPath)
	assert.Equal(t, true, op.InPlaceRenamed)
	assert.Equal(t, "/src/ABC-123", op.OriginalDirPath)
}
