package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type pr260StageFaultFS struct {
	afero.Fs
	operation, path string
	enabled         bool
}

func (f *pr260StageFaultFS) inject(op, name string) error {
	if f.enabled && op == f.operation && filepath.Clean(name) == filepath.Clean(f.path) {
		return errors.New("pr260 finite fault: " + op)
	}
	return nil
}
func (f *pr260StageFaultFS) Stat(name string) (os.FileInfo, error) {
	if err := f.inject("stat", name); err != nil {
		return nil, err
	}
	return f.Fs.Stat(name)
}
func (f *pr260StageFaultFS) MkdirAll(name string, mode os.FileMode) error {
	if err := f.inject("mkdir", name); err != nil {
		return err
	}
	return f.Fs.MkdirAll(name, mode)
}
func (f *pr260StageFaultFS) Remove(name string) error {
	if err := f.inject("remove", name); err != nil {
		return err
	}
	return f.Fs.Remove(name)
}
func (f *pr260StageFaultFS) Rename(old, new string) error {
	if err := f.inject("rename", new); err != nil {
		return err
	}
	return f.Fs.Rename(old, new)
}

func TestPR260FiniteArtifactInstallFailuresAndRetry(t *testing.T) {
	for _, tc := range []struct {
		name, op, where, want string
		existing              bool
	}{
		{"destination mkdir", "mkdir", "parent", "create artifact destination", false},
		{"destination stat", "stat", "target", "inspect artifact destination", false},
		{"existing remove", "remove", "target", "replace artifact destination", true},
		{"rename", "rename", "target", "publish staged artifact", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "install-"+strings.ReplaceAll(tc.name, " ", "-"))
			dest := filepath.Join(root, "published")
			fs := &pr260StageFaultFS{Fs: base}
			cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "pr260-install"}, match, dest)
			stage, _, err := (&applyOrchImpl{fs: fs}).prepareArtifact(context.Background(), cmd)
			require.NoError(t, err)
			defer stage.cleanup()
			staged := filepath.Join(stage.root, "generated.nfo")
			require.NoError(t, afero.WriteFile(base, staged, []byte("new"), 0o644))
			target := filepath.Join(dest, "generated.nfo")
			if tc.existing {
				require.NoError(t, base.MkdirAll(dest, 0o755))
				require.NoError(t, afero.WriteFile(base, target, []byte("current"), 0o644))
			}
			fs.operation = tc.op
			fs.path = target
			if tc.where == "parent" {
				fs.path = dest
			}
			fs.enabled = true
			err = stage.publish(context.Background(), &applyOrchImpl{fs: fs}, &applyPipelineState{}, nil)
			require.ErrorContains(t, err, tc.want)
			if tc.existing {
				b, e := afero.ReadFile(base, target)
				require.NoError(t, e)
				require.Equal(t, "current", string(b))
			} else {
				exists, e := afero.Exists(base, target)
				require.NoError(t, e)
				require.False(t, exists)
			}
			pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
			fs.enabled = false
			_, err = stage.installTree("", "", nil, "", "")
			require.NoError(t, err)
			b, err := afero.ReadFile(base, target)
			require.NoError(t, err)
			require.Equal(t, "new", string(b))
		})
	}
}

func TestPR260FiniteArtifactRehomeFaultsRetainSidecars(t *testing.T) {
	for _, tc := range []struct {
		name, op, where, want string
		existing              bool
	}{
		{"sidecar inspect", "stat", "source", "inspect staged sidecar", false},
		{"sidecar mkdir", "mkdir", "parent", "create staged sidecar directory", false},
		{"target inspect", "stat", "target", "inspect staged sidecar target", false},
		{"target remove", "remove", "target", "replace staged sidecar", true},
		{"sidecar rename", "rename", "target", "stage sidecar", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "rehome-"+strings.ReplaceAll(tc.name, " ", "-"))
			fs := &pr260StageFaultFS{Fs: base}
			dest := filepath.Join(root, "published")
			cmd := ApplyCmd{Movie: &models.Movie{ContentID: "pr260-rehome"}, PublicationFence: pr260FailureArtifactFencer{}, Match: match, DestPath: dest, Organize: OrganizeOptions{MoveFiles: true}}
			stage, _, err := (&applyOrchImpl{fs: fs}).prepareArtifact(context.Background(), cmd)
			require.NoError(t, err)
			defer stage.cleanup()
			require.NotEmpty(t, stage.siblings)
			video := filepath.Join(stage.root, "renamed", "NEW.mp4")
			sibling := stage.siblings[0].stagedPath
			target := filepath.Join(filepath.Dir(video), stagedArtifactSiblingName(filepath.Base(stage.stagedSource), filepath.Base(video), filepath.Base(sibling)))
			require.NoError(t, base.MkdirAll(filepath.Dir(target), 0o755))
			if tc.existing {
				require.NoError(t, afero.WriteFile(base, target, []byte("current"), 0o644))
			}
			fs.operation = tc.op
			fs.path = target
			if tc.where == "source" {
				fs.path = sibling
			}
			if tc.where == "parent" {
				fs.path = filepath.Dir(target)
			}
			fs.enabled = true
			err = stage.rehomeRemainingSiblings(video)
			require.ErrorContains(t, err, tc.want)
			if tc.existing {
				b, e := afero.ReadFile(base, target)
				require.NoError(t, e)
				require.Equal(t, "current", string(b))
			}
			pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
			fs.enabled = false
			require.NoError(t, stage.rehomeRemainingSiblings(video))
			b, err := afero.ReadFile(base, target)
			require.NoError(t, err)
			original, readErr := afero.ReadFile(base, stage.siblings[0].sourcePath)
			require.NoError(t, readErr)
			require.Equal(t, string(original), string(b))
		})
	}
}
