package actress

import (
	"context"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

func importActressRecords(ctx context.Context, repo *database.ActressRepository, actresses []models.Actress) (imported, skipped, errorsCount int) {
	summary := importActressRecordsWithOptions(ctx, repo, actresses, database.ActressImportOptions{})
	return summary.Imported, summary.Skipped, summary.Errors
}
