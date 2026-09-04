package organizer

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// OS-level: a dangling destination symlink is an existing entry — unauthorized moves
// conflict rather than replacing it.
func TestOrganize_DanglingSymlink_Conflicts(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	src := filepath.Join(dir, "IPX-123.mp4")
	dst := filepath.Join(dir, "out", "IPX-123", "IPX-123.mp4")
	require.NoError(t, os.WriteFile(src, []byte("mine"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0755))
	require.NoError(t, os.Symlink(filepath.Join(dir, "does-not-exist.mp4"), dst))

	cfg := &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}
	org := NewOrganizer(fs, cfg, nil, nil)
	movie := testutil.NewMovieBuilder().WithID("IPX-123").Build()
	match := models.FileMatchInfo{Path: src, Name: "IPX-123.mp4", Extension: ".mp4", MovieID: "IPX-123"}

	result, err := org.Organize(context.Background(), OrganizeCmd{
		Match: match, Movie: movie, DestDir: filepath.Join(dir, "out"), MoveFiles: true,
	})
	// A plan-visible KindSymlink conflict fails BEFORE execute (no result).
	// The plan-rendering is the bare path; the refusal sentence is execute-lane only.
	require.Error(t, err)
	require.Nil(t, result)
	// validation wraps plan conflicts as bare paths (OS-native separators)
	assert.Contains(t, filepath.ToSlash(err.Error()), filepath.ToSlash(dst))

	// The symlink entry remains; source is intact.
	info, err := os.Lstat(dst)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink)
	srcData, _ := os.ReadFile(src)
	assert.Equal(t, []byte("mine"), srcData)
}

// Two in-place operations from different old directories converging on one target dir:
// exactly one renames, the other fails without disturbing contents.
func TestInPlaceStrategy_SiblingCollision_NeverIllegalClobber(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{OperationMode: operationmode.OperationModeInPlace, FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}
	strategy := newInPlaceStrategy(fs, cfg, nil, nil)

	require.NoError(t, afero.WriteFile(fs, "/a/old/ABC-555.mp4", []byte("a-content"), 0644))
	require.NoError(t, afero.WriteFile(fs, "/b/old/ABC-555.mp4", []byte("b-content"), 0644))

	mk := func(old string) *OrganizePlan {
		return &OrganizePlan{
			SourcePath: old + "/ABC-555.mp4",
			TargetDir:  "/a/new",
			TargetFile: "ABC-555.mp4",
			TargetPath: "/a/new/ABC-555.mp4",
			WillMove:   true,
			InPlace:    true,
			OldDir:     old,
			Match:      models.FileMatchInfo{Path: old + "/ABC-555.mp4", Name: "ABC-555.mp4"},
		}
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	var mu sync.Mutex
	var r1, r2 *OrganizeResult
	var err1, err2 error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		r, err := strategy.Execute(mk("/a/old"))
		mu.Lock()
		r1, err1 = r, err
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		<-start
		r, err := strategy.Execute(mk("/b/old"))
		mu.Lock()
		r2, err2 = r, err
		mu.Unlock()
	}()
	close(start)
	wg.Wait()

	succeeded, failed := 0, 0
	for _, okres := range []bool{err1 == nil && r1 != nil && r1.Moved, err2 == nil && r2 != nil && r2.Moved} {
		if okres {
			succeeded++
		} else {
			failed++
		}
	}
	assert.Equal(t, 1, succeeded, "exactly one dir rename succeeds")
	assert.Equal(t, 1, failed, "the other refuses")

	data, err := afero.ReadFile(fs, "/a/new/ABC-555.mp4")
	require.NoError(t, err)
	assert.Contains(t, [][]byte{[]byte("a-content"), []byte("b-content")}, data, "winner content intact")
	// The loser source must still be in place where it began (whichever it was)
	if err1 == nil && r1 != nil && r1.Moved {
		loser, err := afero.ReadFile(fs, "/b/old/ABC-555.mp4")
		require.NoError(t, err, "loser source preserved in its original directory")
		assert.Equal(t, loser, []byte("b-content"))
	} else {
		loser, err := afero.ReadFile(fs, "/a/old/ABC-555.mp4")
		require.NoError(t, err, "loser source preserved in its original directory")
		assert.Equal(t, loser, []byte("a-content"))
	}
}

// renameHookFs injects a rival destination write immediately after a specific rename
// completes — deterministically reproducing the interleaving the up-front TargetPath
// lock guards against (a writer placing a different file at the inner target between
// the directory rename and the inner file step).
type renameHookFs struct {
	afero.Fs
	onAfterRename func(oldPath, newPath string)
}

func (f *renameHookFs) Rename(oldPath, newPath string) error {
	err := f.Fs.Rename(oldPath, newPath)
	if err == nil && f.onAfterRename != nil {
		f.onAfterRename(oldPath, newPath)
	}
	return err
}

func TestInPlaceStrategy_RivalWrittenMidSequence_NeverClobbered(t *testing.T) {
	base := afero.NewMemMapFs()
	hook := &renameHookFs{Fs: base}
	cfg := &Config{OperationMode: operationmode.OperationModeInPlace, FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}
	strategy := newInPlaceStrategy(hook, cfg, nil, nil)

	require.NoError(t, afero.WriteFile(base, "/old/ABC-777.mp4", []byte("mine"), 0644))
	plan := &OrganizePlan{
		SourcePath: "/old/ABC-777.mp4",
		TargetDir:  "/new",
		TargetFile: "ABC-777-renamed.mp4",
		TargetPath: "/new/ABC-777-renamed.mp4",
		WillMove:   true,
		InPlace:    true,
		OldDir:     "/old",
		Match:      models.FileMatchInfo{Path: "/old/ABC-777.mp4", Name: "ABC-777.mp4"},
	}
	hook.onAfterRename = func(_, newPath string) {
		if newPath == "/new" {
			_ = afero.WriteFile(base, "/new/ABC-777-renamed.mp4", []byte("rival"), 0644)
		}
	}

	result, err := strategy.Execute(plan)
	require.Error(t, err, "rival at the inner destination must force refusal")
	require.NotNil(t, result)
	assert.False(t, result.Moved)

	// The rival's bytes must survive untouched wherever they ended up — before the fix
	// the inner rename could overwrite them outright. Both outcomes (rollback dragged the
	// entry back under /old, or it never left /new) are acceptable; a clobber is not.
	rival, rerr := afero.ReadFile(base, "/old/ABC-777-renamed.mp4")
	if rerr != nil {
		rival, rerr = afero.ReadFile(base, "/new/ABC-777-renamed.mp4")
	}
	require.NoError(t, rerr)
	assert.Equal(t, []byte("rival"), rival, "rival content must never be overwritten")
	src, serr := afero.ReadFile(base, "/old/ABC-777.mp4")
	require.NoError(t, serr, "rollback must restore the source to its old directory")
	assert.Equal(t, []byte("mine"), src)
}
