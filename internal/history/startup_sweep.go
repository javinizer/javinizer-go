package history

import (
	"context"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/spf13/afero"
)

// SweepOnStartup runs the replacement sweep for synchronous callers such as
// the CLI. API servers should use SweepOnStartupWithContext so the sweep is
// tied to the server lifetime.
func SweepOnStartup(fs afero.Fs, repo database.BatchFileOperationRepositoryInterface) {
	SweepOnStartupWithContext(context.Background(), fs, repo)
}

// SweepOnStartupWithContext runs the replacement sweep with the caller's
// lifetime context. Failures are politely logged — startup availability must
// never hinge on sweep progress.
func SweepOnStartupWithContext(ctx context.Context, fs afero.Fs, repo database.BatchFileOperationRepositoryInterface) {
	sweeper := NewReplacementSweeper(fs, repo)
	healed, err := sweeper.Sweep(ctx)
	if err != nil {
		logging.Warnf("startup replacement sweep failed: %v", err)
		return
	}
	if healed > 0 {
		logging.Infof("startup replacement sweep healed %d backup(s)", healed)
	}
}
