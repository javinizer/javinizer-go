package database

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestImportProvenanceMigrationUpgradeAndIdempotence(t *testing.T) {
	db := newCreditTestDB(t)
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	provider := newMigrationProvider(t, sqlDB)
	_, err = provider.DownTo(t.Context(), 22)
	require.NoError(t, err)
	legacy := models.Actress{FirstName: "Legacy", Origin: ActressOriginImport, Verified: true}
	require.NoError(t, db.Create(&legacy).Error)
	_, err = provider.Up(t.Context())
	require.NoError(t, err)
	requireImportProvenance(t, db, legacy.ID, importNamesLegacy)
	repo := NewActressRepository(db)
	created := models.Actress{FirstName: "New"}
	require.NoError(t, repo.ImportUpsert(t.Context(), &created))
	requireImportProvenance(t, db, created.ID, importNamesManaged)
	require.NoError(t, db.Model(&legacy).Update("first_name", "Known change").Error)
	requireImportProvenance(t, db, legacy.ID, importNamesRevoked)
	_, err = provider.Up(t.Context())
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	requireImportProvenance(t, db, created.ID, importNamesManaged)
	requireImportProvenance(t, db, legacy.ID, importNamesRevoked)
	_, err = provider.DownTo(t.Context(), 22)
	require.NoError(t, err)
	_, err = provider.Up(t.Context())
	require.NoError(t, err)
	requireImportProvenance(t, db, created.ID, importNamesLegacy)
	requireImportProvenance(t, db, legacy.ID, importNamesLegacy)
}

func TestImportProvenanceMigrationTriggerFailureIsAtomic(t *testing.T) {
	db := newCreditTestDB(t)
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	provider := newMigrationProvider(t, sqlDB)
	_, err = provider.DownTo(t.Context(), 22)
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE TRIGGER actress_import_names_revoke AFTER UPDATE ON actresses BEGIN SELECT 1; END").Error)
	_, err = provider.Up(t.Context())
	require.Error(t, err)
	var count int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='actress_import_name_provenance'").Scan(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Exec("DROP TRIGGER actress_import_names_revoke").Error)
	_, err = provider.Up(t.Context())
	require.NoError(t, err)
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='trigger' AND name IN ('actress_import_names_revoke','actress_import_names_delete')").Scan(&count).Error)
	require.EqualValues(t, 2, count)
}
