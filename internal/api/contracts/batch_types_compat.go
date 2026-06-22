package contracts

import (
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
	if len(aux.Movie) > 0 && string(aux.Movie) != "null" {
		var m models.Movie
		if err := json.Unmarshal(aux.Movie, &m); err == nil {
			r.Movie = MovieViewFromModel(&m)
		}
		return nil
	}
	// Otherwise, try to convert legacy "data" field.
	if len(aux.Data) > 0 {
		reencoded, err := json.Marshal(aux.Data)
		if err != nil {
			return fmt.Errorf("legacy data field: %w", err)
		}
		var m models.Movie
		if err := json.Unmarshal(reencoded, &m); err == nil {
			r.Movie = MovieViewFromModel(&m)
		}
	}
	return nil
}
