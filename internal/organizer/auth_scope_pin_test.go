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

// Authorized dedicated in-place rename where the inner rename destination is
// occupied by a foreign symlink planted *inside the source directory before*
// execute (so the directory rename carries it into the new name). The
// authorize leg still refuses on kinds (file-replace only), and the Directory
// must roll back to OldDir with all occupants intact.
func TestAuthScope_AuthorizedInnerRollbackOnSymlink(t *testing.T) {
	dir := t.TempDir()
	oldDir := filepath.Join(dir, "mixed")
	newDir := filepath.Join(dir, "ABC-123")
	require.NoError(t, os.MkdirAll(oldDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(oldDir, "ABC-123.mp4"), []byte("mine"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(oldDir, "foreign.mp4"), []byte("foreign"), 0o644))
	// Plant: the symlink at the eventual inner target name, sitting inside OldDir.
	require.NoError(t, os.Symlink(
		filepath.Join(oldDir, "foreign.mp4"),
		filepath.Join(oldDir, "ABC-123-new.mp4"),
	))

	cfg := &Config{FileFormat: "<ID>", FolderFormat: "<ID>", RenameFile: true}
	strategy := newInPlaceStrategy(afero.NewOsFs(), cfg, nil, nil)
	plan := &OrganizePlan{
		SourcePath:  filepath.Join(oldDir, "ABC-123.mp4"),
		OldDir:      oldDir,
		TargetDir:   newDir,
		TargetFile:  "ABC-123-new.mp4",
		WillMove:    true,
		InPlace:     true,
		IsDedicated: true,
		TargetPath:  filepath.Join(newDir, "ABC-123-new.mp4"),
		Match: models.FileMatchInfo{
			Path: filepath.Join(oldDir, "ABC-123.mp4"),
			Name: "ABC-123.mp4", Extension: ".mp4", MovieID: "ABC-123",
		},
		overwriteAuthorized: true,
		Conflicts:           []PlanConflict{},
	}
	_, err := strategy.Execute(plan)
	require.Error(t, err, "the inner leg must refuse the symlink even when authorized")
	assert.Contains(t, err.Error(), "symlink")

	// The directory rename rolled back: NewDir absent, OldDir intact.
	if _, sd := os.Stat(newDir); !os.IsNotExist(sd) {
		assert.FailNow(t, "newDir must have rolled back to OldDir", "")
	}
	content, _ := os.ReadFile(filepath.Join(oldDir, "ABC-123.mp4"))
	assert.Equal(t, "mine", string(content))
	// The foreign symlink stays intact (it rolled with the dir).
	info, stErr := os.Lstat(filepath.Join(oldDir, "ABC-123-new.mp4"))
	require.NoError(t, stErr)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "planted symlink must survive the rolled-back rename")
}
