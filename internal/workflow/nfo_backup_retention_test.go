package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// Synthetic diagnostic: a successful metadata-only update retains the prior NFO
// for durable Undo; a restart sweep must not mistake that backup for litter.
func TestNFOBackupRetentionEnabledRestartUndoAndPrune(t *testing.T) {
	for _, end := range []string{"undo", "prune"} {
		t.Run(end, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			fs := afero.NewOsFs()
			dir := filepath.Join(root, "library")
			require.NoError(t, fs.MkdirAll(dir, 0755))
			source := filepath.Join(dir, "TEST-001.mp4")
			dest := filepath.Join(dir, "TEST-001.nfo")
			old := []byte("<movie><title>Before</title></movie>")
			require.NoError(t, afero.WriteFile(fs, source, []byte("synthetic video placeholder"), 0644))
			require.NoError(t, afero.WriteFile(fs, dest, old, 0664))
			before, err := fs.Stat(source)
			require.NoError(t, err)
			dsn := filepath.Join(root, "test.sqlite")
			db, err := database.New(&database.Config{Type: "sqlite", DSN: dsn, LogLevel: "error"})
			require.NoError(t, err)
			require.NoError(t, db.RunMigrationsOnStartup(ctx))
			repo := database.NewBatchFileOperationRepository(db)
			movie := models.Movie{ID: "TEST-001", ContentID: "synthetic-nfo-retention", Title: "After"}
			engine := template.NewEngine()
			mt, err := matcher.NewMatcher(&matcher.Config{})
			require.NoError(t, err)
			org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>"}, engine, mt)
			gen := nfo.NewGenerator(fs, &nfo.Config{FilenameTemplate: "<ID>.nfo", TemplateEngine: engine})
			log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "synthetic-job", fs, nil, nil, nil)
			orch := newApplyOrchestrator(fs, org, nil, gen, nfo.NewNFOImplementor(fs, &nfo.Config{FilenameTemplate: "<ID>.nfo"}, engine), ApplyConfig{NFONameCfg: nfo.NFONameConfig{FilenameTemplate: "<ID>.nfo"}}, engine, log, nil, nil)
			cmd := ApplyCmd{Movie: &movie, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mp4", MovieID: movie.ID}, DestPath: dir, Organize: OrganizeOptions{Skip: true}, OperationMode: operationmode.OperationModeMetadataArtwork, GenerateNFO: true, Merge: MergeOptions{ScalarStrategy: nfo.PreferScraper}, OverwriteExistingMedia: true, PublicationFence: replacedArtifactFence{&movie}}
			result, err := orch.Execute(ctx, cmd)
			require.NoError(t, err)
			require.True(t, result.Merged)
			var id uint
			_, err = fmt.Sscanf(result.OperationID, "%d", &id)
			require.NoError(t, err)
			row, err := repo.FindByID(ctx, id)
			require.NoError(t, err)
			gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
			require.NoError(t, err)
			require.Len(t, gf.Replacements, 1)
			backup := gf.Replacements[0].Backup
			require.Equal(t, dest, gf.Replacements[0].Destination)
			require.True(t, gf.Replacements[0].Installed)
			data, err := afero.ReadFile(fs, backup)
			require.NoError(t, err)
			require.Equal(t, old, data)
			data, err = afero.ReadFile(fs, dest)
			require.NoError(t, err)
			require.Contains(t, string(data), "After")
			t.Logf("successful update retained journaled backup %s; status %s", filepath.Base(backup), row.RevertStatus)
			require.NoError(t, db.Close())
			db, err = database.New(&database.Config{Type: "sqlite", DSN: dsn, LogLevel: "error"})
			require.NoError(t, err)
			defer db.Close()
			repo = database.NewBatchFileOperationRepository(db)
			foreign := dest + ".dlbak.0123456789abcdef.123"
			require.NoError(t, afero.WriteFile(fs, foreign, []byte("unowned"), 0644))
			sweeper := history.NewReplacementSweeper(fs, repo)
			_, err = sweeper.Sweep(ctx)
			require.NoError(t, err)
			data, err = afero.ReadFile(fs, backup)
			require.NoError(t, err)
			require.Equal(t, old, data)
			if end == "undo" {
				undo, err := history.NewReverter(fs, repo).RevertBatch(ctx, "synthetic-job")
				require.NoError(t, err)
				require.Equal(t, 1, undo.Succeeded)
				data, err = afero.ReadFile(fs, dest)
				require.NoError(t, err)
				require.Equal(t, old, data)
			} else {
				row, err = repo.FindByID(ctx, id)
				require.NoError(t, err)
				require.NoError(t, sweeper.PruneOperationBackups(ctx, []models.BatchFileOperation{*row}))
				data, err = afero.ReadFile(fs, dest)
				require.NoError(t, err)
				require.Contains(t, string(data), "After")
			}
			_, err = fs.Stat(backup)
			require.True(t, os.IsNotExist(err))
			data, err = afero.ReadFile(fs, foreign)
			require.NoError(t, err)
			require.Equal(t, "unowned", string(data))
			now, err := fs.Stat(source)
			require.NoError(t, err)
			require.True(t, os.SameFile(before, now))
			data, err = afero.ReadFile(fs, source)
			require.NoError(t, err)
			require.Equal(t, "synthetic video placeholder", string(data))
		})
	}
}
