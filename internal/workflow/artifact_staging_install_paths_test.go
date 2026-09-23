package workflow

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestPR260InstallPathsValidatesBeforePublishing(t *testing.T) {
	fs := afero.NewMemMapFs()
	stageRoot := filepath.Join("stage", "owned")
	finalRoot := filepath.Join("library", "movie")
	outside := filepath.Join("incoming", "outside.jpg")
	unrelated := filepath.Join("incoming", "unrelated.txt")
	require.NoError(t, fs.MkdirAll(filepath.Dir(outside), 0o755))
	require.NoError(t, afero.WriteFile(fs, outside, []byte("outside"), 0o644))
	require.NoError(t, afero.WriteFile(fs, unrelated, []byte("unrelated"), 0o644))
	stage := &artifactStage{fs: fs, root: stageRoot, finalRoot: finalRoot}

	preserved, err := stage.installPaths([]string{outside}, nil, "", "")
	require.ErrorContains(t, err, "staged path escapes staging area")
	require.False(t, preserved)
	outsideBytes, readErr := afero.ReadFile(fs, outside)
	require.NoError(t, readErr)
	require.Equal(t, "outside", string(outsideBytes))
	unrelatedBytes, readErr := afero.ReadFile(fs, unrelated)
	require.NoError(t, readErr)
	require.Equal(t, "unrelated", string(unrelatedBytes))
	exists, statErr := afero.Exists(fs, finalRoot)
	require.NoError(t, statErr)
	require.False(t, exists)
}

func TestPR260InstallPathsRelocatesArtifactsToFinalPlannedFolder(t *testing.T) {
	fs := afero.NewMemMapFs()
	stageRoot := filepath.Join("stage", "owned")
	stagedFolder := filepath.Join(stageRoot, "unclamped-title")
	finalRoot := filepath.Join("library", "movie")
	finalFolder := filepath.Join(finalRoot, "clamped-title")
	nfo := filepath.Join(stagedFolder, "movie.nfo")
	manifest := filepath.Join(stageRoot, "manifest.json")
	require.NoError(t, fs.MkdirAll(stagedFolder, 0o755))
	require.NoError(t, afero.WriteFile(fs, nfo, []byte("metadata"), 0o644))
	require.NoError(t, afero.WriteFile(fs, manifest, []byte("manifest"), 0o644))
	stage := &artifactStage{fs: fs, root: stageRoot, finalRoot: finalRoot}

	_, err := stage.installPaths([]string{nfo, manifest}, nil, stagedFolder, finalFolder)
	require.NoError(t, err)
	published, err := afero.ReadFile(fs, filepath.Join(finalFolder, "movie.nfo"))
	require.NoError(t, err)
	require.Equal(t, "metadata", string(published))
	published, err = afero.ReadFile(fs, filepath.Join(finalRoot, "manifest.json"))
	require.NoError(t, err)
	require.Equal(t, "manifest", string(published))
	exists, err := afero.Exists(fs, filepath.Join(finalRoot, "unclamped-title", "movie.nfo"))
	require.NoError(t, err)
	require.False(t, exists)
}

func TestPR260InstallPathsPublishesAndPreservesLegitimatePaths(t *testing.T) {
	fs := afero.NewMemMapFs()
	stageRoot := filepath.Join("stage", "owned")
	finalRoot := filepath.Join("library", "movie")
	publishSource := filepath.Join(stageRoot, "movie.nfo")
	preserveSource := filepath.Join(stageRoot, "poster.jpg")
	preserveTarget := filepath.Join(finalRoot, "poster.jpg")
	require.NoError(t, fs.MkdirAll(stageRoot, 0o755))
	require.NoError(t, afero.WriteFile(fs, publishSource, []byte("metadata"), 0o644))
	require.NoError(t, afero.WriteFile(fs, preserveSource, []byte("new poster"), 0o644))
	require.NoError(t, fs.MkdirAll(finalRoot, 0o755))
	require.NoError(t, afero.WriteFile(fs, preserveTarget, []byte("current poster"), 0o644))
	stage := &artifactStage{fs: fs, root: stageRoot, finalRoot: finalRoot}

	preserved, err := stage.installPaths([]string{publishSource, preserveSource}, []string{preserveSource}, "", "")
	require.NoError(t, err)
	require.True(t, preserved)
	published, err := afero.ReadFile(fs, filepath.Join(finalRoot, "movie.nfo"))
	require.NoError(t, err)
	require.Equal(t, "metadata", string(published))
	poster, err := afero.ReadFile(fs, preserveTarget)
	require.NoError(t, err)
	require.Equal(t, "current poster", string(poster))
	preservedSource, err := afero.ReadFile(fs, preserveSource)
	require.NoError(t, err)
	require.Equal(t, "new poster", string(preservedSource))
}

func TestPR260InstallPathsAllowsInPlaceParentMapping(t *testing.T) {
	fs := afero.NewMemMapFs()
	finalRoot := filepath.Join("incoming", "movie.mp4")
	source := filepath.Join(filepath.Dir(finalRoot), "movie.srt")
	require.NoError(t, fs.MkdirAll(filepath.Dir(source), 0o755))
	require.NoError(t, afero.WriteFile(fs, source, []byte("subtitle"), 0o644))
	stage := &artifactStage{fs: fs, root: filepath.Join("stage", "owned"), finalRoot: finalRoot, inPlace: true}

	preserved, err := stage.installPaths([]string{source}, []string{source}, "", "")
	require.NoError(t, err)
	require.True(t, preserved)
	content, err := afero.ReadFile(fs, source)
	require.NoError(t, err)
	require.Equal(t, "subtitle", string(content))
}
