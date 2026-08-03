package worker

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// fieldOverrideKeys is the canonical set of field-source keys a user may
// re-pick from another scraper's raw results. It mirrors the keys emitted by
// the aggregator (stringFieldSpecs + the dedicated assign* methods) and
// buildFieldSourcesFromCachedMovie, so the override speaks the same language
// as the existing "via {source}" provenance tooltips.
var supportedFieldOverrideKeys = []string{
	"id", "content_id", "title", "display_title", "original_title",
	"description", "director", "maker", "label", "series", "runtime",
	"release_date", "rating_score", "rating_votes", "actresses", "genres",
	"screenshot_urls", "poster_url", "cover_url", "trailer_url",
	"should_crop_poster",
}

var fieldOverrideKeys = func() map[string]struct{} {
	m := make(map[string]struct{}, len(supportedFieldOverrideKeys))
	for _, k := range supportedFieldOverrideKeys {
		m[k] = struct{}{}
	}
	return m
}()

// SupportedFieldOverrideKeys returns the field-source keys a user may override
// via the review-page source viewer, in a stable order for UI rendering.
func SupportedFieldOverrideKeys() []string {
	return append([]string(nil), supportedFieldOverrideKeys...)
}

// applyFieldOverride overwrites a single field on movie with the value from the
// named source's raw ScraperResult, and updates the provenance maps so the
// review UI's "via {source}" tooltip reflects the new attribution. Mirrors the
// original PowerShell Javinizer "Replace" button (javinizergui.ps1:2538):
//
//	$cache:findData[$cache:index].Data.($prop.Name) = $prop.Value
//	$cache:findData[$cache:index].Selected.($prop.Name) = $source
//
// This is a raw assignment from the chosen source — it does not re-run genre
// replacement, actress alias resolution, or word processing. That matches the
// original's semantics (the user explicitly cherry-picked this source's value)
// and avoids re-instantiating the full Aggregator in the review path. movie and
// prov are mutated in place; the caller is expected to persist both.
func applyFieldOverride(movie *models.Movie, prov *resultstore.ProvenanceData, fieldKey, source string) error {
	if movie == nil {
		return fmt.Errorf("cannot override field on nil movie")
	}
	if prov == nil {
		return fmt.Errorf("no provenance available for field override")
	}
	if _, ok := fieldOverrideKeys[fieldKey]; !ok {
		return fmt.Errorf("unsupported field: %s", fieldKey)
	}
	result := findScraperResult(prov.ScraperResults, source)
	if result == nil {
		// Legacy/cache-hit movies may carry no persisted raw ScraperResults,
		// but getBatchMovieSources synthesizes a single-source envelope from
		// the cached movie for display. Mirror that fallback here so the
		// displayed source remains selectable.
		if synth := scrape.ScraperResultFromCachedMovie(movie); synth != nil {
			result = findScraperResult([]*models.ScraperResult{synth}, source)
		}
	}
	if result == nil {
		return fmt.Errorf("source %q did not contribute to this movie", source)
	}

	setFieldSource := func(key string) {
		if prov.FieldSources == nil {
			prov.FieldSources = make(map[string]string)
		}
		prov.FieldSources[key] = source
	}

	switch fieldKey {
	case "id":
		movie.ID = result.ID
		setFieldSource("id")
	case "content_id":
		movie.ContentID = result.ContentID
		setFieldSource("content_id")
	case "title", "display_title":
		// Title and DisplayTitle are linked: the aggregator attributes both to
		// the same source, and the workflow derives DisplayTitle from Title.
		// Keep them in sync so the review Title input (bound to display_title)
		// and the persisted NFO <title> stay consistent.
		movie.Title = result.Title
		movie.DisplayTitle = result.Title
		setFieldSource("title")
		setFieldSource("display_title")
	case "original_title":
		movie.OriginalTitle = result.OriginalTitle
		setFieldSource("original_title")
	case "description":
		movie.Description = result.Description
		setFieldSource("description")
	case "director":
		movie.Director = result.Director
		setFieldSource("director")
	case "maker":
		movie.Maker = result.Maker
		setFieldSource("maker")
	case "label":
		movie.Label = result.Label
		setFieldSource("label")
	case "series":
		movie.Series = result.Series
		setFieldSource("series")
	case "runtime":
		movie.Runtime = result.Runtime
		setFieldSource("runtime")
	case "release_date":
		movie.ReleaseDate = result.ReleaseDate
		if result.ReleaseDate != nil {
			movie.ReleaseYear = result.ReleaseDate.Year()
		} else {
			movie.ReleaseYear = 0
		}
		setFieldSource("release_date")
	case "rating_score":
		movie.RatingScore = scraperRatingScore(result)
		setFieldSource("rating_score")
	case "rating_votes":
		movie.RatingVotes = scraperRatingVotes(result)
		setFieldSource("rating_votes")
	case "actresses":
		movie.Actresses = actressesFromScraperInfo(result.Actresses)
		setFieldSource("actresses")
		rebuildActressSources(prov, movie.Actresses, source)
	case "genres":
		movie.Genres = genresFromScraperStrings(result.Genres)
		setFieldSource("genres")
	case "screenshot_urls":
		movie.Screenshots = append([]string(nil), result.ScreenshotURL...)
		setFieldSource("screenshot_urls")
	case "poster_url":
		// Bounds invalidation and crop-intent re-derivation key off the
		// EFFECTIVE poster source (PosterURL ?? CoverURL — the downloader's own
		// resolution, mirrored by RefreshPosterAssets's no-op comparison), not
		// the raw PosterURL field: a cover-backed movie (PosterURL == "") whose
		// CoverURL is U gains NO new image when the selected source's PosterURL
		// is also U — the downloader feeds the pipeline the same bytes and the
		// asset refresh no-ops, so an approved manual crop measured against
		// that image stays valid and must survive. Clearing it would leave the
		// review preview cropped while Organize applies the scraper's crop
		// intent instead of the user's bounds. SyncCropIntentWithSource's own
		// contract likewise demands an actually-changed effective source (an
		// unchanged one may carry a deliberate user crop decision).
		oldEffective := effectivePosterSource(movie.Poster.PosterURL, movie.Poster.CoverURL)
		if movie.Poster.PosterURL != result.PosterURL {
			movie.Poster.PosterURL = result.PosterURL
			if effectivePosterSource(movie.Poster.PosterURL, movie.Poster.CoverURL) != oldEffective {
				movie.Poster.CropBounds = nil // new source image invalidates a crop measured against the old one
				// The auto-crop decision belongs to the image it described — and the
				// SELECTED source ships that decision paired with its own effective
				// poster URL, so propagate it (sources like javdb/mgstage populate
				// PosterURL from their landscape CoverURL WITH ShouldCropPoster=true;
				// deriving intent from which URL field is populated would flip it to
				// false and Organize would write the landscape image whole while the
				// review preview showed it cropped). SyncCropIntentWithSource also
				// covers the URL-field fallback: a cover-backed movie picking a
				// poster-grade URL must not keep ShouldCropPoster=true — Organize
				// would default-crop the new poster, and a later manual crop would
				// record CropBounds.SourceWasCover=true, so an apply-time geometry
				// failure would degrade to the default cover crop instead of keeping
				// the poster whole. Clearing the URL falls the movie back to its
				// cover, restoring cover-backed semantics. Parity with the
				// whole-movie PATCH path (updateBatchMovie).
				movie.Poster.SyncCropIntentWithSource(result)
			}
		}
		setFieldSource("poster_url")
	case "cover_url":
		// The cover only feeds the poster pipeline when no poster URL is set —
		// a fanart-only change must not discard a crop measured against the poster.
		oldCover := movie.Poster.CoverURL
		movie.Poster.CoverURL = result.CoverURL
		if movie.Poster.PosterURL == "" && oldCover != result.CoverURL {
			movie.Poster.CropBounds = nil
			// The cover IS the effective poster source now: re-establish its
			// crop intent. A manual crop on the previous cover left
			// ShouldCropPoster=false; without the re-sync the refreshed preview
			// shows the new cover auto-cropped while Organize, seeing neither
			// bounds nor intent, would write the new landscape cover unchanged.
			// The selected source's own decision (e.g. mgstage's true) wins when
			// it describes this same image; otherwise the cover-backed fallback
			// restores true.
			movie.Poster.SyncCropIntentWithSource(result)
		}
		setFieldSource("cover_url")
	case "trailer_url":
		movie.TrailerURL = result.TrailerURL
		setFieldSource("trailer_url")
	case "should_crop_poster":
		if movie.Poster.ShouldCropPoster != result.ShouldCropPoster {
			movie.Poster.ShouldCropPoster = result.ShouldCropPoster
			movie.Poster.CropBounds = nil // explicit re-pick of scraper auto-crop supersedes the manual crop
		}
		setFieldSource("should_crop_poster")
	default:
		return fmt.Errorf("unhandled field: %s", fieldKey)
	}
	return nil
}

// mergeOverrideOntoPart rebuilds a MULTIPART sibling's movie for the
// ApplyFieldOverride fan-out (Codex: "preserve per-part fields during
// override fan-out"). The same single-field override is applied to the
// SIBLING'S OWN stored movie — so per-part identity fields survive:
// OriginalFileName is populated from each part's own FileMatchInfo
// (scrapeResultToMovieResult) and read by template contexts for
// <FILENAME>/the NFO original path (internal/template/context.go), so a
// wholesale clone of the selected part would render CD2's templates with
// CD1's filename. The selected part's poster state is then mirrored
// wholesale via a clone (source URLs, cleared CropBounds, synced
// ShouldCropPoster intent, refreshed CroppedPosterURL): the cached poster
// assets the override refreshed are movie-wide — every part shares
// {movie.ID}-full.jpg — so poster identity, unlike file identity, must stay
// identical across parts. prov is the shared fan-out provenance; applying
// the same override to it again is idempotent (setFieldSource writes the
// same key/value; rebuildActressSources deterministically rebuilds).
func mergeOverrideOntoPart(partMovie, selected *models.Movie, prov *resultstore.ProvenanceData, fieldKey, source string) (*models.Movie, error) {
	merged := partMovie.Clone()
	if err := applyFieldOverride(merged, prov, fieldKey, source); err != nil {
		return nil, err
	}
	merged.Poster = selected.Poster.Clone()
	return merged, nil
}

// posterAssetSnapshooter is the optional rollback capability of a
// PosterGenerator: the concrete ScrapePosterGenerator snapshots the job's
// cached -full.jpg/preview assets before a refresh so a failed persistence
// afterwards can restore them. Generators without it (test stubs) offer no
// rollback; the refresh-failure-still-rejects-override rule still guards the
// path where regeneration itself fails.
type posterAssetSnapshooter interface {
	SnapshotPosterAssets(jobID, movieID string) (*poster.AssetsSnapshot, error)
	RestorePosterAssets(snap *poster.AssetsSnapshot) error
}

// posterAssetRemover is the optional cleanup capability of a
// PosterGenerator: removing the job's cached -full.jpg/preview assets when an
// edit clears the last poster source. Generators without it (test stubs)
// hold no assets to clear, so cleanup degrades to clearing the in-memory
// preview URL only.
type posterAssetRemover interface {
	RemovePosterAssets(jobID, movieID string) error
}

// posterAssetMover is the optional re-key capability of a PosterGenerator:
// moving the job's cached -full.jpg/preview assets from one movie ID to
// another. An "id" override adopts the selected source's movie ID, and the
// cache must follow it or the assets are orphaned at the old key (P3-6).
// Generators without it (test stubs) hold no assets to move; the re-key is
// then state-only.
type posterAssetMover interface {
	MovePosterAssets(jobID, fromMovieID, toMovieID string) error
}

// posterAssetCopier is the optional case-variant alias capability of a
// PosterGenerator: duplicating the job's cached -full.jpg/preview assets
// from one movie ID to another WITHOUT freeing the source. The rescrape
// sibling poster fan-out uses it when a mirrored sibling is stored under a
// CASE VARIANT of the rescraped ID (folded family, one lock, but distinct
// cache files on a case-sensitive filesystem) so the variant's raw key is
// refreshed alongside its mirrored poster state (Codex P2). Generators
// without it (test stubs) degrade to a state-only mirror.
type posterAssetCopier interface {
	CopyPosterAssets(jobID, fromMovieID, toMovieID string) error
}

// RewritePosterIDInPreviewURL re-points a temp preview poster URL
// (/api/v1/temp/posters/{jobID}/{posterID}.jpg?...) from oldID to newID when
// a re-key (the field-override "id" fan-out, the whole-movie PATCH rename)
// moves the cached assets: the posterAssetMover renames the files, so the
// persisted preview URL must name the new key or the review preview 404s.
//
// The match is ANCHORED to the /api/v1/temp/posters/ prefix on the path
// portion of a RELATIVE URL, and only the final "{oldID}.jpg" path segment is
// rewritten: a scraper-provided URL that merely ENDS with {oldID}.jpg (e.g.
// https://cdn.example/OLD-1.jpg, or an absolute URL that embeds the temp
// prefix under another host) names a remote asset, not this server's temp
// cache key, and stays untouched — rewriting it would break the poster reset
// flow, which downloads the ORIGINAL URL. The query string is carried over
// verbatim and never consulted for the match.
func RewritePosterIDInPreviewURL(raw, oldID, newID string) string {
	const tempPreviewPrefix = "/api/v1/temp/posters/"
	if raw == "" || oldID == "" || newID == "" || oldID == newID {
		return raw
	}
	pathPart, query, hasQuery := strings.Cut(raw, "?")
	if !strings.HasPrefix(pathPart, tempPreviewPrefix) {
		return raw
	}
	needle := "/" + url.PathEscape(oldID) + ".jpg"
	if !strings.HasSuffix(pathPart, needle) {
		return raw
	}
	out := strings.TrimSuffix(pathPart, needle) + "/" + url.PathEscape(newID) + ".jpg"
	if hasQuery {
		out += "?" + query
	}
	return out
}

// CheckRenameDestinationCollision guards the shared precondition of BOTH
// movie-ID rename paths — the id-override re-key
// (jobEditorImpl.ApplyFieldOverride, batch_job_interface.go) and the
// whole-movie PATCH rename (updateBatchMovie, internal/api/batch/movie_edit.go):
// re-keying a movie family originID→destID is only safe when destID is not
// ALREADY used by another result family in the same job.
// MigratePosterCacheAssets normalizes the destination key to the origin's
// assets (MoveAssets removes the destination's files before renaming, and
// DELETES them outright when the origin's leg is absent), while the state
// fan-out (findPaths(originID)) only walks the ORIGIN's family — so a
// pre-existing destID result would keep its own poster source and crop state
// while sharing the origin's migrated cache, and a later crop of either would
// fan bounds measured on one family over both (Organize then applies them
// against different sources). The rejection here is 400-class: the message
// deliberately avoids "not found" so handlers keying on it stay at 400.
//
// Callers MUST run the check under the held (originID, destID) poster-source
// lock pair, BEFORE any asset or state mutation — holding both keys freezes
// index membership at destID too: a concurrent re-key into it needs that
// key's lock. A path indexed at destID that belongs to the ORIGIN's own
// family (e.g. a multipart sibling whose FileMatchInfo.MovieID already equals
// destID) is the normal fan-out case, not a collision; selfPath excludes the
// selected result belt-and-braces even if the index misses it under originID.
// A nil result means the rename is safe to proceed.
func CheckRenameDestinationCollision(findPaths func(movieID string) []string, originID, selfPath, destID string) error {
	if destID == "" {
		return nil
	}
	ownFamily := make(map[string]struct{})
	for _, p := range findPaths(originID) {
		ownFamily[p] = struct{}{}
	}
	ownFamily[selfPath] = struct{}{} // belt-and-braces: never collide with self
	for _, p := range findPaths(destID) {
		if _, ok := ownFamily[p]; !ok {
			return fmt.Errorf("movie ID rename to %q rejected: another result in this job already uses that movie ID", destID)
		}
	}
	return nil
}

// MigratePosterCacheAssets re-keys the job's cached poster assets
// ({tempDir}/posters/{jobID}/{fromKey}-full.jpg + {fromKey}.jpg) from fromKey
// to toKey through the generator's move capability (posterAssetMover), and
// returns the reversal closure for caller-side compensation. The caller holds
// BOTH keys' poster-source locks in lexical order across the move.
//
// A forward-move FAILURE is reversed immediately, best-effort, before this
// function returns — but the reversal is SNAPSHOT-BASED, never a reversed
// re-key. PosterManager.MoveAssets is state-NORMALIZING, not bidirectional:
// per leg it removes the destination BEFORE renaming, and an absent-source
// leg DELETES the destination counterpart (rekey-direction semantics: the new
// key must not carry an image no persisted state produced). When the forward
// move fails ON a leg before its rename (a non-empty-directory blocker at the
// destination, rename EPERM/EROFS, ...), applying those semantics backwards
// via a second, opposite MovePosterAssets call puts the origin's SURVIVING
// file on the reverse call's destination side — where the absent-source
// branch can delete it — and puts the foreign blocker on the reverse call's
// source side, where it can be renamed ONTO the origin key while the call
// reports success. Both silently destroy the origin's assets.
//
// So when the generator exposes posterAssetSnapshooter, the pre-move state of
// BOTH keys is captured BEFORE the move (fail-closed: a snapshot failure
// rejects the migration without touching anything, because a move against an
// un-capturable pre-state has no honest reversal — a directory filed under a
// destination asset name belongs to exactly that blocker class). A reported
// forward failure then replays the snapshots through RestorePosterAssets:
//
//   - the ORIGIN snapshot rewrites the origin's exact pre-move bytes. The
//     forward move never CREATES a from-key file, so anything absent at
//     snapshot time is still absent: RestoreAssets' absent-leg removal finds
//     nothing to remove and can never delete the origin's surviving file
//     (the property MoveAssets' absent-source branch lacks, because there the
//     absent file is on the re-key direction's source side);
//   - the DESTINATION snapshot removes files the completed legs relocated in
//     (absent pre-move ⇒ removed on restore) and rewrites FROM MEMORY any
//     foreign content a completed leg destructively replaced — a blocker is
//     never relocated onto the origin key, and a leg the forward move never
//     touched is untouched on both sides;
//   - a reversal failure (e.g. foreign destination content that cannot be
//     cleanly displaced again) is joined onto the surfaced error instead of
//     being swallowed.
//
// The returned moveBack closure compensates a later persist/plan failure of
// a SUCCESSFUL move, and its make-up depends on the snapshot arm:
//
//   - WITH both pre-move snapshots captured (the fail-closed path above),
//     the closure is a PURE SNAPSHOT REPLAY —
//     errors.Join(RestorePosterAssets(destSnap),
//     RestorePosterAssets(originSnap)) — a true inverse with NO move
//     operation. This matters because a completed forward move is
//     DESTRUCTIVE at the destination: it replaces any foreign bytes filed
//     under the destination key (a bystander movie's assets the re-key
//     adopts), and no reversed re-key could ever resurrect them — but the
//     destination snapshot is still in scope here, so compensation restores
//     the destination's pre-move bytes to the destination key AND the
//     origin's pre-move bytes to the origin key, exactly;
//   - WITHOUT the snapshot capability the reverse MovePosterAssets re-key
//     is the only safe option: a FULLY completed A→B move leaves the origin
//     key empty at every leg, so the reverse move's absent-source legs can
//     only hit already-absent files and its rename legs invert the
//     completed ones 1:1 — the one direction where MoveAssets' normalizing
//     semantics are a true inverse. Foreign destination bytes the forward
//     move legitimately replaced are then unrecoverable by construction
//     (nothing captured them).
//
// A generator without the move capability (test stubs, nil) holds no assets:
// the call degrades to (nil, nil) — the re-key is then state-only. A mover
// WITHOUT the snapshot capability cannot preflight: a forward failure leaves
// the possibly-partial move in place and says so on the error, instead of
// running the hazardous reverse re-key; its success-path compensation
// likewise falls back to the reverse re-key (the second arm above).
func MigratePosterCacheAssets(gen poster.PosterGenerator, jobID, fromKey, toKey string) (moveBack func() error, err error) {
	mover, ok := gen.(posterAssetMover)
	if !ok {
		return nil, nil
	}
	var originSnap, destSnap *poster.AssetsSnapshot
	snapshooter, reversible := gen.(posterAssetSnapshooter)
	if reversible {
		if originSnap, err = snapshooter.SnapshotPosterAssets(jobID, fromKey); err != nil {
			return nil, fmt.Errorf("migrate poster assets to re-keyed movie %s: snapshot origin poster assets before re-key from %s: %w", toKey, fromKey, err)
		}
		if destSnap, err = snapshooter.SnapshotPosterAssets(jobID, toKey); err != nil {
			return nil, fmt.Errorf("migrate poster assets to re-keyed movie %s: snapshot destination poster assets before re-key: %w", toKey, err)
		}
	}
	if moveErr := mover.MovePosterAssets(jobID, fromKey, toKey); moveErr != nil {
		errMsg := fmt.Errorf("migrate poster assets to re-keyed movie %s: %w", toKey, moveErr)
		if !reversible {
			return nil, fmt.Errorf("%w (no asset snapshot capability: a possibly partial move was left in place rather than reversed unsafely)", errMsg)
		}
		if reverseErr := errors.Join(
			snapshooter.RestorePosterAssets(destSnap),
			snapshooter.RestorePosterAssets(originSnap),
		); reverseErr != nil {
			errMsg = fmt.Errorf("%w (partial move reversal failed: %w)", errMsg, reverseErr)
		}
		return nil, errMsg
	}
	if reversible {
		// Snapshot arm: pure snapshot replay, the true inverse of the
		// completed move (see the doc block above). Destination first, then
		// origin — the same order as the failure reversal, so a partially
		// writable filesystem degrades identically in both directions.
		return func() error {
			return errors.Join(
				snapshooter.RestorePosterAssets(destSnap),
				snapshooter.RestorePosterAssets(originSnap),
			)
		}, nil
	}
	// No-snapshot arm: the reverse re-key is the only safe option — with no
	// captured pre-state there is nothing to replay, and a FULLY completed
	// move is the one state a reversed MoveAssets inverts 1:1.
	return func() error { return mover.MovePosterAssets(jobID, toKey, fromKey) }, nil
}

// effectivePosterSource mirrors ScrapePosterGenerator.GeneratePoster's download
// source resolution: the poster URL when set, otherwise the cover URL. A
// cover_url override changes the poster source only when no poster URL is set.
func effectivePosterSource(posterURL, coverURL string) string {
	if posterURL != "" {
		return posterURL
	}
	return coverURL
}

// RefreshPosterAssets regenerates the job's temporary full-size poster
// ({tempDir}/posters/{jobID}/{movie.ID}-full.jpg) after a poster-url or
// cover-url change switches the effective poster source. Both callers that
// persist such a change share it: the single-field override path
// (refreshOverriddenPosterSource) and the whole-movie PATCH handler in the
// API batch package.
//
// When the edit clears the LAST poster source (no poster and no cover), the
// effective new source is empty: regenerating would fail with "no poster or
// cover URL available" and reject an otherwise valid edit. Instead the change
// is treated as a successful cleanup — the cached -full.jpg/preview are
// removed so a stale crop source cannot linger (an empty source paired with a
// stale cache is the same desync class the refresh exists to prevent), and
// movie.Poster.CroppedPosterURL is cleared in place so the state the caller
// persists no longer points at the removed preview. The snapshot/rollback
// machinery still wraps the cleanup, so a persistence failure afterwards
// restores the pre-edit assets. Removal failures (other than file-absent)
// reject the edit, mirroring the refresh-failure semantics below.
//
// The review crop modal and the poster-crop endpoint both
// key off that file: once the server persists the new URL the client treats
// it as the current source and skips its poster-from-url sync, so without
// this refresh a manual crop is measured against the pre-change image while
// Organize downloads the persisted one — recording bounds against the wrong
// coordinate space. This covers cover changes when the movie has no
// PosterURL, because the downloader falls back to CoverURL as the poster
// source. An unchanged effective source (or a cover change behind an
// explicit poster URL) is a no-op: the existing full-size file is already
// current. A nil gen (no poster infrastructure) skips the refresh.
//
// The returned rollback function restores the pre-refresh cached assets when
// the caller's surrounding persistence fails after regeneration succeeded —
// otherwise the job would keep the old source URL while the crop endpoint
// served the new image, and a subsequent crop would record bounds against the
// wrong image. Its error surfaces a failed restore so the caller can report
// the desync instead of swallowing it. rollback is nil when no refresh
// happened or the generator cannot snapshot.
//
// Failure semantics mirror the poster-from-url endpoint: the change is
// rejected rather than persisting a source URL the crop endpoint cannot match
// to the on-disk image. The underlying PosterManager downloads into a temp
// file and replaces the existing -full.jpg only after the new image is fully
// written, and the snapshot rollback covers even the rare post-swap failure
// cleanup, so a failed refresh leaves a good cached file untouched. The
// generator also rewrites the temp preview and stamps its URL on
// movie.Poster.CroppedPosterURL, which the caller persists in the same
// UpdateMovie call.
func RefreshPosterAssets(ctx context.Context, gen poster.PosterGenerator, jobID string, movie *models.Movie, oldPosterURL, oldCoverURL string) (func() error, error) {
	newSource := effectivePosterSource(movie.Poster.PosterURL, movie.Poster.CoverURL)
	if newSource == effectivePosterSource(oldPosterURL, oldCoverURL) {
		return nil, nil // same image still feeds the poster pipeline; the existing full-size file is already current
	}
	if gen == nil {
		return nil, nil
	}
	var rollback func() error
	if snapshooter, ok := gen.(posterAssetSnapshooter); ok {
		snap, err := snapshooter.SnapshotPosterAssets(jobID, movie.ID)
		if err != nil {
			return nil, fmt.Errorf("snapshot poster before source change: %w", err)
		}
		rollback = func() error { return snapshooter.RestorePosterAssets(snap) }
	}
	if newSource == "" {
		// The edit intentionally cleared the last poster source. This is a
		// successful cleanup, not a regeneration: GeneratePoster would error
		// with "no poster or cover URL available" and roll back the edit.
		movie.Poster.CroppedPosterURL = "" // no preview remains without a source
		remover, ok := gen.(posterAssetRemover)
		if !ok {
			return rollback, nil // generators without a manager hold no assets to clear
		}
		if err := remover.RemovePosterAssets(jobID, movie.ID); err != nil {
			// Reject the edit (refresh-failure parity) and roll a partial
			// removal back to the snapshot so the cache stays consistent.
			if rollback != nil {
				if rollbackErr := rollback(); rollbackErr != nil {
					return nil, fmt.Errorf("clear poster cache after source removal: %w", errors.Join(err, fmt.Errorf("poster rollback failed: %w", rollbackErr)))
				}
			}
			return nil, fmt.Errorf("clear poster cache after source removal: %w", err)
		}
		return rollback, nil
	}
	if err := gen.GeneratePoster(ctx, jobID, movie); err != nil {
		if rollback != nil {
			if rollbackErr := rollback(); rollbackErr != nil {
				return nil, fmt.Errorf("refresh poster after source change: %w", errors.Join(err, fmt.Errorf("poster rollback failed: %w", rollbackErr)))
			}
		}
		return nil, fmt.Errorf("refresh poster after source change: %w", err)
	}
	return rollback, nil
}

// refreshOverriddenPosterSource wires the field-override path into
// RefreshPosterAssets: a poster_url override — or a cover_url override when
// the movie has no PosterURL (the downloader falls back to CoverURL as the
// poster source) — regenerates the temp full-size poster before the
// overridden URLs are persisted.
func (je *jobEditorImpl) refreshOverriddenPosterSource(ctx context.Context, movie *models.Movie, oldPosterURL, oldCoverURL string) (func() error, error) {
	return RefreshPosterAssets(ctx, je.posterGen, je.jobID, movie, oldPosterURL, oldCoverURL)
}

// findScraperResult returns the first raw result whose Source matches, or nil.
func findScraperResult(results []*models.ScraperResult, source string) *models.ScraperResult {
	for _, r := range results {
		if r != nil && r.Source == source {
			return r
		}
	}
	return nil
}

func scraperRatingScore(r *models.ScraperResult) float64 {
	if r.Rating != nil {
		return r.Rating.Score
	}
	return 0
}

func scraperRatingVotes(r *models.ScraperResult) int {
	if r.Rating != nil {
		return r.Rating.Votes
	}
	return 0
}

// actressesFromScraperInfo converts a scraper's ActressInfo slice to the model
// Actress slice stored on Movie. Mirrors the field mapping in the aggregator's
// actressMerger without the alias/dedup pass.
func actressesFromScraperInfo(infos []models.ActressInfo) []models.Actress {
	if len(infos) == 0 {
		return nil
	}
	out := make([]models.Actress, 0, len(infos))
	for _, info := range infos {
		out = append(out, models.Actress{
			DMMID:        info.DMMID,
			FirstName:    info.FirstName,
			LastName:     info.LastName,
			JapaneseName: info.JapaneseName,
			ThumbURL:     info.ThumbURL,
		})
	}
	return out
}

func genresFromScraperStrings(names []string) []models.Genre {
	if len(names) == 0 {
		return nil
	}
	out := make([]models.Genre, 0, len(names))
	for _, name := range names {
		out = append(out, models.Genre{Name: name})
	}
	return out
}

// rebuildActressSources re-attributes every actress in the overridden list to
// the chosen source. The list was wholesale-replaced, so any prior per-actress
// attribution is stale; this keeps the ActressSources map consistent with the
// new Actresses slice. Keying uses scrape.ActressSourceKey so the review
// tooltip lookup matches.
func rebuildActressSources(prov *resultstore.ProvenanceData, actresses []models.Actress, source string) {
	if len(actresses) == 0 {
		prov.ActressSources = nil
		return
	}
	sources := make(map[string]string, len(actresses))
	for _, a := range actresses {
		key := scrape.ActressSourceKey(a)
		if key == "" {
			continue
		}
		sources[key] = source
	}
	if len(sources) == 0 {
		prov.ActressSources = nil
		return
	}
	prov.ActressSources = sources
}
