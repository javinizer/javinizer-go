package worker

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildApplyCmd_InPlaceModeOverride_FallsBackToSourceDir covers the
// in-place destPath fallback branch in buildApplyCmd:
//
//	if destPath == "" {
//	    if mode := cfg.OperationModeOverride; mode != "" && mode.RequiresOrganize() {
//	        destPath = sourceDir
//	    } else if cfg.OrganizeOptions.Skip {
//	        destPath = sourceDir
//	    }
//	}
//
// The worker package's tests never exercised the
// `mode != "" && mode.RequiresOrganize()` branch — they always set
// cfg.Destination (or inputs.Destination), so the empty-destPath fallback was
// never reached with an in-place override mode. This test passes an in-place
// mode that RequiresOrganize() (in-place-norenamefolder) with empty Destination
// on both cfg and inputs, and asserts DestPath falls back to the source file's
// directory so downstream download/NFO steps resolve a real directory.
func TestBuildApplyCmd_InPlaceModeOverride_FallsBackToSourceDir(t *testing.T) {
	wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}}}
	inputs := makeApplyInputs(wf)
	inputs.Destination = "" // no global destination either → fallback branch reached

	const filePath = "/source/folder/IPX-777.mp4"
	sourceDir := filepath.Dir(filePath)
	movie := &models.Movie{ID: "IPX-777", Title: "Test Movie"}
	fileResult := &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Movie:         movie,
	}

	cfg := ApplyPhaseConfig{
		OrganizeOptions:       workflow.OrganizeOptions{MoveFiles: true},
		Destination:           "", // empty → fallback engaged
		OperationModeOverride: operationmode.OperationModeInPlaceNoRenameFolder,
	}

	applyCmd, afc, shouldExecute := buildApplyCmd(filePath, movie, fileResult, inputs, cfg, context.Background())
	require.True(t, shouldExecute, "no PreApply hook → should execute")
	assert.Equal(t, sourceDir, applyCmd.DestPath,
		"in-place override mode requiring organize must fall back to the source file's dir when Destination is empty")
	assert.Equal(t, sourceDir, afc.Destination)
	assert.Equal(t, operationmode.OperationModeInPlaceNoRenameFolder, applyCmd.OperationMode,
		"override mode must propagate onto ApplyCmd.OperationMode")
}

// TestBuildApplyCmd_InPlaceModeOverride_SkipBranchStillHonored pins the sibling
// `else if cfg.OrganizeOptions.Skip` branch is NOT taken when an in-place
// override mode is present (the override branch wins), confirming the new
// branch is distinct from the legacy Skip-based fallback.
func TestBuildApplyCmd_InPlaceModeOverride_SkipBranchStillHonored(t *testing.T) {
	wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "IPX-777"}}}
	inputs := makeApplyInputs(wf)
	inputs.Destination = ""

	const filePath = "/source/folder/IPX-777.mp4"
	sourceDir := filepath.Dir(filePath)
	movie := &models.Movie{ID: "IPX-777"}
	fileResult := &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "IPX-777"},
		Status:        models.JobStatusCompleted,
		Movie:         movie,
	}

	// No override + Skip=true → legacy Skip fallback branch.
	cfg := ApplyPhaseConfig{
		OrganizeOptions:       workflow.OrganizeOptions{Skip: true},
		Destination:           "",
		OperationModeOverride: "",
	}

	applyCmd, _, shouldExecute := buildApplyCmd(filePath, movie, fileResult, inputs, cfg, context.Background())
	require.True(t, shouldExecute)
	assert.Equal(t, sourceDir, applyCmd.DestPath,
		"legacy Skip fallback must still resolve DestPath to the source dir when no override is set")
}
