package actress

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestCLIImportOverwriteRejectsLegacyNegativeDMMContradiction(t *testing.T) {
	cfgPath, _ := setupMissTestDB(t)
	run := func(json string, overwrite bool) string {
		p := filepath.Join(t.TempDir(), "import.json")
		require.NoError(t, os.WriteFile(p, []byte(json), 0600))
		root := &cobra.Command{Use: "test"}
		root.PersistentFlags().String("config", cfgPath, "")
		root.AddCommand(NewCommand())
		args := []string{"actress", "import", p}
		if overwrite {
			args = append(args, "--overwrite-names")
		}
		root.SetArgs(args)
		out := &bytes.Buffer{}
		root.SetOut(out)
		require.NoError(t, root.Execute())
		return out.String()
	}
	out := run(`[{"dmm_id":-7,"first_name":"LegacyNegative"},{"dmm_id":81234,"first_name":"PositiveOwner"}]`, false)
	require.Contains(t, out, "Created: 2")
	cfg, err := config.Load(cfgPath)
	require.NoError(t, err)
	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := database.NewActressRepository(db)
	negative, err := repo.FindByFirstNameLastName(t.Context(), "LegacyNegative", "")
	require.NoError(t, err)
	require.Equal(t, -7, negative.DMMID)
	require.Equal(t, database.ActressOriginImport, negative.Origin)
	owner, err := repo.FindByDMMID(t.Context(), 81234)
	require.NoError(t, err)
	out = run(fmt.Sprintf(`[{"id":%d,"dmm_id":81234,"first_name":"UnexpectedRename"}]`, negative.ID), true)
	after, err := repo.FindByID(t.Context(), negative.ID)
	require.NoError(t, err)
	other, err := repo.FindByID(t.Context(), owner.ID)
	require.NoError(t, err)
	t.Logf("strict import summary: %s; selected name=%q DMM=%d; positive owner name=%q DMM=%d", out, after.FirstName, after.DMMID, other.FirstName, other.DMMID)
	require.Contains(t, out, "Imported: 0, Skipped: 0, Errors: 1")
	require.Contains(t, out, "Updated: 0")
	require.Contains(t, out, "Conflicted: 1")
	require.Equal(t, "LegacyNegative", after.FirstName)
	require.Equal(t, -7, after.DMMID)
	require.Equal(t, database.ActressOriginImport, after.Origin)
	require.Equal(t, "PositiveOwner", other.FirstName)
	require.Equal(t, 81234, other.DMMID)
}
