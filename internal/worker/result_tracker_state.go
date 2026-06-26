package worker

import (
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/javinizer/javinizer-go/internal/models"
)

// resultTrackerState holds the shared mutable state for result tracking.
type resultTrackerState struct {
	mu            sync.RWMutex
	Results       map[string]*MovieResult
	Provenance    map[string]*ProvenanceData
	FileMatchInfo map[string]models.FileMatchInfo
	Completed     int
	Failed        int
	TotalFiles    int
	Progress      float64
	Files         []string
	Excluded      map[string]bool
	movieIDIndex  map[string][]string
	resultIDIndex map[string]string
	goneChecker   func() bool
}

func movieIDsForResult(r *MovieResult) []string {
	if r == nil {
		return nil
	}
	// Deduplicate using normalized (lowercased) keys to match the actual
	// movieIDIndex, which uses indexKey() (ToLower). Without normalization,
	// case-only variants (e.g. "ABC" and "abc") would both be returned but
	// map to the same index key, causing stale entries on removal.
	seen := make(map[string]bool)
	var ids []string
	if r.Movie != nil && r.Movie.ID != "" {
		key := indexKey(r.Movie.ID)
		ids = append(ids, r.Movie.ID)
		seen[key] = true
	}
	if r.FileMatchInfo.MovieID != "" {
		fmiKey := indexKey(r.FileMatchInfo.MovieID)
		if !seen[fmiKey] {
			ids = append(ids, r.FileMatchInfo.MovieID)
			seen[fmiKey] = true
		}
	}
	return ids
}

func indexKey(movieID string) string {
	return strings.ToLower(movieID)
}

func stateAddToMovieIDIndexLocked(s *resultTrackerState, movieID string, filePath string) {
	key := indexKey(movieID)
	s.movieIDIndex[key] = append(s.movieIDIndex[key], filePath)
}

func stateRemoveFromMovieIDIndexLocked(s *resultTrackerState, movieID string, filePath string) {
	key := indexKey(movieID)
	paths := s.movieIDIndex[key]
	for i, p := range paths {
		if p == filePath {
			s.movieIDIndex[key] = append(paths[:i], paths[i+1:]...)
			if len(s.movieIDIndex[key]) == 0 {
				delete(s.movieIDIndex, key)
			}
			return
		}
	}
}

func stateReindexFilePathLocked(s *resultTrackerState, filePath string, oldResult, newResult *MovieResult) {
	if oldResult != nil {
		for _, oldID := range movieIDsForResult(oldResult) {
			stateRemoveFromMovieIDIndexLocked(s, oldID, filePath)
		}
		if oldResult.ResultID != "" {
			delete(s.resultIDIndex, oldResult.ResultID)
		}
	}
	if newResult != nil {
		for _, newID := range movieIDsForResult(newResult) {
			stateAddToMovieIDIndexLocked(s, newID, filePath)
		}
		if newResult.ResultID != "" {
			if s.resultIDIndex == nil {
				s.resultIDIndex = make(map[string]string)
			}
			s.resultIDIndex[newResult.ResultID] = filePath
		}
	}
}

func stateRebuildMovieIDIndexLocked(s *resultTrackerState) {
	s.movieIDIndex = make(map[string][]string, len(s.Results))
	s.resultIDIndex = make(map[string]string, len(s.Results))
	for filePath, result := range s.Results {
		if result == nil {
			continue
		}
		if result.ResultID == "" {
			// Derive a deterministic ResultID from filePath so rebuilt state is
			// stable across runs. uuid.New() gave legacy records a fresh random
			// ID on every rebuild, destabilizing resultIDIndex.
			result.ResultID = uuid.NewSHA1(uuid.NameSpaceURL, []byte(filePath)).String()
		}
		for _, id := range movieIDsForResult(result) {
			stateAddToMovieIDIndexLocked(s, id, filePath)
		}
		s.resultIDIndex[result.ResultID] = filePath
	}
}

func stateUpdateProgressFromCounters(s *resultTrackerState) {
	if s.TotalFiles == 0 {
		s.Progress = 100
	} else {
		s.Progress = float64(s.Completed+s.Failed) / float64(s.TotalFiles) * 100
	}
}

func stateRecalculateProgress(s *resultTrackerState) {
	completed := 0
	failed := 0
	// Excluded files are intentionally removed from BOTH the numerator (the
	// per-file loop below skips them) and the denominator. Previously the
	// denominator still used s.TotalFiles, so after exclusions progress stayed
	// artificially low even when every non-excluded file had resolved.
	excluded := 0
	for _, isExcluded := range s.Excluded {
		if isExcluded {
			excluded++
		}
	}
	for filePath, r := range s.Results {
		if r == nil || s.Excluded[filePath] {
			continue
		}
		switch r.Status {
		case models.JobStatusCompleted:
			completed++
		case models.JobStatusFailed:
			failed++
		}
	}
	s.Completed = completed
	s.Failed = failed
	activeTotal := s.TotalFiles - excluded
	if activeTotal <= 0 {
		s.Progress = 100
	} else {
		s.Progress = float64(completed+failed) / float64(activeTotal) * 100
	}
}

// stateLookupFilePathsForMovieIDLocked resolves a movie ID to the file paths
// that reference it. Normalization (lowercase) is applied HERE via indexKey()
// so every caller — FindFileForMovieID, OtherResultUsesMovieID, and all other
// public lookups — uses one consistent path without needing to pre-normalize.
// The index is built with the same indexKey() in stateAdd/RemoveFromMovieIDIndexLocked.
func stateLookupFilePathsForMovieIDLocked(s *resultTrackerState, movieID string) []string {
	key := indexKey(movieID)
	return s.movieIDIndex[key]
}

func stateLookupFilePathForResultIDLocked(s *resultTrackerState, resultID string) (string, bool) {
	if s.resultIDIndex == nil {
		return "", false
	}
	fp, ok := s.resultIDIndex[resultID]
	return fp, ok
}

func stateCloneResultsLocked(s *resultTrackerState) map[string]*MovieResult {
	clone := make(map[string]*MovieResult, len(s.Results))
	for k, v := range s.Results {
		if v == nil {
			continue
		}
		clone[k] = v.Clone()
	}
	return clone
}

func stateCloneFileMatchInfoLocked(s *resultTrackerState) map[string]models.FileMatchInfo {
	clone := make(map[string]models.FileMatchInfo, len(s.FileMatchInfo))
	for k, v := range s.FileMatchInfo {
		clone[k] = v
	}
	return clone
}
