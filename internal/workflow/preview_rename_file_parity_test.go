package workflow

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
)

// Issue #226, spec requirement "preview and apply agree": the preview
// orchestrator must engage the forced-rename strategy only when the projected
// ForceRenameFile flag is set. rename-in-place projects false(applyplan), so
// preview must use the normal resolver and reflect the honest rename_file config.
func TestPreview_ForceRenameGate_FollowsProjectedFlag(t *testing.T) {
	cases := []struct {
		name        string
		forceRename bool
		wantForced  bool
	}{
		{"rename-in-place projection applies no force", false, false},
		{"rename-file projection keeps the force", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			forcedUsed := 0
			normalUsed := 0
			fs := afero.NewMemMapFs()
			strategy := &mockStrategyPlan{plan: &organizer.OrganizePlan{
				TargetDir:    "/source/ABC-123",
				TargetPath:   "/source/ABC-123/ABC-123.mp4",
				FolderName:   "ABC-123",
				BaseFileName: "ABC-123",
			}}
			orch := newPreviewOrchestrator(
				fs,
				&mockMatcherForPreview{},
				PreviewConfig{
					PathCfg: PreviewPathConfig{MediaFormatConfig: organizer.MediaFormatConfig{
						PosterFormat: ".jpg", FanartFormat: ".jpg",
					}},
					ResolveStrategy: func(_ operationmode.OperationMode) organizer.OperationStrategy {
						normalUsed++
						return strategy
					},
					ResolveForcedRenameStrategy: func(_ operationmode.OperationMode) organizer.OperationStrategy {
						forcedUsed++
						return strategy
					},
					OpMode: operationmode.OperationModeInPlace,
				},
				nfo.NFONameConfig{},
				template.NewEngine(),
				&mockNFOFieldMergerForPreview{},
				nil,
			).(*previewOrchImpl)

			result, err := orch.Execute(context.Background(), PreviewCmd{
				Movie:           &models.Movie{ID: "ABC-123", Title: "T"},
				FileResults:     []models.FileMatchInfo{{Path: "/source/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4", MovieID: "ABC-123"}},
				OperationMode:   operationmode.OperationModeInPlace,
				ForceRenameFile: tc.forceRename,
			})
			require.NoError(t, err)
			require.NotNil(t, result)
			if tc.wantForced {
				assert.GreaterOrEqual(t, forcedUsed, 1, "forced rename strategy must be used")
				assert.Equal(t, 0, normalUsed)
			} else {
				assert.Equal(t, 0, forcedUsed, "forced rename strategy must NOT be used when projection emits no force")
				assert.GreaterOrEqual(t, normalUsed, 1)
			}
		})
	}
}
