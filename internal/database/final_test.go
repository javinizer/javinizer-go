package database

import (
	"fmt"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
)

func TestMovieEntityIDFinal_ContentID(t *testing.T) {
	movie := &models.Movie{ContentID: "abc123", ID: "ABC-123"}
	if id := movieEntityID(movie); id != "abc123" {
		t.Errorf("expected abc123, got %s", id)
	}
}

func TestMovieEntityIDFinal_FallbackToID(t *testing.T) {
	movie := &models.Movie{ContentID: "", ID: "ABC-123"}
	if id := movieEntityID(movie); id != "ABC-123" {
		t.Errorf("expected ABC-123, got %s", id)
	}
}

func TestTranslationEntityIDFinal(t *testing.T) {
	id := translationEntityID("abc123", "en")
	if !strings.Contains(id, "abc123/en") {
		t.Errorf("expected abc123/en in result, got %s", id)
	}
}

func TestWrapDBErrFinal_Nil(t *testing.T) {
	if err := wrapDBErr("test", "entity", nil); err != nil {
		t.Errorf("expected nil for nil error, got %v", err)
	}
}

func TestWrapDBErrFinal_WithError(t *testing.T) {
	err := wrapDBErr("create", "movie", fmt.Errorf("some error"))
	if err == nil {
		t.Error("expected non-nil error")
	}
	if err.Error() != "create movie: some error" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestIsLockedFinal_SQLiteBusy(t *testing.T) {
	err := fmt.Errorf("database is locked")
	if !isLocked(err) {
		t.Error("expected database is locked to be detected")
	}
}

func TestIsLockedFinal_SQLiteTableLocked(t *testing.T) {
	err := fmt.Errorf("database table is locked")
	if !isLocked(err) {
		t.Error("expected database table is locked to be detected")
	}
}

func TestIsLockedFinal_NilError(t *testing.T) {
	if isLocked(nil) {
		t.Error("expected nil error to not be locked")
	}
}

func TestIsLockedFinal_OtherError(t *testing.T) {
	err := fmt.Errorf("some other error")
	if isLocked(err) {
		t.Error("expected other error to not be detected as locked")
	}
}

func TestRetryOnLockedFinal_Success(t *testing.T) {
	callCount := 0
	err := retryOnLocked(func() error {
		callCount++
		return nil
	})
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}
}

func TestRetryOnLockedFinal_NonLockedError(t *testing.T) {
	callCount := 0
	err := retryOnLocked(func() error {
		callCount++
		return fmt.Errorf("some other error")
	})
	if err == nil {
		t.Error("expected non-nil error")
	}
	if callCount != 1 {
		t.Errorf("expected 1 call (non-locked error stops retry), got %d", callCount)
	}
}

func TestFilterIdentifiableActressesFinal(t *testing.T) {
	actresses := []models.Actress{
		{JapaneseName: "Valid Actress"},
		{FirstName: "Jane", LastName: "Doe"},
	}
	result := filterIdentifiableActresses(actresses)
	if len(result) != 2 {
		t.Fatalf("expected 2 identifiable actresses, got %d", len(result))
	}
}
