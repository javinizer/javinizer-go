package workflow

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type artifactDigestFaultFS struct {
	afero.Fs
	openErr, readErr, closeErr bool
}

func (f *artifactDigestFaultFS) Open(name string) (afero.File, error) {
	if f.openErr {
		return nil, errors.New("open denied")
	}
	file, err := f.Fs.Open(name)
	if err != nil {
		return nil, err
	}
	return &artifactDigestFaultFile{File: file, readErr: f.readErr, closeErr: f.closeErr}, nil
}

type artifactDigestFaultFile struct {
	afero.File
	readErr, closeErr bool
}

func (f *artifactDigestFaultFile) Read(p []byte) (int, error) {
	if f.readErr {
		return 0, errors.New("read denied")
	}
	return f.File.Read(p)
}

func (f *artifactDigestFaultFile) Close() error {
	err := f.File.Close()
	if f.closeErr {
		return errors.New("close denied")
	}
	return err
}

func TestArtifactDigestErrorsPropagateFromSharedPreflight(t *testing.T) {
	for _, tc := range []struct {
		name string
		fs   func(afero.Fs) afero.Fs
		want string
	}{
		{name: "open", fs: func(base afero.Fs) afero.Fs { return &artifactDigestFaultFS{Fs: base, openErr: true} }, want: "open staged artifact for digest"},
		{name: "read", fs: func(base afero.Fs) afero.Fs { return &artifactDigestFaultFS{Fs: base, readErr: true} }, want: "digest staged artifact"},
		{name: "close", fs: func(base afero.Fs) afero.Fs { return &artifactDigestFaultFS{Fs: base, closeErr: true} }, want: "close staged artifact digest"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := afero.NewMemMapFs()
			source := filepath.Join("stage", "owned", "movie.nfo")
			require.NoError(t, base.MkdirAll(filepath.Dir(source), 0o755))
			require.NoError(t, afero.WriteFile(base, source, []byte("metadata"), 0o644))
			stage := &artifactStage{
				fs: tc.fs(base), root: filepath.Join("stage", "owned"), finalRoot: filepath.Join("library", "movie"),
				original:   ApplyCmd{ArtifactCoordinator: NewSharedArtifactCoordinator([]string{"part-1"}), ArtifactOwnerKey: "part-1"},
				publishCtx: context.Background(),
			}
			_, err := stage.installPaths([]string{source}, nil, "", "")
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestArtifactSharedConsumerSkipsInstallAndResultPaths(t *testing.T) {
	fs := afero.NewMemMapFs()
	root := filepath.Join("stage", "owned")
	finalRoot := filepath.Join("library", "movie")
	source := filepath.Join(root, "movie.nfo")
	target := filepath.Join(finalRoot, "movie.nfo")
	require.NoError(t, fs.MkdirAll(filepath.Dir(source), 0o755))
	require.NoError(t, afero.WriteFile(fs, source, []byte("metadata"), 0o644))

	coordinator := NewSharedArtifactCoordinator([]string{"part-1", "part-2"})
	digest, err := artifactDigest(fs, source)
	require.NoError(t, err)
	claim, err := coordinator.Claim(t.Context(), target, digest, "part-1")
	require.NoError(t, err)
	require.True(t, claim.OwnsPublication())
	coordinator.Complete(claim, SharedArtifactPublished)

	stage := &artifactStage{
		fs: fs, root: root, finalRoot: finalRoot,
		stagedSource: filepath.Join(root, ".source", "movie.mp4"),
		original:     ApplyCmd{Organize: OrganizeOptions{Skip: true}, ArtifactCoordinator: coordinator, ArtifactOwnerKey: "part-2"},
	}
	state := &applyPipelineState{nfoPath: source, downloadPaths: []string{source}}
	err = stage.publishUnderFence(t.Context(), &applyOrchImpl{fs: fs}, OperationID("test"), state, nil)
	require.NoError(t, err)
	require.Empty(t, state.nfoPath)
	require.Empty(t, state.downloadPaths)
	_, err = fs.Stat(source)
	require.NoError(t, err, "consumer leaves its staged duplicate for cleanup")
	_, err = fs.Stat(target)
	require.Error(t, err, "consumer never publishes the shared target")
}

func TestArtifactSharedClaimRejectsUnknownContender(t *testing.T) {
	fs := afero.NewMemMapFs()
	source := filepath.Join("stage", "owned", "movie.nfo")
	require.NoError(t, fs.MkdirAll(filepath.Dir(source), 0o755))
	require.NoError(t, afero.WriteFile(fs, source, []byte("metadata"), 0o644))
	stage := &artifactStage{
		fs: fs, root: filepath.Join("stage", "owned"), finalRoot: filepath.Join("library", "movie"),
		original:   ApplyCmd{ArtifactCoordinator: NewSharedArtifactCoordinator([]string{"part-1"}), ArtifactOwnerKey: "unknown"},
		publishCtx: context.Background(),
	}
	_, err := stage.installPaths([]string{source}, nil, "", "")
	require.ErrorContains(t, err, "not registered")
}

type panicArtifactFencer struct{}

func (panicArtifactFencer) WithApplyArtifactPublicationFence(context.Context, string, int64, func(*models.Movie) error) error {
	panic("publication panic")
}

func TestArtifactPublishPanicFailsSharedClaim(t *testing.T) {
	path := filepath.Join(t.TempDir(), "movie.nfo")
	coordinator := NewSharedArtifactCoordinator([]string{"part-1", "part-2"})
	claim, err := coordinator.Claim(t.Context(), path, "same", "part-1")
	require.NoError(t, err)
	require.True(t, claim.OwnsPublication())
	stage := &artifactStage{
		fencer:       panicArtifactFencer{},
		original:     ApplyCmd{Movie: &models.Movie{ContentID: "MOVIE"}, ArtifactCoordinator: coordinator},
		sharedClaims: []SharedArtifactClaim{claim},
	}
	require.PanicsWithValue(t, "publication panic", func() {
		_ = stage.publish(t.Context(), &applyOrchImpl{}, &applyPipelineState{}, nil)
	})
	promoted, err := coordinator.Claim(t.Context(), path, "same", "part-2")
	require.NoError(t, err)
	require.True(t, promoted.OwnsPublication(), "panic must fail the old owner and promote a waiter")
}

func TestArtifactSharedCompletionDisposition(t *testing.T) {
	stage := &artifactStage{}
	require.Equal(t, SharedArtifactPublished, stage.sharedCompletionDisposition(false, nil))
	require.Equal(t, SharedArtifactSafeToPromote, stage.sharedCompletionDisposition(false, errors.New("prepublication")))
	require.Equal(t, SharedArtifactSafeToPromote, stage.sharedCompletionDisposition(true, errors.New("prepublication panic")))
	stage.sharedPublishBegan = true
	require.Equal(t, SharedArtifactPoisoned, stage.sharedCompletionDisposition(true, errors.New("postpublication panic")))
	stage.sharedPublishBegan = false
	stage.sharedPoisoned = true
	require.Equal(t, SharedArtifactPoisoned, stage.sharedCompletionDisposition(false, errors.New("rollback uncertain")))
}
