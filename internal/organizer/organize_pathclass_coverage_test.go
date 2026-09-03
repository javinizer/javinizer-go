package organizer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// forceStatFallbackFs wraps an fs so Lstater reports didLstat=false — exercising the
// Stat-following fallback legs (readlink probes) even when the inner fs supports links.
type forceStatFallbackFs struct{ afero.Fs }

func (f forceStatFallbackFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	info, err := f.Fs.Stat(name)
	return info, false, err
}

func (f forceStatFallbackFs) ReadlinkIfPossible(name string) (string, error) {
	if lr, ok := f.Fs.(afero.LinkReader); ok {
		return lr.ReadlinkIfPossible(name)
	}
	return "", os.ErrNotExist
}

// plainStatFs hides Lstater entirely, so callers take the plain-Stat legs.
type plainStatFs struct{ afero.Fs }

func (f plainStatFs) ReadlinkIfPossible(name string) (string, error) {
	if lr, ok := f.Fs.(afero.LinkReader); ok {
		return lr.ReadlinkIfPossible(name)
	}
	return "", os.ErrNotExist
}

// failStatPathsFs injects errors for chosen cleaned paths on both Stat and Lstat legs.
type failStatPathsFs struct {
	afero.Fs
	fail map[string]error
}

func (f failStatPathsFs) Stat(name string) (os.FileInfo, error) {
	if err, ok := f.fail[filepath.Clean(name)]; ok {
		return nil, err
	}
	return f.Fs.Stat(name)
}

func (f failStatPathsFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if err, ok := f.fail[filepath.Clean(name)]; ok {
		return nil, true, err
	}
	if lst, ok := f.Fs.(afero.Lstater); ok {
		info, did, err := lst.LstatIfPossible(name)
		return info, did, err
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

func (f failStatPathsFs) ReadlinkIfPossible(name string) (string, error) {
	if lr, ok := f.Fs.(afero.LinkReader); ok {
		return lr.ReadlinkIfPossible(name)
	}
	return "", os.ErrNotExist
}

// statCountFs fails the Nth Stat of a target path — used to reach the defensive
// second-stat failure branch after an initial stat succeeded.
type statCountFs struct {
	afero.Fs
	target string
	failOn int
	n      int
}

func (f *statCountFs) Stat(name string) (os.FileInfo, error) {
	if filepath.Clean(name) == f.target {
		f.n++
		if f.n == f.failOn {
			return nil, errors.New("injected transient stat failure")
		}
	}
	return f.Fs.Stat(name)
}

// renamePolicyFs can fail renames selectively and run a hook after specific renames.
type renamePolicyFs struct {
	afero.Fs
	failPair func(oldPath, newPath string) bool
	after    func(oldPath, newPath string)
}

func (f *renamePolicyFs) Rename(oldPath, newPath string) error {
	if f.failPair != nil && f.failPair(oldPath, newPath) {
		return errors.New("injected rename failure")
	}
	err := f.Fs.Rename(oldPath, newPath)
	if err == nil && f.after != nil {
		f.after(oldPath, newPath)
	}
	return err
}

// failRemovePathFs fails Remove for one cleaned path.
type failRemovePathFs struct {
	afero.Fs
	target string
}

func (f failRemovePathFs) Remove(name string) error {
	if filepath.Clean(name) == f.target {
		return errors.New("injected remove failure")
	}
	return f.Fs.Remove(name)
}

// errLinker fails selected link operations.
type errLinker struct {
	hardErr error
	softErr error
	copyErr error
}

func (l errLinker) hardlink(_, _ string) error             { return l.hardErr }
func (l errLinker) symlink(_, _ string) error              { return l.softErr }
func (l errLinker) copyFile(_ afero.Fs, _, _ string) error { return l.copyErr }

// --- refuseExistingDestination: dangling-symlink probe under Stat fallback -----------

func TestRefuseExistingDestination_DanglingSymlinkProbe_StatFallbackLegs(t *testing.T) {
	dir := t.TempDir()
	base := afero.NewOsFs()
	src := filepath.Join(dir, "src.mp4")
	dst := filepath.Join(dir, "dst.mp4")
	require.NoError(t, os.WriteFile(src, []byte("mine"), 0644))
	require.NoError(t, os.Symlink(filepath.Join(dir, "gone.mp4"), dst)) // dangling

	for name, fs := range map[string]afero.Fs{
		"forced didLstat=false": forceStatFallbackFs{base},
		"no Lstater at all":     plainStatFs{base},
	} {
		t.Run(name, func(t *testing.T) {
			identical, sameIn, err := refuseExistingDestination(fs, src, dst)
			require.Error(t, err, "dangling symlink must refuse overwrite on Stat-fallback filesystems")
			assert.Contains(t, err.Error(), "refusing to overwrite")
			assert.False(t, identical)
			assert.False(t, sameIn)
		})
	}
}

// --- pathExistsBestEffort: every leg -------------------------------------------------

func TestPathExistsBestEffort_Legs(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "real.txt"), []byte("x"), 0644))
	require.NoError(t, os.Symlink(filepath.Join(dir, "gone.txt"), filepath.Join(dir, "dangling.txt")))

	mem := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(mem, "/present.txt", []byte("x"), 0644))

	permErr := errors.New("permission denied")

	t.Run("lstater hit", func(t *testing.T) {
		ok, err := pathExistsBestEffort(mem, "/present.txt")
		require.NoError(t, err)
		assert.True(t, ok)
	})
	t.Run("lstater miss with didLstat=false falls through to readlink probe", func(t *testing.T) {
		ok, err := pathExistsBestEffort(mem, "/absent.txt")
		require.NoError(t, err)
		assert.False(t, ok)
	})
	t.Run("lstater miss with true didLstat is authoritative", func(t *testing.T) {
		ok, err := pathExistsBestEffort(afero.NewOsFs(), filepath.Join(dir, "never.txt"))
		require.NoError(t, err)
		assert.False(t, ok)
	})
	t.Run("lstater non-NotExist error surfaces", func(t *testing.T) {
		bad := failStatPathsFs{Fs: mem, fail: map[string]error{"/boom.txt": permErr}}
		ok, err := pathExistsBestEffort(bad, "/boom.txt")
		require.ErrorIs(t, err, permErr)
		assert.False(t, ok)
	})
	t.Run("didLstat=false dangling found via readlink", func(t *testing.T) {
		ok, err := pathExistsBestEffort(forceStatFallbackFs{afero.NewOsFs()}, filepath.Join(dir, "dangling.txt"))
		require.NoError(t, err)
		assert.True(t, ok)
	})
	t.Run("plain stat hit", func(t *testing.T) {
		ok, err := pathExistsBestEffort(plainStatFs{afero.NewOsFs()}, filepath.Join(dir, "real.txt"))
		require.NoError(t, err)
		assert.True(t, ok)
	})
	t.Run("plain stat non-NotExist error surfaces", func(t *testing.T) {
		bad := plainStatFs{failStatPathsFs{Fs: mem, fail: map[string]error{"/boom.txt": permErr}}}
		ok, err := pathExistsBestEffort(bad, "/boom.txt")
		require.ErrorIs(t, err, permErr)
		assert.False(t, ok)
	})
	t.Run("plain stat miss found via readlink", func(t *testing.T) {
		ok, err := pathExistsBestEffort(plainStatFs{afero.NewOsFs()}, filepath.Join(dir, "dangling.txt"))
		require.NoError(t, err)
		assert.True(t, ok)
	})
}

// --- no-rename-folder in-place: lexical self is a no-op -------------------------------

func TestInPlaceNoRenameFolderStrategy_LexicalSelf_NoClobber(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{OperationMode: operationmode.OperationModeInPlace, FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}
	strategy := newInPlaceNoRenameFolderStrategy(fs, cfg, nil, nil)
	require.NoError(t, afero.WriteFile(fs, "/dir/ABC-9.mp4", []byte("stay"), 0644))

	plan := &OrganizePlan{
		SourcePath: "/dir/ABC-9.mp4",
		TargetDir:  "/dir",
		TargetFile: "ABC-9.mp4",
		TargetPath: "/dir/ABC-9.mp4",
		WillMove:   true,
		Match:      models.FileMatchInfo{Path: "/dir/ABC-9.mp4", Name: "ABC-9.mp4"},
	}
	result, err := strategy.Execute(plan)
	require.NoError(t, err, "lexically identical source and target must be a no-op, not a conflict")
	require.NotNil(t, result)
	data, _ := afero.ReadFile(fs, "/dir/ABC-9.mp4")
	assert.Equal(t, []byte("stay"), data)
}

// --- in-place TargetDir classification legs -------------------------------------------

func mkInPlacePlan(oldDir, targetDir string) *OrganizePlan {
	return &OrganizePlan{
		SourcePath: oldDir + "/v.mp4",
		TargetDir:  targetDir,
		TargetFile: "v.mp4",
		TargetPath: targetDir + "/v.mp4",
		WillMove:   true,
		InPlace:    true,
		OldDir:     oldDir,
		Match:      models.FileMatchInfo{Path: oldDir + "/v.mp4", Name: "v.mp4"},
	}
}

func TestInPlaceStrategy_TargetDirIsSymlink_Refused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on windows CI")
	}
	dir := t.TempDir()
	base := afero.NewOsFs()
	realDir := filepath.Join(dir, "real")
	oldDir := filepath.Join(dir, "old")
	require.NoError(t, os.MkdirAll(realDir, 0755))
	require.NoError(t, os.MkdirAll(oldDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(oldDir, "v.mp4"), []byte("v"), 0644))

	symlinkDir := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink(realDir, symlinkDir))
	danglingDir := filepath.Join(dir, "dangling")
	require.NoError(t, os.Symlink(filepath.Join(dir, "gone"), danglingDir))

	cfg := &Config{OperationMode: operationmode.OperationModeInPlace, FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}

	t.Run("lstater leg", func(t *testing.T) {
		strategy := newInPlaceStrategy(base, cfg, nil, nil)
		_, err := strategy.Execute(mkInPlacePlan(oldDir, symlinkDir))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "target directory is a symlink")
	})
	t.Run("didLstat=false success leg", func(t *testing.T) {
		strategy := newInPlaceStrategy(forceStatFallbackFs{base}, cfg, nil, nil)
		_, err := strategy.Execute(mkInPlacePlan(oldDir, symlinkDir))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "target directory is a symlink")
	})
	t.Run("didLstat=false dangling leg", func(t *testing.T) {
		strategy := newInPlaceStrategy(forceStatFallbackFs{base}, cfg, nil, nil)
		_, err := strategy.Execute(mkInPlacePlan(oldDir, danglingDir))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "target directory is a symlink")
	})
	t.Run("plain stat symlink leg", func(t *testing.T) {
		strategy := newInPlaceStrategy(plainStatFs{base}, cfg, nil, nil)
		_, err := strategy.Execute(mkInPlacePlan(oldDir, symlinkDir))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "target directory is a symlink")
	})
	t.Run("plain stat dangling leg", func(t *testing.T) {
		strategy := newInPlaceStrategy(plainStatFs{base}, cfg, nil, nil)
		_, err := strategy.Execute(mkInPlacePlan(oldDir, danglingDir))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "target directory is a symlink")
	})
	t.Run("plain stat real dir leg sets dirExists", func(t *testing.T) {
		strategy := newInPlaceStrategy(plainStatFs{base}, cfg, nil, nil)
		_, err := strategy.Execute(mkInPlacePlan(oldDir, realDir))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "target directory already exists")
	})
}

func TestInPlaceStrategy_TargetDirCheckErrors_Surface(t *testing.T) {
	cfg := &Config{OperationMode: operationmode.OperationModeInPlace, FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}
	permErr := errors.New("permission denied")

	t.Run("lstater non-NotExist error", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/old/v.mp4", []byte("v"), 0644))
		bad := failStatPathsFs{Fs: fs, fail: map[string]error{"/new": permErr}}
		strategy := newInPlaceStrategy(bad, cfg, nil, nil)
		_, err := strategy.Execute(mkInPlacePlan("/old", "/new"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to check target directory")
	})
	t.Run("plain stat non-NotExist error", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/old/v.mp4", []byte("v"), 0644))
		bad := plainStatFs{failStatPathsFs{Fs: fs, fail: map[string]error{"/new": permErr}}}
		strategy := newInPlaceStrategy(bad, cfg, nil, nil)
		_, err := strategy.Execute(mkInPlacePlan("/old", "/new"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to check target directory")
	})
	t.Run("second stat of old dir failure is defensive but surfaces", func(t *testing.T) {
		fs := &statCountFs{Fs: afero.NewMemMapFs(), target: "/old", failOn: 2}
		require.NoError(t, afero.WriteFile(fs, "/old/v.mp4", []byte("v"), 0644))
		require.NoError(t, fs.MkdirAll("/new", 0755))
		strategy := newInPlaceStrategy(fs, cfg, nil, nil)
		_, err := strategy.Execute(mkInPlacePlan("/old", "/new"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "target directory already exists")
	})
}

// --- in-place rollback legs -----------------------------------------------------------

func TestInPlaceStrategy_RollbackAlsoFails_RefusalStillReturned(t *testing.T) {
	base := afero.NewMemMapFs()
	cfg := &Config{OperationMode: operationmode.OperationModeInPlace, FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}
	hook := &renamePolicyFs{Fs: base}
	strategy := newInPlaceStrategy(hook, cfg, nil, nil)
	require.NoError(t, afero.WriteFile(base, "/old/ABC-777.mp4", []byte("mine"), 0644))

	plan := mkInPlacePlan("/old", "/new")
	plan.SourcePath = "/old/ABC-777.mp4"
	plan.TargetFile = "ABC-777-renamed.mp4"
	plan.TargetPath = "/new/ABC-777-renamed.mp4"
	plan.Match = models.FileMatchInfo{Path: "/old/ABC-777.mp4", Name: "ABC-777.mp4"}

	hook.after = func(_, newPath string) {
		if newPath == "/new" {
			_ = afero.WriteFile(base, "/new/ABC-777-renamed.mp4", []byte("rival"), 0644)
		}
	}
	hook.failPair = func(oldPath, newPath string) bool {
		return oldPath == "/new" && newPath == "/old" // rollback fails
	}

	_, err := strategy.Execute(plan)
	require.Error(t, err, "refusal must still surface even when rollback fails")
	assert.Contains(t, err.Error(), "refusing to overwrite")
}

func TestInPlaceStrategy_InnerRenameFails_WithAndWithoutRollbackFailure(t *testing.T) {
	cfg := &Config{OperationMode: operationmode.OperationModeInPlace, FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}

	run := func(t *testing.T, rollbackAlsoFails bool) {
		base := afero.NewMemMapFs()
		hook := &renamePolicyFs{Fs: base}
		strategy := newInPlaceStrategy(hook, cfg, nil, nil)
		require.NoError(t, afero.WriteFile(base, "/old/ABC-777.mp4", []byte("mine"), 0644))
		plan := mkInPlacePlan("/old", "/new")
		plan.SourcePath = "/old/ABC-777.mp4"
		plan.TargetFile = "ABC-777-renamed.mp4"
		plan.TargetPath = "/new/ABC-777-renamed.mp4"
		plan.Match = models.FileMatchInfo{Path: "/old/ABC-777.mp4", Name: "ABC-777.mp4"}
		hook.failPair = func(oldPath, newPath string) bool {
			switch {
			case oldPath == "/new/ABC-777.mp4" && newPath == "/new/ABC-777-renamed.mp4":
				return true // inner rename fails
			case rollbackAlsoFails && oldPath == "/new" && newPath == "/old":
				return true
			}
			return false
		}
		_, err := strategy.Execute(plan)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to rename file after directory rename")
	}

	t.Run("rollback succeeds", func(t *testing.T) { run(t, false) })
	t.Run("rollback fails too", func(t *testing.T) { run(t, true) })
}

// --- copy/link execution legs ----------------------------------------------------------

func TestOrganizeStrategy_LinkMode_AuthorizedRemoveFailure(t *testing.T) {
	mem := afero.NewMemMapFs()
	cfg := &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}
	strategy := newOrganizeStrategy(mem, cfg, nil, &MemLinker{})
	movie := testutil.NewMovieBuilder().WithID("IPX-123").Build()
	require.NoError(t, afero.WriteFile(mem, "/src/IPX-123.mp4", []byte("new"), 0644))
	require.NoError(t, afero.WriteFile(mem, "/dest/IPX-123/IPX-123.mp4", []byte("old"), 0644))

	match := models.FileMatchInfo{Path: "/src/IPX-123.mp4", Name: "IPX-123.mp4", Extension: ".mp4", MovieID: "IPX-123"}
	plan, err := strategy.Plan(match, movie, "/dest", true)
	require.NoError(t, err)
	plan.moveFiles = false
	plan.LinkMode = LinkModeHard
	plan.Conflicts = nil

	strategy.fs = failRemovePathFs{Fs: mem, target: "/dest/IPX-123/IPX-123.mp4"}
	_, err = strategy.Execute(plan)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to prepare target path for link")
}

func TestOrganizeStrategy_LinkMode_LexicalSelf_Idempotent(t *testing.T) {
	mem := afero.NewMemMapFs()
	cfg := &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}
	strategy := newOrganizeStrategy(mem, cfg, nil, &MemLinker{})
	require.NoError(t, afero.WriteFile(mem, "/dest/A.mp4", []byte("same"), 0644))

	plan := &OrganizePlan{
		SourcePath: "/dest/A.mp4",
		TargetDir:  "/dest",
		TargetFile: "A.mp4",
		TargetPath: "/dest/A.mp4",
		WillMove:   true,
		LinkMode:   LinkModeHard,
		moveFiles:  false,
		Match:      models.FileMatchInfo{Path: "/dest/A.mp4", Name: "A.mp4"},
	}
	result, err := strategy.Execute(plan)
	require.NoError(t, err, "lexical self with link mode must be a no-op")
	require.NotNil(t, result)
}

func TestOrganizeStrategy_LinkMode_SameInode_Idempotent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hardlink semantics differ on windows CI")
	}
	dir := t.TempDir()
	base := afero.NewOsFs()
	src := filepath.Join(dir, "src.mp4")
	require.NoError(t, os.WriteFile(src, []byte("video"), 0644))
	dst := filepath.Join(dir, "alias.mp4")
	require.NoError(t, os.Link(src, dst)) // pre-existing hardlink alias

	cfg := &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}

	for _, mode := range []LinkMode{LinkModeHard, LinkModeNone} {
		linker := &MemLinker{}
		strategy := newOrganizeStrategy(base, cfg, nil, linker)
		plan := &OrganizePlan{
			SourcePath: src,
			TargetDir:  dir,
			TargetFile: "alias.mp4",
			TargetPath: dst,
			WillMove:   true,
			LinkMode:   mode,
			moveFiles:  false,
			Match:      models.FileMatchInfo{Path: src, Name: "src.mp4"},
		}
		result, err := strategy.Execute(plan)
		require.NoError(t, err, "same-inode destination in unauthorized mode must be an idempotent no-op")
		require.NotNil(t, result)
		assert.Empty(t, linker.Links, "no link operation should run against an already-satisfied inode")
		content, _ := os.ReadFile(src)
		assert.Equal(t, []byte("video"), content, "source content preserved")
	}
}

func TestOrganizeStrategy_LinkMode_SoftPermissionDenied(t *testing.T) {
	mem := afero.NewMemMapFs()
	cfg := &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}
	strategy := newOrganizeStrategy(mem, cfg, nil, errLinker{softErr: os.ErrPermission})
	require.NoError(t, afero.WriteFile(mem, "/src/A.mp4", []byte("v"), 0644))

	plan := &OrganizePlan{
		SourcePath: "/src/A.mp4",
		TargetDir:  "/dest",
		TargetFile: "A.mp4",
		TargetPath: "/dest/A.mp4",
		WillMove:   true,
		LinkMode:   LinkModeSoft,
		moveFiles:  false,
		Match:      models.FileMatchInfo{Path: "/src/A.mp4", Name: "A.mp4"},
	}
	_, err := strategy.Execute(plan)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create soft link")
}

func TestOrganizeStrategy_LinkMode_SoftRelativeSourceResolutionFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cannot remove the working directory on windows")
	}
	mem := afero.NewMemMapFs()
	cfg := &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}
	strategy := newOrganizeStrategy(mem, cfg, nil, &MemLinker{})
	require.NoError(t, afero.WriteFile(mem, "/src/A.mp4", []byte("v"), 0644))

	plan := &OrganizePlan{
		SourcePath:          "relative-src.mp4", // forces filepath.Abs
		TargetDir:           "/dest",
		TargetFile:          "A.mp4",
		TargetPath:          "/dest/A.mp4",
		WillMove:            true,
		LinkMode:            LinkModeSoft,
		moveFiles:           false,
		overwriteAuthorized: true, // classification failure is benign under authorization
		Match:               models.FileMatchInfo{Path: "relative-src.mp4", Name: "relative-src.mp4"},
	}

	doomed := t.TempDir()
	prev, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(doomed))
	defer func() { _ = os.Chdir(prev) }()
	require.NoError(t, os.Remove(doomed))

	_, err = strategy.Execute(plan)
	if err == nil {
		t.Skipf("filepath.Abs still resolves with a removed cwd on %s; the failure branch is covered on linux CI", runtime.GOOS)
	}
	assert.Contains(t, err.Error(), "failed to resolve source path for symlink")
}

// --- subtitle destination check failure ------------------------------------------------

func TestOrganize_SubtitleStatFailure_Surfaced(t *testing.T) {
	mem := afero.NewMemMapFs()
	permErr := errors.New("permission denied")
	fs := failStatPathsFs{Fs: mem, fail: map[string]error{"/dest/IPX-535/IPX-535.srt": permErr}}
	cfg := &Config{
		FolderFormat:       "<ID>",
		FileFormat:         "<ID>",
		RenameFile:         true,
		MoveSubtitles:      true,
		SubtitleExtensions: []string{".srt"},
		OperationMode:      operationmode.OperationModeOrganize,
	}
	org := NewOrganizer(fs, cfg, nil, nil)
	org.fs = fs
	movie := testutil.NewMovieBuilder().WithID("IPX-535").Build()

	require.NoError(t, afero.WriteFile(mem, "/src/IPX-535.mp4", []byte("video"), 0644))
	require.NoError(t, afero.WriteFile(mem, "/src/IPX-535.srt", []byte("subtitle"), 0644))

	match := models.FileMatchInfo{Path: "/src/IPX-535.mp4", Name: "IPX-535.mp4", Extension: ".mp4", MovieID: "IPX-535"}
	result, err := org.Organize(context.Background(), OrganizeCmd{
		Match: match, Movie: movie, DestDir: "/dest", MoveFiles: true,
	})
	require.NoError(t, err)
	require.Len(t, result.Subtitles, 1)
	assert.Error(t, result.Subtitles[0].Error)
	assert.Contains(t, result.Subtitles[0].Error.Error(), "failed to check subtitle destination")
	assert.False(t, result.Subtitles[0].Moved)
	assert.False(t, result.Subtitles[0].Skipped)
}
