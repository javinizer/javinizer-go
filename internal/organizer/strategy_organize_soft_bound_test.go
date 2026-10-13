package organizer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

func TestOrganizeSoftLinkReturnsPublishBoundIdentity(t *testing.T) {
	for _, dangling := range []bool{false, true} {
		t.Run(map[bool]string{false: "regular", true: "dangling"}[dangling], func(t *testing.T) {
			fs := afero.NewOsFs()
			dir := t.TempDir()
			src, dst := filepath.Join(dir, "input.mp4"), filepath.Join(dir, "output.mp4")
			if !dangling {
				require.NoError(t, afero.WriteFile(fs, src, []byte("owned source"), 0o600))
			}
			probe := filepath.Join(dir, "probe")
			if err := os.Symlink(src, probe); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			require.NoError(t, os.Remove(probe))
			strategy := newOrganizeStrategy(fs, &Config{}, nil, OSLinker{})
			plan := &OrganizePlan{SourcePath: src, TargetPath: dst, TargetDir: dir, WillMove: true, LinkMode: LinkModeSoft}
			result, err := strategy.Execute(plan)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotNil(t, result.InstalledIdentity)
			require.Equal(t, src, result.InstalledIdentity.LinkTarget())
			require.Nil(t, result.InstalledIdentity.FileInfo())
			_, err = fsutil.ObserveVerifiedInstall(fs, dst, result.InstalledIdentity)
			require.NoError(t, err)
		})
	}
}
