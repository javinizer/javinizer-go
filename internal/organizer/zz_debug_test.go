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

func TestZZInPlaceConf(t *testing.T) {
	dir := t.TempDir()
	fs := afero.NewOsFs()
	oldDir := filepath.Join(dir, "mixed")
	require.NoError(t, os.MkdirAll(oldDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(oldDir, "ABC.mp4"), []byte("v"), 0o644))
	require.NoError(t, os.Symlink(filepath.Join(dir, "whatever.mp4"), filepath.Join(dir, "ABC")))

	cfg := &Config{FileFormat: "<ID>", FolderFormat: "<ID>", RenameFile: true}
	strategy := newInPlaceStrategy(fs, cfg, nil, nil)
	match := models.FileMatchInfo{Path: filepath.Join(oldDir, "ABC.mp4"), Name: "ABC.mp4", Extension: ".mp4", MovieID: "ABC"}
	movie := &models.Movie{ID: "ABC"}
	plan, err := strategy.Plan(match, movie, "", false)
	require.NoError(t, err)
	fmt.Printf("InPlace=%v WillMove=%v TargetDir=%q Conflicts=%v\n", plan.InPlace, plan.WillMove, plan.TargetDir, plan.Conflicts)
}
