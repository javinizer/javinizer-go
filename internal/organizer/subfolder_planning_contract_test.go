package organizer_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestPlanOrganizeSubfolderTemplateContract(t *testing.T) {
	for _, tc := range []struct {
		name, subfolder string
		invalid         bool
	}{
		{name: "unmatched closing conditional", subfolder: "</IF>", invalid: true},
		{name: "valid ID subfolder", subfolder: "<ID>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			root := t.TempDir()
			source := filepath.Join(root, "source", "FIX-001.mp4")
			destination := filepath.Join(root, "destination")
			original := []byte("owned source video bytes")
			require.NoError(t, fs.MkdirAll(filepath.Dir(source), 0755))
			require.NoError(t, afero.WriteFile(fs, source, original, 0644))
			cfg := &organizer.Config{
				OperationMode: operationmode.OperationModeOrganize,
				RenameFile:    true, FileFormat: "<ID>", FolderFormat: "<ID>",
				SubfolderFormat: []string{tc.subfolder},
			}
			org := organizer.NewOrganizer(fs, cfg, template.NewEngine(), nil)
			plan, err := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{
				Match: models.FileMatchInfo{Path: source, Name: "FIX-001.mp4", Extension: ".mp4", MovieID: "FIX-001"},
				Movie: &models.Movie{ID: "FIX-001", Title: "Fixture"}, DestDir: destination,
			})
			if tc.invalid {
				require.Nil(t, plan)
				require.EqualError(t, err, "failed to generate subfolder from template '</IF>': invalid template conditionals: unexpected closing </IF>")
			} else {
				require.NoError(t, err)
				require.NotNil(t, plan)
				require.Equal(t, source, plan.SourcePath)
				require.Equal(t, filepath.Join(destination, "FIX-001", "FIX-001"), plan.TargetDir)
				require.Equal(t, filepath.Join(plan.TargetDir, "FIX-001.mp4"), plan.TargetPath)
				require.True(t, plan.WillMove)
				require.Empty(t, plan.Conflicts)
			}
			after, readErr := afero.ReadFile(fs, source)
			require.NoError(t, readErr)
			require.Equal(t, original, after, "planning must not alter the source")
			_, statErr := fs.Stat(destination)
			require.True(t, os.IsNotExist(statErr), "planning must not create output: %v", statErr)
		})
	}
}
