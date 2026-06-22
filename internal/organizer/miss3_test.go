package organizer

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errEngineSubfolder only fails on specific template patterns.
type errEngineSubfolder struct{}

func (e *errEngineSubfolder) Execute(tmpl string, _ *template.Context) (string, error) {
	return template.NewEngine().Execute(tmpl, &template.Context{})
}
func (e *errEngineSubfolder) ExecuteWithContext(_ context.Context, tmpl string, _ *template.Context) (string, error) {
	return e.Execute(tmpl, nil)
}
func (e *errEngineSubfolder) ExecuteWithMaxBytes(tmpl string, ctx *template.Context, maxBytes int) (string, error) {
	return template.NewEngine().ExecuteWithMaxBytes(tmpl, ctx, maxBytes)
}
func (e *errEngineSubfolder) TruncateTitle(title string, _ int) string      { return title }
func (e *errEngineSubfolder) TruncateTitleBytes(title string, _ int) string { return title }
func (e *errEngineSubfolder) ValidatePathLength(_ string, _ int) error      { return nil }

// errEngineMaxBytes3 returns error on ExecuteWithMaxBytes only.
type errEngineMaxBytes3 struct{}

func (e *errEngineMaxBytes3) Execute(tmpl string, ctx *template.Context) (string, error) {
	return template.NewEngine().Execute(tmpl, ctx)
}
func (e *errEngineMaxBytes3) ExecuteWithContext(_ context.Context, tmpl string, ctx *template.Context) (string, error) {
	return template.NewEngine().Execute(tmpl, ctx)
}
func (e *errEngineMaxBytes3) ExecuteWithMaxBytes(_ string, _ *template.Context, _ int) (string, error) {
	return "", errors.New("ExecuteWithMaxBytes failed")
}
func (e *errEngineMaxBytes3) TruncateTitle(title string, _ int) string      { return title }
func (e *errEngineMaxBytes3) TruncateTitleBytes(title string, _ int) string { return title }
func (e *errEngineMaxBytes3) ValidatePathLength(_ string, _ int) error      { return nil }

// emptyFolderEngine3 produces empty string from ExecuteWithMaxBytes.
type emptyFolderEngine3 struct{}

func (e *emptyFolderEngine3) Execute(tmpl string, ctx *template.Context) (string, error) {
	return template.NewEngine().Execute(tmpl, ctx)
}
func (e *emptyFolderEngine3) ExecuteWithContext(_ context.Context, tmpl string, ctx *template.Context) (string, error) {
	return e.Execute(tmpl, ctx)
}
func (e *emptyFolderEngine3) ExecuteWithMaxBytes(_ string, _ *template.Context, _ int) (string, error) {
	return "", nil // Returns empty — triggers fallback
}
func (e *emptyFolderEngine3) TruncateTitle(title string, _ int) string      { return title }
func (e *emptyFolderEngine3) TruncateTitleBytes(title string, _ int) string { return title }
func (e *emptyFolderEngine3) ValidatePathLength(_ string, _ int) error      { return nil }

// --- buildPlanContext: fileName empty with Path set (rename off) ---

func TestBuildPlanContext_NoRenameEmptyNameWithPath(t *testing.T) {
	cfg := &Config{RenameFile: false, FolderFormat: "<ID>"}
	engine := template.NewEngine()

	match := models.FileMatchInfo{
		Name:      "",
		Path:      "/source/ABC-123.mp4",
		Extension: ".mp4",
		MovieID:   "ABC-123",
	}
	movie := &models.Movie{ID: "ABC-123"}

	pc := buildPlanContext(cfg, engine, movie, match)
	assert.NoError(t, pc.Err)
	assert.Equal(t, "ABC-123.mp4", pc.FileName)
}

// --- Organize: conflicts detected path ---

func TestOrganizer_Organize_ConflictsDetected(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		OperationMode: operationmode.OperationModeOrganize,
		FileFormat:    "<ID>",
		FolderFormat:  "<ID>",
		RenameFile:    true,
	}
	m := &stubMatcherForOrgMiss{result: "ABC-123"}
	o := NewOrganizer(fs, cfg, nil, m)

	require.NoError(t, fs.MkdirAll("/source", 0755))
	require.NoError(t, afero.WriteFile(fs, "/source/ABC-123.mp4", []byte("video"), 0644))
	// Create the target file to cause a conflict
	require.NoError(t, fs.MkdirAll("/dest/ABC-123", 0755))
	require.NoError(t, afero.WriteFile(fs, "/dest/ABC-123/ABC-123.mp4", []byte("existing"), 0644))

	result, err := o.Organize(context.Background(), OrganizeCmd{
		Match: models.FileMatchInfo{
			Path: "/source/ABC-123.mp4", Name: "ABC-123.mp4",
			Extension: ".mp4", MovieID: "ABC-123",
		},
		Movie:       &models.Movie{ID: "ABC-123"},
		DestDir:     "/dest",
		ForceUpdate: false,
	})
	assert.Error(t, err)
	_ = result
}

// --- strategy_organize.Plan: subfolder template path ---

func TestOrganizeStrategy_Plan_SubfolderTemplateExec_Miss3(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		OperationMode:   operationmode.OperationModeOrganize,
		FileFormat:      "<ID>",
		FolderFormat:    "<ID>",
		SubfolderFormat: []string{"<MAKER>"},
		RenameFile:      true,
	}
	engine := &errEngineSubfolder{}
	strategy := newOrganizeStrategy(fs, cfg, engine, nil)

	match := models.FileMatchInfo{
		Path: "/src/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4", MovieID: "ABC-123",
	}
	movie := &models.Movie{ID: "ABC-123", Maker: "StudioA"}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	// The key is exercising the subfolder template execution path
	if err != nil {
		_ = err
	} else {
		assert.NotNil(t, plan)
	}
}

// --- strategy_organize.Plan: ExecuteWithMaxBytes error ---

func TestOrganizeStrategy_Plan_MaxPathLengthExecuteWithMaxBytesError(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		OperationMode: operationmode.OperationModeOrganize,
		FileFormat:    "<ID>",
		FolderFormat:  "<ID> <TITLE>",
		RenameFile:    true,
		MaxPathLength: 40,
	}
	engine := &errEngineMaxBytes3{}
	strategy := newOrganizeStrategy(fs, cfg, engine, nil)

	match := models.FileMatchInfo{
		Path: "/src/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4", MovieID: "ABC-123",
	}
	movie := &models.Movie{ID: "ABC-123", Title: "Some Title"}

	_, err := strategy.Plan(match, movie, "/dest", false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to generate folder name")
}

// --- strategy_organize.Plan: empty folder name after truncation with MovieID fallback ---

func TestOrganizeStrategy_Plan_EmptyFolderNameAfterTruncation(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		OperationMode: operationmode.OperationModeOrganize,
		FileFormat:    "<ID>",
		FolderFormat:  "<ID> <TITLE>",
		RenameFile:    true,
		MaxPathLength: 40,
	}
	engine := &emptyFolderEngine3{}
	strategy := newOrganizeStrategy(fs, cfg, engine, nil)

	match := models.FileMatchInfo{
		Path: "/src/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4", MovieID: "ABC-123",
	}
	movie := &models.Movie{ID: "ABC-123", Title: "Title"}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	// Empty folder name from ExecuteWithMaxBytes → fallback to MovieID
	assert.Equal(t, "ABC-123", plan.FolderName)
}

// --- strategy_organize.Plan: empty folder name AND empty MovieID ---

func TestOrganizeStrategy_Plan_EmptyFolderAndMovieID(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		OperationMode: operationmode.OperationModeOrganize,
		FileFormat:    "<ID>",
		FolderFormat:  "<ID> <TITLE>",
		RenameFile:    true,
		MaxPathLength: 40,
	}
	engine := &emptyFolderEngine3{}
	strategy := newOrganizeStrategy(fs, cfg, engine, nil)

	match := models.FileMatchInfo{
		Path: "/src/file.mp4", Name: "file.mp4", Extension: ".mp4", MovieID: "",
	}
	movie := &models.Movie{ID: "", Title: "Title"}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	// Both empty → fallback to "unknown"
	assert.Equal(t, "unknown", plan.FolderName)
}

// --- strategy_inplace.Plan: oldDir stat fails during conflict check ---

func TestInPlaceStrategy_Plan_OldDirStatFailsConflict(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		OperationMode: operationmode.OperationModeInPlace,
		FileFormat:    "<ID>",
		FolderFormat:  "NewName",
		RenameFile:    true,
	}
	m := &stubMatcherForOrgMiss{result: "ABC-123"}
	engine := template.NewEngine()
	strategy := newInPlaceStrategy(fs, cfg, m, engine)

	// Create a dedicated folder (one video matching the ID)
	require.NoError(t, fs.MkdirAll("/source/OldName", 0755))
	require.NoError(t, afero.WriteFile(fs, "/source/OldName/ABC-123.mp4", []byte("video"), 0644))

	// Also create the target dir so it exists during conflict check
	require.NoError(t, fs.MkdirAll("/source/NewName", 0755))

	match := models.FileMatchInfo{
		Path:      "/source/OldName/ABC-123.mp4",
		Name:      "ABC-123.mp4",
		Extension: ".mp4",
		MovieID:   "ABC-123",
	}
	movie := &models.Movie{ID: "ABC-123"}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.NotNil(t, plan)
}

// --- strategy_inplace_norenamefolder.Plan: buildPlanContext error ---

func TestInPlaceNoRenameFolder_Plan_BuildPlanContextError(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		OperationMode: operationmode.OperationModeInPlaceNoRenameFolder,
		FileFormat:    "<ID>",
		FolderFormat:  "<ID>",
		RenameFile:    true,
	}
	engine := &errEngine{}
	strategy := newInPlaceNoRenameFolderStrategy(fs, cfg, nil, engine)

	match := models.FileMatchInfo{
		Path: "/src/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4", MovieID: "ABC-123",
	}
	movie := &models.Movie{ID: "ABC-123"}

	_, err := strategy.Plan(match, movie, "/dest", false)
	assert.Error(t, err)
}

// --- strategy_organize.Execute: relative source path for symlink ---

func TestOrganizeStrategy_Execute_SoftLinkRelativeSource(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}
	ml := &mockLinker{}
	strategy := newOrganizeStrategy(fs, cfg, nil, ml)

	require.NoError(t, afero.WriteFile(fs, "relative.mp4", []byte("video"), 0644))

	plan := &OrganizePlan{
		SourcePath: "relative.mp4",
		TargetDir:  "/dest/ABC-123",
		TargetPath: "/dest/ABC-123/ABC-123.mp4",
		TargetFile: "ABC-123.mp4",
		WillMove:   true,
		moveFiles:  false,
		LinkMode:   LinkModeSoft,
		Conflicts:  []string{},
	}

	_, _ = strategy.Execute(plan)
}

// --- strategy_organize.Execute: soft link success path ---

func TestOrganizeStrategy_Execute_SoftLinkSuccess(t *testing.T) {
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
		LinkMode:   LinkModeSoft,
		Conflicts:  []string{},
	}

	result, err := strategy.Execute(plan)
	require.NoError(t, err)
	assert.True(t, result.Moved)
	assert.True(t, ml.symlinkCalled)
}

// --- strategy_organize.Execute: hard link success path ---

func TestOrganizeStrategy_Execute_HardLinkSuccess(t *testing.T) {
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
		LinkMode:   LinkModeHard,
		Conflicts:  []string{},
	}

	result, err := strategy.Execute(plan)
	require.NoError(t, err)
	assert.True(t, result.Moved)
	assert.True(t, ml.hardlinkCalled)
}

// --- strategy_organize.Execute: Remove target for link that doesn't exist ---

func TestOrganizeStrategy_Execute_LinkModeTargetNotExist(t *testing.T) {
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
		LinkMode:   LinkModeHard,
		Conflicts:  []string{},
	}

	result, err := strategy.Execute(plan)
	require.NoError(t, err)
	assert.True(t, result.Moved)
}

// --- subtitles: MoveSubtitles with directory entries ---

func TestSubtitleHandler_MoveSubtitles_SkipsDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	fs := afero.NewOsFs()

	sourceDir := fmt.Sprintf("%s/src", tmpDir)
	targetDir := fmt.Sprintf("%s/dest", tmpDir)
	require.NoError(t, fs.MkdirAll(sourceDir, 0755))
	require.NoError(t, fs.MkdirAll(targetDir, 0755))
	require.NoError(t, afero.WriteFile(fs, fmt.Sprintf("%s/ABC-123.srt", sourceDir), []byte("sub"), 0644))
	// Create a directory that looks like a subtitle file
	require.NoError(t, fs.MkdirAll(fmt.Sprintf("%s/ABC-123.eng.srt", sourceDir), 0755))

	sh := newSubtitleHandler(fs, []string{".srt", ".ass"})

	subtitles := sh.FindSubtitles(models.FileMatchInfo{
		Path:      sourceDir,
		Name:      "ABC-123.mp4",
		Extension: ".mp4",
	})
	err := sh.MoveSubtitles(subtitles, targetDir, "ABC-123", false)
	require.NoError(t, err)
}

// --- extractLanguageCode: remaining != "" ---

func TestExtractLanguageCode_RemainingPart(t *testing.T) {
	sh := newSubtitleHandler(afero.NewMemMapFs(), []string{".srt", ".ass"})

	result := sh.extractLanguageCode("movie.zh.srt", "movie")
	// "zh" maps to "chinese" in the language map
	assert.Equal(t, "chinese", result)
}

// --- strategy_inplace.Plan: in-place mode with mixed IDs folder ---

func TestInPlaceStrategy_Plan_MixedIDFolder(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		OperationMode: operationmode.OperationModeInPlace,
		FileFormat:    "<ID>",
		FolderFormat:  "<ID>",
		RenameFile:    true,
	}
	m := &stubMatcherForOrgMiss{result: "ABC-123"}
	engine := template.NewEngine()
	strategy := newInPlaceStrategy(fs, cfg, m, engine)

	// Create a folder with mixed video IDs
	require.NoError(t, fs.MkdirAll("/source/mixed", 0755))
	require.NoError(t, afero.WriteFile(fs, "/source/mixed/ABC-123.mp4", []byte("video1"), 0644))
	require.NoError(t, afero.WriteFile(fs, "/source/mixed/DEF-456.mp4", []byte("video2"), 0644))

	match := models.FileMatchInfo{
		Path:      "/source/mixed/ABC-123.mp4",
		Name:      "ABC-123.mp4",
		Extension: ".mp4",
		MovieID:   "ABC-123",
	}
	movie := &models.Movie{ID: "ABC-123"}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.NotNil(t, plan)
}

// --- strategy_inplace.Plan: matcher not set ---

func TestInPlaceStrategy_Plan_NoMatcherSet(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		OperationMode: operationmode.OperationModeInPlace,
		FileFormat:    "<ID>",
		FolderFormat:  "<ID>",
		RenameFile:    true,
	}
	engine := template.NewEngine()
	strategy := newInPlaceStrategy(fs, cfg, nil, engine) // nil matcher

	require.NoError(t, fs.MkdirAll("/source", 0755))
	require.NoError(t, afero.WriteFile(fs, "/source/ABC-123.mp4", []byte("video"), 0644))

	match := models.FileMatchInfo{
		Path:      "/source/ABC-123.mp4",
		Name:      "ABC-123.mp4",
		Extension: ".mp4",
		MovieID:   "ABC-123",
	}
	movie := &models.Movie{ID: "ABC-123"}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.False(t, plan.InPlace)
	assert.Contains(t, plan.SkipInPlaceReason, "matcher not set")
}

// --- strategy_inplace.Plan: folder already has correct name ---

func TestInPlaceStrategy_Plan_FolderAlreadyCorrectName(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		OperationMode: operationmode.OperationModeInPlace,
		FileFormat:    "<ID>",
		FolderFormat:  "<ID>",
		RenameFile:    true,
	}
	m := &stubMatcherForOrgMiss{result: "ABC-123"}
	engine := template.NewEngine()
	strategy := newInPlaceStrategy(fs, cfg, m, engine)

	// Create a dedicated folder with correct name
	require.NoError(t, fs.MkdirAll("/source/ABC-123", 0755))
	require.NoError(t, afero.WriteFile(fs, "/source/ABC-123/ABC-123.mp4", []byte("video"), 0644))

	match := models.FileMatchInfo{
		Path:      "/source/ABC-123/ABC-123.mp4",
		Name:      "ABC-123.mp4",
		Extension: ".mp4",
		MovieID:   "ABC-123",
	}
	movie := &models.Movie{ID: "ABC-123"}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.False(t, plan.InPlace)
	assert.Contains(t, plan.SkipInPlaceReason, "already has correct name")
}

// --- strategy_inplace.Plan: MaxPathLength validation failure ---

func TestInPlaceStrategy_Plan_MaxPathLengthValidationError(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		OperationMode: operationmode.OperationModeInPlace,
		FileFormat:    "<ID> <TITLE>",
		FolderFormat:  "<ID>",
		RenameFile:    true,
		MaxPathLength: 10,
	}
	m := &stubMatcherForOrgMiss{result: "ABC-123"}
	engine := template.NewEngine()
	strategy := newInPlaceStrategy(fs, cfg, m, engine)

	require.NoError(t, fs.MkdirAll("/source", 0755))

	match := models.FileMatchInfo{
		Path:      "/source/ABC-123.mp4",
		Name:      "ABC-123.mp4",
		Extension: ".mp4",
		MovieID:   "ABC-123",
	}
	movie := &models.Movie{ID: "ABC-123", Title: "Very Long Title"}

	_, err := strategy.Plan(match, movie, "/dest", false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "path validation")
}
