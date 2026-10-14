package organizer

import (
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/stretchr/testify/require"
)

func TestStagingPathBudgetIsLocalAndSigned(t *testing.T) {
	for _, tc := range []struct {
		name, staged, final string
		max, want           int
		mode                operationmode.OperationMode
		nilConfig           bool
	}{
		{name: "longer stage", staged: "long-private-stage", final: "out", max: 100, want: 115, mode: operationmode.OperationModeInPlaceNoRenameFolder},
		{name: "shorter stage", staged: "tmp", final: "long-final-folder", max: 100, want: 86, mode: operationmode.OperationModeInPlaceNoRenameFolder},
		{name: "nonpositive guard", staged: "a", final: "very-long-final-folder", max: 1, want: 1, mode: operationmode.OperationModeInPlaceNoRenameFolder},
		{name: "ordinary command", staged: "stage", max: 100, want: 100, mode: operationmode.OperationModeInPlaceNoRenameFolder},
		{name: "disabled limit", staged: "stage", final: "out", max: 0, want: 0, mode: operationmode.OperationModeInPlaceNoRenameFolder},
		{name: "other strategy", staged: "stage", final: "out", max: 100, want: 100, mode: operationmode.OperationModeOrganize},
		{name: "nil config", staged: "stage", final: "out", nilConfig: true, mode: operationmode.OperationModeInPlaceNoRenameFolder},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{MaxPathLength: tc.max}
			org := &Organizer{config: cfg}
			if tc.nilConfig {
				org.config = nil
			}
			actual := org.withStagingPathBudget(OrganizeCmd{Match: models.FileMatchInfo{Path: filepath.Join(tc.staged, "movie.mkv")}, StagingFinalDir: tc.final, OperationMode: tc.mode})
			if tc.nilConfig {
				require.Same(t, org, actual)
				return
			}
			require.Equal(t, tc.want, actual.config.MaxPathLength)
			require.Equal(t, tc.max, cfg.MaxPathLength, "shared configuration must remain immutable")
		})
	}
}
