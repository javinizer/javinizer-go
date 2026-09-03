package organizer

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Unauthorized copy (MoveFiles=false) against a late-created destination conflicts
// instead of overwriting.
func TestOrganize_Copy_LateConflictRefused(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}
	org := NewOrganizer(fs, cfg, nil, nil)
	movie := testutil.NewMovieBuilder().WithID("IPX-321").Build()
	match := models.FileMatchInfo{Path: "/src/IPX-321.mp4", Name: "IPX-321.mp4", Extension: ".mp4", MovieID: "IPX-321"}

	strategy := org.strategyFromType(strategyOrganize)
	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	plan.moveFiles = false
	plan.LinkMode = LinkModeNone

	require.NoError(t, afero.WriteFile(fs, "/src/IPX-321.mp4", []byte("incoming"), 0644))
	require.NoError(t, afero.WriteFile(fs, "/dest/IPX-321/IPX-321.mp4", []byte("existing"), 0644))

	result, err := strategy.Execute(plan)
	require.Error(t, err)
	require.NotNil(t, result)
	assert.Contains(t, result.Error.Error(), "refusing to overwrite")

	data, _ := afero.ReadFile(fs, "/dest/IPX-321/IPX-321.mp4")
	assert.Equal(t, []byte("existing"), data)
}

// In-place-norenamefolder: a late-created different destination conflicts instead of
// being replaced by the rename.
func TestInPlaceNoRenameFolder_LateConflictRefused(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeInPlaceNoRenameFolder}
	org := NewOrganizer(fs, cfg, nil, nil)
	movie := testutil.NewMovieBuilder().WithID("IPX-777").Build()
	require.NoError(t, afero.WriteFile(fs, "/lib/IPX-777-odd.mp4", []byte("mine"), 0644))

	strategy := org.strategyFromType(strategyInPlaceNoRenameFolder)
	match := models.FileMatchInfo{Path: "/lib/IPX-777-odd.mp4", Name: "IPX-777-odd.mp4", Extension: ".mp4", MovieID: "IPX-777"}
	plan, err := strategy.Plan(match, movie, "/lib", false)
	require.NoError(t, err)

	require.NoError(t, afero.WriteFile(fs, "/lib/IPX-777.mp4", []byte("exists"), 0644))

	result, err := strategy.Execute(plan)
	require.Error(t, err)
	require.NotNil(t, result)
	assert.Contains(t, result.Error.Error(), "refusing to overwrite")
	data, _ := afero.ReadFile(fs, "/lib/IPX-777.mp4")
	assert.Equal(t, []byte("exists"), data)
}

// In-place strategy's move-to-organize-folder (non-InPlace) branch also refuses late conflicts.
func TestInPlaceStrategy_FileBranch_LateConflictRefused(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{OperationMode: operationmode.OperationModeOrganize, FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}
	m, _ := matcher.NewMatcher(&matcher.Config{})
	strategy := newInPlaceStrategy(fs, cfg, m, nil)

	require.NoError(t, afero.WriteFile(fs, "/source/folder/ABC-888.mp4", []byte("mine"), 0644))
	plan := &OrganizePlan{
		SourcePath: "/source/folder/ABC-888.mp4",
		TargetDir:  "/dest/ABC-888",
		TargetFile: "ABC-888.mp4",
		TargetPath: "/dest/ABC-888/ABC-888.mp4",
		WillMove:   true,
		InPlace:    false,
	}

	require.NoError(t, afero.WriteFile(fs, "/dest/ABC-888/ABC-888.mp4", []byte("exists"), 0644))

	result, err := strategy.Execute(plan)
	require.Error(t, err)
	require.NotNil(t, result)
	assert.Contains(t, result.Error.Error(), "refusing to overwrite")
	data, _ := afero.ReadFile(fs, "/dest/ABC-888/ABC-888.mp4")
	assert.Equal(t, []byte("exists"), data)
}
