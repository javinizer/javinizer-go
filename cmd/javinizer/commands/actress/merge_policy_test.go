package actress

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestCLIMergePolicyDryRunAndApply(t *testing.T) {
	for _, policy := range []string{"fill_empty", "preserve_target"} {
		t.Run(policy, func(t *testing.T) {
			path, _ := setupMissTestDB(t)
			target := models.Actress{FirstName: "桜"}
			source := models.Actress{FirstName: "Source", LastName: "Example", DMMID: 981}
			seedMissActresses(t, path, &target, &source)
			run := func(flags ...string) string {
				root := &cobra.Command{Use: "test"}
				root.PersistentFlags().String("config", path, "")
				root.AddCommand(NewCommand())
				args := []string{"actress", "merge", "--target", strconv.Itoa(int(target.ID)), "--source", strconv.Itoa(int(source.ID)), "--policy", policy}
				root.SetArgs(append(args, flags...))
				output := &bytes.Buffer{}
				root.SetOut(output)
				root.SetIn(strings.NewReader(""))
				require.NoError(t, root.Execute())
				return output.String()
			}
			out := run("--dry-run")
			require.Contains(t, out, "last_name:")
			require.Contains(t, out, `target=""`)
			require.Contains(t, out, "source #")
			cfg, err := config.Load(path)
			require.NoError(t, err)
			db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: "silent"})
			require.NoError(t, err)
			defer db.Close()
			repo := database.NewActressRepository(db)
			_, err = repo.FindByID(t.Context(), source.ID)
			require.NoError(t, err)
			out = run("--non-interactive", "--resolve", "first_name=target")
			require.Contains(t, out, "Merged actress")
			merged, err := repo.FindByID(t.Context(), target.ID)
			require.NoError(t, err)
			require.Equal(t, "桜", merged.FirstName)
			if policy == "preserve_target" {
				require.Empty(t, merged.LastName)
				require.Zero(t, merged.DMMID)
				require.Contains(t, out, "Source DMM ID 981 will not be retained.")
			} else {
				require.Equal(t, "Example", merged.LastName)
				require.Equal(t, 981, merged.DMMID)
			}
			_, err = repo.FindByID(t.Context(), source.ID)
			require.True(t, database.IsNotFound(err))
		})
	}
}

func TestCLIExplicitBlankOverridesFillPolicy(t *testing.T) {
	path, _ := setupMissTestDB(t)
	target := models.Actress{FirstName: "桜"}
	source := models.Actress{FirstName: "Source", LastName: "Example", DMMID: 982}
	seedMissActresses(t, path, &target, &source)
	root := &cobra.Command{Use: "test"}
	root.PersistentFlags().String("config", path, "")
	root.AddCommand(NewCommand())
	root.SetArgs([]string{"actress", "merge", "--target", strconv.Itoa(int(target.ID)), "--source", strconv.Itoa(int(source.ID)), "--dry-run", "--resolve", "last_name=target,dmm_id=target"})
	out := &bytes.Buffer{}
	root.SetOut(out)
	require.NoError(t, root.Execute())
	require.Contains(t, out.String(), `last_name: target="" source="Example" result=""`)
	require.Contains(t, out.String(), "dmm_id: target=0 source=982 result=0")
}

type mergePolicyInput struct {
	read func([]byte) (int, error)
}

func (r mergePolicyInput) Read(p []byte) (int, error) { return r.read(p) }

func TestCLIMergePreviewRefreshRejectsDeletedSource(t *testing.T) {
	path, _ := setupMissTestDB(t)
	target := models.Actress{FirstName: "Target"}
	source := models.Actress{FirstName: "Source"}
	seedMissActresses(t, path, &target, &source)
	cfg, err := config.Load(path)
	require.NoError(t, err)
	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: "silent"})
	require.NoError(t, err)
	defer db.Close()
	root := &cobra.Command{Use: "test"}
	root.PersistentFlags().String("config", path, "")
	root.AddCommand(NewCommand())
	root.SetArgs([]string{"actress", "merge", "--target", strconv.Itoa(int(target.ID)), "--source", strconv.Itoa(int(source.ID)), "--yes"})
	root.SetOut(&bytes.Buffer{})
	reader := strings.NewReader("\n\n\n\n\n")
	root.SetIn(mergePolicyInput{read: func(p []byte) (int, error) {
		require.NoError(t, db.Delete(&models.Actress{}, source.ID).Error)
		return reader.Read(p)
	}})
	require.Error(t, root.Execute())
}
