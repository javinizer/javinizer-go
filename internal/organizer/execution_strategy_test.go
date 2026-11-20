package organizer_test

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
)

func TestExecuteOrganizePlanCallerConstructedCopy(t *testing.T) {
	fs := afero.NewMemMapFs()
	source := filepath.Join("incoming", "movie.mp4")
	destination := filepath.Join("library", "movie.mp4")
	content := []byte("video")
	require.NoError(t, fs.MkdirAll(filepath.Dir(source), 0o755))
	require.NoError(t, afero.WriteFile(fs, source, content, 0o644))
	org := organizer.NewOrganizer(fs, &organizer.Config{OperationMode: operationmode.OperationModeMetadataArtwork}, nil, nil)

	// A caller-built plan has no captured strategy. Its zero strategy type
	// means organize, independently of the organizer's configured planner.
	result, err := org.ExecuteOrganizePlan(&organizer.OrganizePlan{
		SourcePath: source,
		TargetDir:  filepath.Dir(destination),
		TargetFile: filepath.Base(destination),
		TargetPath: destination,
		WillMove:   true,
	}, false, organizer.LinkModeNone)
	require.NoError(t, err)
	require.Equal(t, destination, result.NewPath)
	for _, path := range []string{source, destination} {
		got, err := afero.ReadFile(fs, path)
		require.NoError(t, err, path)
		require.Equal(t, content, got, path)
	}
}
