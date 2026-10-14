package organizer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type residentFollowingFS struct{ afero.Fs }

func (f residentFollowingFS) LstatIfPossible(p string) (os.FileInfo, bool, error) {
	i, e := f.Fs.Stat(p)
	return i, false, e
}

func TestResidentPrimaryExecutionProof(t *testing.T) {
	for _, willMove := range []bool{false, true} {
		for _, kind := range []string{"resident", "no lstat", "following lookup", "missing", "directory", "proof refusal"} {
			t.Run(kind, func(t *testing.T) {
				base := afero.NewOsFs()
				root := t.TempDir()
				source, target := filepath.Join(root, "source.mp4"), filepath.Join(root, "target.mp4")
				require.NoError(t, afero.WriteFile(base, source, []byte("video"), 0600))
				if kind == "directory" {
					require.NoError(t, base.Mkdir(target, 0700))
				} else if kind != "missing" {
					require.NoError(t, afero.WriteFile(base, target, []byte("video"), 0600))
				}
				var fs afero.Fs = base
				if kind == "no lstat" {
					fs = struct{ afero.Fs }{base}
				}
				if kind == "following lookup" {
					fs = residentFollowingFS{base}
				}
				org := &Organizer{fs: fs, config: &Config{}}
				plan := &OrganizePlan{SourcePath: source, TargetPath: target, TargetDir: root, WillMove: willMove, strategy: strategyOrganize}
				calls := 0
				refused := errors.New("admission changed")
				plan.BindResidentSource(fsutil.VerifiedSourceProof(func(path string, info os.FileInfo) error {
					calls++
					if kind == "proof refusal" {
						return refused
					}
					return nil
				}))
				result, err := org.execute(plan)
				if kind == "resident" {
					require.NoError(t, err)
					require.Equal(t, 2, calls)
					require.True(t, result.PrimaryResident)
					require.False(t, result.Moved)
					require.Nil(t, result.InstalledIdentity)
					require.True(t, result.ShouldGenerateMetadata)
				} else {
					require.Error(t, err)
				}
				body, err := afero.ReadFile(base, source)
				require.NoError(t, err)
				require.Equal(t, "video", string(body))
			})
		}
	}
}
