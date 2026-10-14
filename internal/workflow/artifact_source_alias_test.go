package workflow

import (
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

type artifactAliasProbeFS struct {
	afero.Fs
	probe func(string) (os.FileInfo, bool, error)
}

func (f *artifactAliasProbeFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	return f.probe(path)
}

func TestArtifactStageResidentAliasProof(t *testing.T) {
	for _, kind := range []string{"hardlink", "no lstat capability", "source lookup error", "source followed", "source weak", "source directory", "source symlink", "target lookup error", "target followed", "target weak", "target symlink", "different inode same metadata", "source replaced after target", "source recheck error", "source recheck followed"} {
		t.Run(kind, func(t *testing.T) {
			base := afero.NewOsFs()
			dir := t.TempDir()
			source, target := filepath.Join(dir, "source.mkv"), filepath.Join(dir, "target.mkv")
			require.NoError(t, afero.WriteFile(base, source, []byte("video"), 0644))
			require.NoError(t, os.Link(source, target))
			original, err := os.Lstat(source)
			require.NoError(t, err)
			identity := captureArtifactSourceIdentity(base, source, original)
			require.True(t, identity.hasDevIno, "native files must expose eager strong identity")
			switch kind {
			case "source directory":
				require.NoError(t, base.Remove(source))
				require.NoError(t, base.Mkdir(source, 0755))
			case "source symlink", "target symlink":
				path, referent := target, source
				if kind == "source symlink" {
					path, referent = source, target
				}
				require.NoError(t, base.Remove(path))
				if err := os.Symlink(referent, path); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			case "different inode same metadata":
				require.NoError(t, base.Remove(target))
				require.NoError(t, afero.WriteFile(base, target, []byte("video"), original.Mode().Perm()))
				require.NoError(t, base.Chtimes(target, original.ModTime(), original.ModTime()))
			}
			visits := 0
			fs := &artifactAliasProbeFS{Fs: base}
			fs.probe = func(path string) (os.FileInfo, bool, error) {
				if path == source {
					visits++
					if kind == "source lookup error" || (kind == "source recheck error" && visits == 2) {
						return nil, true, os.ErrPermission
					}
				} else if kind == "target lookup error" {
					return nil, true, os.ErrPermission
				}
				info, didLstat, err := base.(afero.Lstater).LstatIfPossible(path)
				if (path == source && kind == "source followed") || (path == target && kind == "target followed") || (path == source && visits == 2 && kind == "source recheck followed") {
					didLstat = false
				}
				if (path == source && kind == "source weak") || (path == target && kind == "target weak") {
					info = weakIdentityInfo{info}
				}
				if path == target && kind == "source replaced after target" {
					require.NoError(t, base.Rename(source, source+".old"))
					require.NoError(t, afero.WriteFile(base, source, []byte("video"), original.Mode().Perm()))
					require.NoError(t, base.Chtimes(source, original.ModTime(), original.ModTime()))
				}
				return info, didLstat, err
			}
			var probe afero.Fs = fs
			if kind == "no lstat capability" {
				probe = struct{ afero.Fs }{base}
			}
			require.Equal(t, kind == "hardlink", artifactSameRegularFile(probe, source, target))
		})
	}
}
