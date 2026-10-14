package database

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func planMergeWhileMutatingPair(t *testing.T, db *DB, repo *ActressRepository, targetID, sourceID uint, resolutions map[string]string, mutate func()) *MergePlan {
	t.Helper()
	loaded := make(chan struct{})
	resume := make(chan struct{})
	var once sync.Once
	callbackName := fmt.Sprintf("test:pause-merge-plan-%d-%d", targetID, sourceID)
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != "actresses" {
			return
		}
		if actress, ok := tx.Statement.Dest.(*models.Actress); ok && actress.ID == sourceID {
			once.Do(func() {
				close(loaded)
				<-resume
			})
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	type planResult struct {
		plan *MergePlan
		err  error
	}
	result := make(chan planResult, 1)
	go func() {
		plan, err := repo.merger.PlanMerge(context.Background(), targetID, sourceID, resolutions)
		result <- planResult{plan: plan, err: err}
	}()
	<-loaded
	mutate()
	close(resume)
	planned := <-result
	require.NoError(t, planned.err)
	require.NoError(t, db.Callback().Query().Remove(callbackName))
	return planned.plan
}

type executeMergeReadContextKey struct{}

func TestExecuteMergeRetriesWholeTransactionAfterWALSnapshotConflict(t *testing.T) {
	tests := []struct {
		name              string
		initialTarget     models.Actress
		initialSource     models.Actress
		mutate            func(context.Context, *ActressRepository, uint, uint) error
		expectedTarget    models.Actress
		expectedSource    models.Actress
		translationOwner  string
		translationSource string
	}{
		{
			name:          "target canonical update",
			initialTarget: models.Actress{DMMID: 10, FirstName: "Old", LastName: "Target", JapaneseName: "旧標", ThumbURL: "old-target.jpg", Aliases: "Target Alias", Origin: ActressOriginScrape},
			initialSource: models.Actress{DMMID: 20, FirstName: "Source", LastName: "Person", JapaneseName: "源", ThumbURL: "source.jpg", Aliases: "Source Alias", Origin: ActressOriginScrape},
			mutate: func(ctx context.Context, repo *ActressRepository, targetID, _ uint) error {
				return repo.UpdateCanonicalFields(ctx, targetID, "Source", "Person", "源", "source.jpg")
			},
			expectedTarget:    models.Actress{DMMID: 10, FirstName: "Source", LastName: "Person", JapaneseName: "源", ThumbURL: "source.jpg", Aliases: "Target Alias", Verified: true, Origin: ActressOriginUser},
			expectedSource:    models.Actress{DMMID: 20, FirstName: "Source", LastName: "Person", JapaneseName: "源", ThumbURL: "source.jpg", Aliases: "Source Alias", Origin: ActressOriginScrape},
			translationOwner:  "target",
			translationSource: "Person Source",
		},
		{
			name:          "source promotion",
			initialTarget: models.Actress{DMMID: 30, Aliases: "Target Alias", Origin: ActressOriginScrape},
			initialSource: models.Actress{DMMID: 40, FirstName: "Old", LastName: "Source", JapaneseName: "旧源", ThumbURL: "old-source.jpg", Aliases: "Source Alias", Origin: ActressOriginScrape},
			mutate: func(ctx context.Context, repo *ActressRepository, _, sourceID uint) error {
				return repo.PromoteCandidate(ctx, sourceID, "Current", "Source", "現源", "current-source.jpg")
			},
			expectedTarget:    models.Actress{DMMID: 30, Aliases: "Target Alias", Origin: ActressOriginScrape},
			expectedSource:    models.Actress{DMMID: 40, FirstName: "Current", LastName: "Source", JapaneseName: "現源", ThumbURL: "current-source.jpg", Aliases: "Source Alias", Verified: true, Origin: ActressOriginUser},
			translationOwner:  "source",
			translationSource: "Source Current",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db, err := New(&Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "merge-race.db"), LogLevel: "silent"})
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
			sqlDB, err := db.DB.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(2)
			repo := NewActressRepository(db)
			target := tc.initialTarget
			source := tc.initialSource
			require.NoError(t, db.Create(&target).Error)
			require.NoError(t, db.Create(&source).Error)
			movie := models.Movie{ContentID: "merge-race", ID: "MERGE-RACE"}
			require.NoError(t, db.Create(&movie).Error)
			require.NoError(t, db.Model(&movie).Association("Actresses").Append(&source))
			require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: movie.ContentID, ActressID: source.ID, CreditedName: "Original Credit"}).Error)
			translationActressID := target.ID
			if tc.translationOwner == "source" {
				translationActressID = source.ID
			}
			require.NoError(t, db.Create(&models.ActressTranslation{ActressID: translationActressID, Language: "en", DisplayName: "Translated", SourceName: tc.translationSource}).Error)
			plan, err := repo.merger.PlanMerge(t.Context(), target.ID, source.ID, nil)
			require.NoError(t, err)

			ctx, cancel := context.WithTimeout(context.WithValue(t.Context(), executeMergeReadContextKey{}, true), 10*time.Second)
			defer cancel()
			pairLoaded := make(chan struct{})
			releaseMerge := make(chan struct{})
			var pauseOnce sync.Once
			var targetLoads atomic.Int32
			callbackName := "test:pause_execute_merge_pair_loaded"
			require.NoError(t, db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
				if tx.Statement == nil || tx.Statement.Table != "actresses" || tx.Statement.Context.Value(executeMergeReadContextKey{}) != true {
					return
				}
				loaded, ok := tx.Statement.Dest.(*models.Actress)
				if !ok || loaded.ID != target.ID {
					return
				}
				targetLoads.Add(1)
				pauseOnce.Do(func() {
					close(pairLoaded)
					select {
					case <-releaseMerge:
					case <-ctx.Done():
					}
				})
			}))
			t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

			type mergeOutcome struct {
				result *ActressMergeResult
				err    error
			}
			mergeDone := make(chan mergeOutcome, 1)
			go func() {
				result, err := repo.merger.ExecuteMerge(ctx, plan, db)
				mergeDone <- mergeOutcome{result: result, err: err}
			}()
			select {
			case <-pairLoaded:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			require.NoError(t, tc.mutate(t.Context(), repo, target.ID, source.ID))
			close(releaseMerge)

			var outcome mergeOutcome
			select {
			case outcome = <-mergeDone:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			require.NoError(t, outcome.err)
			require.GreaterOrEqual(t, targetLoads.Load(), int32(2))

			tc.expectedTarget.ID = target.ID
			tc.expectedSource.ID = source.ID
			expectedMerged := mergeActressValuesResolved(&tc.expectedTarget, &tc.expectedSource, mergeDecisions{})
			expectedAliases, expectedAliasesAdded, _ := mergeAliasValues(tc.expectedTarget.Aliases, collectActressAliasCandidates(&tc.expectedSource), canonicalActressName(&expectedMerged))
			require.Equal(t, expectedMerged.DMMID, outcome.result.MergedActress.DMMID)
			require.Equal(t, expectedMerged.FirstName, outcome.result.MergedActress.FirstName)
			require.Equal(t, expectedMerged.LastName, outcome.result.MergedActress.LastName)
			require.Equal(t, expectedMerged.JapaneseName, outcome.result.MergedActress.JapaneseName)
			require.Equal(t, expectedMerged.ThumbURL, outcome.result.MergedActress.ThumbURL)
			require.Equal(t, expectedMerged.Verified, outcome.result.MergedActress.Verified)
			require.Equal(t, expectedMerged.Origin, outcome.result.MergedActress.Origin)
			require.Equal(t, expectedAliases, outcome.result.MergedActress.Aliases)
			require.Equal(t, expectedAliasesAdded, outcome.result.AliasesAdded)
			require.Equal(t, len(buildActressMergeConflicts(&tc.expectedTarget, &tc.expectedSource)), outcome.result.ConflictsResolved)
			require.Equal(t, 1, outcome.result.UpdatedMovies)

			var storedSource models.Actress
			require.ErrorIs(t, db.First(&storedSource, source.ID).Error, gorm.ErrRecordNotFound)
			var credit models.MovieCredit
			require.NoError(t, db.Where("movie_content_id = ?", movie.ContentID).First(&credit).Error)
			require.Equal(t, target.ID, credit.ActressID)
			var translations []models.ActressTranslation
			require.NoError(t, db.Where("actress_id = ?", target.ID).Find(&translations).Error)
			require.Empty(t, translations, "the concurrent canonical mutation invalidates its prior translations")
			var aliasRows []models.ActressAlias
			require.NoError(t, db.Order("id").Find(&aliasRows).Error)
			require.NotEmpty(t, aliasRows)
			for _, alias := range aliasRows {
				require.Equal(t, canonicalActressName(&expectedMerged), alias.CanonicalName)
			}
		})
	}
}

func TestExecuteMergeReturnsTransactionCapturedTargetWithoutPostCommitRead(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	target := models.Actress{DMMID: 510, FirstName: "Target", LastName: "Person", ThumbURL: "target.jpg"}
	source := models.Actress{DMMID: 520, FirstName: "Source", LastName: "Person", ThumbURL: "source.jpg"}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	plan, err := repo.merger.PlanMerge(t.Context(), target.ID, source.ID, nil)
	require.NoError(t, err)

	postCommitReadErr := fmt.Errorf("synthetic post-commit actress read failure")
	var transactionTargetReads atomic.Int32
	var postCommitTargetReads atomic.Int32
	callbackName := "test:fail_execute_merge_post_commit_target_read"
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		actress, ok := tx.Statement.Dest.(*models.Actress)
		if !ok || actress.ID != target.ID {
			return
		}
		if _, inTransaction := tx.Statement.ConnPool.(*sql.Tx); inTransaction {
			transactionTargetReads.Add(1)
			return
		}
		postCommitTargetReads.Add(1)
		tx.AddError(postCommitReadErr)
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callbackName) })

	result, err := repo.merger.ExecuteMerge(t.Context(), plan, db)
	require.NoError(t, err)
	require.GreaterOrEqual(t, transactionTargetReads.Load(), int32(2), "target is loaded and finally captured within the transaction")
	require.Zero(t, postCommitTargetReads.Load(), "the committed result must not be re-read through the repository")
	require.NoError(t, db.Callback().Query().Remove(callbackName))

	var storedTarget models.Actress
	require.NoError(t, db.First(&storedTarget, target.ID).Error)
	require.Equal(t, storedTarget, result.MergedActress)
	var storedSource models.Actress
	require.ErrorIs(t, db.First(&storedSource, source.ID).Error, gorm.ErrRecordNotFound)
}

func TestExecuteMergeRollsBackWhenFinalTargetCaptureFails(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	target := models.Actress{DMMID: 610, FirstName: "Target", ThumbURL: "target.jpg"}
	source := models.Actress{DMMID: 620, FirstName: "Source", ThumbURL: "source.jpg"}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	plan, err := repo.merger.PlanMerge(t.Context(), target.ID, source.ID, nil)
	require.NoError(t, err)

	finalCaptureErr := fmt.Errorf("synthetic final target capture failure")
	var sourceDeleted atomic.Bool
	deleteCallback := "test:mark_execute_merge_source_deleted"
	queryCallback := "test:fail_execute_merge_final_target_capture"
	require.NoError(t, db.Callback().Delete().After("gorm:delete").Register(deleteCallback, func(tx *gorm.DB) {
		if tx.Statement.Table == "actresses" {
			sourceDeleted.Store(true)
		}
	}))
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(queryCallback, func(tx *gorm.DB) {
		actress, ok := tx.Statement.Dest.(*models.Actress)
		if ok && actress.ID == target.ID && sourceDeleted.Load() {
			tx.AddError(finalCaptureErr)
		}
	}))
	t.Cleanup(func() {
		_ = db.Callback().Delete().Remove(deleteCallback)
		_ = db.Callback().Query().Remove(queryCallback)
	})

	result, err := repo.merger.ExecuteMerge(t.Context(), plan, db)
	require.Nil(t, result)
	require.ErrorIs(t, err, finalCaptureErr)
	require.NoError(t, db.Callback().Delete().Remove(deleteCallback))
	require.NoError(t, db.Callback().Query().Remove(queryCallback))

	var actresses []models.Actress
	require.NoError(t, db.Order("id").Find(&actresses).Error)
	require.Len(t, actresses, 2)
	require.Equal(t, target.DMMID, actresses[0].DMMID)
	require.Equal(t, target.ThumbURL, actresses[0].ThumbURL)
	require.Equal(t, source.DMMID, actresses[1].DMMID)
}

func TestExecuteMergeRecomputesExplicitThumbDecisionFromCurrentRows(t *testing.T) {
	tests := []struct {
		name       string
		resolution string
		expected   string
	}{
		{name: "target wins preserves current target thumb when source changes", resolution: MergeResolutionTarget, expected: "current-target.jpg"},
		{name: "source wins uses current source thumb", resolution: MergeResolutionSource, expected: "current-source.jpg"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			repo := NewActressRepository(db)
			target := models.Actress{FirstName: "Target", ThumbURL: "old-target.jpg"}
			source := models.Actress{FirstName: "Source", ThumbURL: "old-source.jpg"}
			require.NoError(t, db.Create(&target).Error)
			require.NoError(t, db.Create(&source).Error)
			plan, err := repo.merger.PlanMerge(t.Context(), target.ID, source.ID, map[string]string{"thumb_url": tc.resolution})
			require.NoError(t, err)
			require.NoError(t, db.Model(&models.Actress{}).Where("id = ?", target.ID).Update("thumb_url", "current-target.jpg").Error)
			require.NoError(t, db.Model(&models.Actress{}).Where("id = ?", source.ID).Update("thumb_url", "current-source.jpg").Error)

			result, err := repo.merger.ExecuteMerge(t.Context(), plan, db)
			require.NoError(t, err)
			require.Equal(t, tc.expected, result.MergedActress.ThumbURL)
		})
	}
}

func TestPlanMergePreservesOnlyExplicitDecisionIntent(t *testing.T) {
	tests := []struct {
		name          string
		initialTarget string
		initialSource string
		currentTarget string
		currentSource string
		resolutions   map[string]string
		expected      string
	}{
		{name: "old conflict to current fill omitted nil", initialTarget: "Old Target", initialSource: "Old Source", currentSource: "Current Source", expected: "Current Source"},
		{name: "old conflict to current fill omitted empty", initialTarget: "Old Target", initialSource: "Old Source", currentSource: "Current Source", resolutions: map[string]string{}, expected: "Current Source"},
		{name: "old conflict to current fill explicit target", initialTarget: "Old Target", initialSource: "Old Source", currentSource: "Current Source", resolutions: map[string]string{colFirstName: MergeResolutionTarget}},
		{name: "old conflict to current fill explicit source", initialTarget: "Old Target", initialSource: "Old Source", currentSource: "Current Source", resolutions: map[string]string{colFirstName: MergeResolutionSource}, expected: "Current Source"},
		{name: "old conflict to source cleared omitted", initialTarget: "Old Target", initialSource: "Old Source", currentTarget: "Current Target", expected: "Current Target"},
		{name: "old conflict to source cleared explicit target", initialTarget: "Old Target", initialSource: "Old Source", currentTarget: "Current Target", resolutions: map[string]string{colFirstName: MergeResolutionTarget}, expected: "Current Target"},
		{name: "old conflict to source cleared explicit source", initialTarget: "Old Target", initialSource: "Old Source", currentTarget: "Current Target", resolutions: map[string]string{colFirstName: MergeResolutionSource}},
		{name: "old fill to current conflict omitted", initialSource: "Old Source", currentTarget: "Current Target", currentSource: "Current Source", expected: "Current Target"},
		{name: "old fill to current conflict explicit target", initialSource: "Old Source", currentTarget: "Current Target", currentSource: "Current Source", resolutions: map[string]string{colFirstName: MergeResolutionTarget}, expected: "Current Target"},
		{name: "old fill to current conflict explicit source", initialSource: "Old Source", currentTarget: "Current Target", currentSource: "Current Source", resolutions: map[string]string{colFirstName: MergeResolutionSource}, expected: "Current Source"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			repo := NewActressRepository(db)
			target := models.Actress{FirstName: tc.initialTarget, LastName: "Target"}
			source := models.Actress{FirstName: tc.initialSource, LastName: "Source"}
			require.NoError(t, db.Create(&target).Error)
			require.NoError(t, db.Create(&source).Error)

			plan, err := repo.merger.PlanMerge(t.Context(), target.ID, source.ID, tc.resolutions)
			require.NoError(t, err)
			if tc.resolutions == nil || len(tc.resolutions) == 0 {
				require.Empty(t, plan.decisions.fields)
			}
			require.NoError(t, db.Model(&models.Actress{}).Where("id = ?", target.ID).Update(colFirstName, tc.currentTarget).Error)
			require.NoError(t, db.Model(&models.Actress{}).Where("id = ?", source.ID).Update(colFirstName, tc.currentSource).Error)

			result, err := repo.merger.ExecuteMerge(t.Context(), plan, db)
			require.NoError(t, err)
			require.Equal(t, tc.expected, result.MergedActress.FirstName)
		})
	}
}

func TestPlanMergeCopiesNormalizedDecisionMap(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	target := models.Actress{FirstName: "Target"}
	source := models.Actress{FirstName: "Source"}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	resolutions := map[string]string{" FIRST_NAME ": " SOURCE "}

	plan, err := repo.merger.PlanMerge(t.Context(), target.ID, source.ID, resolutions)
	require.NoError(t, err)
	resolutions[" FIRST_NAME "] = MergeResolutionTarget
	resolutions[colFirstName] = MergeResolutionTarget

	require.True(t, plan.decisions.sourceWins(colFirstName))
	require.Len(t, plan.decisions.fields, 1)
}

func TestExecuteMergeRollsBackUniquenessLookupError(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	target := models.Actress{DMMID: 10, FirstName: "Target"}
	source := models.Actress{DMMID: 20, FirstName: "Source"}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	plan, err := repo.merger.PlanMerge(t.Context(), target.ID, source.ID, nil)
	require.NoError(t, err)
	injectDatabaseCallbackError(t, db, "query", "actresses", 3)

	_, err = repo.merger.ExecuteMerge(t.Context(), plan, db)

	require.ErrorContains(t, err, "actress by dmm_id")
	var actresses []models.Actress
	require.NoError(t, db.Order("id").Find(&actresses).Error)
	require.Len(t, actresses, 2)
}

func TestExecuteMergeRejectsExternalDMMConflictWithoutPartialWrites(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	require.NoError(t, db.Exec("DROP INDEX idx_actresses_dmm_id_positive").Error)
	target := models.Actress{DMMID: 10, FirstName: "Target"}
	source := models.Actress{DMMID: 20, FirstName: "Source"}
	conflict := models.Actress{DMMID: 20, FirstName: "Existing"}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	require.NoError(t, db.Create(&conflict).Error)
	plan, err := repo.merger.PlanMerge(t.Context(), target.ID, source.ID, map[string]string{colDMMID: MergeResolutionSource})
	require.NoError(t, err)

	_, err = repo.merger.ExecuteMerge(t.Context(), plan, db)

	require.ErrorIs(t, err, ErrActressMergeUniqueConstraint)
	var actresses []models.Actress
	require.NoError(t, db.Order("id").Find(&actresses).Error)
	require.Len(t, actresses, 3)
	require.Equal(t, 10, actresses[0].DMMID)
	require.Equal(t, 20, actresses[1].DMMID)
}

func TestExecuteMergeRecomputesFromCurrentTarget(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	target := models.Actress{DMMID: 10, FirstName: "Old", LastName: "Target", ThumbURL: "old.jpg", Aliases: "Old Alias", Origin: ActressOriginScrape}
	source := models.Actress{DMMID: 20, FirstName: "Source", LastName: "Name", ThumbURL: "source.jpg", Aliases: "Source Alias", Origin: ActressOriginScrape}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)

	plan := planMergeWhileMutatingPair(t, db, repo, target.ID, source.ID, nil, func() {
		require.NoError(t, db.Model(&models.Actress{}).Where("id = ?", target.ID).Updates(map[string]any{
			"dmm_id": 30, "first_name": "Promoted", "last_name": "Target", "thumb_url": "",
			"aliases": "Promoted Alias", "verified": true, "origin": ActressOriginUser,
		}).Error)
		require.NoError(t, db.Create(&models.ActressTranslation{ActressID: target.ID, Language: "en", DisplayName: "Translated", SourceName: "Target Promoted"}).Error)
	})
	result, err := repo.merger.ExecuteMerge(context.Background(), plan, db)
	require.NoError(t, err)
	require.Equal(t, 30, result.MergedActress.DMMID)
	require.Equal(t, "Promoted", result.MergedActress.FirstName)
	require.Equal(t, "source.jpg", result.MergedActress.ThumbURL)
	require.True(t, result.MergedActress.Verified)
	require.Equal(t, ActressOriginUser, result.MergedActress.Origin)
	require.Contains(t, result.MergedActress.Aliases, "Promoted Alias")
	require.Contains(t, result.MergedActress.Aliases, "Source Alias")
	require.Equal(t, len(buildActressMergeConflicts(&models.Actress{DMMID: 30, FirstName: "Promoted", LastName: "Target"}, &source)), result.ConflictsResolved)
	var translations []models.ActressTranslation
	require.NoError(t, db.Where("actress_id = ?", target.ID).Find(&translations).Error)
	require.Len(t, translations, 1)
	require.Equal(t, "Target Promoted", translations[0].SourceName)
}

func TestExecuteMergeRejectsCanceledContextBeforeTransaction(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	target := models.Actress{FirstName: "Target", LastName: "Name"}
	source := models.Actress{FirstName: "Source", LastName: "Name"}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	plan, err := repo.merger.PlanMerge(t.Context(), target.ID, source.ID, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err := repo.merger.ExecuteMerge(ctx, plan, db)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, result)

	var actresses []models.Actress
	require.NoError(t, db.Order("id").Find(&actresses).Error)
	require.Len(t, actresses, 2)
	require.Equal(t, target.ID, actresses[0].ID)
	require.Equal(t, source.ID, actresses[1].ID)
}

func TestExecuteMergeRecomputesExplicitSourceDecisions(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	target := models.Actress{DMMID: 10, FirstName: "Target", LastName: "Name", ThumbURL: "target.jpg", Verified: false, Origin: ActressOriginScrape}
	source := models.Actress{DMMID: 20, FirstName: "Old", LastName: "Source", ThumbURL: "old-source.jpg", Aliases: "Old Source Alias", Verified: false, Origin: ActressOriginScrape}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	resolutions := map[string]string{"dmm_id": "source", "first_name": "source", "last_name": "source", "thumb_url": "source"}
	plan := planMergeWhileMutatingPair(t, db, repo, target.ID, source.ID, resolutions, func() {
		require.NoError(t, db.Model(&models.Actress{}).Where("id = ?", source.ID).Updates(map[string]any{
			"dmm_id": 40, "first_name": "Current", "last_name": "Source", "thumb_url": "current-source.jpg",
			"aliases": "Current Source Alias", "verified": true, "origin": ActressOriginUser,
		}).Error)
	})
	result, err := repo.merger.ExecuteMerge(context.Background(), plan, db)
	require.NoError(t, err)
	require.Equal(t, 40, result.MergedActress.DMMID)
	require.Equal(t, "Current", result.MergedActress.FirstName)
	require.Equal(t, "Source", result.MergedActress.LastName)
	require.Equal(t, "current-source.jpg", result.MergedActress.ThumbURL)
	require.True(t, result.MergedActress.Verified)
	require.Equal(t, ActressOriginUser, result.MergedActress.Origin)
	require.Contains(t, result.MergedActress.Aliases, "Current Source Alias")
	require.NotContains(t, result.MergedActress.Aliases, "Old Source Alias")
}
