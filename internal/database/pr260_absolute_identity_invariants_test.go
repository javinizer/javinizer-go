package database

import (
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestActressTranslationIndicesRemainInCallerSpaceAcrossPlaceholders(t *testing.T) {
	db := newCreditTestDB(t)
	movie := &models.Movie{
		ContentID: "caller-index-placeholders",
		ID:        "CALLER-INDEX-PLACEHOLDERS",
		Actresses: []models.Actress{
			{},
			{DMMID: 984001, FirstName: "Alpha"},
			{},
			{DMMID: 984002, FirstName: "Beta"},
			{},
		},
		Credits: []models.MovieCredit{
			{Scraped: models.Actress{DMMID: 984002, FirstName: "Beta"}},
			{Scraped: models.Actress{DMMID: 984001, FirstName: "Alpha"}},
		},
	}
	translations := []models.ActressTranslationData{
		{ActressIndex: 0, Language: "en", DisplayName: "placeholder zero"},
		{ActressIndex: 1, Language: "en", DisplayName: "Alpha EN"},
		{ActressIndex: 2, Language: "en", DisplayName: "placeholder two"},
		{ActressIndex: 3, Language: "en", DisplayName: "Beta EN"},
		{ActressIndex: 4, Language: "en", DisplayName: "placeholder four"},
	}

	saved, err := NewMovieRepository(db).UpsertWithTranslations(t.Context(), movie, nil, translations)
	require.NoError(t, err)
	require.Len(t, saved.Credits, 2)
	require.Equal(t, map[uint]string{
		saved.Credits[0].ActressID: "Beta EN",
		saved.Credits[1].ActressID: "Alpha EN",
	}, finalStoredTranslationNames(t, db))
}

func TestLegacyActressTranslationIndicesRemainInCallerSpaceAcrossPlaceholders(t *testing.T) {
	db := newCreditTestDB(t)
	movie := &models.Movie{
		ContentID: "legacy-caller-index-placeholders",
		ID:        "LEGACY-CALLER-INDEX-PLACEHOLDERS",
		Actresses: []models.Actress{{}, {DMMID: 984011, FirstName: "Alpha"}, {}, {DMMID: 984012, FirstName: "Beta"}},
	}
	translations := []models.ActressTranslationData{
		{ActressIndex: 1, Language: "en", DisplayName: "Alpha EN"},
		{ActressIndex: 3, Language: "en", DisplayName: "Beta EN"},
	}

	saved, err := NewMovieRepository(db).UpsertWithTranslations(t.Context(), movie, nil, translations)
	require.NoError(t, err)
	require.Len(t, saved.Actresses, 2)
	byDMM := make(map[int]uint, len(saved.Actresses))
	for _, actress := range saved.Actresses {
		byDMM[actress.DMMID] = actress.ID
	}
	require.Equal(t, map[uint]string{
		byDMM[984011]: "Alpha EN",
		byDMM[984012]: "Beta EN",
	}, finalStoredTranslationNames(t, db))
}

func TestTranslationIdentityEvidenceAuthorityTiers(t *testing.T) {
	tests := []struct {
		name      string
		aggregate models.Actress
		credit    func(a, b models.Actress) models.MovieCredit
		want      bool
	}{
		{
			name:      "scraped rejects contradictory caller and resolved evidence",
			aggregate: models.Actress{DMMID: 984102, FirstName: "Beta"},
			credit: func(a, b models.Actress) models.MovieCredit {
				return models.MovieCredit{ActressID: a.ID, Scraped: a, Actress: &b}
			},
		},
		{
			name:      "scraped outranks contradictory credited evidence",
			aggregate: models.Actress{DMMID: 984102, FirstName: "Beta"},
			credit: func(a, _ models.Actress) models.MovieCredit {
				return models.MovieCredit{ActressID: a.ID, Scraped: a, CreditedName: "Beta"}
			},
		},
		{
			name:      "credited evidence outranks contradictory caller evidence",
			aggregate: models.Actress{DMMID: 984102, FirstName: "Beta"},
			credit: func(a, b models.Actress) models.MovieCredit {
				return models.MovieCredit{ActressID: a.ID, CreditedName: "Alpha", Actress: &b}
			},
		},
		{
			name:      "caller evidence outranks contradictory resolved evidence",
			aggregate: models.Actress{DMMID: 984101, FirstName: "Alpha"},
			credit: func(a, b models.Actress) models.MovieCredit {
				return models.MovieCredit{ActressID: b.ID, Actress: &a}
			},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			a := models.Actress{DMMID: 984101, FirstName: "Alpha", Verified: true, Origin: ActressOriginUser}
			b := models.Actress{DMMID: 984102, FirstName: "Beta", Verified: true, Origin: ActressOriginUser}
			require.NoError(t, db.Create(&a).Error)
			require.NoError(t, db.Create(&b).Error)
			credit := tc.credit(a, b)
			mapped, err := actressTranslationIDsByIdentityTx(db.DB, []models.Actress{tc.aggregate}, []models.MovieCredit{credit})
			require.NoError(t, err)
			if tc.want {
				require.Equal(t, map[int]uint{0: credit.ActressID}, mapped)
			} else {
				require.Empty(t, mapped)
			}
		})
	}
}

func TestTranslationIdentityEvidenceRejectsConflictingStableIDs(t *testing.T) {
	db := newCreditTestDB(t)
	destination := models.Actress{DMMID: 984201, FirstName: "Destination", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&destination).Error)

	for _, tc := range []struct {
		name      string
		aggregate models.Actress
		scraped   models.Actress
	}{
		{
			name:      "matching database id cannot override dmm conflict",
			aggregate: models.Actress{ID: 41, DMMID: 984211, FirstName: "Same"},
			scraped:   models.Actress{ID: 41, DMMID: 984212, FirstName: "Same"},
		},
		{
			name:      "matching dmm cannot override database id conflict",
			aggregate: models.Actress{ID: 51, DMMID: 984221, FirstName: "Same"},
			scraped:   models.Actress{ID: 52, DMMID: 984221, FirstName: "Same"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mapped, err := actressTranslationIDsByIdentityTx(db.DB, []models.Actress{tc.aggregate}, []models.MovieCredit{{ActressID: destination.ID, Scraped: tc.scraped}})
			require.NoError(t, err)
			require.Empty(t, mapped)
		})
	}
}

func TestTranslationIdentityEvidenceSupportsEachFallbackTier(t *testing.T) {
	db := newCreditTestDB(t)
	canonical := models.Actress{DMMID: 984301, FirstName: "Canonical", LastName: "Person", JapaneseName: "正規名", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&canonical).Error)

	tests := []struct {
		name      string
		aggregate models.Actress
		credit    models.MovieCredit
	}{
		{
			name:      "credited full alias is matched directly",
			aggregate: models.Actress{FirstName: "Mary Jane", LastName: "Alias"},
			credit:    models.MovieCredit{ActressID: canonical.ID, CreditedName: "Alias Mary Jane"},
		},
		{
			name:      "credited Japanese alias is matched directly",
			aggregate: models.Actress{JapaneseName: "別名"},
			credit:    models.MovieCredit{ActressID: canonical.ID, CreditedJapaneseName: "別名"},
		},
		{
			name:      "caller actress fallback",
			aggregate: models.Actress{DMMID: 984302, FirstName: "Caller"},
			credit:    models.MovieCredit{ActressID: canonical.ID, Actress: &models.Actress{DMMID: 984302, FirstName: "Caller"}},
		},
		{
			name:      "resolved actress id fallback",
			aggregate: canonical,
			credit:    models.MovieCredit{ActressID: canonical.ID},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mapped, err := actressTranslationIDsByIdentityTx(db.DB, []models.Actress{tc.aggregate}, []models.MovieCredit{tc.credit})
			require.NoError(t, err)
			require.Equal(t, map[int]uint{0: canonical.ID}, mapped)
		})
	}
}

func TestTranslationIdentityEvidenceAmbiguousAliasesAreDroppedAcrossTiersAndOrders(t *testing.T) {
	for _, tc := range []struct {
		name    string
		credits func(a, b models.Actress) []models.MovieCredit
	}{
		{
			name: "credited aliases",
			credits: func(a, b models.Actress) []models.MovieCredit {
				return []models.MovieCredit{{ActressID: a.ID, CreditedName: "Shared Alias"}, {ActressID: b.ID, CreditedName: "Shared Alias"}}
			},
		},
		{
			name: "scraped and credited aliases",
			credits: func(a, b models.Actress) []models.MovieCredit {
				return []models.MovieCredit{{ActressID: a.ID, Scraped: models.Actress{FirstName: "Shared", LastName: "Alias"}}, {ActressID: b.ID, CreditedName: "Shared Alias"}}
			},
		},
		{
			name: "caller and resolved aliases",
			credits: func(a, b models.Actress) []models.MovieCredit {
				caller := models.Actress{FirstName: "Shared", LastName: "Alias"}
				return []models.MovieCredit{{ActressID: a.ID, Actress: &caller}, {ActressID: b.ID}}
			},
		},
	} {
		for _, reverse := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/forward", true: "/reverse"}[reverse], func(t *testing.T) {
				db := newCreditTestDB(t)
				a := models.Actress{FirstName: "Shared", LastName: "Alias"}
				b := models.Actress{FirstName: "Shared", LastName: "Alias"}
				require.NoError(t, db.Create(&a).Error)
				require.NoError(t, db.Create(&b).Error)
				credits := tc.credits(a, b)
				if reverse {
					credits[0], credits[1] = credits[1], credits[0]
				}
				mapped, err := actressTranslationIDsByIdentityTx(db.DB, []models.Actress{{FirstName: "Shared", LastName: "Alias"}}, credits)
				require.NoError(t, err)
				require.Empty(t, mapped)
			})
		}
	}
}

func TestTranslationIdentityEvidenceMapsSourceToReassignedDestination(t *testing.T) {
	db := newCreditTestDB(t)
	target := models.Actress{DMMID: 984402, FirstName: "Target", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	source := models.Actress{DMMID: 984401, FirstName: "Source"}
	mapped, err := actressTranslationIDsByIdentityTx(db.DB, []models.Actress{source}, []models.MovieCredit{{ActressID: target.ID, Scraped: source, Actress: &target}})
	require.NoError(t, err)
	require.Equal(t, map[int]uint{0: target.ID}, mapped)
}

func TestTranslationIdentityEvidenceWithoutAnySourceIdentityIsDropped(t *testing.T) {
	db := newCreditTestDB(t)
	mapped, err := actressTranslationIDsByIdentityTx(db.DB, []models.Actress{{FirstName: "Aggregate"}}, []models.MovieCredit{{ActressID: 999999}})
	require.NoError(t, err)
	require.Empty(t, mapped)
}

func TestResolveActressGroupDuplicateReloadMissingReturnsLookupError(t *testing.T) {
	db := newCreditTestDB(t)
	name := "pr260:duplicate-reload-missing"
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "actresses" {
			_ = tx.AddError(gorm.ErrDuplicatedKey)
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Create().Remove(name) })

	lookupErr := errors.New("duplicate actress remained unavailable")
	lookups := 0
	lookup := func(*gorm.DB, *models.Actress) (models.Actress, bool, error) {
		lookups++
		if lookups == 1 {
			return models.Actress{}, false, nil
		}
		return models.Actress{}, false, lookupErr
	}
	actresses := []models.Actress{{DMMID: 984501, FirstName: "Missing"}}
	err := NewMovieRepository(db).upserter.resolveActressGroup(db.DB, actresses, []actressGroupEntry{{index: 0, act: &actresses[0]}}, lookup)
	require.ErrorIs(t, err, lookupErr)
	require.Equal(t, 2, lookups)
}
