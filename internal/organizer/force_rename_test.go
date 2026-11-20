package organizer

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
)

func TestOrganizer_ForceRenameFileOverridesDisabledConfig(t *testing.T) {
	fs := afero.NewMemMapFs()
	sourceDir := "/source"
	sourceFile := filepath.Join(sourceDir, "old.mp4")
	require.NoError(t, fs.MkdirAll(sourceDir, 0o755))
	require.NoError(t, afero.WriteFile(fs, sourceFile, []byte("video"), 0o644))
	org := NewOrganizer(fs, &Config{
		FileFormat: "<ID>", RenameFile: false,
		OperationMode: operationmode.OperationModeOrganize,
	}, nil, nil)

	ctx := context.Background()
	cmd := OrganizeCmd{
		Match: models.FileMatchInfo{Path: sourceFile, Name: "old.mp4", Extension: ".mp4", MovieID: "ABC-123"},
		Movie: &models.Movie{ID: "ABC-123"}, MoveFiles: true, ForceUpdate: true,
		OperationMode:   operationmode.OperationModeInPlaceNoRenameFolder,
		ForceRenameFile: true,
	}
	plan, err := org.PlanOrganize(ctx, cmd)
	require.NoError(t, err)
	assert.Equal(t, "/source/ABC-123.mp4", filepath.ToSlash(plan.TargetPath))
	exists, err := afero.Exists(fs, plan.TargetPath)
	require.NoError(t, err)
	assert.False(t, exists, "priming must not install the planned target")

	result, err := org.Organize(ctx, cmd)
	require.NoError(t, err)
	assert.Equal(t, plan.TargetPath, result.NewPath)
	assert.True(t, result.Moved)
	got, err := afero.ReadFile(fs, result.NewPath)
	require.NoError(t, err)
	assert.Equal(t, "video", string(got))
	assert.False(t, org.config.RenameFile)
	assert.Equal(t, operationmode.OperationModeOrganize, org.config.OperationMode)
}
