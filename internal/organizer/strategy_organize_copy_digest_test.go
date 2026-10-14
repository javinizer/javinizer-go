package organizer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

// The copy leg's digest capture tees the payload's sha256 off the single
// publish stream and returns it on the result — the fenced deferred
// publication seals its interim partial pin with exactly these bytes
// (PRRT_kwDORn9KaM6nBUrF). Bound plans only; every other lane stays
// untouched.
func TestOrganizeStrategy_VerifiedCopyDigestCapture(t *testing.T) {
	root := t.TempDir()
	fs := afero.NewOsFs()
	src := filepath.Join(root, "in", "m.mp4")
	dstDir := filepath.Join(root, "out", "m")
	dst := filepath.Join(dstDir, "m.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.MkdirAll(dstDir, 0o755))
	content := []byte("admitted primary video payload")
	require.NoError(t, os.WriteFile(src, content, 0o644))
	sum := sha256.Sum256(content)
	want := hex.EncodeToString(sum[:])

	newPlan := func() *OrganizePlan {
		return &OrganizePlan{
			Match:      models.FileMatchInfo{Path: src, Name: "m.mp4", Extension: ".mp4", MovieID: "m"},
			SourcePath: src, TargetDir: dstDir, TargetPath: dst, TargetFile: "m.mp4",
			WillMove: true, LinkMode: LinkModeNone,
		}
	}
	strategy := func() *organizeStrategy {
		return newOrganizeStrategy(fs, &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}, nil, OSLinker{})
	}
	admit := func(string, os.FileInfo) error { return nil }

	t.Run("bound proof plus capture tees the digest", func(t *testing.T) {
		plan := newPlan()
		plan.BindVerifiedSource(admit)
		plan.BindCopyDigestCapture()
		res, err := strategy().Execute(plan)
		require.NoError(t, err)
		assert.Equal(t, want, res.PrimaryCopySHA256)
		got, readErr := os.ReadFile(dst)
		require.NoError(t, readErr)
		assert.Equal(t, content, got, "the tee counted the admitted bytes that actually landed")
	})

	t.Run("bound proof without capture carries no digest", func(t *testing.T) {
		plan := newPlan()
		plan.TargetDir = filepath.Join(root, "out2", "m")
		plan.TargetPath = filepath.Join(plan.TargetDir, "m.mp4")
		require.NoError(t, os.MkdirAll(plan.TargetDir, 0o755))
		plan.BindVerifiedSource(admit)
		res, err := strategy().Execute(plan)
		require.NoError(t, err)
		assert.Empty(t, res.PrimaryCopySHA256)
	})

	t.Run("capture without a proof keeps the legacy leg and no digest", func(t *testing.T) {
		plan := newPlan()
		plan.TargetDir = filepath.Join(root, "out3", "m")
		plan.TargetPath = filepath.Join(plan.TargetDir, "m.mp4")
		require.NoError(t, os.MkdirAll(plan.TargetDir, 0o755))
		plan.BindCopyDigestCapture()
		res, err := strategy().Execute(plan)
		require.NoError(t, err)
		assert.Empty(t, res.PrimaryCopySHA256, "the digest rides the verified leg only (documented constraint)")
		assert.FileExists(t, plan.TargetPath)
	})

	t.Run("a refusing proof publishes nothing and no digest", func(t *testing.T) {
		sentinel := errors.New("not the admitted object")
		plan := newPlan()
		plan.TargetDir = filepath.Join(root, "out4", "m")
		plan.TargetPath = filepath.Join(plan.TargetDir, "m.mp4")
		require.NoError(t, os.MkdirAll(plan.TargetDir, 0o755))
		plan.BindVerifiedSource(func(string, os.FileInfo) error { return sentinel })
		plan.BindCopyDigestCapture()
		res, err := strategy().Execute(plan)
		require.Error(t, err)
		assert.Empty(t, res.PrimaryCopySHA256)
		_, statErr := os.Stat(plan.TargetPath)
		assert.True(t, os.IsNotExist(statErr))
	})

	t.Run("a move lane never engages the capture", func(t *testing.T) {
		plan := newPlan()
		plan.TargetDir = filepath.Join(root, "out5", "m")
		plan.TargetPath = filepath.Join(plan.TargetDir, "m.mp4")
		require.NoError(t, os.MkdirAll(plan.TargetDir, 0o755))
		plan.moveFiles = true
		plan.BindVerifiedSource(admit)
		plan.BindCopyDigestCapture()
		res, err := strategy().Execute(plan)
		require.NoError(t, err)
		assert.Empty(t, res.PrimaryCopySHA256, "moves carry no digest — same-volume renames stream nothing")
	})
}

// The symlink pin and the installed link MUST carry the same payload: the
// strategy's soft leg resolves its target through SymlinkLinkTarget, the
// same route the workflow's delete-intent pin uses.
func TestOrganizeStrategy_SoftLinkPayloadMatchesSharedComputation(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/in/m.mp4", []byte("v"), 0o644))
	linker := &MemLinker{}
	strategy := newOrganizeStrategy(fs, &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}, nil, linker)
	dstDir := "/out/m"
	dst := filepath.Join(dstDir, "m.mp4")
	require.NoError(t, fs.MkdirAll(dstDir, 0o755))
	plan := &OrganizePlan{
		Match:      models.FileMatchInfo{Path: "/in/m.mp4", Name: "m.mp4", Extension: ".mp4", MovieID: "m"},
		SourcePath: "/in/m.mp4", TargetDir: dstDir, TargetPath: dst, TargetFile: "m.mp4",
		WillMove: true, LinkMode: LinkModeSoft,
	}
	_, err := strategy.Execute(plan)
	require.NoError(t, err)
	require.Len(t, linker.Links, 1)
	want, wantErr := SymlinkLinkTarget("/in/m.mp4")
	require.NoError(t, wantErr)
	assert.Equal(t, want, linker.Links[0].OldName, "one computation route feeds the install and the pin")
	assert.Equal(t, dst, linker.Links[0].NewName)
}

func TestSymlinkLinkTarget_Computation(t *testing.T) {
	t.Run("absolute passthrough", func(t *testing.T) {
		abs := filepath.Join(t.TempDir(), "already", "absolute", "m.mkv")
		require.True(t, filepath.IsAbs(abs))
		got, err := SymlinkLinkTarget(abs)
		require.NoError(t, err)
		assert.Equal(t, abs, got)
	})
	t.Run("relative resolves through the seam", func(t *testing.T) {
		got, err := SymlinkLinkTarget("relative/m.mkv")
		require.NoError(t, err)
		assert.True(t, filepath.IsAbs(got))
	})
	t.Run("absolutization failure surfaces", func(t *testing.T) {
		sentinel := errors.New("getwd wedged")
		prev := filepathAbsFn
		filepathAbsFn = func(string) (string, error) { return "", sentinel }
		t.Cleanup(func() { filepathAbsFn = prev })
		_, err := SymlinkLinkTarget("relative/m.mkv")
		require.ErrorIs(t, err, sentinel)
		assert.Contains(t, err.Error(), "failed to resolve source path for symlink",
			"the leg's error text is unchanged so equivalence-based callers keep classifying")
	})
}
