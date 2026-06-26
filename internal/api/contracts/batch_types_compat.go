package contracts

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
)

// UnmarshalJSON supports backward compatibility with the legacy "data" JSON field
// that was renamed to "movie". Old persisted results in the database use "data",
// while new results use "movie". Both are unmarshaled via *models.Movie (which has
// custom UnmarshalJSON for poster flattening + content_id tag) and then projected
// to *MovieView via MovieViewFromModel.
func (r *BatchFileResult) UnmarshalJSON(data []byte) error {
	type Alias BatchFileResult
	// Shadow "movie" with RawMessage so we can unmarshal via *models.Movie
	// (which has the custom UnmarshalJSON for poster flattening + content_id).
	// Then convert to *MovieView via MovieViewFromModel.
	aux := &struct {
		Movie json.RawMessage `json:"movie"`
		Data  json.RawMessage `json:"data"`
		*Alias
	}{
		Alias: (*Alias)(r),
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	// If "movie" was present in JSON, unmarshal via *models.Movie (which
	// handles the persistence format: content_id + flat poster fields via
	// custom UnmarshalJSON), then project to *MovieView.
	if rawJSONPresent(aux.Movie) {
		var m models.Movie
		if err := json.Unmarshal(aux.Movie, &m); err != nil {
			return fmt.Errorf("movie field: %w", err)
		}
		r.Movie = MovieViewFromModel(&m)
		return nil
	}
	// Otherwise, try to convert legacy "data" field.
	if rawJSONPresent(aux.Data) {
		var m models.Movie
		if err := json.Unmarshal(aux.Data, &m); err != nil {
			return fmt.Errorf("legacy data field: %w", err)
		}
		r.Movie = MovieViewFromModel(&m)
	}
	return nil
}

// rawJSONPresent reports whether a JSON RawMessage is present and non-null.
// It is used to skip both absent fields and explicit `null` values so a legacy
// `data: null` does not unmarshal into a phantom non-nil MovieView.
func rawJSONPresent(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}
