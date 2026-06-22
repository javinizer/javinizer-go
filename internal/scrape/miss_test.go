package scrape

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/translation"
	"github.com/stretchr/testify/assert"
)

// --- translationAdapter.Translate: with real translationService ---

func TestTranslationAdapter_Translate_WithNilMovie_Miss(t *testing.T) {
	adapter := &translationAdapter{
		svc:      &translationService{provider: "test"},
		enabled:  true,
		provider: "test",
	}
	warning, translated, _ := adapter.Translate(context.Background(), nil)
	assert.Empty(t, warning)
	assert.False(t, translated, "adapter returns translated=false when movie is nil")
}

// --- applyTranslation: translator returns warning ---

func TestApplyTranslation_TranslatorReturnsWarning_Miss(t *testing.T) {
	warningTranslator := &stubWarningTranslatorMiss{warning: "partial failure"}
	movie := &models.Movie{ID: "WARN-001"}
	warning, _ := applyTranslation(context.Background(), movie, warningTranslator)
	assert.Equal(t, "partial failure", warning)
}

// --- applyTranslation: translator returns empty ---

func TestApplyTranslation_TranslatorReturnsEmpty_Miss(t *testing.T) {
	emptyTranslator := &stubWarningTranslatorMiss{warning: ""}
	movie := &models.Movie{ID: "EMPTY-001"}
	warning, _ := applyTranslation(context.Background(), movie, emptyTranslator)
	assert.Empty(t, warning)
}

// --- mergeOrAppendTranslation: empty target language ---

func TestMergeOrAppendTranslation_EmptyTargetLanguage_Miss(t *testing.T) {
	existing := []models.MovieTranslation{
		{Language: "ja", Title: "Existing"},
	}
	incoming := models.MovieTranslation{Language: "", Title: "New"}
	result := mergeOrAppendTranslation(existing, incoming, false)
	assert.Len(t, result, 1, "empty language should not add a translation")
}

// --- mergeOrAppendTranslation: overwrite existing ---

func TestMergeOrAppendTranslation_OverwriteExisting_Miss(t *testing.T) {
	existing := []models.MovieTranslation{
		{Language: "ja", Title: "Old Title", Description: "Old Desc"},
	}
	incoming := models.MovieTranslation{Language: "ja", Title: "New Title"}
	result := mergeOrAppendTranslation(existing, incoming, true)
	assert.Len(t, result, 1)
	assert.Equal(t, "New Title", result[0].Title)
	assert.Equal(t, "Old Desc", result[0].Description, "non-overwritten fields should be preserved")
}

// --- mergeOrAppendTranslation: append new language ---

func TestMergeOrAppendTranslation_AppendNewLanguage_Miss(t *testing.T) {
	existing := []models.MovieTranslation{
		{Language: "ja", Title: "Japanese"},
	}
	incoming := models.MovieTranslation{Language: "en", Title: "English"}
	result := mergeOrAppendTranslation(existing, incoming, false)
	assert.Len(t, result, 2)
	assert.Equal(t, "en", result[1].Language)
}

// --- mergeOrAppendTranslation: same language, no overwrite ---

func TestMergeOrAppendTranslation_SameLanguageNoOverwrite_Miss(t *testing.T) {
	existing := []models.MovieTranslation{
		{Language: "ja", Title: "Existing"},
	}
	incoming := models.MovieTranslation{Language: "ja", Title: "New"}
	result := mergeOrAppendTranslation(existing, incoming, false)
	assert.Len(t, result, 1)
	assert.Equal(t, "Existing", result[0].Title, "should not overwrite when overwrite=false")
}

// --- mergeTranslationFields: all fields ---

func TestMergeTranslationFields_AllFields_Miss(t *testing.T) {
	current := models.MovieTranslation{
		Language:      "ja",
		Title:         "Old",
		OriginalTitle: "OldOrig",
		Description:   "OldDesc",
		Director:      "OldDir",
		Maker:         "OldMaker",
		Label:         "OldLabel",
		Series:        "OldSeries",
		SourceName:    "OldSource",
		SettingsHash:  "oldhash",
	}
	incoming := models.MovieTranslation{
		Language:      "ja",
		Title:         "New",
		OriginalTitle: "NewOrig",
		Description:   "NewDesc",
		Director:      "NewDir",
		Maker:         "NewMaker",
		Label:         "NewLabel",
		Series:        "NewSeries",
		SourceName:    "NewSource",
		SettingsHash:  "newhash",
	}
	merged := mergeTranslationFields(current, incoming)
	assert.Equal(t, "New", merged.Title)
	assert.Equal(t, "NewOrig", merged.OriginalTitle)
	assert.Equal(t, "NewDesc", merged.Description)
	assert.Equal(t, "NewDir", merged.Director)
	assert.Equal(t, "NewMaker", merged.Maker)
	assert.Equal(t, "NewLabel", merged.Label)
	assert.Equal(t, "NewSeries", merged.Series)
	assert.Equal(t, "NewSource", merged.SourceName)
	assert.Equal(t, "newhash", merged.SettingsHash)
}

// --- mergeTranslationFields: incoming with empty fields preserves current ---

func TestMergeTranslationFields_EmptyIncomingPreservesCurrent_Miss(t *testing.T) {
	current := models.MovieTranslation{
		Language:    "ja",
		Title:       "Kept",
		Description: "KeptDesc",
		Maker:       "KeptMaker",
	}
	incoming := models.MovieTranslation{
		Language: "ja",
		Title:    "",
	}
	merged := mergeTranslationFields(current, incoming)
	assert.Equal(t, "Kept", merged.Title, "empty incoming title should preserve current")
	assert.Equal(t, "KeptDesc", merged.Description)
	assert.Equal(t, "KeptMaker", merged.Maker)
}

// --- Stub translator that returns a configurable warning ---

type stubWarningTranslatorMiss struct {
	warning string
}

func (s *stubWarningTranslatorMiss) Translate(_ context.Context, _ *models.Movie) (string, bool, *translation.TranslationOutput) {
	return s.warning, true, nil
}
