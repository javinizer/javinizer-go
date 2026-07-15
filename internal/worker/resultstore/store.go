package resultstore

import (
	"github.com/javinizer/javinizer-go/internal/models"
)

// FileLookupResult holds the output of looking up a movie ID in job results.
type FileLookupResult struct {
	FilePath         string
	OldMovieID       string
	CapturedRevision uint64
}

// ResultUpdater updates file results, provenance, and exclusion state in the job.
// *ResultTracker satisfies this interface.
type ResultUpdater interface {
	UpdateFileResult(filePath string, result *MovieResult)
	SetProvenance(filePath string, prov *ProvenanceData)
	AtomicUpdateFileResult(filePath string, updateFn func(*MovieResult) (*MovieResult, error)) error // updateFn MUST NOT call methods on the same Store — it executes under the write lock and re-entering will deadlock

	// UpdateMovie atomically updates the movie for a file result.
	UpdateMovie(filePath string, movie *models.Movie) error

	// MarkExcluded marks a file as excluded and adjusts progress counters.
	MarkExcluded(filePath string)
}

// ResultMapAccessor provides read-only and atomic-commit access to the result map.
// *ResultTracker satisfies this interface.
//
// Callers that only need to read results or atomically commit rescrape outcomes
// should accept ResultMapAccessor instead of the broader Store.
type ResultMapAccessor interface {
	// IsGone returns true if the job is deleted or transitioned to a terminal state.
	IsGone() bool

	// GetFileMatchInfo returns match metadata for a file.
	GetFileMatchInfo(filePath string) (models.FileMatchInfo, bool)

	// GetCurrentMovieID returns the movie ID for a file's current result.
	// Returns empty string if no result or no movie.
	GetCurrentMovieID(filePath string) string

	// CommitResult atomically writes a result and recalculates progress.
	// Returns an error if the revision doesn't match expectedRevision (optimistic concurrency).
	CommitResult(filePath string, result *MovieResult, expectedRevision uint64) error

	// OtherResultUsesMovieID checks if any result OTHER than excludePath uses the given movieID.
	OtherResultUsesMovieID(excludePath string, movieID string) bool

	// GetMovieResult returns a clone of the MovieResult for a file path.
	GetMovieResult(filePath string) (*MovieResult, error)

	// CloneFileMatchInfo returns a shallow clone of the FileMatchInfo map.
	CloneFileMatchInfo() map[string]models.FileMatchInfo

	// SnapshotData returns a point-in-time clone of all result data.
	SnapshotData() ResultSnapshot

	// IsAllExcluded checks whether all files are excluded.
	IsAllExcluded() bool
}

// FileFinder provides the FindFileForMovieID method for resolving a movie ID
// to its primary file path and revision. This is separate from
// MovieLookup because FindFileForMovieID has a different return type.
// *ResultTracker satisfies this interface.
type FileFinder interface {
	FindFileForMovieID(movieID string) (*FileLookupResult, error)
	// GetRevision returns the current revision for a file's result.
	GetRevision(filePath string) uint64
}

// MovieLookup provides methods to find movies within a job.
type MovieLookup interface {
	FindFilePathsForMovieID(movieID string) []string
	FindMovieResultForMovieID(movieID string) (*MovieResult, error)
	GetMovieResultsForMovieID(movieID string) []*MovieResult
	GetFileMatchInfosForMovieID(movieID string) []models.FileMatchInfo

	// GetFileResultByResultID returns the MovieResult and its file path for
	// a given stable ResultID (thread-safe). Returns (result, filePath, found).
	GetFileResultByResultID(resultID string) (*MovieResult, string, bool)

	// GetProvenance returns the provenance (field/actress source attribution
	// plus the in-memory raw ScraperResults) for a file path, or nil. The
	// returned value is a clone and safe for the caller to mutate.
	GetProvenance(filePath string) *ProvenanceData
}

// ResultReadFacade composes the three read-oriented narrow interfaces that
// external consumers (adapters, phase inputs) typically need from ResultTracker.
// Callers that only need lookup/access should accept ResultReadFacade instead
// of the full Store, reducing the exposed method surface.
//
// *ResultTracker satisfies this interface.
type ResultReadFacade interface {
	ResultMapAccessor
	MovieLookup
	FileFinder
}

// Store is the primary external seam for batch job result state. It subsumes
// all methods from the five narrow sub-interfaces (ResultUpdater,
// ResultMapAccessor, MovieLookup, FileFinder, ResultReadFacade) plus the
// concrete-type methods that have real callers plus seven new methods that
// replace former direct field access.
//
// The method set is exactly 36 methods:
//
//	From ResultUpdater (5):
//	  UpdateFileResult, SetProvenance, AtomicUpdateFileResult, UpdateMovie, MarkExcluded
//
//	From ResultMapAccessor (9):
//	  IsGone, GetFileMatchInfo, GetCurrentMovieID, CommitResult,
//	  OtherResultUsesMovieID, GetMovieResult, CloneFileMatchInfo,
//	  SnapshotData, IsAllExcluded
//
//	From FileFinder (2):
//	  FindFileForMovieID, GetRevision
//
//	From MovieLookup (6):
//	  FindFilePathsForMovieID, FindMovieResultForMovieID,
//	  GetMovieResultsForMovieID, GetFileMatchInfosForMovieID,
//	  GetFileResultByResultID, GetProvenance
//
//	Concrete-type methods with real callers (7):
//	  GetResults, GetFiles, CloneResults, SnapshotForStatus,
//	  SetFileMatchInfo, SetFileMatchInfoMap, SetFileResultRevision
//
//	New methods (7):
//	  SetGoneChecker, ForceCompleteProgress, ReplaceResultRaw,
//	  RawResults, RebuildIndexes, RecalculateProgress, LoadResultsRaw
//
// The 7 new methods live on Store only — NOT on any narrow sub-interface —
// because none have test-fake callers. This preserves stubResultMap at its
// current 10 methods without growth.
//
// All phases, BatchJob, PosterEditor, JobStore, jobEditorImpl, jobReaderImpl,
// and recovery.go interact with result state exclusively through this
// interface — never through concrete struct types or unexported fields.
type Store interface {
	ResultUpdater
	ResultMapAccessor
	FileFinder
	MovieLookup

	// Concrete-type methods with real callers.
	GetResults() []MovieResult
	GetFiles() []string
	CloneResults() map[string]*MovieResult
	SnapshotForStatus() (ResultSnapshot, ProgressSnapshot)
	SetFileMatchInfo(path string, info models.FileMatchInfo)
	SetFileMatchInfoMap(info map[string]models.FileMatchInfo)
	SetFileResultRevision(filePath string, revision uint64) error

	// New methods (Store-only — not on any narrow sub-interface).
	// SetGoneChecker installs the callback consulted by IsGone.
	SetGoneChecker(fn func() bool)
	// ForceCompleteProgress recalculates progress and, if below 100%, sets it to 100%.
	ForceCompleteProgress()
	// ReplaceResultRaw directly replaces a file's result without recalculating
	// progress counters or incrementing revisions, but performs a FULL rebuild of
	// both movieIDIndex and resultIDIndex. Does NOT clone the input. Test setup only.
	ReplaceResultRaw(filePath string, result *MovieResult)
	// RawResults returns the internal results map without cloning. The caller
	// MUST NOT add or remove keys from the returned map. Returned *MovieResult
	// pointers are mutable; callers may modify fields through them. Used by
	// ClearMissingTempPosters to iterate and mutate poster URLs in place.
	RawResults() map[string]*MovieResult
	// RebuildIndexes performs a full rebuild of both movieIDIndex and resultIDIndex
	// from the current Results map. Test setup only.
	RebuildIndexes()
	// RecalculateProgress recalculates progress counters from the Results map
	// without forcing to 100%.
	RecalculateProgress()
	// LoadResultsRaw bulk-replaces the results map and file match info map
	// without cloning, recalculating progress, or incrementing revisions.
	// Test setup only — follow with RebuildIndexes().
	LoadResultsRaw(results map[string]*MovieResult, fileMatchInfo map[string]models.FileMatchInfo)
}

// Compile-time assertion that *ResultTracker satisfies Store.
var _ Store = (*ResultTracker)(nil)
