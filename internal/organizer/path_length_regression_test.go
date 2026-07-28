package organizer

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPathLengthRegression_OrganizeStrategy_LongDestDir(t *testing.T) {
	fs := afero.NewMemMapFs()
	engine := template.NewEngine()
	cfg := &Config{
		FolderFormat:      "<ID> - <TITLE>",
		FileFormat:        "<ID>.mp4",
		MaxTitleLength:    100,
		MaxPathLength:     240,
		OperationMode:     operationmode.OperationModeOrganize,
		RenameFile:        true,
		MediaFormatConfig: MediaFormatConfig{},
	}

	longDestDir := "/very/long/destination/directory/path/that/exceeds/the/max/path/length/limit/by/itself/when/combined/with/overhead/and/makes/the/total/exceed/two/hundred/and/forty/bytes/easily/so/that/folderMaxBytes/is/zero/and/then/some/more/path/components/just/to/be/sure"
	sourceFile := "/source/ABC-001.mp4"
	_ = fs.MkdirAll(longDestDir, 0755)
	_ = afero.WriteFile(fs, sourceFile, []byte("test"), 0644)

	match := models.FileMatchInfo{Path: sourceFile, MovieID: "ABC-001", Name: "ABC-001.mp4"}
	movie := &models.Movie{ID: "ABC-001", Title: "Test Movie Title"}

	strategy := newOrganizeStrategy(fs, cfg, engine, &MemLinker{})
	_, err := strategy.Plan(match, movie, longDestDir, false)

	require.Error(t, err, "should error when overhead exceeds max_path_length")
	assert.Contains(t, err.Error(), "overhead", "error should explain the overhead issue")
	assert.Contains(t, err.Error(), "max_path_length", "error should mention max_path_length")
}

func TestPathLengthRegression_InplaceStrategy_LongDir(t *testing.T) {
	fs := afero.NewMemMapFs()
	engine := template.NewEngine()
	cfg := &Config{
		FolderFormat:      "<ID> - <TITLE>",
		FileFormat:        "<ID>.mp4",
		MaxTitleLength:    100,
		MaxPathLength:     240,
		OperationMode:     operationmode.OperationModeInPlace,
		RenameFile:        true,
		MediaFormatConfig: MediaFormatConfig{},
	}

	longSourceDir := "/very/long/source/directory/path/that/exceeds/the/max/path/length/limit/by/itself/when/combined/with/overhead/and/makes/the/total/exceed/two/hundred/and/forty/bytes/easily/so/that/folderMaxBytes/is/zero/and/then/some/more/path/components/just/to/be/sure"
	sourceFile := longSourceDir + "/ABC-001.mp4"
	_ = fs.MkdirAll(longSourceDir, 0755)
	_ = afero.WriteFile(fs, sourceFile, []byte("test"), 0644)

	match := models.FileMatchInfo{Path: sourceFile, MovieID: "ABC-001", Name: "ABC-001.mp4"}
	movie := &models.Movie{ID: "ABC-001", Title: "Test Movie Title"}

	strategy := newInPlaceStrategy(fs, cfg, nil, engine)
	_, err := strategy.Plan(match, movie, "/dest", false)

	require.Error(t, err, "should error when path exceeds max_path_length")
	assert.Contains(t, err.Error(), "path validation failed", "error should mention path validation")
}

func TestPathLengthRegression_InplaceNoRenameFolder_LongPath(t *testing.T) {
	fs := afero.NewMemMapFs()
	engine := template.NewEngine()
	cfg := &Config{
		FolderFormat:      "<ID> - <TITLE>",
		FileFormat:        "<ID>.mp4",
		MaxTitleLength:    100,
		MaxPathLength:     10,
		OperationMode:     operationmode.OperationModeInPlaceNoRenameFolder,
		RenameFile:        true,
		MediaFormatConfig: MediaFormatConfig{},
	}

	sourceFile := "/source/ABC-001-very-long-filename-that-exceeds-the-limit.mp4"
	_ = fs.MkdirAll("/source", 0755)
	_ = afero.WriteFile(fs, sourceFile, []byte("test"), 0644)

	match := models.FileMatchInfo{Path: sourceFile, MovieID: "ABC-001", Name: "ABC-001-very-long-filename-that-exceeds-the-limit.mp4"}
	movie := &models.Movie{ID: "ABC-001", Title: "Test Movie Title"}

	strategy := newInPlaceNoRenameFolderStrategy(fs, cfg, nil, engine)
	plan, err := strategy.Plan(match, movie, "/dest", false)
	if err != nil {
		assert.Contains(t, err.Error(), "path validation failed", "error should mention path validation")
	} else {
		assert.LessOrEqual(t, len(plan.TargetPath), 10, "path should be truncated to fit within max_path_length")
	}
}

func TestPathLengthRegression_ExecuteWithMaxBytes_NeverExceedsLimit(t *testing.T) {
	engine := template.NewEngine()
	testCases := []struct {
		name     string
		tmpl     string
		ctx      *template.Context
		maxBytes int
	}{
		{
			name:     "title budget exhausted",
			tmpl:     "<ID> - <TITLE> (<YEAR>)",
			ctx:      &template.Context{ID: "ABC-123", Title: "T", ReleaseYear: 2024},
			maxBytes: 5,
		},
		{
			name:     "multiple TITLE tags",
			tmpl:     "<ID> - <TITLE> / <TITLE> (<YEAR>)",
			ctx:      &template.Context{ID: "ABC-123", Title: "Very Long Title That Should Be Truncated", ReleaseYear: 2024},
			maxBytes: 50,
		},
		{
			name:     "TITLE and ORIGINALTITLE",
			tmpl:     "<ID> - <TITLE> (<ORIGINALTITLE>) (<YEAR>)",
			ctx:      &template.Context{ID: "ABC-123", Title: "Very Long Title", OriginalTitle: "Very Long Original Title", ReleaseYear: 2024},
			maxBytes: 50,
		},
		{
			name:     "CJK title with small budget",
			tmpl:     "<ID> - <TITLE>",
			ctx:      &template.Context{ID: "ABC", Title: "これは非常に長い日本語のタイトルです"},
			maxBytes: 20,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := engine.ExecuteWithMaxBytes(tc.tmpl, tc.ctx, tc.maxBytes)
			require.NoError(t, err)
			assert.LessOrEqual(t, len(got), tc.maxBytes,
				"ExecuteWithMaxBytes result (%d bytes) must not exceed maxBytes (%d): got %q",
				len(got), tc.maxBytes, got)
		})
	}
}
