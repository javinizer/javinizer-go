package database

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestImportOverwritePreservesManualNameEdits(t *testing.T) {
	for _, path := range []string{"update", "rename names", "rename identity"} {
		for _, field := range []string{"first", "last", "japanese", "clear first"} {
			t.Run(path+"/"+field, func(t *testing.T) {
				db := newCreditTestDB(t)
				repo := NewActressRepository(db)
				a := models.Actress{DMMID: 811, FirstName: "Old", LastName: "Family", JapaneseName: "旧名"}
				require.NoError(t, repo.ImportUpsert(t.Context(), &a))
				switch field {
				case "first":
					a.FirstName = "Manual"
				case "last":
					a.LastName = "Manual"
				case "japanese":
					a.JapaneseName = "手動"
				case "clear first":
					a.FirstName = ""
				}
				switch path {
				case "update":
					require.NoError(t, repo.Update(t.Context(), &a))
				case "rename names":
					require.NoError(t, repo.RenameNameFields(t.Context(), a.ID, a.FirstName, a.LastName, a.JapaneseName))
				case "rename identity":
					require.NoError(t, repo.RenameIdentityFields(t.Context(), a.ID, a.FirstName, a.LastName, a.JapaneseName, "manual.jpg"))
				}
				got, err := repo.FindByID(t.Context(), a.ID)
				require.NoError(t, err)
				require.Equal(t, ActressOriginUser, got.Origin)
				incoming := models.Actress{DMMID: 811, FirstName: "Replacement", LastName: "Replacement", JapaneseName: "置換"}
				result, err := repo.ImportUpsertWithOptions(t.Context(), &incoming, ActressImportOptions{OverwriteNames: true})
				require.NoError(t, err)
				require.Equal(t, "protected", result.Outcome)
				require.Equal(t, a.FirstName, incoming.FirstName)
				require.Equal(t, a.LastName, incoming.LastName)
				require.Equal(t, a.JapaneseName, incoming.JapaneseName)
			})
		}
	}
}

func TestImportManualOwnershipRequiresNameChange(t *testing.T) {
	for _, path := range []string{"update noop", "update thumbnail", "rename names noop", "rename identity thumbnail"} {
		t.Run(path, func(t *testing.T) {
			db := newCreditTestDB(t)
			repo := NewActressRepository(db)
			a := models.Actress{DMMID: 812, FirstName: "Imported"}
			require.NoError(t, repo.ImportUpsert(t.Context(), &a))
			switch path {
			case "update noop":
				require.NoError(t, repo.Update(t.Context(), &a))
			case "update thumbnail":
				a.ThumbURL = "thumb.jpg"
				require.NoError(t, repo.Update(t.Context(), &a))
			case "rename names noop":
				require.NoError(t, repo.RenameNameFields(t.Context(), a.ID, a.FirstName, "", ""))
			case "rename identity thumbnail":
				require.NoError(t, repo.RenameIdentityFields(t.Context(), a.ID, a.FirstName, "", "", "thumb.jpg"))
			}
			got, err := repo.FindByID(t.Context(), a.ID)
			require.NoError(t, err)
			require.Equal(t, ActressOriginImport, got.Origin)
			incoming := models.Actress{DMMID: 812, FirstName: "DefaultIgnored"}
			require.NoError(t, repo.ImportUpsert(t.Context(), &incoming))
			require.Equal(t, "Imported", incoming.FirstName)
			require.Equal(t, ActressOriginImport, incoming.Origin)
			incoming.FirstName = "Replacement"
			result, err := repo.ImportUpsertWithOptions(t.Context(), &incoming, ActressImportOptions{OverwriteNames: true})
			require.NoError(t, err)
			require.Equal(t, "updated", result.Outcome)
			require.Equal(t, ActressOriginImport, incoming.Origin)
		})
	}
}

func TestImportManualOwnershipRollsBackWithRename(t *testing.T) {
	for _, path := range []string{"update", "rename names", "rename identity"} {
		t.Run(path, func(t *testing.T) {
			db := newCreditTestDB(t)
			repo := NewActressRepository(db)
			a := models.Actress{DMMID: 813, FirstName: "Imported"}
			require.NoError(t, repo.ImportUpsert(t.Context(), &a))
			require.NoError(t, db.Exec("CREATE TRIGGER fail_manual_alias BEFORE INSERT ON actress_aliases BEGIN SELECT RAISE(ABORT, 'manual alias failure'); END").Error)
			switch path {
			case "update":
				a.FirstName = "Manual"
				require.Error(t, repo.Update(t.Context(), &a))
			case "rename names":
				require.Error(t, repo.RenameNameFields(t.Context(), a.ID, "Manual", "", ""))
			case "rename identity":
				require.Error(t, repo.RenameIdentityFields(t.Context(), a.ID, "Manual", "", "", ""))
			}
			got, err := repo.FindByID(t.Context(), a.ID)
			require.NoError(t, err)
			require.Equal(t, "Imported", got.FirstName)
			require.Equal(t, ActressOriginImport, got.Origin)
		})
	}
}

func TestImportManualOwnershipSurvivesStaleCatalogUpdate(t *testing.T) {
	for _, editName := range []bool{false, true} {
		t.Run(map[bool]string{false: "thumbnail", true: "name"}[editName], func(t *testing.T) {
			db := newCreditTestDB(t)
			repo := NewActressRepository(db)
			a := models.Actress{DMMID: 814, FirstName: "Imported"}
			require.NoError(t, repo.ImportUpsert(t.Context(), &a))
			stale := a
			require.NoError(t, repo.RenameNameFields(t.Context(), a.ID, "Manual", "", ""))
			stale.FirstName = "Manual"
			stale.ThumbURL = "manual.jpg"
			if editName {
				stale.FirstName = "AnotherManual"
			}
			require.NoError(t, repo.Update(t.Context(), &stale))
			require.Equal(t, ActressOriginUser, stale.Origin)
			in := models.Actress{DMMID: 814, FirstName: "Replacement"}
			result, err := repo.ImportUpsertWithOptions(t.Context(), &in, ActressImportOptions{OverwriteNames: true})
			require.NoError(t, err)
			require.Equal(t, "protected", result.Outcome)
			require.Equal(t, stale.FirstName, in.FirstName)
		})
	}
}
