package worker

import (
	"context"
	"encoding/json"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

func recordHistory(ctx context.Context, repo database.HistoryRepositoryInterface, h models.History) {
	if repo == nil {
		return
	}
	if err := repo.Create(ctx, &h); err != nil {
		logging.Warnf("[history] create failed for %s %s: %v", h.Operation, h.MovieID, err)
	}
}

func jobIDPtr(id models.JobID) *string {
	s := string(id)
	if s == "" {
		return nil
	}
	return &s
}

func nilGuardOrganizeNewPath(result *workflow.ApplyResult) string {
	if result == nil || result.OrganizeResult == nil {
		return ""
	}
	return result.OrganizeResult.NewPath
}

func organizeMetadata(mode string, result *workflow.ApplyResult) string {
	steps := "{}"
	if result != nil {
		if b, err := json.Marshal(result.Steps); err == nil {
			steps = string(b)
		}
	}
	m := map[string]any{
		"operation_mode": mode,
		"steps":          json.RawMessage(steps),
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func historyAuditContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}
