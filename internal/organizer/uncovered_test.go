package organizer

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errEngine always returns an error from Execute.
type errEngine struct{}

func (e *errEngine) Execute(_ string, _ *template.Context) (string, error) {
	return "", errors.New("template execution failed")
}
func (e *errEngine) ExecuteWithContext(_ context.Context, _ string, _ *template.Context) (string, error) {
	return "", errors.New("template execution failed")
}
func (e *errEngine) ExecuteWithMaxBytes(_ string, _ *template.Context, _ int) (string, error) {
	return "", errors.New("template execution failed")
}
func (e *errEngine) TruncateTitle(title string, _ int) string      { return title }
func (e *errEngine) TruncateTitleBytes(title string, _ int) string { return title }
func (e *errEngine) ValidatePathLength(_ string, _ int) error      { return nil }

// --- resolveBaseFileName: uncovered fallback branches ---

func TestResolveBaseFileName_NoRenameEmptyNameWithPathFallback(t *testing.T) {
	cfg := &Config{RenameFile: false}
	engine := template.NewEngine()

	match := models.FileMatchInfo{
		Name:      ".mp4",
		Extension: ".mp4",
		MovieID:   "ABC-123",
		Path:      "/input/ABC-123.mp4",
	}
	result := resolveBaseFileName(cfg, engine, &models.Movie{ID: "ABC-123"}, match)
	assert.Equal(t, "ABC-123", result)
}

func TestResolveBaseFileName_NoRenameEmptyNameNoPathWithMovieID(t *testing.T) {
	cfg := &Config{RenameFile: false}
	engine := template.NewEngine()

	match := models.FileMatchInfo{
		Name:      ".mp4",
		Extension: ".mp4",
		MovieID:   "ABC-123",
		Path:      "",
	}
	result := resolveBaseFileName(cfg, engine, &models.Movie{ID: "ABC-123"}, match)
	assert.Equal(t, "ABC-123", result)
}

func TestResolveBaseFileName_NoRenameAllEmpty_ReturnsFile(t *testing.T) {
	cfg := &Config{RenameFile: false}
	engine := template.NewEngine()

	match := models.FileMatchInfo{
		Name:      ".mp4",
		Extension: ".mp4",
		MovieID:   "",
		Path:      "",
	}
	result := resolveBaseFileName(cfg, engine, &models.Movie{ID: ""}, match)
	assert.Equal(t, "file", result)
}

func TestResolveBaseFileName_RenameTemplateFailsFallbackToName(t *testing.T) {
	cfg := &Config{RenameFile: true, FileFormat: "<ID>"}
	engine := &errEngine{}

	match := models.FileMatchInfo{
		Name:      "mymovie.mp4",
		Extension: ".mp4",
		MovieID:   "",
	}
	// Template fails, MovieID is empty, falls back to name
	result := resolveBaseFileName(cfg, engine, &models.Movie{ID: ""}, match)
	assert.Equal(t, "mymovie", result)
}

func TestResolveBaseFileName_RenameTemplateFailsFallbackToPath(t *testing.T) {
	cfg := &Config{RenameFile: true, FileFormat: "<ID>"}
	engine := &errEngine{}

	match := models.FileMatchInfo{
		Name:      ".mp4",
		Extension: ".mp4",
		MovieID:   "",
		Path:      "/input/mymovie.mp4",
	}
	result := resolveBaseFileName(cfg, engine, &models.Movie{ID: ""}, match)
	assert.Equal(t, "mymovie", result)
}

func TestResolveBaseFileName_RenameAllEmpty_ReturnsFile(t *testing.T) {
	cfg := &Config{RenameFile: true, FileFormat: "<ID>"}
	engine := &errEngine{}

	match := models.FileMatchInfo{
		Name:      ".mp4",
		Extension: ".mp4",
		MovieID:   "",
		Path:      "",
	}
	result := resolveBaseFileName(cfg, engine, &models.Movie{ID: ""}, match)
	assert.Equal(t, "file", result)
}

// --- subtitleFileInfo: uncovered InPlace branch ---

func TestSubtitleFileInfo_InPlaceDifferentFileName(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{OperationMode: operationmode.OperationModeOrganize}
	o := NewOrganizer(fs, cfg, nil, nil)

	plan := &OrganizePlan{
		Match: models.FileMatchInfo{
			Path:      "/source/old-name.mp4",
			Name:      "old-name.mp4",
			Extension: ".mp4",
		},
		TargetDir:  "/source/NewFolder",
		TargetFile: "ABC-123.mp4",
		InPlace:    true,
	}
	info := o.subtitleFileInfo(plan)
	// InPlace + different filename: path = filepath.Join(TargetDir, oldFileName)
	assert.Equal(t, filepath.Join("/source/NewFolder", "old-name.mp4"), info.Path)
}

func TestSubtitleFileInfo_InPlaceSameFileName(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{OperationMode: operationmode.OperationModeOrganize}
	o := NewOrganizer(fs, cfg, nil, nil)

	plan := &OrganizePlan{
		Match: models.FileMatchInfo{
			Path:      "/source/ABC-123.mp4",
			Name:      "ABC-123.mp4",
			Extension: ".mp4",
		},
		TargetPath: "/source/NewFolder/ABC-123.mp4",
		TargetDir:  "/source/NewFolder",
		TargetFile: "ABC-123.mp4",
		InPlace:    true,
	}
	info := o.subtitleFileInfo(plan)
	// InPlace + same filename: path stays as TargetPath
	assert.Equal(t, "/source/NewFolder/ABC-123.mp4", info.Path)
}

func TestSubtitleFileInfo_InPlaceEmptyNameWithPathFallback(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{OperationMode: operationmode.OperationModeOrganize}
	o := NewOrganizer(fs, cfg, nil, nil)

	plan := &OrganizePlan{
		Match: models.FileMatchInfo{
			Path:      "/source/old-name.mp4",
			Name:      "",
			Extension: ".mp4",
		},
		TargetDir:  "/source/NewFolder",
		TargetFile: "ABC-123.mp4",
		InPlace:    true,
	}
	info := o.subtitleFileInfo(plan)
	// Name empty, fallback to filepath.Base(Path), then "old-name.mp4" != "ABC-123.mp4"
	assert.Equal(t, filepath.Join("/source/NewFolder", "old-name.mp4"), info.Path)
}

func TestSubtitleFileInfo_NotInPlace(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{OperationMode: operationmode.OperationModeOrganize}
	o := NewOrganizer(fs, cfg, nil, nil)

	plan := &OrganizePlan{
		Match: models.FileMatchInfo{
			Path:      "/source/old-name.mp4",
			Name:      "old-name.mp4",
			Extension: ".mp4",
		},
		TargetDir:  "/dest/ABC-123",
		TargetFile: "ABC-123.mp4",
		InPlace:    false,
	}
	info := o.subtitleFileInfo(plan)
	assert.Equal(t, "/source/old-name.mp4", info.Path)
	assert.Equal(t, "old-name.mp4", info.Name)
}

// --- metadataArtworkStrategy.Plan: empty Name with Path set ---

func TestMetadataArtworkStrategy_Plan_EmptyNameWithPath(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{RenameFile: false}
	strategy := newMetadataArtworkStrategy(fs, cfg)

	match := models.FileMatchInfo{
		MovieID:   "ABC-123",
		Path:      "/source/ABC-123.mp4",
		Name:      "",
		Extension: ".mp4",
	}
	movie := &models.Movie{ID: "ABC-123"}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.Equal(t, "ABC-123.mp4", plan.TargetFile)
	assert.Equal(t, filepath.ToSlash("/source"), filepath.ToSlash(plan.TargetDir))
}

// --- strategy_organize: newOrganizeStrategy with nil engine/linker ---

func TestNewOrganizeStrategy_NilEngineAndLinker(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}
	strategy := newOrganizeStrategy(fs, cfg, nil, nil)
	require.NotNil(t, strategy)
	require.NotNil(t, strategy.templateEngine)
	require.NotNil(t, strategy.linker)
}

// --- strategy_organize.Execute: copy/link path (moveFiles=false) ---

func TestOrganizeStrategy_Execute_CopyPath(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}
	ml := &mockLinker{}
	strategy := newOrganizeStrategy(fs, cfg, nil, ml)

	require.NoError(t, afero.WriteFile(fs, "/source/ABC-123.mp4", []byte("video"), 0644))

	plan := &OrganizePlan{
		SourcePath: "/source/ABC-123.mp4",
		TargetDir:  "/dest/ABC-123",
		TargetPath: "/dest/ABC-123/ABC-123.mp4",
		TargetFile: "ABC-123.mp4",
		WillMove:   true,
		moveFiles:  false,
		LinkMode:   LinkModeNone,
		Conflicts:  []string{},
	}

	result, err := strategy.Execute(plan)
	require.NoError(t, err)
	assert.True(t, result.Moved)
	assert.True(t, ml.copyCalled)
}

func TestOrganizeStrategy_Execute_InvalidLinkMode(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}
	ml := &mockLinker{}
	strategy := newOrganizeStrategy(fs, cfg, nil, ml)

	require.NoError(t, afero.WriteFile(fs, "/source/ABC-123.mp4", []byte("video"), 0644))

	plan := &OrganizePlan{
		SourcePath: "/source/ABC-123.mp4",
		TargetDir:  "/dest/ABC-123",
		TargetPath: "/dest/ABC-123/ABC-123.mp4",
		TargetFile: "ABC-123.mp4",
		WillMove:   true,
		moveFiles:  false,
		LinkMode:   LinkMode("invalid"), // invalid
		Conflicts:  []string{},
	}

	result, err := strategy.Execute(plan)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported link mode")
	_ = result
}

func TestOrganizeStrategy_Execute_CopyPathWithConflicts(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}
	ml := &mockLinker{}
	strategy := newOrganizeStrategy(fs, cfg, nil, ml)

	plan := &OrganizePlan{
		SourcePath: "/source/ABC-123.mp4",
		TargetDir:  "/dest/ABC-123",
		TargetPath: "/dest/ABC-123/ABC-123.mp4",
		TargetFile: "ABC-123.mp4",
		WillMove:   true,
		moveFiles:  false,
		LinkMode:   LinkModeNone,
		Conflicts:  []string{"/dest/ABC-123/ABC-123.mp4"},
	}

	result, err := strategy.Execute(plan)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "conflicts detected")
	_ = result
}

// --- strategy_inplace.Execute: error branches (unique test names) ---

func TestInPlaceStrategy_Execute_OldDirIsFileNotDir(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{OperationMode: operationmode.OperationModeInPlace, FileFormat: "<ID>", FolderFormat: "<ID>", RenameFile: true}
	m := &stubMatcherForOrgMiss{result: "ABC-123"}
	strategy := newInPlaceStrategy(fs, cfg, m, nil)

	// Create a file where a directory is expected
	require.NoError(t, afero.WriteFile(fs, "/source/notadir", []byte("data"), 0644))

	plan := &OrganizePlan{
		Match: models.FileMatchInfo{
			Path: "/source/notadir/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4", MovieID: "ABC-123",
		},
		SourcePath: "/source/notadir/ABC-123.mp4",
		TargetDir:  "/source/NewName",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/source/NewName/ABC-123.mp4",
		OldDir:     "/source/notadir",
		InPlace:    true,
		WillMove:   true,
	}

	result, err := strategy.Execute(plan)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
	_ = result
}

func TestInPlaceStrategy_Execute_SuccessfulInPlaceRename(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{OperationMode: operationmode.OperationModeInPlace, FileFormat: "<ID>", FolderFormat: "<ID>", RenameFile: true}
	m := &stubMatcherForOrgMiss{result: "ABC-123"}
	strategy := newInPlaceStrategy(fs, cfg, m, nil)

	// Create old directory with a file
	require.NoError(t, fs.MkdirAll("/source/OldDir", 0755))
	require.NoError(t, afero.WriteFile(fs, "/source/OldDir/ABC-123.mp4", []byte("video"), 0644))

	plan := &OrganizePlan{
		Match: models.FileMatchInfo{
			Path: "/source/OldDir/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4", MovieID: "ABC-123",
		},
		SourcePath: "/source/OldDir/ABC-123.mp4",
		TargetDir:  "/source/NewName",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/source/NewName/ABC-123.mp4",
		OldDir:     "/source/OldDir",
		InPlace:    true,
		WillMove:   true,
	}

	result, err := strategy.Execute(plan)
	require.NoError(t, err)
	assert.True(t, result.Moved)
	assert.True(t, result.InPlaceRenamed)
	assert.Equal(t, "/source/OldDir", result.OldDirectoryPath)
	assert.Equal(t, "/source/NewName", result.NewDirectoryPath)
}

func TestInPlaceStrategy_Execute_InPlaceRenameThenFileRename(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{OperationMode: operationmode.OperationModeInPlace, FileFormat: "<ID>", FolderFormat: "<ID>", RenameFile: true}
	m := &stubMatcherForOrgMiss{result: "ABC-123"}
	strategy := newInPlaceStrategy(fs, cfg, m, nil)

	// Create old directory with a file that needs renaming
	require.NoError(t, fs.MkdirAll("/source/OldDir", 0755))
	require.NoError(t, afero.WriteFile(fs, "/source/OldDir/original.mp4", []byte("video"), 0644))

	plan := &OrganizePlan{
		Match: models.FileMatchInfo{
			Path: "/source/OldDir/original.mp4", Name: "original.mp4", Extension: ".mp4", MovieID: "ABC-123",
		},
		SourcePath: "/source/OldDir/original.mp4",
		TargetDir:  "/source/NewName",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/source/NewName/ABC-123.mp4",
		OldDir:     "/source/OldDir",
		InPlace:    true,
		WillMove:   true,
	}

	result, err := strategy.Execute(plan)
	require.NoError(t, err)
	assert.True(t, result.Moved)
	assert.True(t, result.InPlaceRenamed)

	// Verify the file was renamed inside the new directory
	exists, err := afero.Exists(fs, "/source/NewName/ABC-123.mp4")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestInPlaceStrategy_Execute_InPlaceEmptyNameFallback(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{OperationMode: operationmode.OperationModeInPlace, FileFormat: "<ID>", FolderFormat: "<ID>", RenameFile: true}
	m := &stubMatcherForOrgMiss{result: "ABC-123"}
	strategy := newInPlaceStrategy(fs, cfg, m, nil)

	// Create old directory with a file — match.Name is empty, should fallback to filepath.Base
	require.NoError(t, fs.MkdirAll("/source/OldDir", 0755))
	require.NoError(t, afero.WriteFile(fs, "/source/OldDir/original.mp4", []byte("video"), 0644))

	plan := &OrganizePlan{
		Match: models.FileMatchInfo{
			Path: "/source/OldDir/original.mp4", Name: "", Extension: ".mp4", MovieID: "ABC-123",
		},
		SourcePath: "/source/OldDir/original.mp4",
		TargetDir:  "/source/NewName",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/source/NewName/ABC-123.mp4",
		OldDir:     "/source/OldDir",
		InPlace:    true,
		WillMove:   true,
	}

	result, err := strategy.Execute(plan)
	require.NoError(t, err)
	assert.True(t, result.Moved)
}

func TestInPlaceStrategy_Execute_NonInPlaceMove(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{OperationMode: operationmode.OperationModeInPlace, FileFormat: "<ID>", FolderFormat: "<ID>", RenameFile: true}
	m := &stubMatcherForOrgMiss{result: "ABC-123"}
	strategy := newInPlaceStrategy(fs, cfg, m, nil)

	require.NoError(t, fs.MkdirAll("/source", 0755))
	require.NoError(t, afero.WriteFile(fs, "/source/ABC-123.mp4", []byte("video"), 0644))

	plan := &OrganizePlan{
		Match: models.FileMatchInfo{
			Path: "/source/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4", MovieID: "ABC-123",
		},
		SourcePath: "/source/ABC-123.mp4",
		TargetDir:  "/dest/ABC-123",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/dest/ABC-123/ABC-123.mp4",
		InPlace:    false,
		WillMove:   true,
	}

	result, err := strategy.Execute(plan)
	require.NoError(t, err)
	assert.True(t, result.Moved)
}

// --- strategy_inplace_norenamefolder Plan: max path length truncation ---

func TestInPlaceNoRenameFolder_Plan_MaxPathLengthTruncation(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		OperationMode: operationmode.OperationModeInPlaceNoRenameFolder,
		FileFormat:    "<ID> <TITLE>",
		FolderFormat:  "<ID>",
		RenameFile:    true,
		MaxPathLength: 50,
	}
	engine := template.NewEngine()
	m := &stubMatcherForOrgMiss{result: "ABC-123"}
	strategy := newInPlaceNoRenameFolderStrategy(fs, cfg, m, engine)

	longPath := "/source/dir/ABC-123-very-long-name-that-exceeds-max-path-length-and-needs-truncation.mp4"
	match := models.FileMatchInfo{
		Path:      longPath,
		Name:      "ABC-123-very-long-name-that-exceeds-max-path-length-and-needs-truncation.mp4",
		Extension: ".mp4",
		MovieID:   "ABC-123",
	}
	movie := &models.Movie{ID: "ABC-123", Title: "Some Title"}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	// May or may not error depending on whether path can be truncated enough
	if err != nil {
		assert.Contains(t, err.Error(), "path validation")
	} else {
		assert.NotNil(t, plan)
	}
}

// --- Organize: copy/link path for subtitles ---

func TestOrganizer_Organize_CopyPath_WithSubtitles(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		OperationMode:      operationmode.OperationModeOrganize,
		FileFormat:         "<ID>",
		FolderFormat:       "<ID>",
		RenameFile:         true,
		MoveSubtitles:      true,
		SubtitleExtensions: []string{".srt", ".ass"},
	}
	m := &stubMatcherForOrgMiss{result: "ABC-123"}
	o := NewOrganizer(fs, cfg, nil, m)

	require.NoError(t, fs.MkdirAll("/source", 0755))
	require.NoError(t, afero.WriteFile(fs, "/source/ABC-123.mp4", []byte("video"), 0644))
	require.NoError(t, afero.WriteFile(fs, "/source/ABC-123.srt", []byte("subtitle"), 0644))

	result, err := o.Organize(context.Background(), OrganizeCmd{
		Match: models.FileMatchInfo{
			Path: "/source/ABC-123.mp4", Name: "ABC-123.mp4",
			Extension: ".mp4", MovieID: "ABC-123",
		},
		Movie:     &models.Movie{ID: "ABC-123"},
		DestDir:   "/dest",
		MoveFiles: false,
		LinkMode:  LinkModeNone,
	})
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// --- joinPathUNC: backslash path with trailing separator on elem ---

func TestJoinPathUNC_WindowsTrailingSeparatorOnElem(t *testing.T) {
	result := joinPathUNC(`C:\base`, `sub\`)
	assert.Equal(t, `C:\base\sub`, result)
}

func TestJoinPathUNC_WindowsEmptyElem(t *testing.T) {
	result := joinPathUNC(`C:\base`, ``, `sub`)
	assert.Equal(t, `C:\base\sub`, result)
}

// --- pathDir: UNC path where dir would be shorter than share root ---

func TestPathDir_UNCShareRootProtection(t *testing.T) {
	// UNC path where trimming would go below share root
	result := pathDir(`\\server\share`)
	assert.Equal(t, `\\server\share`, result)
}

// --- strategy_organize.Plan: subfolder format ---

func TestOrganizeStrategy_Plan_WithSubfolderFormat(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		OperationMode:   operationmode.OperationModeOrganize,
		FileFormat:      "<ID>",
		FolderFormat:    "<ID>",
		SubfolderFormat: []string{"<LABEL>"},
		RenameFile:      true,
	}
	engine := template.NewEngine()
	strategy := newOrganizeStrategy(fs, cfg, engine, nil)

	match := models.FileMatchInfo{
		Path: "/source/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4", MovieID: "ABC-123",
	}
	movie := &models.Movie{ID: "ABC-123", Label: "StudioA"}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.Contains(t, plan.TargetDir, "StudioA")
	assert.NotEmpty(t, plan.SubfolderPath)
}
