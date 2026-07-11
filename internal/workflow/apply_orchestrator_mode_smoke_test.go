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

// TestApplyOrchestrator_AllModes_Smoke drives the real apply pipeline
// (applyOrchImpl + real organizer + afero MemMapFs) for every operation mode
// and asserts each mode's filesystem contract. This is the regression guard
// that prevents the skip-gate bug (in-place-norenamefolder skipping organize)
// from silently recurring or from being "fixed" in a way that breaks the
// other modes.
//
// Per-mode expectations:
//   - organize               : file moves to dest/<folder>/<file>; source gone
//   - in-place               : file renamed in source dir; source dir unchanged
//   - in-place-norenamefolder : file renamed in source dir; source dir unchanged
//   - metadata-artwork       : organize SKIPPED; file stays at source path
//   - preview                : organize SKIPPED; file stays at source path
func TestApplyOrchestrator_AllModes_Smoke(t *testing.T) {
	const (
		sourceDir  = "/source/folder"
		sourceFile = "/source/folder/old-name.mp4"
		destDir    = "/dest"
	)

	tests := []struct {
		name           string
		mode           operationmode.OperationMode
		destPath       string
		expectSkip     bool
		expectOrganize bool
		// expectSourceGone: true if the original source file must no longer exist
		expectSourceGone bool
		// expectNewPath: non-empty if a specific new path must exist after the run
		expectNewPath string
	}{
		{
			name:             "organize mode moves file to destination",
			mode:             operationmode.OperationModeOrganize,
			destPath:         destDir,
			expectSkip:       false,
			expectOrganize:   true,
			expectSourceGone: true,
			expectNewPath:    "/dest/ABC-123 Test Movie/ABC-123 Test Movie.mp4",
		},
		{
			name:             "in-place mode renames file in source dir",
			mode:             operationmode.OperationModeInPlace,
			destPath:         "",
			expectSkip:       false,
			expectOrganize:   true,
			expectSourceGone: true,
			expectNewPath:    "/source/folder/ABC-123 Test Movie.mp4",
		},
		{
			name:             "in-place-norenamefolder mode renames file only",
			mode:             operationmode.OperationModeInPlaceNoRenameFolder,
			destPath:         "",
			expectSkip:       false,
			expectOrganize:   true,
			expectSourceGone: true,
			expectNewPath:    "/source/folder/ABC-123 Test Movie.mp4",
		},
		{
			name:             "metadata-artwork mode skips organize (file untouched)",
			mode:             operationmode.OperationModeMetadataArtwork,
			destPath:         "",
			expectSkip:       true,
			expectOrganize:   false,
			expectSourceGone: false,
			expectNewPath:    "",
		},
		{
			name:             "preview mode skips organize (file untouched)",
			mode:             operationmode.OperationModePreview,
			destPath:         "",
			expectSkip:       true,
			expectOrganize:   false,
			expectSourceGone: false,
			expectNewPath:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			require.NoError(t, fs.MkdirAll(sourceDir, 0777))
			if tt.destPath != "" {
				require.NoError(t, fs.MkdirAll(tt.destPath, 0777))
			}
			require.NoError(t, afero.WriteFile(fs, sourceFile, []byte("video"), 0644))

			orgCfg := &organizer.Config{
				FileFormat:    "<ID> <TITLE>",
				FolderFormat:  "<ID> <TITLE>",
				RenameFile:    true,
				OperationMode: tt.mode,
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

			skipOrganize := !tt.mode.RequiresOrganize()
			require.Equal(t, tt.expectSkip, skipOrganize,
				"RequiresOrganize() skip-gate mismatch for %s", tt.mode)

			cmd := ApplyCmd{
				Movie:    &models.Movie{ID: "ABC-123", Title: "Test Movie"},
				Match:    models.FileMatchInfo{MovieID: "ABC-123", Path: sourceFile, Name: "old-name.mp4", Extension: ".mp4"},
				DestPath: tt.destPath,
				Organize: OrganizeOptions{
					Skip:        skipOrganize,
					MoveFiles:   true,
					ForceUpdate: true,
				},
				OperationMode: tt.mode,
			}

			result, err := impl.Execute(context.Background(), cmd, nil)
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, tt.expectOrganize, result.Steps.Organized,
				"Steps.Organized mismatch for %s", tt.mode)

			oldExists, _ := afero.Exists(fs, sourceFile)
			if tt.expectSourceGone {
				assert.False(t, oldExists, "source file should be gone for %s", tt.mode)
			} else {
				assert.True(t, oldExists, "source file should remain for %s", tt.mode)
			}

			if tt.expectNewPath != "" {
				newExists, _ := afero.Exists(fs, tt.expectNewPath)
				assert.True(t, newExists, "expected new path %s to exist for %s", tt.expectNewPath, tt.mode)
			}
		})
	}
}
