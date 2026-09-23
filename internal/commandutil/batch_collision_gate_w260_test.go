package commandutil

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunBatchCommand_OpenCollisionBlocksCLIOrganize(t *testing.T) {
	configPath, src, dest, dbPath := setupSingleFileBatch(t, "GOOD-704")

	db := openAssertionDB(t, dbPath)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	movie := models.Movie{ContentID: "GOOD-704", ID: "GOOD-704"}
	require.NoError(t, db.DB.Create(&movie).Error)
	actress := models.Actress{
		DMMID: 1, FirstName: "Test", LastName: "Actor", Verified: true, Origin: "user",
	}
	require.NoError(t, db.DB.Create(&actress).Error)
	credit := models.MovieCredit{
		MovieContentID: movie.ContentID, ActressID: actress.ID, CreditedName: "Test Actor", Origin: string(models.CreditOriginScrape),
	}
	require.NoError(t, db.DB.Create(&credit).Error)
	require.NoError(t, db.DB.Create(&models.CreditCollision{
		CreditID: credit.ID, MovieContentID: movie.ContentID, Field: "fixture", Status: models.CollisionStatusOpen,
	}).Error)
	require.NoError(t, db.Close())

	var buf bytes.Buffer
	err := RunBatchCommand(context.Background(), &buf, BatchCommandOptions{
		ConfigFile:   configPath,
		SourcePath:   src,
		Destination:  dest,
		Recursive:    true,
		MoveFiles:    true,
		CommandLabel: "Javinizer Sort",
		ActionVerb:   "Processing files",
		Resolved:     &workflow.ResolvedSeamStrings{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "apply phase failed")
	assert.FileExists(t, filepath.Join(src, "GOOD-704.mp4"))
	assert.NoDirExists(t, filepath.Join(dest, "GOOD-704"))
	assert.NotContains(t, buf.String(), "Organized 1 file(s)")

	verifyDB := openAssertionDB(t, dbPath)
	var openCount int64
	require.NoError(t, verifyDB.DB.Model(&models.CreditCollision{}).
		Where("movie_content_id = ? AND status = ?", movie.ContentID, models.CollisionStatusOpen).
		Count(&openCount).Error)
	assert.EqualValues(t, 1, openCount)
}

func TestRunBatchCommand_ApplyFailureIsTerminal(t *testing.T) {
	configPath, src, dest, _ := setupSingleFileBatch(t, "GOOD-705")
	occupied := filepath.Join(dest, "GOOD-705", "GOOD-705.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(occupied), 0o700))
	require.NoError(t, os.WriteFile(occupied, []byte("occupied"), 0o600))

	var buf bytes.Buffer
	err := RunBatchCommand(context.Background(), &buf, BatchCommandOptions{
		ConfigFile: configPath, SourcePath: src, Destination: dest, Recursive: true,
		MoveFiles: true, CommandLabel: "Javinizer Sort", ActionVerb: "Processing files",
		Resolved: &workflow.ResolvedSeamStrings{},
	})
	require.ErrorContains(t, err, "apply failed for 1 file(s)")
	assert.Contains(t, buf.String(), "Apply failed for 1 file(s)")
	assert.NotContains(t, buf.String(), "Complete!")
	assert.FileExists(t, filepath.Join(src, "GOOD-705.mp4"))
}

func TestDefaultSummaryPrinter_ApplyFailure(t *testing.T) {
	var buf bytes.Buffer
	defaultSummaryPrinter(&buf, BatchCommandOptions{}, BatchCommandResult{
		ScanResult: &workflow.ScanAndMatchResult{}, ApplyFailedCount: 2,
	})
	assert.Contains(t, buf.String(), "Apply failed for 2 file(s)")
	assert.NotContains(t, buf.String(), "Complete!")
}
