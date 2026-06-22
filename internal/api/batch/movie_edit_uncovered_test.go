package batch

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
)

func TestResolvePosterID_Uncovered(t *testing.T) {
	tests := []struct {
		name      string
		movieID   string
		lookup    worker.MovieLookup
		expectID  string
		expectErr bool
	}{
		{
			name:     "simple valid ID",
			movieID:  "ABC-123",
			lookup:   &stubMovieLookup{},
			expectID: "ABC-123",
		},
		{
			name:     "uses canonical movie ID when available",
			movieID:  "abc-123",
			lookup:   &stubMovieLookup{result: &worker.MovieResult{Movie: &models.Movie{ID: "ABC-123"}}},
			expectID: "ABC-123",
		},
		{
			name:      "path traversal in movie ID",
			movieID:   "../../../etc/passwd",
			lookup:    &stubMovieLookup{},
			expectErr: true,
		},
		{
			name:      "empty movie ID",
			movieID:   "",
			lookup:    &stubMovieLookup{},
			expectErr: true,
		},
		{
			name:      "dot as movie ID",
			movieID:   ".",
			lookup:    &stubMovieLookup{},
			expectErr: true,
		},
		{
			name:     "movie result with empty ID falls back to movieID",
			movieID:  "TEST-001",
			lookup:   &stubMovieLookup{result: &worker.MovieResult{Movie: &models.Movie{ID: ""}}},
			expectID: "TEST-001",
		},
		{
			name:     "nil movie result falls back to movieID",
			movieID:  "TEST-002",
			lookup:   &stubMovieLookup{result: nil},
			expectID: "TEST-002",
		},
		{
			name:     "movie result with nil Movie falls back to movieID",
			movieID:  "TEST-003",
			lookup:   &stubMovieLookup{result: &worker.MovieResult{Movie: nil}},
			expectID: "TEST-003",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			posterID, err := resolvePosterID(tt.lookup, tt.movieID)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectID, posterID)
			}
		})
	}
}

// stubMovieLookup implements worker.MovieLookup for tests.
type stubMovieLookup struct {
	result   *worker.MovieResult
	filePath string
}

func (s *stubMovieLookup) FindFilePathsForMovieID(movieID string) []string {
	return nil
}

func (s *stubMovieLookup) FindMovieResultForMovieID(movieID string) (*worker.MovieResult, error) {
	if s.result == nil {
		return nil, nil
	}
	return s.result, nil
}

func (s *stubMovieLookup) GetMovieResultsForMovieID(movieID string) []*worker.MovieResult {
	return nil
}

func (s *stubMovieLookup) GetFileMatchInfosForMovieID(movieID string) []models.FileMatchInfo {
	return nil
}

func (s *stubMovieLookup) GetFileResultByResultID(resultID string) (*worker.MovieResult, string, bool) {
	if s.result != nil && s.result.ResultID == resultID {
		return s.result, s.filePath, true
	}
	return nil, "", false
}
