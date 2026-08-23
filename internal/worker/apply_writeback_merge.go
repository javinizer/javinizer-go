package worker

import (
	"reflect"
	"strings"

	"github.com/javinizer/javinizer-go/internal/logging"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// mergeLiveReviewEdits is the per-file three-way apply write-back merge
// (POSTER-WRITE-HARDENING D5-lite, codex P6-B):
//
//   - phaseOut wins everything it computed during the phase (post-apply
//     movie state — poster bytes written, NFO-derived fields, etc.).
//   - For the REVIEW-EDITABLE field set, when the LIVE in-memory value has
//     drifted from the phase-entry BASELINE (i.e. the user committed an edit
//     while the phase was running), the live value wins — apply admission
//     D16 promised those edits survive the phase.
//   - Unedited-but-first-class computed fields (organized paths, statuses)
//     live outside this set and stay phase-side.
//
// baseline is the frozen pre-phase snapshot; live is the current in-memory
// movie (may be nil → baseline passes through); phaseOut may be nil (panic
// path produced nothing → baseline+drift merge is the output).
func mergeLiveReviewEdits(baseline, phaseOut, live *models.Movie) *models.Movie {
	switch {
	case phaseOut == nil && live == nil:
		return baseline.Clone()
	case phaseOut == nil:
		phaseOut = baseline.Clone()
	case live == nil:
		return phaseOut.Clone()
	}
	out := phaseOut.Clone()
	if baseline == nil {
		return out
	}

	// Identity drift (rescrape rekey committed mid-phase): the live identity
	// survives, or the result ends with FileMatchInfo pointing at a family
	// that no longer matches the movie (codex P7-C).
	if live.ID != baseline.ID {
		out.ID = live.ID
	}

	if !reflect.DeepEqual(live.Poster, baseline.Poster) {
		out.Poster = live.Poster
	}
	if live.Title != baseline.Title {
		out.Title = live.Title
	}
	if live.DisplayTitle != baseline.DisplayTitle {
		out.DisplayTitle = live.DisplayTitle
	}
	if live.OriginalTitle != baseline.OriginalTitle {
		out.OriginalTitle = live.OriginalTitle
	}
	if live.Director != baseline.Director {
		out.Director = live.Director
	}
	if live.Maker != baseline.Maker {
		out.Maker = live.Maker
	}
	if live.Label != baseline.Label {
		out.Label = live.Label
	}
	if live.Series != baseline.Series {
		out.Series = live.Series
	}
	if live.Runtime != baseline.Runtime {
		out.Runtime = live.Runtime
	}
	if live.ReleaseYear != baseline.ReleaseYear {
		out.ReleaseYear = live.ReleaseYear
	}
	if !reflect.DeepEqual(live.ReleaseDate, baseline.ReleaseDate) {
		out.ReleaseDate = live.ReleaseDate
	}
	if live.RatingScore != baseline.RatingScore {
		out.RatingScore = live.RatingScore
	}
	if live.RatingVotes != baseline.RatingVotes {
		out.RatingVotes = live.RatingVotes
	}
	if live.Description != baseline.Description {
		out.Description = live.Description
	}
	if live.TrailerURL != baseline.TrailerURL {
		out.TrailerURL = live.TrailerURL
	}
	if live.ContentID != baseline.ContentID {
		out.ContentID = live.ContentID
	}
	// codex cloud P2 (@apply_closeout): EVERY PATCH-editable field merges —
	// the phase's stale value must never clobber a concurrent review edit.
	if live.OriginalFileName != baseline.OriginalFileName {
		out.OriginalFileName = live.OriginalFileName
	}
	if live.RatingWarning != baseline.RatingWarning {
		out.RatingWarning = live.RatingWarning
	}
	if live.SourceName != baseline.SourceName {
		out.SourceName = live.SourceName
	}
	if live.SourceURL != baseline.SourceURL {
		out.SourceURL = live.SourceURL
	}
	if !reflect.DeepEqual(live.Translations, baseline.Translations) {
		out.Translations = append([]models.MovieTranslation(nil), live.Translations...)
	}
	if !reflect.DeepEqual(live.Actresses, baseline.Actresses) {
		out.Actresses = append([]models.Actress(nil), live.Actresses...)
	}
	if !reflect.DeepEqual(live.Genres, baseline.Genres) {
		out.Genres = append([]models.Genre(nil), live.Genres...)
	}
	if !reflect.DeepEqual(live.Screenshots, baseline.Screenshots) {
		out.Screenshots = append([]string(nil), live.Screenshots...)
	}
	return out
}

// mergeWriteBackProvenance merges per-file provenance for a write-back commit
// (D5): live (phase-time edited) attribution wins the keys it covers; the
// phase-frozen snapshot fills keys the user never touched. Raw ScraperResults
// are one global set rather than per-key data: when a live set exists and
// differs, it represents a post-phase rescrape and wins as a whole; otherwise
// the frozen set is retained. Every returned slice/map is detached from the
// result store so the atomic updater can publish it safely.
func mergeWriteBackProvenance(frozen, live *resultstore.ProvenanceData) *resultstore.ProvenanceData {
	switch {
	case frozen == nil && live == nil:
		return nil
	case live == nil:
		return frozen.Clone()
	case frozen == nil:
		return live.Clone()
	}
	out := frozen.Clone()
	out.FieldSources = mergeSourceMap(frozen.FieldSources, live.FieldSources)
	out.ActressSources = mergeSourceMap(frozen.ActressSources, live.ActressSources)
	if live.ScraperResults != nil && !reflect.DeepEqual(frozen.ScraperResults, live.ScraperResults) {
		out.ScraperResults = live.Clone().ScraperResults
	}
	return out
}

// upsertWriteBackResultWithProvenance keeps missing-row fallbacks atomic when
// the concrete result store supports the lock-held upsert seam. Test doubles
// that only implement the older interface retain the safe two-call fallback.
func upsertWriteBackResultWithProvenance(updater resultstore.ResultUpdater, filePath string, result *resultstore.MovieResult, prov *resultstore.ProvenanceData) {
	if atomic, ok := updater.(interface {
		UpsertFileResultWithProvenance(string, *resultstore.MovieResult, *resultstore.ProvenanceData)
	}); ok {
		atomic.UpsertFileResultWithProvenance(filePath, result, prov)
		return
	}
	updater.UpdateFileResult(filePath, result)
	if prov != nil {
		updater.SetProvenance(filePath, prov)
	}
}

func mergeSourceMap(frozen, live map[string]string) map[string]string {
	if len(frozen) == 0 && len(live) == 0 {
		return nil
	}
	merged := make(map[string]string, len(frozen)+len(live))
	for k, v := range frozen {
		merged[k] = v
	}
	for k, v := range live {
		merged[k] = v
	}
	return merged
}

// applyWritebackIdentityMismatch reports whether the live result already
// belongs to a different movie family than the apply phase's frozen
// baseline (codex r14-B): in that case the phase output was computed for
// the OLD identity and the write-back must be a pure no-op (D5's
// identity-mismatch rule), never a stale-metadata overlay onto the rekeyed
// result.
func applyWritebackIdentityMismatch(phaseMovie *models.Movie, current *resultstore.MovieResult) bool {
	if phaseMovie == nil || current == nil {
		return false
	}
	// codex r27: FileMatchInfo.MovieID is the matcher alias and legitimately
	// differs from Movie.ID (see 96b37354); compare the live canonical Movie
	// identity, falling back to the match alias only when the movie is gone.
	liveID := ""
	if current.Movie != nil {
		liveID = strings.TrimSpace(current.Movie.ID)
	}
	if liveID == "" {
		liveID = strings.TrimSpace(current.FileMatchInfo.MovieID)
	}
	phaseID := strings.TrimSpace(phaseMovie.ID)
	return liveID != "" && phaseID != "" && !strings.EqualFold(liveID, phaseID)
}

// applyMatchFollowedByLiveIdentity keeps the result's FileMatchInfo identity
// aligned with a live rekey drift (codex P7-C): when the in-memory result
// moved to a different movie family mid-phase, the match map must follow it
// even though the apply was recorded against the pre-phase match.
// writebackPreSkipped checks the identity BEFORE the mutating AtomicUpdate:
// AtomicUpdateFileResult bumps the revision even on a callback no-op
// (codex P2-C: a skip would otherwise advance the revision with no state
// change, breaking the client's CAS against the just-committed rekey).
// Returns true to skip the atomic call entirely. The in-callback mismatch
// check stays in place as the same-race safety net.
func writebackPreSkipped(updater resultstore.ResultUpdater, movie *models.Movie, filePath, label string) bool {
	reader, ok := updater.(interface {
		GetMovieResult(string) (*resultstore.MovieResult, error)
	})
	if !ok {
		return false // unknown seam: the in-callback check remains the guard
	}
	cur, err := reader.GetMovieResult(filePath)
	if err != nil || cur == nil {
		return false
	}
	if applyWritebackIdentityMismatch(movie, cur) {
		logging.Warnf("[%s] skipping write-back for %s — result rekeyed to %s mid-phase (pre-checked)", label, filePath, cur.FileMatchInfo.MovieID)
		return true
	}
	return false
}

func applyMatchFollowedByLiveIdentity(fm models.FileMatchInfo, current *resultstore.MovieResult) models.FileMatchInfo {
	if current != nil && current.FileMatchInfo.MovieID != "" && current.FileMatchInfo.MovieID != fm.MovieID {
		fm.MovieID = current.FileMatchInfo.MovieID
	}
	return fm
}
