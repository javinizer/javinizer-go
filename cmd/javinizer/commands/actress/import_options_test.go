package actress

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestCLIImportOverwriteNames(t *testing.T) {
	for _, overwrite := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "explicit"}[overwrite], func(t *testing.T) {
			configPath, _ := setupMissTestDB(t)
			cfg, err := config.Load(configPath)
			require.NoError(t, err)
			db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: "silent"})
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			repo := database.NewActressRepository(db)
			imported := &models.Actress{FirstName: "Original", DMMID: 123, Origin: database.ActressOriginImport, Verified: true}
			require.NoError(t, db.Create(imported).Error)
			manual := &models.Actress{FirstName: "Manual", DMMID: 456, Origin: database.ActressOriginUser, Verified: true}
			require.NoError(t, db.Create(manual).Error)
			rows := []models.Actress{{ID: imported.ID, FirstName: "Replacement"}, {ID: manual.ID, FirstName: "DoNotReplace"}}
			data, err := json.Marshal(rows)
			require.NoError(t, err)
			file := filepath.Join(t.TempDir(), "import.json")
			require.NoError(t, os.WriteFile(file, data, 0600))
			root := &cobra.Command{Use: "root"}
			root.PersistentFlags().String("config", configPath, "config file")
			root.AddCommand(NewCommand())
			args := []string{"actress", "import", file}
			if overwrite {
				args = append(args, "--overwrite-names")
			}
			root.SetArgs(args)
			var output bytes.Buffer
			root.SetOut(&output)
			require.NoError(t, root.Execute())
			got, err := repo.FindByID(t.Context(), imported.ID)
			require.NoError(t, err)
			if overwrite {
				require.Equal(t, "Replacement", got.FirstName)
				require.Contains(t, output.String(), "Updated: 1")
				require.Contains(t, output.String(), "Protected: 1")
			} else {
				require.Equal(t, "Original", got.FirstName)
				require.Contains(t, output.String(), "Protected: 2")
			}
			got, err = repo.FindByID(t.Context(), manual.ID)
			require.NoError(t, err)
			require.Equal(t, "Manual", got.FirstName)
		})
	}
}
