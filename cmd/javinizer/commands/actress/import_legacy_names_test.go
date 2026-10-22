package actress

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestCLIImportLegacyOverrideRequestOnly(t *testing.T) {
	configPath, _ := setupMissTestDB(t)
	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	legacy := models.Actress{DMMID: 9733, FirstName: "Legacy", Origin: database.ActressOriginImport, Verified: true}
	require.NoError(t, db.Create(&legacy).Error)
	repo := database.NewActressRepository(db)
	file := filepath.Join(t.TempDir(), "import.json")
	require.NoError(t, os.WriteFile(file, []byte(`[{"dmm_id":9733,"first_name":"Allowed"}]`), 0600))
	call := func(flags ...string) (string, error) {
		root := &cobra.Command{Use: "root"}
		root.PersistentFlags().String("config", configPath, "config file")
		root.AddCommand(NewCommand())
		root.SetArgs(append([]string{"actress", "import", file}, flags...))
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		err := root.Execute()
		return out.String(), err
	}
	_, err = call("--overwrite-legacy-names")
	require.ErrorContains(t, err, "requires --overwrite-names")
	out, err := call("--overwrite-names")
	require.NoError(t, err)
	require.Contains(t, out, "Protected: 1")
	out, err = call("--overwrite-names", "--overwrite-legacy-names")
	require.NoError(t, err)
	require.Contains(t, out, "Updated: 1")
	require.NoError(t, os.WriteFile(file, []byte(`[{"dmm_id":9733,"first_name":"Forbidden"}]`), 0600))
	out, err = call("--overwrite-names")
	require.NoError(t, err)
	require.Contains(t, out, "Protected: 1")
	got, err := repo.FindByID(t.Context(), legacy.ID)
	require.NoError(t, err)
	require.Equal(t, "Allowed", got.FirstName)
}
