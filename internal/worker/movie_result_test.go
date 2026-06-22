package worker

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
)

func TestMovieResultClone_NilReceiver(t *testing.T) {
	t.Parallel()
	var mr *MovieResult
	if got := mr.Clone(); got != nil {
		t.Errorf("(*MovieResult)(nil).Clone() = %v, want nil", got)
	}
}

func TestMovieResultClone_DelegatesToMovieClone(t *testing.T) {
	t.Parallel()
	orig := &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file.mp4", MovieID: "ABCD-123"},
		Movie: &models.Movie{
			ContentID: "ABCD-123",
			Actresses: []models.Actress{
				{FirstName: "Yui", LastName: "Hatano"},
			},
		},
		Revision: 1,
		Status:   models.JobStatusCompleted,
	}
	clone := orig.Clone()
	if clone == nil {
		t.Fatal("Clone() returned nil for non-nil receiver")
	}

	// Mutate clone.Movie.Actresses — original must be unchanged
	clone.Movie.Actresses = append(clone.Movie.Actresses, models.Actress{FirstName: "Aoi"})

	if len(orig.Movie.Actresses) != 1 {
		t.Errorf("original Movie.Actresses length: got %d, want 1 (clone mutation affected original)", len(orig.Movie.Actresses))
	}
}

func TestMovieResultClone_PointerFields_Independent(t *testing.T) {
	t.Parallel()
	now := time.Now()
	posterErr := "download failed"
	transWarn := "translation incomplete"

	orig := &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file.mp4"},
		EndedAt:       &now,
		OrchestrationState: models.OrchestrationState{
			PosterError:        &posterErr,
			TranslationWarning: &transWarn,
		},
	}
	clone := orig.Clone()

	// Modify clone's pointers
	later := now.Add(time.Hour)
	clone.EndedAt = &later
	*clone.PosterError = "new error"
	*clone.TranslationWarning = "new warning"

	// Verify originals unchanged
	if !orig.EndedAt.Equal(now) {
		t.Errorf("original EndedAt changed: got %v, want %v", *orig.EndedAt, now)
	}
	if *orig.PosterError != "download failed" {
		t.Errorf("original PosterError changed: got %q, want %q", *orig.PosterError, "download failed")
	}
	if *orig.TranslationWarning != "translation incomplete" {
		t.Errorf("original TranslationWarning changed: got %q, want %q", *orig.TranslationWarning, "translation incomplete")
	}
}

func TestProvenanceData_Clone_NilReceiver(t *testing.T) {
	t.Parallel()
	var p *ProvenanceData
	clone := p.Clone()
	if clone != nil {
		t.Errorf("Clone() on nil receiver should return nil, got %v", clone)
	}
}

func TestProvenanceData_Clone_Independent(t *testing.T) {
	t.Parallel()
	orig := &ProvenanceData{
		FieldSources:   map[string]string{"title": "r18dev", "maker": "dmm"},
		ActressSources: map[string]string{"actress_0": "r18dev"},
	}

	clone := orig.Clone()

	// Modify clone's maps
	clone.FieldSources["title"] = "javlibrary"
	clone.FieldSources["newkey"] = "newvalue"
	clone.ActressSources["actress_1"] = "dmm"

	// Verify originals unchanged
	if orig.FieldSources["title"] != "r18dev" {
		t.Errorf("original FieldSources[\"title\"] changed: got %q, want %q", orig.FieldSources["title"], "r18dev")
	}
	if _, ok := orig.FieldSources["newkey"]; ok {
		t.Error("original FieldSources should not contain 'newkey'")
	}
	if orig.ActressSources["actress_0"] != "r18dev" {
		t.Errorf("original ActressSources[\"actress_0\"] changed: got %q, want %q", orig.ActressSources["actress_0"], "r18dev")
	}
	if _, ok := orig.ActressSources["actress_1"]; ok {
		t.Error("original ActressSources should not contain 'actress_1'")
	}
}

func TestMovieResultClone_PrimitiveFields_Equal(t *testing.T) {
	t.Parallel()
	orig := &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/file.mp4", MovieID: "ABCD-123", IsMultiPart: true, PartNumber: 2, PartSuffix: "CD2"},
		Revision:      5,
		Status:        models.JobStatusCompleted,
		Error:         "some error",
		StartedAt:     time.Now(),
	}
	clone := orig.Clone()

	if clone.FileMatchInfo.Path != orig.FileMatchInfo.Path {
		t.Errorf("FilePath: got %q, want %q", clone.FileMatchInfo.Path, orig.FileMatchInfo.Path)
	}
	if clone.FileMatchInfo.MovieID != orig.FileMatchInfo.MovieID {
		t.Errorf("MovieID: got %q, want %q", clone.FileMatchInfo.MovieID, orig.FileMatchInfo.MovieID)
	}
	if clone.Revision != orig.Revision {
		t.Errorf("Revision: got %d, want %d", clone.Revision, orig.Revision)
	}
	if clone.Status != orig.Status {
		t.Errorf("Status: got %v, want %v", clone.Status, orig.Status)
	}
	if clone.Error != orig.Error {
		t.Errorf("Error: got %q, want %q", clone.Error, orig.Error)
	}
	if clone.FileMatchInfo.IsMultiPart != orig.FileMatchInfo.IsMultiPart {
		t.Errorf("IsMultiPart: got %v, want %v", clone.FileMatchInfo.IsMultiPart, orig.FileMatchInfo.IsMultiPart)
	}
	if clone.FileMatchInfo.PartNumber != orig.FileMatchInfo.PartNumber {
		t.Errorf("PartNumber: got %d, want %d", clone.FileMatchInfo.PartNumber, orig.FileMatchInfo.PartNumber)
	}
	if clone.FileMatchInfo.PartSuffix != orig.FileMatchInfo.PartSuffix {
		t.Errorf("PartSuffix: got %q, want %q", clone.FileMatchInfo.PartSuffix, orig.FileMatchInfo.PartSuffix)
	}
}
