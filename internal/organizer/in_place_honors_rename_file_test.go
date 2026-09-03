package organizer

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/testutil"
)

func setupInPlaceRenameFileFixture(t *testing.T) (afero.Fs, models.FileMatchInfo, *models.Movie) {
	t.Helper()
	fs := afero.NewMemMapFs()
	src := "/source/old folder/IPX-535-original.mp4"
	require.NoError(t, fs.MkdirAll("/source/old folder", 0o755))
	require.NoError(t, afero.WriteFile(fs, src, []byte("video"), 0o644))
	match := models.FileMatchInfo{
		Path: src, Name: "IPX-535-original.mp4", Extension: ".mp4", MovieID: "IPX-535",
	}
	movie := testutil.NewMovieBuilder().WithID("IPX-535").Build()
	return fs, match, movie
}

// Issue #226: rename-in-place projected from a web apply plan must honor the
// global rename_file=false setting — the plan projection no longer sets
// ForceRenameFile, so the organizer must rename only the folder.
func TestInPlace_HonorsRenameFileFalse_FolderOnly(t *testing.T) {
	fs, match, movie := setupInPlaceRenameFileFixture(t)
	m, err := matcher.NewMatcher(&matcher.Config{})
	require.NoError(t, err)
	org := NewOrganizer(fs, &Config{
		FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: false,
		OperationMode: operationmode.OperationModeInPlace,
	}, nil, m)

	result, err := org.Organize(context.Background(), OrganizeCmd{
		Match: match, Movie: movie, MoveFiles: true,
		OperationMode: operationmode.OperationModeInPlace,
		// No ForceRenameFile: this mirrors the post-fix projection output.
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	exists, err := afero.Exists(fs, "/source/IPX-535/IPX-535-original.mp4")
	require.NoError(t, err)
	assert.True(t, exists, "folder renamed to <ID>, original file name preserved")

	targetDir, err := afero.Exists(fs, "/source/old folder")
	require.NoError(t, err)
	assert.False(t, targetDir, "legacy folder should be renamed away")
}

// Default configuration (rename_file=true) behavior must remain unchanged:
// rename-in-place renames both folder and file.
func TestInPlace_RenameFileDefault_RenamesFolderAndFile(t *testing.T) {
	fs, match, movie := setupInPlaceRenameFileFixture(t)
	m, err := matcher.NewMatcher(&matcher.Config{})
	require.NoError(t, err)
	org := NewOrganizer(fs, &Config{
		FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true,
		OperationMode: operationmode.OperationModeInPlace,
	}, nil, m)

	_, err = org.Organize(context.Background(), OrganizeCmd{
		Match: match, Movie: movie, MoveFiles: true,
		OperationMode: operationmode.OperationModeInPlace,
	})
	require.NoError(t, err)

	exists, err := afero.Exists(fs, "/source/IPX-535/IPX-535.mp4")
	require.NoError(t, err)
	assert.True(t, exists, "folder and file both renamed when rename_file=true")
}

// The rename-file operation (in-place-norenamefolder) keeps forcing file
// renaming even with rename_file=false — renaming the file is the definition
// of that operation. Folder must stay untouched.
func TestInPlaceNoRenameFolder_ForceStillRenamesFileOnly(t *testing.T) {
	fs, match, movie := setupInPlaceRenameFileFixture(t)
	m, err := matcher.NewMatcher(&matcher.Config{})
	require.NoError(t, err)
	org := NewOrganizer(fs, &Config{
		FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: false,
		OperationMode: operationmode.OperationModeInPlaceNoRenameFolder,
	}, nil, m)

	_, err = org.Organize(context.Background(), OrganizeCmd{
		Match: match, Movie: movie, MoveFiles: true,
		OperationMode:   operationmode.OperationModeInPlaceNoRenameFolder,
		ForceRenameFile: true,
	})
	require.NoError(t, err)

	exists, err := afero.Exists(fs, "/source/old folder/IPX-535.mp4")
	require.NoError(t, err)
	assert.True(t, exists, "file renamed in the untouched source folder")
}
