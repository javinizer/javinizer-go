package history

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
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

// SweepRootsOnStartupWithContext runs the replacement sweep over the given
// roots ONLY (codex P2 CLI bound): the pre-revert sweep heals the directories
// the target batch can touch, not every journaled root, so an unrelated hung
// network share cannot block the revert. Failures are politely logged — the
// revert never aborts on sweep progress; ctx cancellation (the caller's
// deadline) simply ends the sweep early.
func SweepRootsOnStartupWithContext(ctx context.Context, fs afero.Fs, repo database.BatchFileOperationRepositoryInterface, roots []string) {
	sweeper := NewReplacementSweeper(fs, repo)
	healed, err := sweeper.SweepDirs(ctx, roots)
	if err != nil {
		logging.Warnf("scoped pre-revert replacement sweep failed: %v", err)
		return
	}
	if healed > 0 {
		logging.Infof("scoped pre-revert replacement sweep healed %d backup(s)", healed)
	}
}

// OperationSweepRoots computes the unique, sorted set of directories a revert
// of ops can touch — every place a leftover replacement backup from an
// interrupted apply of THIS batch could sit. Sources span both the row
// columns and the generated-files ledger:
//   - original/new/NFO paths and the recorded original directory;
//   - delete-listed artifacts and move-back pairs;
//   - Begin-seeded destination roots;
//   - replacement destinations AND their backups.
//
// Rows whose ledger is unparseable still contribute their column paths (the
// sweep must not lose scope to the same corruption it exists to heal).
func OperationSweepRoots(ops []models.BatchFileOperation) []string {
	seen := map[string]bool{}
	var roots []string
	addDir := func(d string) {
		if strings.TrimSpace(d) == "" || seen[d] {
			return
		}
		seen[d] = true
		roots = append(roots, d)
	}
	addPath := func(p string) {
		if strings.TrimSpace(p) == "" {
			return
		}
		addDir(filepath.Dir(p))
	}
	for i := range ops {
		op := &ops[i]
		addPath(op.OriginalPath)
		addPath(op.NewPath)
		addPath(op.NFOPath)
		addDir(op.OriginalDirPath)
		gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
		if err != nil {
			continue
		}
		for _, p := range gf.Delete {
			addPath(p)
		}
		for _, fm := range gf.MoveBack {
			addPath(fm.OriginalPath)
			addPath(fm.NewPath)
		}
		for _, r := range gf.Roots {
			addDir(r)
		}
		for _, rep := range gf.Replacements {
			addPath(rep.Destination)
			addPath(rep.Backup)
		}
	}
	sort.Strings(roots)
	return roots
}
