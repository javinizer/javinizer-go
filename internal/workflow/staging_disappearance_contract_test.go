package workflow_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// This public generator adapter performs the real write before simulating an
// external cleanup of the private staging directory. Only a stage beneath the
// fixture root is eligible; the source/library trees are never removal targets.
type disappearingStageGenerator struct {
	nfo.GeneratorInterface
	fixtureRoot string
	removed     string
	calls       int
}

func (g *disappearingStageGenerator) ResolveAndGenerate(ctx context.Context, movie *models.Movie, dest string, names nfo.NFONameConfig, video string, tags []string) (string, error) {
	path, err := g.GeneratorInterface.ResolveAndGenerate(ctx, movie, dest, names, video, tags)
	if err != nil || path == "" {
		return path, err
	}
	g.calls++
	if g.removed != "" {
		return path, nil
	}
	for dir := filepath.Dir(path); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if !strings.HasPrefix(filepath.Base(dir), ".javinizer-apply-") {
			continue
		}
		rel, relErr := filepath.Rel(g.fixtureRoot, dir)
		if relErr != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("refuse stage eviction outside fixture: %s", dir)
		}
		stageInfo, stageErr := os.Lstat(dir)
		if stageErr != nil {
			return "", stageErr
		}
		if !stageInfo.IsDir() || stageInfo.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("refuse eviction of non-directory or symlink stage: %s", dir)
		}
		if _, statErr := os.Stat(path); statErr != nil {
			return "", statErr
		}
		if removeErr := os.RemoveAll(dir); removeErr != nil {
			return "", removeErr
		}
		g.removed = dir
		return path, nil
	}
	return "", fmt.Errorf("generated NFO has no owned stage ancestor: %s", path)
}

func TestApplyStagingDisappearancePreservesSourceAndAllowsRetry(t *testing.T) {
	for _, move := range []bool{false, true} {
		t.Run(fmt.Sprintf("move=%t", move), func(t *testing.T) {
			root := t.TempDir()
			incoming, library := filepath.Join(root, "incoming"), filepath.Join(root, "library")
			require.NoError(t, os.MkdirAll(incoming, 0700))
			require.NoError(t, os.MkdirAll(library, 0700))
			source, foreign := filepath.Join(incoming, "STAGE-001 original.mp4"), filepath.Join(incoming, "unrelated.txt")
			require.NoError(t, os.WriteFile(source, []byte("owned primary bytes"), 0600))
			require.NoError(t, os.WriteFile(foreign, []byte("foreign bytes"), 0600))
			handle, err := os.Open(source)
			require.NoError(t, err)
			originalInfo, err := handle.Stat()
			require.NoError(t, err)
			require.NoError(t, handle.Close())
			cfg := config.DefaultConfig(nil, nil)
			cfg.Database.DSN = filepath.Join(root, "history.sqlite")
			cfg.System.TempDir = filepath.Join(root, "temp")
			cfg.Output.Template.FolderFormat = "<ID>"
			cfg.Output.Template.FileFormat = "<ID>"
			cfg.Output.Operation.AllowRevert = true
			_, err = config.Prepare(cfg)
			require.NoError(t, err)
			db, err := database.New(database.ConfigFromAppConfig(cfg))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
			movie := models.Movie{ContentID: "stage-disappearance", ID: "STAGE-001", Title: "Stage disappearance", RenderGeneration: 1, RenderDirty: true}
			require.NoError(t, db.Create(&movie).Error)
			repos := db.Repositories()
			fc, err := workflow.NewFactoryConfigFromRepos(cfg, scraperutil.NewScraperRegistry(), repos)
			require.NoError(t, err)
			gen := &disappearingStageGenerator{GeneratorInterface: fc.NFOGenerator, fixtureRoot: root}
			fc.NFOGenerator = gen
			factory, err := workflow.NewWorkflowFactory(fc)
			require.NoError(t, err)
			apply, err := factory.NewWorkflow("stage-disappearance")
			require.NoError(t, err)
			cmd := workflow.ApplyCmd{Movie: &movie, PersistedMovie: true, PublicationFence: repos.MovieRepo.(database.ApplyPublicationFencer), Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mp4", MovieID: movie.ID}, DestPath: library, Organize: workflow.OrganizeOptions{MoveFiles: move}, GenerateNFO: true, OperationMode: operationmode.OperationModeOrganize}
			result, err := apply.Apply(t.Context(), cmd)
			require.Error(t, err)
			require.ErrorIs(t, err, os.ErrNotExist)
			require.NotNil(t, result)
			require.Equal(t, "artifact_publication", result.FailedStep)
			require.Equal(t, 1, gen.calls)
			require.NotEmpty(t, gen.removed)
			require.NoDirExists(t, gen.removed)
			bytes, err := os.ReadFile(source)
			require.NoError(t, err)
			require.Equal(t, "owned primary bytes", string(bytes))
			afterInfo, err := os.Stat(source)
			require.NoError(t, err)
			require.True(t, os.SameFile(originalInfo, afterInfo), "failed publication must restore the admitted source object")
			bytes, err = os.ReadFile(foreign)
			require.NoError(t, err)
			require.Equal(t, "foreign bytes", string(bytes))
			require.NoFileExists(t, filepath.Join(library, movie.ID, movie.ID+".mp4"))
			rows, err := repos.BatchFileOpRepo.FindByBatchJobID(t.Context(), "stage-disappearance")
			require.NoError(t, err)
			require.Len(t, rows, 1)
			// Fully compensated publication is a terminal no-op: retaining target
			// ownership here could let Undo remove a later retry's output.
			require.True(t, result.PrePublication)
			require.Equal(t, models.RevertStatusNoOp, rows[0].RevertStatus)
			require.Empty(t, rows[0].NewPath)
			// NFOPath retains the original snapshot address, not publication
			// ownership; a no-op ledger must never turn it into a delete.
			require.Equal(t, filepath.Join(incoming, movie.ID+".nfo"), rows[0].NFOPath)
			require.NoFileExists(t, rows[0].NFOPath)
			ledger, err := models.ParseGeneratedFiles(rows[0].GeneratedFiles)
			require.NoError(t, err)
			require.False(t, ledger.PublicationPending)
			require.Empty(t, ledger.Delete)
			require.Empty(t, ledger.PlannedDeletes)
			if move {
				require.Len(t, ledger.MoveBack, 1, "durable move intent survives compensation, fenced by terminal NoOp")
				require.Equal(t, source, ledger.MoveBack[0].OriginalPath)
				require.NoFileExists(t, ledger.MoveBack[0].NewPath)
			} else {
				require.Empty(t, ledger.MoveBack)
			}
			require.Empty(t, ledger.Replacements)
			fresh, err := repos.MovieRepo.FindByContentID(t.Context(), movie.ContentID)
			require.NoError(t, err)
			cmd.Movie = fresh
			retryWorkflow, err := factory.NewWorkflow("stage-disappearance-retry")
			require.NoError(t, err)
			retry, err := retryWorkflow.Apply(t.Context(), cmd)
			require.NoError(t, err)
			require.Empty(t, retry.FailedStep)
			require.NotNil(t, retry.OrganizeResult)
			require.FileExists(t, retry.NFOPath)
			bytes, err = os.ReadFile(retry.OrganizeResult.NewPath)
			require.NoError(t, err)
			require.Equal(t, "owned primary bytes", string(bytes))
			require.Equal(t, 2, gen.calls)
			if move {
				require.Equal(t, retry.OrganizeResult.NewPath, ledger.MoveBack[0].NewPath)
			}
			// A terminal no-op must not replay an old move intent against the
			// successful new job, even when it names the exact same destination.
			published, err := os.Stat(retry.OrganizeResult.NewPath)
			require.NoError(t, err)
			undo, err := history.NewReverter(afero.NewOsFs(), repos.BatchFileOpRepo).RevertBatch(t.Context(), "stage-disappearance")
			require.NoError(t, err)
			require.Zero(t, undo.Failed)
			require.Zero(t, undo.Skipped)
			stillPublished, err := os.Stat(retry.OrganizeResult.NewPath)
			require.NoError(t, err)
			require.True(t, os.SameFile(published, stillPublished))
			bytes, err = os.ReadFile(retry.OrganizeResult.NewPath)
			require.NoError(t, err)
			require.Equal(t, "owned primary bytes", string(bytes))
			require.FileExists(t, retry.NFOPath)
			retained, err := repos.BatchFileOpRepo.FindByBatchJobID(t.Context(), "stage-disappearance")
			require.NoError(t, err)
			require.Equal(t, rows, retained, "terminal no-op Undo must not rewrite the original journal")
			if move {
				require.NoFileExists(t, source)
			} else {
				require.FileExists(t, source)
			}
			bytes, err = os.ReadFile(foreign)
			require.NoError(t, err)
			require.Equal(t, "foreign bytes", string(bytes))
		})
	}
}
