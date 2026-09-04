package organizer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

// Authorization scoping (#224): force update suppresses only ConflictFile.
// Directories and symlinks still conflict, regardless of authorization mode.

func dirPlantFixture(t *testing.T) (afero.Fs, string, string) {
	t.Helper()
	fs := afero.NewMemMapFs()
	require.NoError(t, os.MkdirAll("/", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/in/m.mp4", []byte("new"), 0o644))
	require.NoError(t, fs.MkdirAll("/out/m", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/out/m/m.mp4", []byte("existing"), 0o644))
	return fs, "/in/m.mp4", "/out/m/m.mp4"
}

func TestAuthScope_AuthorizedFileReplaceStillWorks(t *testing.T) {
	fs, src, dst := dirPlantFixture(t)

	strategy := newOrganizeStrategy(fs, &Config{
		FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true,
	}, nil, &MemLinker{})
	plan := &OrganizePlan{
		Match:      models.FileMatchInfo{Path: src, Name: "m.mp4", Extension: ".mp4", MovieID: "m"},
		SourcePath: src, TargetDir: filepath.Dir(dst), TargetPath: dst, TargetFile: "m.mp4",
		WillMove: true, moveFiles: true, overwriteAuthorized: true,
	}
	res, err := strategy.Execute(plan)
	require.NoError(t, err)
	require.True(t, res.Moved)
	content, _ := afero.ReadFile(fs, dst)
	assert.Equal(t, "new", string(content))
}

func TestAuthScope_MoveOntoSymlinkRefusedEvenAuthorized(t *testing.T) {
	dir := t.TempDir()
	fs := afero.NewOsFs()
	src := filepath.Join(dir, "in", "m.mp4")
	dstDir := filepath.Join(dir, "out", "m")
	dst := filepath.Join(dstDir, "m.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.MkdirAll(dstDir, 0o755))
	require.NoError(t, os.WriteFile(src, []byte("new"), 0o644))
	foreignTarget := filepath.Join(dir, "real target")
	require.NoError(t, os.WriteFile(foreignTarget, []byte("foreign"), 0o644))
	require.NoError(t, os.Symlink(foreignTarget, dst)) // dangling-*capable* — here it's a live symlink

	strategy := newOrganizeStrategy(fs, &Config{
		FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true,
	}, nil, &MemLinker{})
	plan := &OrganizePlan{
		Match:      models.FileMatchInfo{Path: src, Name: "m.mp4", Extension: ".mp4", MovieID: "m"},
		SourcePath: src, TargetDir: dstDir, TargetPath: dst, TargetFile: "m.mp4",
		WillMove: true, moveFiles: true, overwriteAuthorized: true,
	}

	res, err := strategy.Execute(plan)
	require.Error(t, err, "symlink destination must refuse even when authorized")
	require.NotNil(t, res)
	assert.Error(t, res.Error)
	assert.False(t, res.Moved)
	// The symlink object is intact.
	info, lerr := os.Lstat(dst)
	require.NoError(t, lerr)
	require.NotZero(t, info.Mode()&os.ModeSymlink)
	content, _ := os.ReadFile(foreignTarget)
	assert.Equal(t, "foreign", string(content))
}

// Plan-level kind matrix: each occupant kind, with force authorization applied.
// Destination entity placed at the exact destination address computed by the
// strategy (destDir/<ID>/m.mp4 for the file kind, destDir/<ID> for dir kind,
// symlink at the target path).
func TestAuthScope_PlanKindMatrix(t *testing.T) {
	cases := []struct {
		name  string
		plant func(dir string) (destDir string)
		kinds []ConflictKind
	}{
		{"regular file + force", func(dir string) string {
			destDir := filepath.Join(dir, "target1")
			require.NoError(t, os.MkdirAll(filepath.Join(destDir, "m"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(destDir, "m", "m.mp4"), []byte("x"), 0o644))
			return destDir
		}, nil},
		{"dir dest + force", func(dir string) string {
			destDir := filepath.Join(dir, "target2")
			require.NoError(t, os.MkdirAll(destDir, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(destDir, "m"), []byte("i am a file at folder address"), 0o644))
			return destDir
		}, []ConflictKind{ConflictDirectory}},
		{"symlink + force", func(dir string) string {
			destDir := filepath.Join(dir, "target3")
			require.NoError(t, os.MkdirAll(filepath.Join(destDir, "m"), 0o755))
			require.NoError(t, os.Symlink(filepath.Join(dir, "dangling.mp4"), filepath.Join(destDir, "m", "m.mp4")))
			return destDir
		}, []ConflictKind{ConflictSymlink}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			fs := afero.NewOsFs()
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "in"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "in", "m.mp4"), []byte("v"), 0o644))

			destDir := tc.plant(dir)
			strategy := newOrganizeStrategy(fs, &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}, nil, &MemLinker{})
			match := models.FileMatchInfo{Path: filepath.Join(dir, "in", "m.mp4"), Name: "m.mp4", Extension: ".mp4", MovieID: "m"}
			movie := &models.Movie{ID: "m"}
			plan, err := strategy.Plan(match, movie, destDir, true) // force
			require.NoError(t, err)

			var kinds []ConflictKind
			for _, c := range plan.Conflicts {
				kinds = append(kinds, c.Kind)
			}
			assert.Equal(t, tc.kinds, kinds)
		})
	}
}

// Authorized in-place norenamefolder onto a live symlink refuses (leg 4 classification).
func TestAuthScope_AuthorizedInPlaceOntoSymlinkRefused(t *testing.T) {
	dir := t.TempDir()
	fs := afero.NewOsFs()
	src := filepath.Join(dir, "in", "ABC-123.mp4")
	dstAS := filepath.Join(dir, "in", "ABC-123-renamed.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("mine"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "foreign.mp4"), []byte("foreign"), 0o644))
	require.NoError(t, os.Symlink(filepath.Join(dir, "foreign.mp4"), dstAS))

	cfg := &Config{FileFormat: "<ID>", FolderFormat: "<ID>", RenameFile: true}
	strategy := newInPlaceNoRenameFolderStrategy(fs, cfg, nil, nil)
	plan := &OrganizePlan{
		SourcePath: src, TargetDir: filepath.Dir(src), TargetPath: dstAS, TargetFile: "ABC-123-renamed.mp4",
		WillMove: true, overwriteAuthorized: true,
	}
	res, err := strategy.Execute(plan)
	_ = res
	require.Error(t, err, "symlink destination refuses even when authorized (leg 4)")
	// Symlink object intact.
	info, _ := os.Lstat(dstAS)
	assert.NotZero(t, info.Mode()&os.ModeSymlink)
}

// plainStatFs (in organize_pathclass_coverage_test.go) hides Lstater — the
// dangling symlink probe's Stat-fallback leg must still classify as symlink.

// Authorized same-inode alias stays no-op under authorization (new lane on the
// execute move legs per #224).
func TestAuthScope_AuthorizedSameInodeNoOp(t *testing.T) {
	dir := t.TempDir()
	fs := afero.NewOsFs()
	src := filepath.Join(dir, "in", "ABC-123.mp4")
	dst := filepath.Join(dir, "out", "ABC-123", "ABC-123.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("video"), 0o644))
	require.NoError(t, os.Link(src, dst))

	strategy := newOrganizeStrategy(fs, &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}, nil, &MemLinker{})
	plan := &OrganizePlan{
		Match:      models.FileMatchInfo{Path: src, Name: "ABC-123.mp4", Extension: ".mp4", MovieID: "ABC-123"},
		SourcePath: src, TargetDir: filepath.Dir(dst), TargetPath: dst, TargetFile: "ABC-123.mp4",
		WillMove: true, moveFiles: true, overwriteAuthorized: true,
	}
	res, err := strategy.Execute(plan)
	require.NoError(t, err)
	require.NotNil(t, res)
	_, srcErr := os.Stat(src)
	assert.NoError(t, srcErr, "authorized move onto hardlink alias stays no-op (source preserved)")
}

// Authorized hardlink install onto an empty directory refuses; the dir is never
// deleted to make room for the link (hole #1 closure per #224).
func TestAuthScope_AuthorizedLinkOntoEmptyDirRefused(t *testing.T) {
	dir := t.TempDir()
	fs := afero.NewOsFs()
	src := filepath.Join(dir, "in", "ABC-123.mp4")
	dst := filepath.Join(dir, "out", "ABC-123", "ABC-123.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("video"), 0o644))
	require.NoError(t, os.MkdirAll(dst, 0o755)) // empty dir occupying the link target

	strategy := newOrganizeStrategy(fs, &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}, nil, &MemLinker{})
	plan := &OrganizePlan{
		Match:      models.FileMatchInfo{Path: src, Name: "ABC-123.mp4", Extension: ".mp4", MovieID: "ABC-123"},
		SourcePath: src, TargetDir: filepath.Dir(dst), TargetPath: dst, TargetFile: "ABC-123.mp4",
		WillMove: true, moveFiles: false, LinkMode: LinkModeHard, overwriteAuthorized: true,
	}
	_, err := strategy.Execute(plan)
	require.Error(t, err, "empty dir destination refuses even under authorized link install")
	assert.Contains(t, err.Error(), "not a regular file")
	info, statErr := os.Stat(dst)
	require.NoError(t, statErr, "dir intact")
	assert.True(t, info.IsDir(), "empty dir preserved")
}

// Parity pin: authorized copy onto a regular file overwrites as before
// (file-kind authorization lane).
func TestAuthScope_AuthorizedCopyOntoRegularFileReplaces(t *testing.T) {
	dir := t.TempDir()
	fs := afero.NewOsFs()
	src := filepath.Join(dir, "in", "ABC-123.mp4")
	dst := filepath.Join(dir, "out", "ABC-123", "ABC-123.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("new"), 0o644))
	require.NoError(t, os.WriteFile(dst, []byte("old"), 0o644))

	strategy := newOrganizeStrategy(fs, &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}, nil, &MemLinker{})
	plan := &OrganizePlan{
		Match:      models.FileMatchInfo{Path: src, Name: "ABC-123.mp4", Extension: ".mp4", MovieID: "ABC-123"},
		SourcePath: src, TargetDir: filepath.Dir(dst), TargetPath: dst, TargetFile: "ABC-123.mp4",
		WillMove: true, moveFiles: false, LinkMode: LinkModeNone, overwriteAuthorized: true,
	}
	_, err := strategy.Execute(plan)
	require.NoError(t, err)
	content, _ := os.ReadFile(dst)
	assert.Equal(t, "new", string(content))
}

// Authorized hardlink onto a SYMLINK occupant refuses + the link object is
// never deleted (the IsRegular gate also covers symlink destinations — #224 hole-1).
func TestAuthScope_AuthorizedLinkOntoSymlinkRefused(t *testing.T) {
	dir := t.TempDir()
	fs := afero.NewOsFs()
	src := filepath.Join(dir, "in", "ABC-123.mp4")
	dst := filepath.Join(dir, "out", "ABC-123", "ABC-123.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("mine"), 0o644))
	foreign := filepath.Join(dir, "foreign.mp4")
	require.NoError(t, os.WriteFile(foreign, []byte("foreign"), 0o644))
	require.NoError(t, os.Symlink(foreign, dst))

	strategy := newOrganizeStrategy(fs, &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}, nil, &MemLinker{})
	plan := &OrganizePlan{
		Match:      models.FileMatchInfo{Path: src, Name: "ABC-123.mp4", Extension: ".mp4", MovieID: "ABC-123"},
		SourcePath: src, TargetDir: filepath.Dir(dst), TargetPath: dst, TargetFile: "ABC-123.mp4",
		WillMove: true, moveFiles: false, LinkMode: LinkModeHard, overwriteAuthorized: true,
	}
	_, err := strategy.Execute(plan)
	require.Error(t, err, "authorized install onto a symlink must refuse")
	assert.Contains(t, err.Error(), "regular file")
	info, lerr := os.Lstat(dst)
	require.NoError(t, lerr)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "the symlink object must remain")
}

// Authorized dedicated in-place rename: refuse + rollback when the inner rename
// destination is a live symlink. The dir must roll back and both the source and
// the foreign occupant stay intact.

func TestPlanCheckTargetConflict_DanglingViaStatFallback(t *testing.T) {
	dir := t.TempDir()
	ofs := afero.NewOsFs()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src", "m.mp4"), []byte("mine"), 0o644))
	dst := filepath.Join(dir, "dest", "m", "m.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(dir, "dangling-target.mp4"), dst))

	wrapped := plainStatFs{Fs: ofs}
	conflicts := checkTargetConflict(wrapped, filepath.Join(dir, "src", "m.mp4"), dst, true, true)
	require.Len(t, conflicts, 1)
	assert.Equal(t, ConflictSymlink, conflicts[0].Kind)
}
