package actress_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/actress"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func isolateActressCLI(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Chdir(root)
	for _, key := range []string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "JAVINIZER_HOME"} {
		t.Setenv(key, filepath.Join(root, key))
	}
	return root
}

// Every invocation gets a fresh Cobra tree and exercises its public flag/RunE
// path. Capture both command writers and the export summary's process stdout.
func executeActressCLI(t *testing.T, configPath string, input io.Reader, args ...string) (string, error) {
	t.Helper()
	root := &cobra.Command{Use: "test", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("config", "", "config file")
	root.AddCommand(actress.NewCommand())
	var output, diagnostics bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&diagnostics)
	root.SetIn(input)
	root.SetArgs(append([]string{"--config", configPath, "actress"}, args...))
	var err error
	stdout, stderr := captureOutput(t, func() { err = root.Execute() })
	return output.String() + diagnostics.String() + stdout + stderr, err
}

func TestActressCLIRejectsUnusableDatabaseWithoutChangingFiles(t *testing.T) {
	for _, operation := range []string{"merge", "export", "import"} {
		t.Run(operation, func(t *testing.T) {
			root := isolateActressCLI(t)
			dbPath := filepath.Join(root, "not-sqlite.db")
			invalidDB := bytes.Repeat([]byte("plain fixture text, not a SQLite database\n"), 128)
			require.NoError(t, os.WriteFile(dbPath, invalidDB, 0o600))
			cfg := config.DefaultConfig(nil, nil)
			cfg.Database.DSN = dbPath
			configPath := filepath.Join(root, "config.yaml")
			require.NoError(t, config.Save(cfg, configPath))
			loaded, err := config.Load(configPath)
			require.NoError(t, err, "this must be database initialization failure, not invalid configuration")
			require.Equal(t, dbPath, loaded.Database.DSN)

			importPath := filepath.Join(root, "input.json")
			importData := []byte(`[{"first_name":"Fixture","last_name":"Import","dmm_id":71003}]`)
			require.NoError(t, os.WriteFile(importPath, importData, 0o600))
			exportPath := filepath.Join(root, "output.json")
			sentinel := []byte("existing export must survive\n")
			require.NoError(t, os.WriteFile(exportPath, sentinel, 0o600))
			args := map[string][]string{
				"merge":  {"merge", "--target", "1", "--source", "2", "--non-interactive"},
				"export": {"export", exportPath},
				"import": {"import", importPath},
			}[operation]

			output, err := executeActressCLI(t, configPath, strings.NewReader(""), args...)
			require.ErrorContains(t, err, "failed to initialize dependencies: failed to initialize database:")
			require.ErrorContains(t, err, "file is not a database")
			for _, summary := range []string{"Merged actress", "Exported ", "Imported:", "Created:", "Preparing merge", "Apply merge?"} {
				require.NotContains(t, output, summary)
			}
			for path, want := range map[string][]byte{dbPath: invalidDB, importPath: importData, exportPath: sentinel} {
				got, err := os.ReadFile(path)
				require.NoError(t, err)
				require.Equal(t, want, got, "failed %s changed %s", operation, filepath.Base(path))
			}
		})
	}
}

type actressCLIMovieLink struct {
	MovieContentID string
	ActressID      uint
}

type actressCLIMergeState struct {
	Actresses []models.Actress
	Aliases   []models.ActressAlias
	Movies    []models.Movie
	Links     []actressCLIMovieLink
	Credits   []models.MovieCredit
}

func snapshotActressCLIMerge(t *testing.T, db *database.DB) actressCLIMergeState {
	t.Helper()
	var state actressCLIMergeState
	require.NoError(t, db.Order("id").Find(&state.Actresses).Error)
	require.NoError(t, db.Order("id").Find(&state.Aliases).Error)
	require.NoError(t, db.Order("content_id").Find(&state.Movies).Error)
	require.NoError(t, db.Table("movie_actresses").Order("movie_content_id, actress_id").Find(&state.Links).Error)
	require.NoError(t, db.Order("id").Find(&state.Credits).Error)
	return state
}

func setupActressCLIMergeFixture(t *testing.T) (string, *database.DB, models.Actress, models.Actress) {
	t.Helper()
	isolateActressCLI(t)
	configPath, _ := setupActressTestDB(t)
	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	// Complete the same migrations and default-alias/word seeding as command
	// startup before taking any logical snapshots. SQLite file bytes are not
	// a useful mutation oracle across legitimate startup and WAL activity.
	deps, err := commandutil.NewDependencies(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, deps.Close()) })
	target := models.Actress{DMMID: 71001, FirstName: "Target", LastName: "Fixture", JapaneseName: "試験対象", Aliases: "target fixture alias"}
	source := models.Actress{DMMID: 71002, FirstName: "Source", LastName: "Example", JapaneseName: "試験元", Aliases: "source fixture alias"}
	seedActresses(t, configPath, &target, &source)
	aliases := []models.ActressAlias{
		{AliasName: "target fixture alias", CanonicalName: target.JapaneseName},
		{AliasName: "source fixture alias", CanonicalName: source.JapaneseName},
	}
	require.NoError(t, deps.DB.Create(&aliases).Error)
	movie := models.Movie{ContentID: "cli-merge-fixture", ID: "CLI-MERGE-FIXTURE", Title: "Merge fixture", RenderGeneration: 7, Actresses: []models.Actress{source}}
	require.NoError(t, deps.DB.Create(&movie).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: source.ID, CreditedName: source.FullName(), CreditedJapaneseName: source.JapaneseName, Origin: string(models.CreditOriginUser)}
	require.NoError(t, deps.DB.Create(&credit).Error)
	return configPath, deps.DB, target, source
}

func actressCLIMergeArgs(targetID, sourceID uint) []string {
	return []string{"merge", "--target", strconv.FormatUint(uint64(targetID), 10), "--source", strconv.FormatUint(uint64(sourceID), 10)}
}

func TestActressCLIInterruptedFieldReviewDoesNotMerge(t *testing.T) {
	configPath, db, target, source := setupActressCLIMergeFixture(t)
	before := snapshotActressCLIMerge(t, db)
	output, err := executeActressCLI(t, configPath, strings.NewReader(""), actressCLIMergeArgs(target.ID, source.ID)...)
	require.ErrorIs(t, err, io.EOF)
	require.Contains(t, output, "Preparing merge source")
	require.Contains(t, output, "Choose value [t=target/s=source")
	require.NotContains(t, output, "Apply merge?")
	require.NotContains(t, output, "Merged actress")
	require.Equal(t, before, snapshotActressCLIMerge(t, db), "EOF during field review must leave identities, aliases, movie links, and credits unchanged")
}

func TestActressCLILateMergeFailureRollsBackAndCanRetry(t *testing.T) {
	configPath, db, target, source := setupActressCLIMergeFixture(t)
	before := snapshotActressCLIMerge(t, db)
	// Source deletion happens after identity/DMM, alias, association, and credit
	// writes. A real constraint in this owned fixture fails that late operation.
	require.NoError(t, db.Exec(fmt.Sprintf(`CREATE TRIGGER block_cli_merge_delete
		BEFORE DELETE ON actresses WHEN OLD.id = %d
		BEGIN SELECT RAISE(ABORT, 'blocked merge deletion'); END`, source.ID)).Error)
	args := append(actressCLIMergeArgs(target.ID, source.ID), "--non-interactive", "--prefer", "source")
	output, err := executeActressCLI(t, configPath, strings.NewReader(""), args...)
	require.ErrorContains(t, err, "blocked merge deletion")
	require.ErrorContains(t, err, fmt.Sprintf("merge source actress %d", source.ID))
	require.NotContains(t, output, "Merged actress")
	require.NotContains(t, output, "Updated movies:")
	require.Equal(t, before, snapshotActressCLIMerge(t, db), "late deletion failure must roll back all fixture state, including the temporary source DMM ID")

	require.NoError(t, db.Exec("DROP TRIGGER block_cli_merge_delete").Error)
	output, err = executeActressCLI(t, configPath, strings.NewReader(""), args...)
	require.NoError(t, err)
	require.Contains(t, output, fmt.Sprintf("Merged actress #%d into #%d", source.ID, target.ID))
	require.Contains(t, output, "Updated movies: 1")
	after := snapshotActressCLIMerge(t, db)
	require.Len(t, after.Actresses, 1, "retry must delete the source, not duplicate either identity")
	merged := after.Actresses[0]
	require.Equal(t, target.ID, merged.ID)
	require.Equal(t, source.DMMID, merged.DMMID)
	require.Equal(t, source.FirstName, merged.FirstName)
	require.Equal(t, source.LastName, merged.LastName)
	require.Equal(t, source.JapaneseName, merged.JapaneseName)
	require.Contains(t, strings.Split(merged.Aliases, "|"), "target fixture alias")
	require.Contains(t, strings.Split(merged.Aliases, "|"), "source fixture alias")
	for _, name := range []string{"target fixture alias", "source fixture alias", target.JapaneseName} {
		var alias models.ActressAlias
		require.NoError(t, db.Where("alias_name = ?", name).First(&alias).Error)
		require.Equal(t, source.JapaneseName, alias.CanonicalName)
	}
	require.Equal(t, []actressCLIMovieLink{{MovieContentID: "cli-merge-fixture", ActressID: target.ID}}, after.Links)
	require.Len(t, after.Credits, 1)
	wantCredit := before.Credits[0]
	wantCredit.ActressID = target.ID
	wantCredit.UpdatedAt = after.Credits[0].UpdatedAt
	require.Equal(t, wantCredit, after.Credits[0], "retry must move the existing credit without losing its provenance")
}
