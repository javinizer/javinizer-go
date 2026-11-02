package workflow_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// Preserve all real organizer capabilities and delegate every plan before an
// owned staging directory exists. The configured planner becomes unavailable
// once allocation has actually completed, independent of preflight call count.
// Observation is read-only and never follows symlinks or accesses private state.
type stagedApplyPlanFailure struct {
	*organizer.Organizer
	root          string
	failure       error
	stage         string
	failedCommand organizer.OrganizeCmd
	delegated     int
}

func (p *stagedApplyPlanFailure) PlanOrganize(ctx context.Context, cmd organizer.OrganizeCmd) (*organizer.OrganizePlan, error) {
	if p.stage == "" {
		var stage string
		err := filepath.WalkDir(p.root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !strings.HasPrefix(entry.Name(), ".javinizer-apply-") {
				return nil
			}
			rel, err := filepath.Rel(p.root, path)
			if err != nil {
				return err
			}
			if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return fmt.Errorf("stage observation escaped fixture: %s", path)
			}
			info, err := os.Lstat(path)
			if err != nil {
				return err
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return nil
			}
			stage = path
			return filepath.SkipAll
		})
		if err != nil {
			return nil, fmt.Errorf("observe allocated fixture stage: %w", err)
		}
		if stage != "" {
			p.stage, p.failedCommand = stage, cmd
			return nil, p.failure
		}
	}
	plan, err := p.Organizer.PlanOrganize(ctx, cmd)
	if err == nil {
		p.delegated++
	}
	return plan, err
}

func TestApplyPlannerFailureAfterStageAllocationPreservesSourceAndCanRetry(t *testing.T) {
	for _, move := range []bool{false, true} {
		t.Run(fmt.Sprintf("move=%t", move), func(t *testing.T) {
			root := t.TempDir()
			incoming, library := filepath.Join(root, "incoming"), filepath.Join(root, "library")
			require.NoError(t, os.MkdirAll(incoming, 0700))
			require.NoError(t, os.MkdirAll(library, 0700))
			source, foreign := filepath.Join(incoming, "PLAN-001 original.mp4"), filepath.Join(incoming, "unrelated.txt")
			require.NoError(t, os.WriteFile(source, []byte("source primary"), 0600))
			require.NoError(t, os.WriteFile(foreign, []byte("foreign content"), 0600))
			handle, err := os.Open(source)
			require.NoError(t, err)
			original, err := handle.Stat()
			require.NoError(t, err)
			require.NoError(t, handle.Close())
			foreignHandle, err := os.Open(foreign)
			require.NoError(t, err)
			foreignOriginal, err := foreignHandle.Stat()
			require.NoError(t, err)
			require.NoError(t, foreignHandle.Close())
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
			movie := models.Movie{ContentID: "planner-recovery", ID: "PLAN-001", Title: "Planner recovery", RenderGeneration: 1, RenderDirty: true}
			require.NoError(t, db.Create(&movie).Error)
			repos := db.Repositories()
			registry := scraperutil.NewScraperRegistry()
			t.Cleanup(func() { require.NoError(t, registry.Close()) })
			fc, err := workflow.NewFactoryConfigFromRepos(cfg, registry, repos)
			require.NoError(t, err)
			realPlanner, ok := fc.Organizer.(*organizer.Organizer)
			require.True(t, ok)
			failure := errors.New("configured organizer planning service unavailable")
			planner := &stagedApplyPlanFailure{Organizer: realPlanner, failure: failure, root: root}
			fc.Organizer = planner
			factory, err := workflow.NewWorkflowFactory(fc)
			require.NoError(t, err)
			apply, err := factory.NewWorkflow("planner-failure")
			require.NoError(t, err)
			cmd := workflow.ApplyCmd{Movie: &movie, PersistedMovie: true, PublicationFence: repos.MovieRepo.(database.ApplyPublicationFencer), Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mp4", MovieID: movie.ID}, DestPath: library, Organize: workflow.OrganizeOptions{MoveFiles: move}, GenerateNFO: true, OperationMode: operationmode.OperationModeOrganize}
			result, err := apply.Apply(t.Context(), cmd)
			require.ErrorIs(t, err, failure)
			require.NotNil(t, result)
			require.Equal(t, "organize", result.FailedStep)
			require.True(t, result.PrePublication)
			require.Nil(t, result.OrganizeResult)
			require.Empty(t, result.NFOPath)
			require.GreaterOrEqual(t, planner.delegated, 1, "real pre-allocation planning must succeed")
			require.NotEmpty(t, planner.stage, "failure requires an observed allocated stage, not a call count")
			require.NoDirExists(t, planner.stage)
			require.Equal(t, source, planner.failedCommand.Match.Path)
			require.Equal(t, library, planner.failedCommand.DestDir)
			require.Equal(t, move, planner.failedCommand.MoveFiles)
			after, err := os.Stat(source)
			require.NoError(t, err)
			require.True(t, os.SameFile(original, after))
			data, err := os.ReadFile(source)
			require.NoError(t, err)
			require.Equal(t, "source primary", string(data))
			data, err = os.ReadFile(foreign)
			require.NoError(t, err)
			require.Equal(t, "foreign content", string(data))
			foreignAfter, statErr := os.Stat(foreign)
			require.NoError(t, statErr)
			require.True(t, os.SameFile(foreignOriginal, foreignAfter))
			require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				require.False(t, strings.HasPrefix(entry.Name(), ".javinizer-apply-"), "failed plan must clean its owned staging tree: %s", path)
				if !entry.IsDir() && strings.HasPrefix(path, library+string(filepath.Separator)) {
					t.Errorf("failed plan published a library file: %s", path)
				}
				return nil
			}))
			rows, err := repos.BatchFileOpRepo.FindByBatchJobID(t.Context(), "planner-failure")
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.Equal(t, models.RevertStatusNoOp, rows[0].RevertStatus)
			require.Empty(t, rows[0].NewPath)
			ledger, err := models.ParseGeneratedFiles(rows[0].GeneratedFiles)
			require.NoError(t, err)
			require.False(t, ledger.PublicationPending)
			require.Empty(t, ledger.MoveBack)
			require.Empty(t, ledger.Delete)
			require.Empty(t, ledger.PlannedDeletes)
			require.Empty(t, ledger.Replacements)
			fresh, err := repos.MovieRepo.FindByContentID(t.Context(), movie.ContentID)
			require.NoError(t, err)
			cmd.Movie = fresh
			retry, err := factory.NewWorkflow("planner-retry")
			require.NoError(t, err)
			published, err := retry.Apply(t.Context(), cmd)
			require.NoError(t, err)
			require.Empty(t, published.FailedStep)
			require.NotNil(t, published.OrganizeResult)
			require.FileExists(t, published.NFOPath)
			data, err = os.ReadFile(published.OrganizeResult.NewPath)
			require.NoError(t, err)
			require.Equal(t, "source primary", string(data))
			winner, err := os.Stat(published.OrganizeResult.NewPath)
			require.NoError(t, err)
			undo, err := history.NewReverter(afero.NewOsFs(), repos.BatchFileOpRepo).RevertBatch(t.Context(), "planner-failure")
			require.NoError(t, err)
			require.Zero(t, undo.Total)
			require.Zero(t, undo.Failed)
			require.Zero(t, undo.Skipped)
			afterUndo, err := os.Stat(published.OrganizeResult.NewPath)
			require.NoError(t, err)
			require.True(t, os.SameFile(winner, afterUndo))
			data, err = os.ReadFile(published.OrganizeResult.NewPath)
			require.NoError(t, err)
			require.Equal(t, "source primary", string(data))
			require.FileExists(t, published.NFOPath)
			retained, err := repos.BatchFileOpRepo.FindByBatchJobID(t.Context(), "planner-failure")
			require.NoError(t, err)
			require.Equal(t, rows, retained)
			if move {
				require.NoFileExists(t, source)
			} else {
				require.FileExists(t, source)
			}
			data, err = os.ReadFile(foreign)
			require.NoError(t, err)
			require.Equal(t, "foreign content", string(data))
		})
	}
}
