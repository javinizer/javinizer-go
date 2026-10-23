package database

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestImportCandidateSparseOptionsPreserveDefault(t *testing.T) {
	for _, overwrite := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "overwrite"}[overwrite], func(t *testing.T) {
			db := newCreditTestDB(t)
			repo := NewActressRepository(db)
			candidate := models.Actress{DMMID: 822, FirstName: "Given", LastName: "Family", JapaneseName: "日本名", ThumbURL: "candidate.jpg", Origin: ActressOriginScrape}
			require.NoError(t, repo.Create(t.Context(), &candidate))
			in := models.Actress{ID: candidate.ID, FirstName: "New"}
			result, err := repo.ImportUpsertWithOptions(t.Context(), &in, ActressImportOptions{OverwriteNames: overwrite})
			require.NoError(t, err)
			require.Equal(t, "updated", result.Outcome)
			require.Equal(t, "New", in.FirstName)
			require.True(t, in.Verified)
			require.Equal(t, ActressOriginImport, in.Origin)
			if overwrite {
				require.Equal(t, candidate.LastName, in.LastName)
				require.Equal(t, candidate.JapaneseName, in.JapaneseName)
				require.Equal(t, candidate.ThumbURL, in.ThumbURL)
			} else {
				require.Empty(t, in.LastName)
				require.Empty(t, in.JapaneseName)
				require.Empty(t, in.ThumbURL)
			}
		})
	}
}

func TestImportSparseCandidateConflictAndRollback(t *testing.T) {
	for _, failure := range []string{"identity conflict", "alias rollback"} {
		t.Run(failure, func(t *testing.T) {
			db := newCreditTestDB(t)
			repo := NewActressRepository(db)
			candidate := models.Actress{DMMID: 823, FirstName: "Given", LastName: "Family", JapaneseName: "日本名", ThumbURL: "candidate.jpg", Origin: ActressOriginScrape}
			require.NoError(t, repo.Create(t.Context(), &candidate))
			in := models.Actress{ID: candidate.ID, FirstName: "New"}
			if failure == "identity conflict" {
				in.DMMID = 824
			} else {
				require.NoError(t, db.Exec("CREATE TRIGGER fail_sparse_candidate_alias BEFORE INSERT ON actress_aliases BEGIN SELECT RAISE(ABORT, 'candidate alias failure'); END").Error)
			}
			_, err := repo.ImportUpsertWithOptions(t.Context(), &in, ActressImportOptions{OverwriteNames: true})
			require.Error(t, err)
			got, err := repo.FindByID(t.Context(), candidate.ID)
			require.NoError(t, err)
			require.Equal(t, candidate.FirstName, got.FirstName)
			require.Equal(t, candidate.LastName, got.LastName)
			require.Equal(t, candidate.JapaneseName, got.JapaneseName)
			require.Equal(t, candidate.ThumbURL, got.ThumbURL)
			require.Equal(t, candidate.DMMID, got.DMMID)
			require.False(t, got.Verified)
			require.Equal(t, ActressOriginScrape, got.Origin)
		})
	}
}

func TestImportSparseFieldsAcrossStoredOrigins(t *testing.T) {
	for _, origin := range []string{ActressOriginUser, ActressOriginImport, ActressOriginScrape, "", "legacy"} {
		for _, verified := range []bool{false, true} {
			t.Run(origin+map[bool]string{false: "/candidate", true: "/verified"}[verified], func(t *testing.T) {
				db := newCreditTestDB(t)
				repo := NewActressRepository(db)
				a := models.Actress{DMMID: 825, FirstName: "Given", LastName: "Family", JapaneseName: "日本名", ThumbURL: "stored.jpg", Origin: origin, Verified: verified}
				require.NoError(t, db.Create(&a).Error)
				in := models.Actress{ID: a.ID}
				_, err := repo.ImportUpsertWithOptions(t.Context(), &in, ActressImportOptions{OverwriteNames: true})
				require.NoError(t, err)
				require.Equal(t, a.FirstName, in.FirstName)
				require.Equal(t, a.LastName, in.LastName)
				require.Equal(t, a.JapaneseName, in.JapaneseName)
				require.Equal(t, a.ThumbURL, in.ThumbURL)
				require.Equal(t, a.DMMID, in.DMMID)
				if origin == ActressOriginUser || origin == ActressOriginImport {
					require.Equal(t, origin, in.Origin)
					require.Equal(t, verified, in.Verified)
				} else {
					require.Equal(t, ActressOriginImport, in.Origin)
					require.True(t, in.Verified)
				}
			})
		}
	}
}
