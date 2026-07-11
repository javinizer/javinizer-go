package workflow

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestApplyOrchestrator_InPlaceNoRenameFolder_RenamesFile is the end-to-end
// regression guard for the "Rename file only" operation mode
// (in-place-norenamefolder). The strategy
// (strategy_inplace_norenamefolder.go) correctly calls
// fsutil.MoveFileFs to rename the file in place, but the apply orchestrator
// only reaches the strategy when ApplyCmd.Organize.Skip is false. The skip
// gate lives in resolveOrganizeApplyConfig (internal/api/batch), which
// historically set `Skip: effectiveMode != Organize` — skipping organize for
// EVERY non-organize mode, so the file was never renamed.
//
// This test drives the real orchestrator with a real organizer over afero's
// MemMapFs and mirrors the builder's skip-gate decision, so it fails as long as
// the gate treats in-place-norenamefolder as skip. When the gate is fixed to
// let in-place rename modes run organize, the mirrored decision flips and the
// file is renamed on disk.
func TestApplyOrchestrator_InPlaceNoRenameFolder_RenamesFile(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/source/folder", 0777))
	require.NoError(t, afero.WriteFile(fs, "/source/folder/old-name.mp4", []byte("video"), 0644))

	orgCfg := &organizer.Config{
		FileFormat:    "<ID> <TITLE>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeInPlaceNoRenameFolder,
	}
	m, err := matcher.NewMatcher(&matcher.Config{})
	require.NoError(t, err)
	org := organizer.NewOrganizer(fs, orgCfg, template.NewEngine(), m)

	impl := newApplyOrchestrator(
		fs,
		org,
		nil, // downloader: stepDownload no-ops when nil
		nil, // nfoGen: stepNFO no-ops when nil
		&applyStubNFO{},
		ApplyConfig{},
		nil, // templateEngine: stepDisplayTitle falls back to Title
		noOpRevertLog{},
		nil, // tagRepo
		nil, // logger
	)

	mode := operationmode.OperationModeInPlaceNoRenameFolder
	// Mirror resolveOrganizeApplyConfig's skip gate (apply_config_builder.go):
	// `Skip: !effectiveMode.RequiresOrganize()`. in-place-norenamefolder
	// requires organize (it renames the file in place), so Skip is false and
	// the orchestrator runs the real strategy — renaming the file on disk.
	skipOrganize := !mode.RequiresOrganize()

	cmd := ApplyCmd{
		Movie: &models.Movie{ID: "ABC-123", Title: "Test Movie"},
		Match: models.FileMatchInfo{
			MovieID:   "ABC-123",
			Path:      "/source/folder/old-name.mp4",
			Name:      "old-name.mp4",
			Extension: ".mp4",
		},
		Organize: OrganizeOptions{
			Skip:        skipOrganize,
			MoveFiles:   true,
			ForceUpdate: true,
		},
		OperationMode: mode,
	}

	result, err := impl.Execute(context.Background(), cmd, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Steps.Organized, "organize step must run for in-place-norenamefolder")

	const wantNewPath = "/source/folder/ABC-123 Test Movie.mp4"
	exists, _ := afero.Exists(fs, wantNewPath)
	assert.True(t, exists, "file should be renamed to the templated filename")

	oldExists, _ := afero.Exists(fs, "/source/folder/old-name.mp4")
	assert.False(t, oldExists, "original file should be gone after rename")
}
