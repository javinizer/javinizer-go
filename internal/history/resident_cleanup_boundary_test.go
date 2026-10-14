package history

import (
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

type residentCleanupMaskedInfo struct{ os.FileInfo }

func (residentCleanupMaskedInfo) Sys() any { return nil }

type residentCleanupMaskedFS struct{ afero.Fs }

func (f residentCleanupMaskedFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	info, follow, err := f.Fs.(afero.Lstater).LstatIfPossible(path)
	if err != nil {
		return nil, follow, err
	}
	return residentCleanupMaskedInfo{info}, follow, nil
}

func TestResidentCleanupAliasBoundaryRequiresNativeDirectoryProof(t *testing.T) {
	for _, scenario := range []string{"match", "empty-stop", "dot-stop", "no-lstat", "weak", "masked-native", "missing-stop", "missing-child", "file-stop", "file-child", "foreign-root", "following", "symlink-child"} {
		t.Run(scenario, func(t *testing.T) {
			var fs afero.Fs = afero.NewOsFs()
			root := t.TempDir()
			if scenario == "weak" {
				fs = afero.NewMemMapFs()
			}
			stop := filepath.Join(root, "resident")
			dir := filepath.Join(stop, "deep")
			require.NoError(t, fs.MkdirAll(dir, 0755))
			switch scenario {
			case "empty-stop":
				stop = ""
			case "dot-stop":
				stop = "."
			case "no-lstat":
				fs = struct{ afero.Fs }{fs}
			case "masked-native":
				fs = residentCleanupMaskedFS{fs}
			case "missing-stop":
				stop = filepath.Join(root, "missing")
			case "missing-child":
				dir = filepath.Join(root, "missing")
			case "file-stop":
				stop = filepath.Join(root, "file")
				require.NoError(t, afero.WriteFile(fs, stop, []byte("file"), 0644))
			case "file-child":
				dir = filepath.Join(root, "file")
				require.NoError(t, afero.WriteFile(fs, dir, []byte("file"), 0644))
			case "foreign-root":
				dir = filepath.Join(root, "other", "deep")
				require.NoError(t, fs.MkdirAll(dir, 0755))
			case "following":
				fs = &residentDirFaultFS{Fs: fs, fault: "following"}
			case "symlink-child":
				dir = filepath.Join(root, "link")
				if err := os.Symlink(stop, dir); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			got := residentCleanupAliasBoundary(fs, dir, stop)
			if scenario == "match" {
				require.Equal(t, stop, got)
			} else {
				require.Empty(t, got)
			}
		})
	}
}

func TestResidentCleanupCaseSensitiveRootsStayDistinct(t *testing.T) {
	fs := afero.NewOsFs()
	root := t.TempDir()
	lower := filepath.Join(root, "resident")
	upper := filepath.Join(root, "RESIDENT")
	require.NoError(t, fs.Mkdir(lower, 0755))
	if _, err := fs.Stat(upper); err == nil {
		t.Skip("fixture requires a case-sensitive volume")
	}
	require.NoError(t, fs.Mkdir(upper, 0755))
	branch := filepath.Join(upper, "deep")
	require.NoError(t, fs.Mkdir(branch, 0755))
	require.Empty(t, residentCleanupAliasBoundary(fs, branch, lower), "spelling similarity must not authorize another native directory")
	require.DirExists(t, lower)
	require.DirExists(t, branch)
}
