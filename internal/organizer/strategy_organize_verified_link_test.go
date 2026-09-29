package organizer

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
)

// The LinkModeHard install leg consults a bound admission proof exactly like
// the move/copy verified legs (codex P1, PRRT_kwDORn9KaM6nEnUw): the link
// shares its inode with the source object, so the admitted identity is wired
// into the install itself rather than only into the validation gate.

func verifiedLinkPlan(src, dstDir, dst string) *OrganizePlan {
	return &OrganizePlan{
		Match:      models.FileMatchInfo{Path: src, Name: filepath.Base(src), Extension: filepath.Ext(src), MovieID: "m"},
		SourcePath: src, TargetDir: dstDir, TargetPath: dst, TargetFile: filepath.Base(dst),
		WillMove: true, moveFiles: false, LinkMode: LinkModeHard,
		Conflicts: []PlanConflict{},
	}
}

// Happy path: the bound install lands a link that provably aliases the
// admitted source object (os.SameFile — the destination IS the admitted
// object), the source is retained, and the result is clean.
func TestOrganizeStrategy_VerifiedHardLinkInstallsAdmittedObject(t *testing.T) {
	dir := t.TempDir()
	fs := afero.NewOsFs()
	src := filepath.Join(dir, "in", "m.mp4")
	dstDir := filepath.Join(dir, "out", "m")
	dst := filepath.Join(dstDir, "m.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("admitted video bytes"), 0o644))
	admitted, err := os.Stat(src)
	require.NoError(t, err)
	verdict := errors.New("not the admitted object")
	proof := func(_ string, info os.FileInfo) error {
		if info == nil || !os.SameFile(admitted, info) {
			return verdict
		}
		return nil
	}
	strategy := newOrganizeStrategy(fs, &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}, nil, OSLinker{})
	plan := verifiedLinkPlan(src, dstDir, dst)
	plan.BindVerifiedSource(proof)

	res, err := strategy.Execute(plan)
	require.NoError(t, err)
	assert.True(t, res.Moved)
	assert.Empty(t, res.Warnings)
	dstInfo, statErr := os.Stat(dst)
	require.NoError(t, statErr)
	assert.True(t, os.SameFile(admitted, dstInfo), "the install aliases the admitted object — the identity the post-link proof certified")
	srcInfo, statErr := os.Stat(src)
	require.NoError(t, statErr, "a link install never consumes the source")
	assert.True(t, os.SameFile(admitted, srcInfo))
}

// The refusal pinned at the wiring level: a source swapped onto a foreign
// object after validation is NEVER linked — the leg refuses before the link
// verb runs (the linker seam records no call), the typed admission class
// rides out, and the destination stays vacant.
func TestOrganizeStrategy_VerifiedHardLinkRefusesSwappedSourceBeforeLink(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/in/m.mp4", []byte("video"), 0o644))
	// Scalars, not the FileInfo: in-memory afero FileInfos are live views
	// whose Size() would follow the swap itself.
	info, err := fs.Stat("/in/m.mp4")
	require.NoError(t, err)
	admittedSize, admittedMod := info.Size(), info.ModTime()
	verdict := errors.New("not the admitted object")
	proof := func(_ string, got os.FileInfo) error {
		if got == nil || got.Size() != admittedSize || !got.ModTime().Equal(admittedMod) {
			return verdict
		}
		return nil
	}
	// The post-validation swap: the source name now names a foreign object.
	require.NoError(t, afero.WriteFile(fs, "/in/m.mp4", []byte("replacement video — different size"), 0o644))

	ml := &mockLinker{}
	strategy := newOrganizeStrategy(fs, &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}, nil, ml)
	plan := verifiedLinkPlan("/in/m.mp4", "/out/m", "/out/m/m.mp4")
	plan.BindVerifiedSource(proof)

	res, err := strategy.Execute(plan)
	require.Error(t, err)
	assert.True(t, errors.Is(err, fsutil.ErrTakeAsideForeign), "the typed refusal carries: %v", err)
	assert.True(t, errors.Is(err, verdict), "the admission verdict rides the refusal: %v", err)
	assert.False(t, fsutil.PublishRefusal(err), "a pre-publish admission refusal never classifies as a publish refusal")
	assert.False(t, fsutil.PublishCompleted(err), "nothing reached the destination")
	assert.False(t, res.Moved)
	assert.False(t, ml.hardlinkCalled, "the link verb must never run for an unadmitted source")
	exists, _ := afero.Exists(fs, "/out/m/m.mp4")
	assert.False(t, exists)
	got, _ := afero.ReadFile(fs, "/in/m.mp4")
	assert.Equal(t, "replacement video — different size", string(got), "the foreign object stays byte-intact at the source name")
}

// The EXDEV / permission / generic classification the legacy leg carried
// rides the verified leg unchanged — the raw link error passes through the
// composite unwrapped.
func TestOrganizeStrategy_VerifiedHardLinkErrorClassesMatchLegacy(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"EXDEV", syscall.EXDEV, "source and destination must be on the same filesystem"},
		{"permission", os.ErrPermission, "permission denied"},
		{"generic", errors.New("link failed"), "failed to create hard link"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			require.NoError(t, afero.WriteFile(fs, "/in/m.mp4", []byte("video"), 0o644))
			ml := &mockLinker{hardlinkErr: tt.err}
			strategy := newOrganizeStrategy(fs, &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}, nil, ml)
			plan := verifiedLinkPlan("/in/m.mp4", "/out/m", "/out/m/m.mp4")
			plan.BindVerifiedSource(func(string, os.FileInfo) error { return nil })

			_, err := strategy.Execute(plan)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			assert.True(t, errors.Is(err, tt.err), "the raw class stays reachable: %v", err)
			assert.True(t, ml.hardlinkCalled, "the admitted source did reach the link verb")
		})
	}
}

// The swap subject's view of a swap that wins the validation→link window: the
// installed entry fails the post-link admission proof and the rejected
// install is compensated off the destination — ErrTakeAsideForeign is what
// the caller sees, never a silently published foreign object.
func TestOrganizeStrategy_VerifiedHardLinkForeignInstallCompensated(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/in/m.mp4", []byte("video"), 0o644))
	info, err := fs.Stat("/in/m.mp4")
	require.NoError(t, err)
	admittedSize, admittedMod := info.Size(), info.ModTime()
	verdict := errors.New("not the admitted object")
	proof := func(_ string, got os.FileInfo) error {
		if got == nil || got.Size() != admittedSize || !got.ModTime().Equal(admittedMod) {
			return verdict
		}
		return nil
	}
	linker := &foreignPlantLinker{fs: fs, plant: []byte("foreign replacement — a different size")}
	strategy := newOrganizeStrategy(fs, &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}, nil, linker)
	plan := verifiedLinkPlan("/in/m.mp4", "/out/m", "/out/m/m.mp4")
	plan.BindVerifiedSource(proof)

	res, err := strategy.Execute(plan)
	require.Error(t, err)
	assert.True(t, errors.Is(err, fsutil.ErrTakeAsideForeign), "the swap subject sees the typed refusal: %v", err)
	assert.False(t, res.Moved)
	assert.True(t, linker.called, "the kernel resolved the link against the swapped name")
	exists, _ := afero.Exists(fs, "/out/m/m.mp4")
	assert.False(t, exists, "the rejected install was unlinked off the destination")
	got, _ := afero.ReadFile(fs, "/in/m.mp4")
	assert.Equal(t, "video", string(got), "the link lane consumes nothing at the source")
}

// foreignPlantLinker models link(2) resolving the source NAME at the link
// instant after a swap won the open→link window: the destination lands as the
// foreign replacement object.
type foreignPlantLinker struct {
	MemLinker
	fs     afero.Fs
	plant  []byte
	called bool
}

func (l *foreignPlantLinker) hardlink(_, newname string) error {
	l.called = true
	return afero.WriteFile(l.fs, newname, l.plant, 0o644)
}
