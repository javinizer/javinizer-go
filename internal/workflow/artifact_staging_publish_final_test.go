package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type pr260PublishFinalFS struct {
	afero.Fs
	op, path string
}

func (f *pr260PublishFinalFS) Remove(name string) error {
	if f.op == "remove" && name == f.path {
		return errors.New("original cleanup denied")
	}
	return f.Fs.Remove(name)
}
func (f *pr260PublishFinalFS) Stat(name string) (os.FileInfo, error) {
	if f.op == "stat" && name == f.path {
		return nil, errors.New("sidecar inspection denied")
	}
	return f.Fs.Stat(name)
}
func (f *pr260PublishFinalFS) Rename(old, new string) error {
	if f.op == "rename" && old == f.path {
		return errors.New("sidecar rename denied")
	}
	return f.Fs.Rename(old, new)
}

func TestPR260PublicationFinalCleanupFaults(t *testing.T) {
	for _, tc := range []struct {
		name, role string
		want       string
	}{
		{"video original removal", "video", "remove original after artifact publication"},
		{"sidecar original removal", "sidecar", "remove original sidecar after artifact publication"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, _ := pr260ArtifactDB(t)
			movie := pr260FencedMovie(t, db, "cleanup-"+tc.role, "")
			base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "cleanup-"+tc.role)
			fs := &pr260PublishFinalFS{Fs: base, op: "remove", path: source}
			if tc.role == "sidecar" {
				fs.path = subtitle
			}
			dest := filepath.Join(root, "library")
			orch := pr260RealApply(fs, &movie, organizer.MediaFormatConfig{}, nil, false)
			cmd := pr260FencedCommand(&movie, match, dest, pr260FencedCounter(t, db), operationmode.OperationModeOrganize, false, true, organizer.LinkModeNone, false, false)
			stage, _, err := orch.prepareArtifact(context.Background(), cmd)
			require.NoError(t, err)
			state := &applyPipelineState{organizeResult: &organizer.OrganizeResult{NewPath: stage.stagedSource}}
			err = stage.publish(context.Background(), orch, state, nil)
			require.ErrorContains(t, err, tc.want)
			require.NotNil(t, stage)
			require.NotEmpty(t, stage.root)
			exists, e := afero.Exists(base, source)
			require.NoError(t, e)
			require.True(t, exists, "cleanup failure restores the original video before final rollback")
			exists, e = afero.Exists(base, subtitle)
			require.NoError(t, e)
			require.True(t, exists, "cleanup failure preserves/restores original sidecars")
			finalFiles := 0
			_ = afero.Walk(base, dest, func(_ string, info os.FileInfo, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if info.Mode().IsRegular() {
					finalFiles++
				}
				return nil
			})
			require.Zero(t, finalFiles, "cleanup failure rolls published final files back")
			bytes, e := afero.ReadFile(base, unrelated)
			require.NoError(t, e)
			require.Equal(t, "unrelated", string(bytes))
			exists, e = afero.Exists(base, multipart)
			require.NoError(t, e)
			require.True(t, exists, "multipart original is restored before final rollback")
			stage.cleanup()
			pr260AssertStageGone(t, base, root)
		})
	}
}

func TestPR260PublicationSidecarFinalizeFaults(t *testing.T) {
	for _, tc := range []struct{ op, want string }{{"stat", "preflight staged artifacts"}, {"rename", "stage sidecar"}} {
		t.Run(tc.op, func(t *testing.T) {
			db, _ := pr260ArtifactDB(t)
			movie := pr260FencedMovie(t, db, "sidecar-"+tc.op, "")
			base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "sidecar-"+tc.op)
			fs := &pr260PublishFinalFS{Fs: base, op: tc.op}
			dest := filepath.Join(root, "library")
			orch := pr260RealApply(fs, &movie, organizer.MediaFormatConfig{}, nil, false)
			cmd := pr260FencedCommand(&movie, match, dest, pr260FencedCounter(t, db), operationmode.OperationModeOrganize, false, true, organizer.LinkModeNone, false, false)
			stage, _, err := orch.prepareArtifact(context.Background(), cmd)
			require.NoError(t, err)
			require.NotEmpty(t, stage.siblings)
			old := stage.stagedSource
			renamed := filepath.Join(filepath.Dir(old), "renamed.mp4")
			require.NoError(t, base.Rename(old, renamed))
			fs.path = stage.siblings[0].stagedPath
			state := &applyPipelineState{organizeResult: &organizer.OrganizeResult{NewPath: renamed}}
			err = stage.publish(context.Background(), orch, state, nil)
			require.ErrorContains(t, err, tc.want)
			pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
			regularFiles := 0
			walkErr := afero.Walk(base, dest, func(_ string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if info.Mode().IsRegular() {
					regularFiles++
				}
				return nil
			})
			if walkErr != nil {
				require.True(t, os.IsNotExist(walkErr))
			}
			require.Zero(t, regularFiles, "failed staged publication rolls every final file back")
			exists, e := afero.Exists(base, renamed)
			require.NoError(t, e)
			require.Equal(t, tc.op == "stat", exists, "preflight rejection preserves staging; post-publish rollback may consume it")
			stage.cleanup()
			pr260AssertStageGone(t, base, root)
		})
	}
}
