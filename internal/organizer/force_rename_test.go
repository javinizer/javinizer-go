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

	result, err := org.Organize(context.Background(), OrganizeCmd{
		Match: models.FileMatchInfo{Path: sourceFile, Name: "old.mp4", Extension: ".mp4", MovieID: "ABC-123"},
		Movie: &models.Movie{ID: "ABC-123"}, MoveFiles: true, ForceUpdate: true,
		OperationMode:   operationmode.OperationModeInPlaceNoRenameFolder,
		ForceRenameFile: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "/source/ABC-123.mp4", filepath.ToSlash(result.NewPath))
	exists, statErr := afero.Exists(fs, "/source/ABC-123.mp4")
	require.NoError(t, statErr)
	assert.True(t, exists)
	assert.False(t, org.config.RenameFile)
}
