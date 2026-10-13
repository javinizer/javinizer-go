//go:build !windows

package organizer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
)

// A plan bound to the admitted source's permission bits publishes them on
// every lane that streams into staging: the deferred publication copies the
// real source, and the pre-deferral flow never widened a private or read-only
// source on its way into the library (codex P2, PRRT_kwDORn9KaM6pw_EY).
func TestOrganizeStrategy_CopyPublishesAdmittedSourceMode(t *testing.T) {
	root := t.TempDir()
	fs := afero.NewOsFs()
	srcDir := filepath.Join(root, "in")
	src := filepath.Join(srcDir, "m.mp4")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	content := []byte("admitted primary video payload")
	require.NoError(t, os.WriteFile(src, content, 0o600))
	require.NoError(t, os.Chmod(src, 0o640))

	newPlan := func(name string) *OrganizePlan {
		t.Helper()
		dstDir := filepath.Join(root, name, "m")
		require.NoError(t, os.MkdirAll(dstDir, 0o755))
		return &OrganizePlan{
			Match:      models.FileMatchInfo{Path: src, Name: "m.mp4", Extension: ".mp4", MovieID: "m"},
			SourcePath: src, TargetDir: dstDir, TargetPath: filepath.Join(dstDir, "m.mp4"), TargetFile: "m.mp4",
			WillMove: true, LinkMode: LinkModeNone,
		}
	}
	strategy := func() *organizeStrategy {
		return newOrganizeStrategy(fs, &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}, nil, OSLinker{})
	}
	admit := func(string, os.FileInfo) error { return nil }
	permOf := func(t *testing.T, path string) os.FileMode {
		t.Helper()
		info, err := os.Lstat(path)
		require.NoError(t, err)
		return info.Mode().Perm()
	}

	t.Run("bound perm plus digest capture publishes the admitted bits", func(t *testing.T) {
		plan := newPlan("digest")
		plan.BindVerifiedSource(admit)
		plan.BindCopySourcePerm(0o640)
		plan.BindCopyDigestCapture()
		res, err := strategy().Execute(plan)
		require.NoError(t, err)
		assert.NotEmpty(t, res.PrimaryCopySHA256)
		assert.Equal(t, os.FileMode(0o640), permOf(t, plan.TargetPath), "a 0640 source stays private")
		require.NotNil(t, res.InstalledIdentity, "the publish-proven identity rides the result")
		info, statErr := os.Lstat(plan.TargetPath)
		require.NoError(t, statErr)
		assert.True(t, os.SameFile(res.InstalledIdentity.FileInfo(), info), "the identity IS the published object")
		got, readErr := os.ReadFile(plan.TargetPath)
		require.NoError(t, readErr)
		assert.Equal(t, content, got)
	})

	t.Run("bound perm without capture publishes the admitted bits", func(t *testing.T) {
		plan := newPlan("plain")
		plan.BindVerifiedSource(admit)
		plan.BindCopySourcePerm(0o640)
		res, err := strategy().Execute(plan)
		require.NoError(t, err)
		assert.Empty(t, res.PrimaryCopySHA256)
		assert.Equal(t, os.FileMode(0o640), permOf(t, plan.TargetPath), "the plain verified leg honors the same mode")
		require.NotNil(t, res.InstalledIdentity)
		info, statErr := os.Lstat(plan.TargetPath)
		require.NoError(t, statErr)
		assert.True(t, os.SameFile(res.InstalledIdentity.FileInfo(), info))
	})

	t.Run("an unbound plan keeps the staging default", func(t *testing.T) {
		plan := newPlan("unbound")
		plan.BindVerifiedSource(admit)
		_, err := strategy().Execute(plan)
		require.NoError(t, err)
		assert.Equal(t, fsutil.StagingFileMode(), permOf(t, plan.TargetPath))
	})

	t.Run("the hard-link lane hands back the linked entry", func(t *testing.T) {
		plan := newPlan("link")
		plan.LinkMode = LinkModeHard
		plan.BindVerifiedSource(admit)
		plan.BindCopySourcePerm(0o640)
		res, err := strategy().Execute(plan)
		require.NoError(t, err)
		require.NotNil(t, res.InstalledIdentity, "the verified link re-proves the entry it installed and hands it back")
		info, statErr := os.Lstat(plan.TargetPath)
		require.NoError(t, statErr)
		assert.True(t, os.SameFile(res.InstalledIdentity.FileInfo(), info))
		assert.Equal(t, os.FileMode(0o640), permOf(t, plan.TargetPath))
	})

	t.Run("the move lane's same-volume rename carries the bits", func(t *testing.T) {
		plan := newPlan("move")
		plan.moveFiles = true
		plan.BindVerifiedSource(admit)
		plan.BindCopySourcePerm(0o640)
		res, err := strategy().Execute(plan)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o640), permOf(t, plan.TargetPath))
		require.NotNil(t, res.InstalledIdentity, "the move lane hands back the object it installed")
		info, statErr := os.Lstat(plan.TargetPath)
		require.NoError(t, statErr)
		assert.True(t, os.SameFile(res.InstalledIdentity.FileInfo(), info))
		exists, existsErr := afero.Exists(fs, src)
		require.NoError(t, existsErr)
		assert.False(t, exists, "the move lane consumed the source")
	})
}
