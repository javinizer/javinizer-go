package workflow

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyOrchestrator_OperationModeOverride_PicksCorrectStrategy(t *testing.T) {
	fs := afero.NewMemMapFs()
	const (
		sourceDir   = "/source/folder"
		sourceFile  = "/source/folder/old-name.mp4"
		wantNewPath = "/source/folder/ABC-123 Test Movie.mp4"
	)
	require.NoError(t, fs.MkdirAll(sourceDir, 0777))
	require.NoError(t, afero.WriteFile(fs, sourceFile, []byte("video"), 0644))

	orgCfg := &organizer.Config{
		FileFormat:    "<ID> <TITLE>",
		FolderFormat:  "<ID> <TITLE>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}
	m, err := matcher.NewMatcher(&matcher.Config{})
	require.NoError(t, err)
	org := organizer.NewOrganizer(fs, orgCfg, template.NewEngine(), m)

	impl := newApplyOrchestrator(
		fs,
		org,
		nil,
		nil,
		&applyStubNFO{},
		ApplyConfig{},
		nil,
		noOpRevertLog{},
		nil,
		nil,
	)

	overrideMode := operationmode.OperationModeInPlaceNoRenameFolder
	skipOrganize := !overrideMode.RequiresOrganize()
	require.False(t, skipOrganize, "in-place-norenamefolder must run organize")

	cmd := ApplyCmd{
		Movie: &models.Movie{ID: "ABC-123", Title: "Test Movie"},
		Match: models.FileMatchInfo{
			MovieID:   "ABC-123",
			Path:      sourceFile,
			Name:      "old-name.mp4",
			Extension: ".mp4",
		},
		DestPath: "",
		Organize: OrganizeOptions{
			Skip:        skipOrganize,
			MoveFiles:   true,
			ForceUpdate: true,
		},
		OperationMode: overrideMode,
	}

	result, err := impl.Execute(context.Background(), cmd, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Steps.Organized, "organize step must run for in-place-norenamefolder override")

	newExists, _ := afero.Exists(fs, wantNewPath)
	assert.True(t, newExists, "file should be renamed in place to %s, not moved to a generated relative folder", wantNewPath)

	oldExists, _ := afero.Exists(fs, sourceFile)
	assert.False(t, oldExists, "original file should be gone after in-place rename")

	relFolderExists, _ := afero.DirExists(fs, "ABC-123 Test Movie")
	assert.False(t, relFolderExists, "no relative generated folder should be created for an in-place override")
}
