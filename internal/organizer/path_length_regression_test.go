package organizer

import (
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/matcher"
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

func TestPathLengthRegression_InplaceEmptyMovieIDFallback(t *testing.T) {
	fs := afero.NewMemMapFs()
	engine := template.NewEngine()
	m, _ := matcher.NewMatcher(&matcher.Config{})
	cfg := &Config{
		FolderFormat:      "<TITLE>",
		FileFormat:        "<ID>.mp4",
		MaxPathLength:     240,
		OperationMode:     operationmode.OperationModeInPlace,
		RenameFile:        true,
		MediaFormatConfig: MediaFormatConfig{},
	}

	sourceDir := "/source"
	sourceFile := sourceDir + "/***.mp4"
	_ = fs.MkdirAll(sourceDir, 0755)
	_ = afero.WriteFile(fs, sourceFile, []byte("test"), 0644)

	match := models.FileMatchInfo{Path: sourceFile, MovieID: "***", Name: "***.mp4"}
	movie := &models.Movie{ID: "***", Title: ""}

	strategy := newInPlaceStrategy(fs, cfg, m, engine)
	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.Equal(t, "unknown", plan.FolderName, "should fall back to 'unknown' when movie ID sanitizes to empty")
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
		wantErr  bool
	}{
		{
			name:     "title budget exhausted",
			tmpl:     "<ID> - <TITLE> (<YEAR>)",
			ctx:      &template.Context{ID: "ABC-123", Title: "T", ReleaseYear: 2024},
			maxBytes: 5,
			wantErr:  true,
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
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.LessOrEqual(t, len(got), tc.maxBytes,
				"ExecuteWithMaxBytes result (%d bytes) must not exceed maxBytes (%d): got %q",
				len(got), tc.maxBytes, got)
		})
	}
}

func TestPathLengthRegression_InplaceOrganizeFallbackUsesDestinationBudget(t *testing.T) {
	testCases := []struct {
		name      string
		sourceDir string
		destDir   string
	}{
		{
			name:      "long destination truncates folder",
			sourceDir: "/source/mixed",
			destDir:   "/destination/with/a/very/long/path/that/requires/the/generated/folder/title/to/be/truncated",
		},
		{
			name:      "long source parent does not consume destination budget",
			sourceDir: "/source/with/a/very/long/path/that/would/exceed/the/configured/limit/if/used/for/the/fallback/budget/mixed",
			destDir:   "/dest",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			engine := template.NewEngine()
			m, err := matcher.NewMatcher(&matcher.Config{})
			require.NoError(t, err)
			cfg := &Config{
				FolderFormat:  "<ID> - <TITLE>",
				FileFormat:    "<ID>.mp4",
				MaxPathLength: 120,
				OperationMode: operationmode.OperationModeOrganize,
				RenameFile:    true,
			}
			sourceFile := tc.sourceDir + "/ABC-001.mp4"
			require.NoError(t, fs.MkdirAll(tc.sourceDir, 0755))
			require.NoError(t, afero.WriteFile(fs, sourceFile, []byte("test"), 0644))
			require.NoError(t, afero.WriteFile(fs, tc.sourceDir+"/XYZ-002.mp4", []byte("test"), 0644))

			match := models.FileMatchInfo{Path: sourceFile, MovieID: "ABC-001", Name: "ABC-001.mp4"}
			movie := &models.Movie{ID: "ABC-001", Title: "A Very Long Movie Title That Must Be Truncated To Fit The Destination Path Budget"}
			plan, err := newInPlaceStrategy(fs, cfg, m, engine).Plan(match, movie, tc.destDir, false)

			require.NoError(t, err)
			assert.False(t, plan.InPlace)
			assert.LessOrEqual(t, len(plan.TargetPath), cfg.MaxPathLength)
			assert.Equal(t, filepath.Clean(tc.destDir), filepath.Dir(plan.TargetDir))
		})
	}
}
