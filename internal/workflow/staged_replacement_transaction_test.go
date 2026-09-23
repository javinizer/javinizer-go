package workflow

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestStagedReplacementBatchRestoresPriorBytesOnHistoryRevert(t *testing.T) {
	for _, tc := range []struct {
		name string
		fs   afero.Fs
		root func(*testing.T) string
	}{
		{name: "afero", fs: afero.NewMemMapFs(), root: func(*testing.T) string { return "/txn" }},
		{name: "os", fs: afero.NewOsFs(), root: func(t *testing.T) string { return t.TempDir() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			root := tc.root(t)
			dest, staged := filepath.Join(root, "poster.jpg"), filepath.Join(root, "poster.stage")
			require.NoError(t, tc.fs.MkdirAll(root, 0o755))
			require.NoError(t, afero.WriteFile(tc.fs, dest, []byte("prior poster"), 0o640))
			require.NoError(t, afero.WriteFile(tc.fs, staged, []byte("new poster"), 0o600))

			db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "ledger.db"), LogLevel: "error"})
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			require.NoError(t, db.RunMigrationsOnStartup(ctx))
			repo := database.NewBatchFileOperationRepository(db)
			rl := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "staged-replace", tc.fs, nil, nil, nil)
			opID := beginP3Op(t, rl, "TXN-001")

			batch, err := downloader.NewReplacementBatch(tc.fs, opID, rl)
			require.NoError(t, err)
			require.NoError(t, batch.Preflight([]string{dest}))
			replaced, err := batch.BeforePublish(ctx, dest, true)
			require.NoError(t, err)
			require.True(t, replaced)
			require.NoError(t, tc.fs.Rename(staged, dest))
			require.NoError(t, batch.ConfirmPublish(ctx, dest))
			require.Equal(t, []byte("new poster"), mustReadStagedTxn(t, tc.fs, dest))
			require.NoError(t, rl.Complete(ctx, opID, &ApplyResult{Movie: &models.Movie{ID: "TXN-001"}}))

			ledger := p3Ledger(t, repo, opID)
			require.Len(t, ledger.Replacements, 1)
			require.True(t, ledger.Replacements[0].Installed)
			result, err := history.NewReverter(tc.fs, repo).RevertBatch(ctx, "staged-replace")
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, []byte("prior poster"), mustReadStagedTxn(t, tc.fs, dest))
		})
	}
}

func TestStagedFinalVideoReplacementApplyRevertModes(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode operationmode.OperationMode
		move bool
		link organizer.LinkMode
	}{
		{name: "move", mode: operationmode.OperationModeOrganize, move: true},
		{name: "copy", mode: operationmode.OperationModeOrganize},
		{name: "hardlink", mode: operationmode.OperationModeOrganize, link: organizer.LinkModeHard},
		{name: "symlink", mode: operationmode.OperationModeOrganize, link: organizer.LinkModeSoft},
		{name: "in-place", mode: operationmode.OperationModeInPlaceNoRenameFolder, move: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, _ := pr260ArtifactDB(t)
			movie := pr260FencedMovie(t, db, "replace-"+tc.name, "")
			fs, root, source, _, _, _, match := pr260FencedFiles(t, "replace-"+tc.name)
			if tc.link != organizer.LinkModeNone && !pr260LinkSupported(t, tc.link, source) {
				return
			}
			destRoot := filepath.Join(root, "library")
			if tc.mode == operationmode.OperationModeInPlaceNoRenameFolder {
				destRoot = filepath.Dir(source)
			}
			orch := pr260RealApply(fs, &movie, organizer.MediaFormatConfig{}, nil, false)
			plan, err := orch.organizer.(artifactPlanExecutor).PlanOrganize(t.Context(), organizer.OrganizeCmd{Match: match, Movie: &movie, DestDir: destRoot, ForceUpdate: true, MoveFiles: tc.move, LinkMode: tc.link, OperationMode: tc.mode})
			require.NoError(t, err)
			require.NoError(t, fs.MkdirAll(filepath.Dir(plan.TargetPath), 0o755))
			require.NoError(t, afero.WriteFile(fs, plan.TargetPath, []byte("prior final video"), 0o640))
			repo := database.NewBatchFileOperationRepository(db)
			orch.revertLog = NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "replace-"+tc.name, fs, nil, nil, nil)
			cmd := pr260FencedCommand(&movie, match, destRoot, pr260FencedCounter(t, db), tc.mode, false, tc.move, tc.link, false, false)

			result, err := orch.Execute(t.Context(), cmd)
			require.NoError(t, err)
			require.Equal(t, []byte("video"), mustReadStagedTxn(t, fs, result.OrganizeResult.NewPath))
			ledger := p3Ledger(t, repo, result.OperationID)
			require.Len(t, ledger.Replacements, 1)
			require.Equal(t, plan.TargetPath, ledger.Replacements[0].Destination)
			require.True(t, ledger.Replacements[0].Installed)

			_, err = history.NewReverter(fs, repo).RevertBatch(t.Context(), "replace-"+tc.name)
			require.NoError(t, err)
			require.Equal(t, []byte("prior final video"), mustReadStagedTxn(t, fs, plan.TargetPath))
			require.Equal(t, []byte("video"), mustReadStagedTxn(t, fs, source))
		})
	}
}

func TestStagedReplacementBatchLateFailureRollsBackCreatedAndReplaced(t *testing.T) {
	for _, fs := range []afero.Fs{afero.NewMemMapFs(), afero.NewOsFs()} {
		root := "/txn"
		if _, ok := fs.(*afero.OsFs); ok {
			root = t.TempDir()
		}
		require.NoError(t, fs.MkdirAll(root, 0o755))
		replacedDest := filepath.Join(root, "movie.nfo")
		createdDest := filepath.Join(root, "trailer.mp4")
		require.NoError(t, afero.WriteFile(fs, replacedDest, []byte("prior nfo"), 0o640))

		db, repo, rl := newP3RecorderHarness(t, filepath.Join(t.TempDir(), "rollback.db"))
		opID := beginP3Op(t, rl, "TXN-ROLLBACK")
		batch, err := downloader.NewReplacementBatch(fs, opID, rl)
		require.NoError(t, err)
		require.NoError(t, batch.Preflight([]string{replacedDest, createdDest}))
		_, err = batch.BeforePublish(t.Context(), replacedDest, true)
		require.NoError(t, err)
		require.NoError(t, afero.WriteFile(fs, replacedDest, []byte("new nfo"), 0o600))
		require.NoError(t, batch.ConfirmPublish(t.Context(), replacedDest))
		_, err = batch.BeforePublish(t.Context(), createdDest, true)
		require.NoError(t, err)
		require.NoError(t, afero.WriteFile(fs, createdDest, []byte("partial trailer"), 0o600))
		require.NoError(t, batch.ConfirmPublish(t.Context(), createdDest))

		require.NoError(t, batch.Rollback(context.Background()))
		require.Equal(t, []byte("prior nfo"), mustReadStagedTxn(t, fs, replacedDest))
		_, err = fs.Stat(createdDest)
		require.Error(t, err)
		require.Empty(t, p3Ledger(t, repo, opID).Replacements)
		require.NoError(t, db.Close())
	}
}

func mustReadStagedTxn(t *testing.T, fs afero.Fs, path string) []byte {
	t.Helper()
	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	return data
}

func TestStagedReplacementRevertRefusesForeignInstalledSubstitution(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "replace-foreign", "")
	fs, root, _, _, _, _, match := pr260FencedFiles(t, "replace-foreign")
	destRoot := filepath.Join(root, "library")
	orch := pr260RealApply(fs, &movie, organizer.MediaFormatConfig{}, nil, false)
	plan, err := orch.organizer.(artifactPlanExecutor).PlanOrganize(t.Context(), organizer.OrganizeCmd{Match: match, Movie: &movie, DestDir: destRoot, ForceUpdate: true, OperationMode: operationmode.OperationModeOrganize})
	require.NoError(t, err)
	require.NoError(t, fs.MkdirAll(filepath.Dir(plan.TargetPath), 0o755))
	require.NoError(t, afero.WriteFile(fs, plan.TargetPath, []byte("prior final video"), 0o640))
	repo := database.NewBatchFileOperationRepository(db)
	orch.revertLog = NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "replace-foreign", fs, nil, nil, nil)
	cmd := pr260FencedCommand(&movie, match, destRoot, pr260FencedCounter(t, db), operationmode.OperationModeOrganize, false, false, organizer.LinkModeNone, false, false)
	result, err := orch.Execute(t.Context(), cmd)
	require.NoError(t, err)
	ledger := p3Ledger(t, repo, result.OperationID)
	require.NotEmpty(t, ledger.Replacements[0].InstalledSHA256)
	require.NoError(t, afero.WriteFile(fs, plan.TargetPath, []byte("foreign replacement"), 0o640))
	_, err = history.NewReverter(fs, repo).RevertBatch(t.Context(), "replace-foreign")
	require.NoError(t, err)
	require.Equal(t, []byte("foreign replacement"), mustReadStagedTxn(t, fs, plan.TargetPath))
	ledger = p3Ledger(t, repo, result.OperationID)
	require.Len(t, ledger.Replacements, 1)
	require.Equal(t, []byte("prior final video"), mustReadStagedTxn(t, fs, ledger.Replacements[0].Backup))
}

type stagedLateOccupantOrganizer struct {
	organizer.OrganizerInterface
	artifactPlanExecutor
	fs      afero.Fs
	planted bool
}

func (o *stagedLateOccupantOrganizer) ExecuteOrganizePlan(plan *organizer.OrganizePlan, moveFiles bool, linkMode organizer.LinkMode) (*organizer.OrganizeResult, error) {
	if !o.planted && filepath.Clean(plan.SourcePath) != filepath.Clean(plan.TargetPath) {
		o.planted = true
		if err := afero.WriteFile(o.fs, plan.TargetPath, []byte("late foreign"), 0o644); err != nil {
			return nil, err
		}
	}
	return o.artifactPlanExecutor.ExecuteOrganizePlan(plan, moveFiles, linkMode)
}

func TestStagedFinalVideoLateOccupantFailsClosed(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "replace-late-occupant", "")
	fs, root, source, _, _, _, match := pr260FencedFiles(t, "replace-late-occupant")
	destRoot := filepath.Join(root, "library")
	orch := pr260RealApply(fs, &movie, organizer.MediaFormatConfig{}, nil, false)
	realOrganizer := orch.organizer
	executor := realOrganizer.(artifactPlanExecutor)
	plan, err := executor.PlanOrganize(t.Context(), organizer.OrganizeCmd{Match: match, Movie: &movie, DestDir: destRoot, ForceUpdate: true, MoveFiles: true, OperationMode: operationmode.OperationModeOrganize})
	require.NoError(t, err)
	require.NoError(t, fs.MkdirAll(filepath.Dir(plan.TargetPath), 0o755))
	require.NoError(t, afero.WriteFile(fs, plan.TargetPath, []byte("prior final video"), 0o640))
	orch.organizer = &stagedLateOccupantOrganizer{OrganizerInterface: realOrganizer, artifactPlanExecutor: executor, fs: fs}
	repo := database.NewBatchFileOperationRepository(db)
	orch.revertLog = NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "replace-late-occupant", fs, nil, nil, nil)
	cmd := pr260FencedCommand(&movie, match, destRoot, pr260FencedCounter(t, db), operationmode.OperationModeOrganize, false, true, organizer.LinkModeNone, false, false)

	result, err := orch.Execute(t.Context(), cmd)
	require.Error(t, err)
	require.NotNil(t, result)
	require.Equal(t, "late foreign", string(mustReadStagedTxn(t, fs, plan.TargetPath)))
	require.Equal(t, "video", string(mustReadStagedTxn(t, fs, source)))
	ledger := p3Ledger(t, repo, result.OperationID)
	require.Len(t, ledger.Replacements, 1)
	require.Equal(t, "prior final video", string(mustReadStagedTxn(t, fs, ledger.Replacements[0].Backup)))
}
