package database

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func requireImportProvenance(t *testing.T, db *DB, id uint, want int) {
	t.Helper()
	state, err := importNameProvenanceTx(db.DB, id)
	require.NoError(t, err)
	require.Equal(t, want, state)
}

func TestImportLegacySafeguardPolicy(t *testing.T) {
	for _, kind := range []string{"legacy", "managed", "revoked", "user", "candidate", "revoked candidate", "unknown"} {
		for _, mode := range []string{"default", "overwrite", "legacy override"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				db := newCreditTestDB(t)
				repo := NewActressRepository(db)
				a := models.Actress{DMMID: 9021, FirstName: "Old", LastName: "Family", ThumbURL: "stored.jpg", Aliases: "Aliases", Origin: ActressOriginImport, Verified: true}
				switch kind {
				case "candidate", "revoked candidate":
					a.Origin = ActressOriginScrape
					a.Verified = false
				case "unknown":
					a.Origin = "unknown"
				}
				if kind == "managed" || kind == "revoked" || kind == "user" {
					require.NoError(t, repo.ImportUpsert(t.Context(), &a))
				} else {
					require.NoError(t, db.Create(&a).Error)
				}
				if kind == "revoked" || kind == "revoked candidate" {
					require.NoError(t, db.Model(&a).Update("first_name", "Nonimport edit").Error)
				}
				if kind == "user" {
					require.NoError(t, repo.RenameNameFields(t.Context(), a.ID, "Manual", a.LastName, ""))
				}
				before, err := repo.FindByID(t.Context(), a.ID)
				require.NoError(t, err)
				prior, err := importNameProvenanceTx(db.DB, a.ID)
				require.NoError(t, err)
				options := ActressImportOptions{OverwriteNames: mode != "default", OverwriteLegacyNames: mode == "legacy override"}
				in := models.Actress{ID: a.ID, DMMID: a.DMMID, FirstName: "New", Aliases: a.Aliases, ThumbURL: "replacement.jpg"}
				result, err := repo.ImportUpsertWithOptions(t.Context(), &in, options)
				require.NoError(t, err)
				protected := mode != "default" && (kind == "user" || kind == "revoked" || kind == "revoked candidate" || (kind == "legacy" && mode != "legacy override"))
				rename := !protected && ((mode != "default" && (kind == "managed" || kind == "legacy")) || kind == "candidate" || kind == "revoked candidate" || kind == "unknown")
				if rename {
					require.Equal(t, "New", in.FirstName)
				} else {
					require.Equal(t, before.FirstName, in.FirstName)
				}
				if protected {
					require.Equal(t, "protected", result.Outcome)
					require.Equal(t, before.Origin, in.Origin)
					require.Equal(t, before.Verified, in.Verified)
					require.Equal(t, before.ThumbURL, in.ThumbURL)
				}
				require.Equal(t, a.DMMID, in.DMMID)
				requireImportProvenance(t, db, a.ID, prior)
			})
		}
	}
}

func TestImportLegacyOverrideIsRequestOnlyAndStrict(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	a := models.Actress{DMMID: 9022, FirstName: "Old", LastName: "Keep", JapaneseName: "保持", ThumbURL: "keep.jpg", Verified: true, Origin: ActressOriginImport}
	require.NoError(t, db.Create(&a).Error)
	options := ActressImportOptions{OverwriteNames: true, OverwriteLegacyNames: true}
	in := models.Actress{ID: a.ID, FirstName: "First only"}
	result, err := repo.ImportUpsertWithOptions(t.Context(), &in, options)
	require.NoError(t, err)
	require.Equal(t, "updated", result.Outcome)
	require.Equal(t, "Keep", in.LastName)
	require.Equal(t, "保持", in.JapaneseName)
	require.Equal(t, "keep.jpg", in.ThumbURL)
	require.Equal(t, a.DMMID, in.DMMID)
	requireImportProvenance(t, db, a.ID, importNamesLegacy)
	in = models.Actress{ID: a.ID, LastName: "Must stay protected"}
	result, err = repo.ImportUpsertWithOptions(t.Context(), &in, ActressImportOptions{OverwriteNames: true})
	require.NoError(t, err)
	require.Equal(t, "protected", result.Outcome)
	require.Equal(t, "Keep", in.LastName)
	in = models.Actress{ID: a.ID, DMMID: 9999, FirstName: "Conflicting"}
	result, err = repo.ImportUpsertWithOptions(t.Context(), &in, options)
	require.ErrorIs(t, err, ErrActressImportIdentityConflict)
	require.Equal(t, "conflicted", result.Outcome)
	_, err = repo.ImportUpsertWithOptions(t.Context(), &in, ActressImportOptions{OverwriteLegacyNames: true})
	require.ErrorContains(t, err, "requires overwrite_names")
	requireImportProvenance(t, db, a.ID, importNamesLegacy)
}

func TestImportProvenanceTriggersAndDefaultWriters(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	a := models.Actress{DMMID: 9023, FirstName: "Managed"}
	require.NoError(t, repo.ImportUpsert(t.Context(), &a))
	requireImportProvenance(t, db, a.ID, importNamesManaged)
	// Default importer filling a blank is authorized import work, not enrollment.
	in := models.Actress{ID: a.ID, FirstName: "Managed", LastName: "Filled"}
	require.NoError(t, repo.ImportUpsert(t.Context(), &in))
	requireImportProvenance(t, db, a.ID, importNamesManaged)
	require.NoError(t, repo.RenameIdentityFields(t.Context(), a.ID, in.FirstName, in.LastName, in.JapaneseName, "thumb-only"))
	requireImportProvenance(t, db, a.ID, importNamesManaged)
	require.NoError(t, db.Model(&a).Update("first_name", nil).Error)
	requireImportProvenance(t, db, a.ID, importNamesRevoked)
	require.NoError(t, restoreImportNameProvenanceTx(db.DB, a.ID, importNamesManaged, ActressOriginImport))
	require.NoError(t, db.Model(&a).Update("first_name", nil).Error)
	requireImportProvenance(t, db, a.ID, importNamesManaged)
	require.NoError(t, db.Model(&a).Update("first_name", "").Error)
	requireImportProvenance(t, db, a.ID, importNamesRevoked)
	in = models.Actress{ID: a.ID, FirstName: "Default fill"}
	require.NoError(t, repo.ImportUpsert(t.Context(), &in))
	requireImportProvenance(t, db, a.ID, importNamesRevoked)
	// A direct model save cannot carry eligibility, and even stale known names revoke.
	require.NoError(t, db.Save(&a).Error)
	requireImportProvenance(t, db, a.ID, importNamesRevoked)
	require.NoError(t, repo.Delete(t.Context(), a.ID))
	requireImportProvenance(t, db, a.ID, importNamesLegacy)
	replacement := models.Actress{ID: a.ID, FirstName: "Reused", Origin: ActressOriginImport}
	require.NoError(t, db.Create(&replacement).Error)
	requireImportProvenance(t, db, a.ID, importNamesLegacy)
}

func TestImportProvenanceRollbackAndReopen(t *testing.T) {
	for _, managed := range []bool{false, true} {
		t.Run(fmt.Sprint(managed), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "provenance.db")
			db, err := New(&Config{Type: "sqlite", DSN: path, LogLevel: "silent"})
			require.NoError(t, err)
			require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
			repo := NewActressRepository(db)
			a := models.Actress{FirstName: "Old", Origin: ActressOriginImport, Verified: true}
			prior := importNamesLegacy
			if managed {
				require.NoError(t, repo.ImportUpsert(t.Context(), &a))
				prior = importNamesManaged
			} else {
				require.NoError(t, db.Create(&a).Error)
			}
			require.NoError(t, db.Exec("CREATE TRIGGER fail_legacy_alias BEFORE INSERT ON actress_aliases BEGIN SELECT RAISE(ABORT, 'alias failure'); END").Error)
			in := models.Actress{ID: a.ID, FirstName: "New"}
			_, err = repo.ImportUpsertWithOptions(t.Context(), &in, ActressImportOptions{OverwriteNames: true, OverwriteLegacyNames: true})
			require.Error(t, err)
			got, err := repo.FindByID(t.Context(), a.ID)
			require.NoError(t, err)
			require.Equal(t, "Old", got.FirstName)
			requireImportProvenance(t, db, a.ID, prior)
			require.NoError(t, db.Close())
			db, err = New(&Config{Type: "sqlite", DSN: path, LogLevel: "silent"})
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
			require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
			requireImportProvenance(t, db, a.ID, prior)
		})
	}
}

func TestImportProvenanceMergeRevokesEvenUnchangedNames(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	target := models.Actress{FirstName: "Same"}
	require.NoError(t, repo.ImportUpsert(t.Context(), &target))
	source := models.Actress{FirstName: "Same", Origin: ActressOriginUser, Verified: true}
	require.NoError(t, db.Create(&source).Error)
	_, err := repo.Merge(t.Context(), target.ID, source.ID, nil)
	require.NoError(t, err)
	requireImportProvenance(t, db, target.ID, importNamesRevoked)
	in := models.Actress{ID: target.ID, FirstName: "Forbidden"}
	result, err := repo.ImportUpsertWithOptions(t.Context(), &in, ActressImportOptions{OverwriteNames: true, OverwriteLegacyNames: true})
	require.NoError(t, err)
	require.Equal(t, "protected", result.Outcome)
	require.Equal(t, "Same", in.FirstName)
}

func TestImportLegacyOverrideRetryCannotReviveKnownChange(t *testing.T) {
	for _, managed := range []bool{false, true} {
		t.Run(fmt.Sprint(managed), func(t *testing.T) {
			db, err := New(&Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "retry.db"), LogLevel: "silent"})
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
			repo := NewActressRepository(db)
			a := models.Actress{FirstName: "Old", Origin: ActressOriginImport, Verified: true}
			if managed {
				require.NoError(t, repo.ImportUpsert(t.Context(), &a))
			} else {
				require.NoError(t, db.Create(&a).Error)
			}
			sqlDB, err := db.DB.DB()
			require.NoError(t, err)
			var once sync.Once
			require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:legacy_manual_retry", func(tx *gorm.DB) {
				if tx.Statement != nil && tx.Statement.Table == "actresses" {
					once.Do(func() {
						_, err := sqlDB.ExecContext(t.Context(), "UPDATE actresses SET first_name = ? WHERE id = ?", "Known edit", a.ID)
						require.NoError(t, err)
					})
				}
			}))
			incoming := models.Actress{ID: a.ID, FirstName: "Must not replace"}
			result, err := repo.ImportUpsertWithOptions(t.Context(), &incoming, ActressImportOptions{OverwriteNames: true, OverwriteLegacyNames: true})
			require.NoError(t, err)
			require.Equal(t, "protected", result.Outcome)
			require.Equal(t, "Known edit", incoming.FirstName)
			requireImportProvenance(t, db, a.ID, importNamesRevoked)
		})
	}
}

func TestImportProvenanceFailureRollback(t *testing.T) {
	for _, operation := range []string{"read", "new grant", "managed restore", "legacy restore", "merge revoke"} {
		t.Run(operation, func(t *testing.T) {
			db := newCreditTestDB(t)
			repo := NewActressRepository(db)
			a := models.Actress{FirstName: "Old", Origin: ActressOriginImport, Verified: true}
			if operation == "managed restore" || operation == "merge revoke" {
				require.NoError(t, repo.ImportUpsert(t.Context(), &a))
			} else if operation != "new grant" {
				require.NoError(t, db.Create(&a).Error)
			}
			switch operation {
			case "read":
				require.NoError(t, db.Exec("DROP TABLE actress_import_name_provenance").Error)
			case "new grant", "managed restore":
				require.NoError(t, db.Exec("CREATE TRIGGER fail_grant BEFORE INSERT ON actress_import_name_provenance WHEN NEW.managed = 1 BEGIN SELECT RAISE(ABORT, 'provenance failure'); END").Error)
			case "legacy restore":
				require.NoError(t, db.Exec("CREATE TRIGGER fail_restore BEFORE DELETE ON actress_import_name_provenance BEGIN SELECT RAISE(ABORT, 'provenance failure'); END").Error)
			case "merge revoke":
				require.NoError(t, db.Exec("CREATE TRIGGER fail_revoke BEFORE INSERT ON actress_import_name_provenance WHEN NEW.managed = 0 BEGIN SELECT RAISE(ABORT, 'provenance failure'); END").Error)
			}
			in := models.Actress{ID: a.ID, FirstName: "New"}
			if operation == "merge revoke" {
				source := models.Actress{FirstName: "Old", Origin: ActressOriginUser, Verified: true}
				require.NoError(t, db.Create(&source).Error)
				_, err := repo.Merge(t.Context(), a.ID, source.ID, nil)
				require.ErrorContains(t, err, "provenance failure")
				requireImportProvenance(t, db, a.ID, importNamesManaged)
				return
			}
			_, err := repo.ImportUpsertWithOptions(t.Context(), &in, ActressImportOptions{OverwriteNames: true, OverwriteLegacyNames: true})
			require.Error(t, err)
			if operation == "new grant" {
				var count int64
				require.NoError(t, db.Model(&models.Actress{}).Count(&count).Error)
				require.Zero(t, count)
			} else {
				got, err := repo.FindByID(t.Context(), a.ID)
				require.NoError(t, err)
				require.Equal(t, "Old", got.FirstName)
			}
		})
	}
}

func TestImportProvenancePublicManualWriters(t *testing.T) {
	for _, method := range []string{"set user", "canonical", "name", "identity", "update", "promote"} {
		t.Run(method, func(t *testing.T) {
			db := newCreditTestDB(t)
			repo := NewActressRepository(db)
			a := models.Actress{FirstName: "Imported"}
			require.NoError(t, repo.ImportUpsert(t.Context(), &a))
			stale := a
			switch method {
			case "set user":
				require.NoError(t, repo.SetUserOwned(t.Context(), a.ID))
			case "canonical":
				require.NoError(t, repo.UpdateCanonicalFields(t.Context(), a.ID, "Manual", "", "", ""))
			case "name":
				require.NoError(t, repo.RenameNameFields(t.Context(), a.ID, "Manual", "", ""))
			case "identity":
				require.NoError(t, repo.RenameIdentityFields(t.Context(), a.ID, "Manual", "", "", "thumb"))
			case "update":
				a.FirstName = "Manual"
				require.NoError(t, repo.Update(t.Context(), &a))
			case "promote":
				require.NoError(t, db.Model(&a).Update("verified", false).Error)
				require.NoError(t, repo.PromoteCandidate(t.Context(), a.ID, "Manual", "", "", ""))
			}
			requireImportProvenance(t, db, a.ID, importNamesRevoked)
			stale.ThumbURL = "stale metadata"
			require.NoError(t, repo.Update(t.Context(), &stale))
			requireImportProvenance(t, db, a.ID, importNamesRevoked)
			require.Equal(t, ActressOriginUser, stale.Origin)
			// Even an old internal caller cannot restore managed/absent provenance on user ownership.
			require.NoError(t, restoreImportNameProvenanceTx(db.DB, a.ID, importNamesManaged, ActressOriginImport))
			requireImportProvenance(t, db, a.ID, importNamesRevoked)
			require.NoError(t, restoreImportNameProvenanceTx(db.DB, a.ID, importNamesLegacy, ActressOriginImport))
			requireImportProvenance(t, db, a.ID, importNamesRevoked)
		})
	}
}

func TestImportDefaultUserCandidateCannotBecomeLegacyEligible(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	a := models.Actress{FirstName: "Manual", Origin: ActressOriginUser, Verified: false}
	require.NoError(t, db.Create(&a).Error)
	in := models.Actress{ID: a.ID, FirstName: "Default promotion"}
	require.NoError(t, repo.ImportUpsert(t.Context(), &in))
	requireImportProvenance(t, db, a.ID, importNamesRevoked)
	incoming := models.Actress{ID: a.ID, FirstName: "Forbidden"}
	result, err := repo.ImportUpsertWithOptions(t.Context(), &incoming, ActressImportOptions{OverwriteNames: true, OverwriteLegacyNames: true})
	require.NoError(t, err)
	require.Equal(t, "protected", result.Outcome)
	require.Equal(t, in.FirstName, incoming.FirstName)
}
