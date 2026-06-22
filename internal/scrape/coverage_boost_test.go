package scrape

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- lookupActress error branches ---

func TestLookupActress_DMMIDNotFound(t *testing.T) {
	repo := &mockActressRepoForUncovered{findByDMMIDErr: database.ErrNotFound}
	actress := &models.Actress{DMMID: 42}
	found, err := lookupActress(context.Background(), repo, actress)
	assert.Nil(t, found)
	assert.ErrorIs(t, err, database.ErrNotFound)
}

func TestLookupActress_DMMIDNonNotFoundError(t *testing.T) {
	repo := &mockActressRepoForUncovered{findByDMMIDErr: errors.New("connection lost")}
	actress := &models.Actress{DMMID: 42}
	found, err := lookupActress(context.Background(), repo, actress)
	assert.Nil(t, found)
	assert.ErrorIs(t, err, database.ErrNotFound)
}

func TestLookupActress_DMMIDFound(t *testing.T) {
	expected := &models.Actress{DMMID: 42, ThumbURL: "http://example.com/pic.jpg"}
	repo := &mockActressRepoForUncovered{findByDMMIDVal: expected}
	actress := &models.Actress{DMMID: 42}
	found, err := lookupActress(context.Background(), repo, actress)
	require.NoError(t, err)
	assert.Equal(t, "http://example.com/pic.jpg", found.ThumbURL)
}

func TestLookupActress_JapaneseNameFound(t *testing.T) {
	expected := &models.Actress{JapaneseName: "鈴村あいり", ThumbURL: "http://example.com/pic.jpg"}
	repo := &mockActressRepoForUncovered{
		findByDMMIDErr: database.ErrNotFound,
		findByNameVal:  expected,
	}
	actress := &models.Actress{DMMID: 42, JapaneseName: "鈴村あいり"}
	found, err := lookupActress(context.Background(), repo, actress)
	require.NoError(t, err)
	assert.Equal(t, "http://example.com/pic.jpg", found.ThumbURL)
}

func TestLookupActress_JapaneseNameNonNotFoundError(t *testing.T) {
	repo := &mockActressRepoForUncovered{
		findByDMMIDErr:  database.ErrNotFound,
		findByNameErr:   errors.New("timeout"),
		findByFirstLast: nil,
	}
	actress := &models.Actress{DMMID: 0, JapaneseName: "テスト"}
	found, err := lookupActress(context.Background(), repo, actress)
	assert.Nil(t, found)
	assert.ErrorIs(t, err, database.ErrNotFound)
}

func TestLookupActress_JapaneseNameOnly(t *testing.T) {
	expected := &models.Actress{JapaneseName: "テスト", ThumbURL: "http://example.com/thumb.jpg"}
	repo := &mockActressRepoForUncovered{findByNameVal: expected}
	actress := &models.Actress{JapaneseName: "テスト"}
	found, err := lookupActress(context.Background(), repo, actress)
	require.NoError(t, err)
	assert.Equal(t, "http://example.com/thumb.jpg", found.ThumbURL)
}

func TestLookupActress_FirstNameLastNameNonNotFoundError(t *testing.T) {
	repo := &mockActressRepoForUncovered{
		findByFirstLastErr: errors.New("timeout"),
	}
	actress := &models.Actress{FirstName: "Airi", LastName: "Suzumura"}
	found, err := lookupActress(context.Background(), repo, actress)
	assert.Nil(t, found)
	assert.ErrorIs(t, err, database.ErrNotFound)
}

func TestLookupActress_NoIdentifiers(t *testing.T) {
	repo := &mockActressRepoForUncovered{}
	actress := &models.Actress{}
	found, err := lookupActress(context.Background(), repo, actress)
	assert.Nil(t, found)
	assert.ErrorIs(t, err, database.ErrNotFound)
}

func TestLookupActress_FirstNameOnly_NoLookup(t *testing.T) {
	repo := &mockActressRepoForUncovered{}
	actress := &models.Actress{FirstName: "Airi"}
	// FirstName only (no LastName) should not trigger FindByFirstNameLastName
	found, err := lookupActress(context.Background(), repo, actress)
	assert.Nil(t, found)
	assert.ErrorIs(t, err, database.ErrNotFound)
}

// --- translateWithContext nil movie ---

func TestTranslateWithContext_NilMovie(t *testing.T) {
	ts := &translationService{provider: "test"}
	warning, _ := ts.translateWithContext(context.Background(), nil)
	assert.Empty(t, warning)
}

// --- translationAdapter.Translate nil movie ---

func TestTranslationAdapter_Translate_NilMovie(t *testing.T) {
	adapter := &translationAdapter{svc: &translationService{provider: "test"}, enabled: true}
	warning, translated, _ := adapter.Translate(context.Background(), nil)
	assert.Empty(t, warning)
	assert.False(t, translated)
}

// --- noOpTranslator ---

func TestNoOpTranslator_Translate(t *testing.T) {
	var tr noOpTranslator
	warning, translated, _ := tr.Translate(context.Background(), &models.Movie{})
	assert.Empty(t, warning)
	assert.False(t, translated)
}

// --- enrichActressFields full coverage ---

func TestEnrichActressFields_NoChanges(t *testing.T) {
	actress := &models.Actress{ThumbURL: "existing.jpg", FirstName: "A", LastName: "B", JapaneseName: "C"}
	dbActress := &models.Actress{ThumbURL: "new.jpg", FirstName: "X", LastName: "Y", JapaneseName: "Z"}
	changed := enrichActressFields(actress, dbActress)
	assert.False(t, changed)
	assert.Equal(t, "existing.jpg", actress.ThumbURL)
}

func TestEnrichActressFields_LastNameAdded(t *testing.T) {
	actress := &models.Actress{FirstName: "Airi"}
	dbActress := &models.Actress{LastName: "Suzumura"}
	changed := enrichActressFields(actress, dbActress)
	assert.True(t, changed)
	assert.Equal(t, "Suzumura", actress.LastName)
}

func TestEnrichActressFields_JapaneseNameAdded(t *testing.T) {
	actress := &models.Actress{FirstName: "Airi"}
	dbActress := &models.Actress{JapaneseName: "鈴村あいり"}
	changed := enrichActressFields(actress, dbActress)
	assert.True(t, changed)
	assert.Equal(t, "鈴村あいり", actress.JapaneseName)
}

// --- ConfigFromAppConfig with translation enabled ---

func TestConfigFromAppConfig_TranslationEnabledWithHash(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Metadata.Translation.Enabled = true
	cfg.Metadata.Translation.TargetLanguage = "ja"
	cfg.Metadata.Translation.SourceLanguage = "en"
	result := ConfigFromAppConfig(cfg)
	require.NotNil(t, result)
	assert.True(t, result.TranslationEnabled)
	assert.NotEmpty(t, result.TranslationSettingsHash)
}

func TestConfigFromAppConfig_TempDir(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.System.TempDir = "/tmp/javinizer"
	result := ConfigFromAppConfig(cfg)
	require.NotNil(t, result)
	assert.Equal(t, "/tmp/javinizer", result.TempDir)
}

// --- enrichActressesFromDB with all lookup paths ---

func TestEnrichActressesFromDB_DMMIDLookupError(t *testing.T) {
	repo := &mockActressRepoForUncovered{findByDMMIDErr: errors.New("db error")}
	movie := &models.Movie{Actresses: []models.Actress{{DMMID: 1}}}
	count := enrichActressesFromDB(context.Background(), movie, repo, &Config{ActressDBEnabled: true})
	assert.Equal(t, 0, count)
}

func TestEnrichActressesFromDB_MultipleActresses(t *testing.T) {
	repo := &mockActressRepoForUncovered{
		findByDMMIDVal: &models.Actress{ThumbURL: "thumb.jpg"},
	}
	movie := &models.Movie{Actresses: []models.Actress{
		{DMMID: 1},
		{DMMID: 2},
	}}
	count := enrichActressesFromDB(context.Background(), movie, repo, &Config{ActressDBEnabled: true})
	assert.Equal(t, 2, count)
}
