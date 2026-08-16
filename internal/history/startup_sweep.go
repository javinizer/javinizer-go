package history

import (
	"context"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/spf13/afero"
)

// SweepOnStartup runs the replacement sweep at API boot (P3). Failures are
// politely logged — startup availability must never hinge on sweep progress.
func SweepOnStartup(fs afero.Fs, repo database.BatchFileOperationRepositoryInterface) {
	sweeper := NewReplacementSweeper(fs, repo)
	healed, err := sweeper.Sweep(context.Background())
	if err != nil {
		logging.Warnf("startup replacement sweep failed: %v", err)
		return
	}
	if healed > 0 {
		logging.Infof("startup replacement sweep healed %d backup(s)", healed)
	}
}
