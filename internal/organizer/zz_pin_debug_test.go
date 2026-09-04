package organizer

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

func TestZZDebugInPlace(t *testing.T) {
	dir := t.TempDir()
	fs := afero.NewOsFs()
	oldDir := filepath.Join(dir, "mixed")
	require.NoError(t, os.MkdirAll(oldDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(oldDir, "ABC.mp4"), []byte("v"), 0o644))
	require.NoError(t, os.Symlink(oldDir, filepath.Join(dir, "ABC")))

	strategy := newInPlaceStrategy(fs, &Config{FileFormat: "<ID>", FolderFormat: "<ID>", RenameFile: true}, nil, nil)
	match := models.FileMatchInfo{Path: filepath.Join(oldDir, "ABC.mp4"), Name: "ABC.mp4", Extension: ".mp4", MovieID: "ABC"}
	movie := &models.Movie{ID: "ABC"}
	plan, err := strategy.Plan(match, movie, "", false)
	require.NoError(t, err)
	fmt.Printf("InPlace=%v TargetDir=%q willMove=%v conflicts=%v\n", plan.InPlace, plan.TargetDir, plan.WillMove, plan.Conflicts)
	info, _ := os.Lstat(filepath.Join(dir, "ABC"))
	if info != nil {
		fmt.Printf("probe link mode symlink bits: %v\n", info.Mode()&os.ModeSymlink)
	}
}
