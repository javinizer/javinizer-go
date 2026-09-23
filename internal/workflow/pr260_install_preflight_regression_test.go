package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type preflightMutationFS struct {
	afero.Fs
	mutations []string
}

func (f *preflightMutationFS) MkdirAll(path string, perm os.FileMode) error {
	f.mutations = append(f.mutations, "mkdir:"+path)
	return f.Fs.MkdirAll(path, perm)
}

func (f *preflightMutationFS) Remove(path string) error {
	f.mutations = append(f.mutations, "remove:"+path)
	return f.Fs.Remove(path)
}

func (f *preflightMutationFS) Rename(oldpath, newpath string) error {
	f.mutations = append(f.mutations, "rename:"+oldpath+":"+newpath)
	return f.Fs.Rename(oldpath, newpath)
}

func TestPR260InstallPathsPreflightsEveryArtifactBeforeMutation(t *testing.T) {
	base := afero.NewMemMapFs()
	stageRoot := filepath.Join("stage", "owned")
	finalRoot := filepath.Join("library", "movie")
	firstSource := filepath.Join(stageRoot, "movie.nfo")
	lateSource := filepath.Join(stageRoot, "poster.jpg")
	firstTarget := filepath.Join(finalRoot, "movie.nfo")
	lateTarget := filepath.Join(finalRoot, "poster.jpg")
	require.NoError(t, base.MkdirAll(stageRoot, 0o755))
	require.NoError(t, base.MkdirAll(finalRoot, 0o755))
	require.NoError(t, afero.WriteFile(base, firstSource, []byte("new nfo"), 0o644))
	require.NoError(t, afero.WriteFile(base, lateSource, []byte("new poster"), 0o644))
	require.NoError(t, afero.WriteFile(base, firstTarget, []byte("old nfo"), 0o644))
	require.NoError(t, base.MkdirAll(lateTarget, 0o755))
	fs := &preflightMutationFS{Fs: base}
	stage := &artifactStage{fs: fs, root: stageRoot, finalRoot: finalRoot, original: ApplyCmd{OverwriteExistingMedia: true}}

	_, err := stage.installPaths([]string{firstSource, lateSource}, nil, "", "")
	require.ErrorContains(t, err, "artifact destination is a directory")
	require.Empty(t, fs.mutations, "predictable validation failures must happen before publication starts")
	for path, want := range map[string]string{firstSource: "new nfo", lateSource: "new poster", firstTarget: "old nfo"} {
		got, readErr := afero.ReadFile(base, path)
		require.NoError(t, readErr)
		require.Equal(t, want, string(got))
	}
	info, statErr := base.Stat(lateTarget)
	require.NoError(t, statErr)
	require.True(t, info.IsDir())
}

func TestPR260InstallPathsPreflightsLateInvalidMappingAndPreserveSkip(t *testing.T) {
	base := afero.NewMemMapFs()
	stageRoot := filepath.Join("stage", "owned")
	finalRoot := filepath.Join("library", "movie")
	publishSource := filepath.Join(stageRoot, "movie.nfo")
	preserveSource := filepath.Join(stageRoot, "poster.jpg")
	preserveTarget := filepath.Join(finalRoot, "poster.jpg")
	outside := filepath.Join("incoming", "escape.jpg")
	for _, path := range []string{publishSource, preserveSource, outside} {
		require.NoError(t, base.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, afero.WriteFile(base, path, []byte(path), 0o644))
	}
	require.NoError(t, base.MkdirAll(finalRoot, 0o755))
	require.NoError(t, afero.WriteFile(base, preserveTarget, []byte("existing poster"), 0o644))
	fs := &preflightMutationFS{Fs: base}
	stage := &artifactStage{fs: fs, root: stageRoot, finalRoot: finalRoot}

	_, err := stage.installPaths([]string{publishSource, preserveSource, outside}, []string{preserveSource}, "", "")
	require.ErrorContains(t, err, "staged path escapes staging area")
	require.Empty(t, fs.mutations)
	got, readErr := afero.ReadFile(base, preserveTarget)
	require.NoError(t, readErr)
	require.Equal(t, "existing poster", string(got))
}

type preflightStatFS struct {
	*preflightMutationFS
	faults map[string]error
}

func (f *preflightStatFS) Stat(name string) (os.FileInfo, error) {
	if err, ok := f.faults[filepath.Clean(name)]; ok {
		return nil, err
	}
	return f.preflightMutationFS.Stat(name)
}

func TestPR260InstallPathsPreflightSourceAndParentBoundaries(t *testing.T) {
	t.Run("missing source", func(t *testing.T) {
		base := afero.NewMemMapFs()
		fs := &preflightMutationFS{Fs: base}
		stage := &artifactStage{fs: fs, root: filepath.Join("stage", "owned"), finalRoot: filepath.Join("library", "movie")}
		_, err := stage.installPaths([]string{filepath.Join(stage.root, "missing.nfo")}, nil, "", "")
		require.ErrorContains(t, err, "inspect staged artifact")
		require.Empty(t, fs.mutations)
	})

	t.Run("nonregular source", func(t *testing.T) {
		base := afero.NewMemMapFs()
		source := filepath.Join("stage", "owned", "directory.nfo")
		require.NoError(t, base.MkdirAll(source, 0o755))
		fs := &preflightMutationFS{Fs: base}
		stage := &artifactStage{fs: fs, root: filepath.Join("stage", "owned"), finalRoot: filepath.Join("library", "movie")}
		_, err := stage.installPaths([]string{source}, nil, "", "")
		require.ErrorContains(t, err, "not a regular file")
		require.Empty(t, fs.mutations)
	})

	t.Run("destination parent is a file", func(t *testing.T) {
		base := afero.NewMemMapFs()
		source := filepath.Join("stage", "owned", "movie.nfo")
		blocker := filepath.Join("library", "blocked")
		target := filepath.Join(blocker, "movie", "movie.nfo")
		require.NoError(t, base.MkdirAll(filepath.Dir(source), 0o755))
		require.NoError(t, afero.WriteFile(base, source, []byte("staged"), 0o644))
		require.NoError(t, base.MkdirAll(filepath.Dir(blocker), 0o755))
		require.NoError(t, afero.WriteFile(base, blocker, []byte("not a directory"), 0o644))
		mutations := &preflightMutationFS{Fs: base}
		fs := &preflightStatFS{preflightMutationFS: mutations, faults: map[string]error{filepath.Clean(target): os.ErrNotExist}}
		stage := &artifactStage{fs: fs, root: filepath.Join("stage", "owned"), finalRoot: filepath.Join(blocker, "movie")}
		_, err := stage.installPaths([]string{source}, nil, "", "")
		require.ErrorContains(t, err, "destination parent is not a directory")
		require.Empty(t, mutations.mutations)
	})

	t.Run("destination parent inspection error", func(t *testing.T) {
		base := afero.NewMemMapFs()
		source := filepath.Join("stage", "owned", "movie.nfo")
		parent := filepath.Join("library", "denied")
		require.NoError(t, base.MkdirAll(filepath.Dir(source), 0o755))
		require.NoError(t, afero.WriteFile(base, source, []byte("staged"), 0o644))
		mutations := &preflightMutationFS{Fs: base}
		fs := &preflightStatFS{preflightMutationFS: mutations, faults: map[string]error{filepath.Clean(parent): os.ErrPermission}}
		stage := &artifactStage{fs: fs, root: filepath.Join("stage", "owned"), finalRoot: parent}
		_, err := stage.installPaths([]string{source}, nil, "", "")
		require.ErrorContains(t, err, "inspect artifact destination parent")
		require.Empty(t, mutations.mutations)
	})

	t.Run("missing filesystem root terminates parent walk", func(t *testing.T) {
		base := afero.NewMemMapFs()
		source := filepath.Join("stage", "owned", "movie.nfo")
		require.NoError(t, base.MkdirAll(filepath.Dir(source), 0o755))
		require.NoError(t, afero.WriteFile(base, source, []byte("staged"), 0o644))
		mutations := &preflightMutationFS{Fs: base}
		fs := &preflightStatFS{preflightMutationFS: mutations, faults: map[string]error{string(filepath.Separator): os.ErrNotExist}}
		stage := &artifactStage{fs: fs, root: filepath.Join("stage", "owned"), finalRoot: filepath.Join(string(filepath.Separator), "missing", "movie")}
		_, err := stage.installPaths([]string{source}, nil, "", "")
		require.NoError(t, err)
	})
}
