package r18devdump

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"

	_ "github.com/mattn/go-sqlite3"
)

const channel003Prefix = "h_003"

// dmmCDNBase is the host prefix for DMM image paths stored as relative paths
// in the dump (e.g. "digital/video/118abw00013/118abw00013pl").
const dmmCDNBase = "https://pics.dmm.co.jp/"

// Store is a read-only local lookup over a cached r18.dev dump. It implements
// models.R18DevDumpLookup. The underlying SQLite connection is opened in
// read-only mode (mode=ro), so concurrent writes from a `javinizer dump
// download` import (which targets a .tmp file) never conflict with runtime
// lookups.
type Store struct {
	db   *sql.DB
	path string

	contentIndexMu     sync.Mutex
	contentIndexKnown  bool
	contentFoldIndexed bool
}

// Open opens a read-only connection to the dump sidecar database. Returns an
// error if the file does not exist or is not a valid dump database; callers
// should treat that as "dump not available" and fall back to HTTP resolution.
func Open(path string) (*Store, error) {
	return OpenContext(context.Background(), path)
}

// Close releases the underlying database connection.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

type dumpDVDRow struct {
	contentID   string
	dvdID       string
	releaseDate string
	serviceCode string
	// Set by DVD queries before filtering; zero means queryContentIDs already
	// returned every service variant for ambiguity checks.
	variantCount int64
}

func (s *Store) selectDVDRow(ctx context.Context, key, id string) (dumpDVDRow, bool, error) {
	queryIdentity := dumpMarkerDisplayKey(id)
	rawMarker := isRawDumpMarkerQuery(id)
	rows, err := s.db.QueryContext(ctx,
		`SELECT content_id, dvd_id, release_date, service_code,
		(SELECT COUNT(*) FROM videos AS all_variants WHERE all_variants.content_id = videos.content_id)
		FROM videos WHERE dvd_id_norm = ? ORDER BY content_id`,
		key,
	)
	if err != nil {
		return dumpDVDRow{}, false, fmt.Errorf("dump dvd_id lookup failed for %q: %w", key, err)
	}
	defer func() { _ = rows.Close() }()

	matches := make([]dumpDVDRow, 0)
	for rows.Next() {
		var row dumpDVDRow
		var dvdID, releaseDate, serviceCode sql.NullString
		var variants int64
		if err := rows.Scan(&row.contentID, &dvdID, &releaseDate, &serviceCode, &variants); err != nil {
			return dumpDVDRow{}, false, fmt.Errorf("dump dvd_id lookup failed for %q: scan: %w", key, err)
		}
		// A DVD predicate can hide a different service's DVD spelling. Never
		// choose metadata until the complete raw content identity is unique.
		row.variantCount = variants
		row.dvdID = dvdID.String
		row.releaseDate = releaseDate.String
		row.serviceCode = serviceCode.String
		// Normalization can erase the T/T28 boundary on both primary and
		// derived keys. A CID candidate is not proof of a conflicting DVD ID.
		rawOwned := rawMarker && dumpRawMarkerEqual(row.contentID, id)
		if !rawOwned && (rawMarker || queryIdentity != "") {
			if queryIdentity == "" || dumpMarkerDisplayKey(row.dvdID) != queryIdentity {
				continue
			}
		}
		matches = append(matches, row)
	}
	if err := rows.Err(); err != nil {
		return dumpDVDRow{}, false, fmt.Errorf("dump dvd_id lookup failed for %q: %w", key, err)
	}
	if len(matches) == 0 {
		return dumpDVDRow{}, false, nil
	}

	buckets := contentIDBuckets(matches)
	literal := strings.TrimSpace(id)
	for _, row := range matches {
		if row.contentID == literal && row.variantCount == 1 {
			return row, true, nil
		}
	}
	for _, candidate := range ContentIDCandidatesWithMarker(id) {
		if row, ok := selectContentID(buckets[foldContentID(candidate)], candidate, false); ok {
			return row, true, nil
		}
	}
	for _, row := range matches {
		if len(buckets[foldContentID(row.contentID)]) == 1 && row.variantCount == 1 {
			return row, true, nil
		}
	}
	return dumpDVDRow{}, false, nil
}

// LookupByDVDID resolves a display dvd_id (e.g. "IPX-535") to its DMM
// content_id, provided that raw ID has only one service row. The query is
// matched against a normalized dvd_id column
// (uppercase, hyphens and whitespace stripped), so "IPX-535", "ipx535", and
// " IPX 535 " all resolve identically. Returns models.ErrDumpMiss on a miss.
func (s *Store) LookupByDVDID(ctx context.Context, dvdID string) (string, error) {
	if s == nil || dvdID == "" {
		return "", models.ErrDumpMiss
	}
	exact, err := s.queryContentIDs(ctx, []string{strings.TrimSpace(dvdID)}, false)
	if err != nil {
		return "", err
	}
	if len(exact) > 1 {
		return "", models.ErrDumpMiss
	}
	for _, key := range dumpNormKeys(dvdID) {
		row, found, err := s.selectDVDRow(ctx, key, dvdID)
		if err != nil {
			return "", err
		}
		if found {
			return row.contentID, nil
		}
	}
	return "", models.ErrDumpMiss
}

// LookupByContentID resolves a DMM content_id back to its display dvd_id.
// An exact raw ID with one service row wins; a uniquely matching ASCII-case
// alias is also accepted. Multiple services never select an arbitrary DVD.
// Absent or ambiguous aliases return ErrDumpMiss; a matched row without a DVD
// ID returns ErrDumpNoDVDID.
func (s *Store) LookupByContentID(ctx context.Context, contentID string) (string, error) {
	if s == nil || contentID == "" {
		return "", models.ErrDumpMiss
	}
	rows, err := s.queryContentIDs(ctx, []string{contentID}, false)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		rows, err = s.queryContentIDs(ctx, []string{contentID}, true)
		if err != nil {
			return "", err
		}
	}
	row, found := selectContentID(rows, contentID, true)
	if !found {
		return "", models.ErrDumpMiss
	}
	if row.dvdID == "" {
		return "", models.ErrDumpNoDVDID
	}
	return row.dvdID, nil
}

// MatchByDisplayID resolves a display ID to matching dump rows in candidate
// priority order. A literal BINARY content ID wins; otherwise an identity-
// compatible dvd_id_norm hit wins before candidate expansion. Folded aliases
// must identify exactly one raw ID, never merging case-distinct source keys.
// Marker display queries preserve their published series boundary. Null-DVD
// rows remain unverified candidate evidence; scrapers still verify the response.
// Returns ErrDumpMiss when there is no unambiguous match.
func (s *Store) MatchByDisplayID(ctx context.Context, id string) ([]models.DumpMatch, error) {
	if s == nil {
		return nil, models.ErrDumpMiss
	}
	literal := strings.TrimSpace(id)
	exact, err := s.queryContentIDs(ctx, []string{literal}, false)
	if err != nil {
		return nil, err
	}
	hasExact := len(exact) != 0
	norm := normalizeDVDID(id)
	if norm != "" && !hasExact {
		for _, key := range dumpNormKeys(id) {
			row, found, err := s.selectDVDRow(ctx, key, id)
			if err != nil {
				return nil, err
			}
			if found {
				return []models.DumpMatch{{
					ContentID:   row.contentID,
					DVDID:       row.dvdID,
					ReleaseDate: row.releaseDate,
					ServiceCode: row.serviceCode,
				}}, nil
			}
		}
	}
	if !hasExact {
		exact, err = s.queryContentIDs(ctx, []string{literal}, true)
		if err != nil {
			return nil, err
		}
	}
	// Published DVD evidence is independent of a folded content-ID alias.
	// Without that evidence, an ambiguous literal must not enter expansion.
	if len(exact) > 1 {
		return nil, models.ErrDumpMiss
	}

	// An exact content_id row is honored before candidate expansion so a
	// literal content_id query always surfaces its own row first, ahead of
	// canonical zero-padded variants derived from the same display ID. The probe
	// set is the exact input first, then the expanded candidates, deduped —
	// order within the result follows probe order.
	exactID := literal
	compacted := strings.NewReplacer("_", "", ".", "").Replace(normalizeDVDID(id))
	candidates := ContentIDCandidates(id)
	if remasterMarkerTailRgx.MatchString(compacted) {
		candidates = ContentIDCandidatesWithMarker(id)
	}

	probe := make([]string, 0, len(candidates)+1)
	seen := make(map[string]bool, len(candidates)+1)
	if exactID != "" {
		seen[foldContentID(exactID)] = true
		probe = append(probe, exactID)
	}
	for _, cand := range candidates {
		if !seen[foldContentID(cand)] {
			seen[foldContentID(cand)] = true
			probe = append(probe, cand)
		}
	}
	if len(probe) == 0 {
		return nil, models.ErrDumpMiss
	}

	rows, err := s.queryContentIDs(ctx, probe, true)
	if err != nil {
		return nil, fmt.Errorf("dump candidate lookup failed for %q: %w", id, err)
	}
	buckets := contentIDBuckets(rows)
	queryIdentity := dumpMarkerDisplayKey(id)
	rawMarker := isRawDumpMarkerQuery(id)
	matches := make([]models.DumpMatch, 0, len(rows))
	for _, candidate := range probe {
		row, ok := selectContentID(buckets[foldContentID(candidate)], candidate, candidate == exactID)
		if !ok {
			continue
		}
		// Check ambiguity before published-identity filtering. A conflicting
		// case-distinct row must never disappear to make an alias look unique.
		rawOwned := rawMarker && dumpRawMarkerEqual(row.contentID, exactID)
		if row.dvdID != "" && !rawOwned && (rawMarker || queryIdentity != "") {
			if queryIdentity == "" || dumpMarkerDisplayKey(row.dvdID) != queryIdentity {
				continue
			}
		}
		matches = append(matches, models.DumpMatch{
			ContentID: row.contentID, DVDID: row.dvdID,
			ReleaseDate: row.releaseDate, ServiceCode: row.serviceCode,
		})
	}
	if len(matches) == 0 {
		return nil, models.ErrDumpMiss
	}
	return matches, nil
}

// LookupMovie resolves an exact raw content_id or a display dvd_id to a
// fully-populated DumpMovie with all related entities joined. Multiple service
// rows for a raw content ID return a miss rather than choosing metadata. Exact raw IDs
// win over conflicting normalized DVD spellings, but null-DVD rows remain
// unverified candidates, not full-movie hits. This is the zero-HTTP fast path
// for the r18.dev scraper: when it hits, no r18.dev API call is needed. Returns
// models.ErrDumpMiss on a miss; any other wrapped error indicates a degraded
// dump (corrupt file, schema drift, I/O failure) that the caller should log
// before falling back to HTTP.
func (s *Store) LookupMovie(ctx context.Context, dvdID string) (*models.DumpMovie, error) {
	if s == nil || dvdID == "" {
		return nil, models.ErrDumpMiss
	}
	keys := dumpNormKeys(dvdID)
	exact, err := s.queryContentIDs(ctx, []string{strings.TrimSpace(dvdID)}, false)
	if err != nil {
		return nil, err
	}
	if len(exact) > 1 {
		return nil, models.ErrDumpMiss
	}
	if len(exact) != 0 {
		if exact[0].dvdID == "" {
			return nil, models.ErrDumpMiss
		}
		keys = dumpNormKeys(exact[0].dvdID)
	}
	if len(keys) == 0 {
		return nil, models.ErrDumpMiss
	}

	var m models.DumpMovie
	var makerID, labelID, seriesID, dvdIDCol sql.NullString
	// All text columns are nullable in the dump (encoded as \N -> NULL on
	// import), so every column scans into a sql.Null* to avoid Scan panics on
	// NULL. The thumb gallery columns are retained only as a fallback when the
	// full-size gallery range is absent (some dump rows populate only thumbs).
	var titleEn, titleJa, commentEn, commentJa, sampleURL sql.NullString
	var jacketFull, jacketThumb, galleryFirst, galleryLast sql.NullString
	var thumbFirst, thumbLast sql.NullString
	var siteID, serviceCode, releaseDate sql.NullString
	var runtime sql.NullInt64
	var found bool
	for _, key := range keys {
		var row dumpDVDRow
		if len(exact) != 0 {
			row = exact[0]
		} else {
			var rowFound bool
			var selectErr error
			row, rowFound, selectErr = s.selectDVDRow(ctx, key, dvdID)
			if selectErr != nil {
				return nil, selectErr
			}
			if !rowFound {
				continue
			}
		}
		err = s.db.QueryRowContext(ctx, `SELECT
		content_id, dvd_id, title_en, title_ja, comment_en, comment_ja,
		runtime_mins, release_date, sample_url,
		maker_id, label_id, series_id,
		jacket_full_url, jacket_thumb_url,
		gallery_full_first, gallery_full_last,
		gallery_thumb_first, gallery_thumb_last,
		site_id, service_code
		FROM videos WHERE dvd_id_norm = ? AND content_id = ? LIMIT 1`, key, row.contentID,
		).Scan(
			&m.ContentID, &dvdIDCol, &titleEn, &titleJa, &commentEn, &commentJa,
			&runtime, &releaseDate, &sampleURL,
			&makerID, &labelID, &seriesID,
			&jacketFull, &jacketThumb,
			&galleryFirst, &galleryLast,
			&thumbFirst, &thumbLast,
			&siteID, &serviceCode,
		)
		if err == nil {
			found = true
			break
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("dump lookup movie %q: %w", key, err)
		}
	}
	if !found {
		return nil, models.ErrDumpMiss
	}

	m.DVDID = dvdIDCol.String
	m.TitleEn = titleEn.String
	m.TitleJa = titleJa.String
	m.CommentEn = commentEn.String
	m.CommentJa = commentJa.String
	m.Runtime = int(runtime.Int64)
	m.ReleaseDate = releaseDate.String
	m.SampleURL = sampleURL.String
	m.JacketFullURL = jacketFull.String
	m.JacketThumbURL = jacketThumb.String
	m.GalleryFirst = galleryFirst.String
	m.GalleryLast = galleryLast.String
	// Fall back to the thumb gallery range when the full-size range is
	// absent. Either endpoint missing means the full range is unusable, so
	// guard on both — a half-populated full range would otherwise feed
	// ExpandGallery a mismatched (first, last) pair and yield no screenshots.
	if m.GalleryFirst == "" || m.GalleryLast == "" {
		m.GalleryFirst = thumbFirst.String
		m.GalleryLast = thumbLast.String
	}
	m.SiteID = siteID.String
	m.ServiceCode = serviceCode.String

	// Joined entities. A missing row on a join is not an error (the movie
	// simply has no maker/director/etc.). A real query failure on a
	// non-critical join is logged at warn and the field degrades to nil/empty
	// so a single corrupt join table does not force the entire movie back to
	// rate-limit-prone HTTP — the core video data and already-resolved joins
	// are still returned. Only the core videos-row query (above) hard-fails.
	if e, err := s.lookupNamedEntity(ctx, "makers", makerID); err != nil {
		s.logJoinDegraded(m.ContentID, "maker", err)
	} else {
		m.Maker = e
	}
	if e, err := s.lookupNamedEntity(ctx, "labels", labelID); err != nil {
		s.logJoinDegraded(m.ContentID, "label", err)
	} else {
		m.Label = e
	}
	if e, err := s.lookupNamedEntity(ctx, "series", seriesID); err != nil {
		s.logJoinDegraded(m.ContentID, "series", err)
	} else {
		m.Series = e
	}
	if d, err := s.lookupDirector(ctx, m.ContentID); err != nil {
		s.logJoinDegraded(m.ContentID, "director", err)
	} else {
		m.Director = d
	}
	if a, err := s.lookupActresses(ctx, m.ContentID); err != nil {
		s.logJoinDegraded(m.ContentID, "actresses", err)
	} else {
		m.Actresses = a
	}
	if c, err := s.lookupCategories(ctx, m.ContentID); err != nil {
		s.logJoinDegraded(m.ContentID, "categories", err)
	} else {
		m.Categories = c
	}
	if t, err := s.lookupTrailer(ctx, m.ContentID); err != nil {
		s.logJoinDegraded(m.ContentID, "trailer", err)
	} else {
		m.TrailerURL = t
	}

	return &m, nil
}

// logJoinDegraded logs a non-critical join failure. Benign context
// cancellation is classified at debug (consistent with searchFromDump's
// quiet-cancel handling); genuine database errors are logged at warn so a
// degraded dump does not silently revert to rate-limit-prone HTTP with no
// signal.
func (s *Store) logJoinDegraded(contentID, join string, err error) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		logging.Debugf("r18.dev dump: %s lookup cancelled for %q: %v", join, contentID, err)
		return
	}
	logging.Warnf("r18.dev dump: %s lookup failed for %q (degraded): %v", join, contentID, err)
}

// lookupNamedEntity fetches a maker/label/series by id. table is a trusted
// internal constant (never user input), so it is interpolated rather than
// parameterized — SQL cannot bind table names. Returns nil, nil when the id is
// absent or the row does not exist.
func (s *Store) lookupNamedEntity(ctx context.Context, table string, id sql.NullString) (*models.DumpNamedEntity, error) {
	if !id.Valid || id.String == "" {
		return nil, nil
	}
	var e models.DumpNamedEntity
	var nameEn, nameJa sql.NullString
	err := s.db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT name_en, name_ja FROM %s WHERE id = ?", table),
		id.String,
	).Scan(&nameEn, &nameJa)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup %s %q: %w", table, id.String, err)
	}
	e.NameEn = nameEn.String
	e.NameJa = nameJa.String
	return &e, nil
}

func (s *Store) lookupDirector(ctx context.Context, contentID string) (*models.DumpDirector, error) {
	var dirID string
	err := s.db.QueryRowContext(ctx,
		"SELECT director_id FROM video_directors WHERE content_id = ? ORDER BY director_id LIMIT 1",
		contentID,
	).Scan(&dirID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup director join for %q: %w", contentID, err)
	}
	var d models.DumpDirector
	var kanji, kana, romaji sql.NullString
	err = s.db.QueryRowContext(ctx,
		"SELECT name_kanji, name_kana, name_romaji FROM directors WHERE id = ?",
		dirID,
	).Scan(&kanji, &kana, &romaji)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup director %q: %w", dirID, err)
	}
	d.NameKanji = kanji.String
	d.NameKana = kana.String
	d.NameRomaji = romaji.String
	return &d, nil
}

func (s *Store) lookupActresses(ctx context.Context, contentID string) ([]models.DumpActress, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT a.id, a.name_romaji, a.image_url, a.name_kanji, a.name_kana
		FROM video_actresses va JOIN actresses a ON va.actress_id = a.id
		WHERE va.content_id = ? ORDER BY COALESCE(va.ordinality, 999999999), a.id`, contentID,
	)
	if err != nil {
		return nil, fmt.Errorf("lookup actresses for %q: %w", contentID, err)
	}
	defer func() { _ = rows.Close() }()
	var out []models.DumpActress
	for rows.Next() {
		var a models.DumpActress
		var romaji, imageURL, kanji, kana sql.NullString
		if err := rows.Scan(&a.ID, &romaji, &imageURL, &kanji, &kana); err != nil {
			return nil, fmt.Errorf("scan actress for %q: %w", contentID, err)
		}
		a.NameRomaji = romaji.String
		a.ImageURL = imageURL.String
		a.NameKanji = kanji.String
		a.NameKana = kana.String
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate actresses for %q: %w", contentID, err)
	}
	return out, nil
}

func (s *Store) lookupCategories(ctx context.Context, contentID string) ([]models.DumpNamedEntity, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.name_en, c.name_ja
		FROM video_categories vc JOIN categories c ON vc.category_id = c.id
		WHERE vc.content_id = ? ORDER BY c.id`, contentID,
	)
	if err != nil {
		return nil, fmt.Errorf("lookup categories for %q: %w", contentID, err)
	}
	defer func() { _ = rows.Close() }()
	var out []models.DumpNamedEntity
	for rows.Next() {
		var c models.DumpNamedEntity
		var nameEn, nameJa sql.NullString
		if err := rows.Scan(&nameEn, &nameJa); err != nil {
			return nil, fmt.Errorf("scan category for %q: %w", contentID, err)
		}
		c.NameEn = nameEn.String
		c.NameJa = nameJa.String
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate categories for %q: %w", contentID, err)
	}
	return out, nil
}

func (s *Store) lookupTrailer(ctx context.Context, contentID string) (string, error) {
	var url sql.NullString
	err := s.db.QueryRowContext(ctx,
		"SELECT url FROM trailers WHERE content_id = ?", contentID,
	).Scan(&url)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("lookup trailer for %q: %w", contentID, err)
	}
	return url.String, nil
}

// Stats reports row count and stored dump metadata.
func (s *Store) Stats(ctx context.Context) (models.DumpStats, error) {
	if s == nil {
		return models.DumpStats{}, fmt.Errorf("dump store is nil")
	}
	var rowCount int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM videos").Scan(&rowCount); err != nil {
		return models.DumpStats{}, fmt.Errorf("count videos: %w", err)
	}
	meta, err := s.loadMeta(ctx)
	if err != nil {
		return models.DumpStats{}, err
	}
	return models.DumpStats{
		RowCount:   rowCount,
		SourceURL:  meta["source_url"],
		SourceDate: meta["source_date"],
		ImportedAt: meta["imported_at"],
		Path:       s.path,
	}, nil
}

func (s *Store) loadMeta(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT key, value FROM dump_meta")
	if err != nil {
		return nil, fmt.Errorf("query dump_meta: %w", err)
	}
	defer func() { _ = rows.Close() }()
	meta := map[string]string{}
	for rows.Next() {
		var k string
		var v sql.NullString // value is schema-nullable; key is PRIMARY KEY NOT NULL
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("scan dump_meta: %w", err)
		}
		meta[k] = v.String
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dump_meta: %w", err)
	}
	return meta, nil
}

// ExpandGallery expands a dump gallery range into individual relative paths.
// The dump stores the first and last URLs of a numbered screenshot sequence,
// e.g. first="digital/video/118abw00013/118abw00013jp-1",
// last=".../118abw00013jp-12". This function generates all 12 paths by
// splitting on the last hyphen and iterating the numeric suffix.
//
// The prefix (everything before the numeric suffix) must match between first
// and last; a mismatch indicates a malformed range and yields no URLs rather
// than silently emitting paths with the wrong prefix. A non-numeric suffix
// also yields no URLs.
func ExpandGallery(first, last string) []string {
	if first == "" || last == "" {
		return nil
	}
	i := strings.LastIndexByte(first, '-')
	j := strings.LastIndexByte(last, '-')
	if i < 0 || j < 0 {
		return nil
	}
	prefix := first[:i+1]
	lastPrefix := last[:j+1]
	// Reject mismatched prefixes to avoid emitting paths with the wrong base.
	if prefix != lastPrefix {
		return nil
	}
	startStr := first[i+1:]
	endStr := last[j+1:]
	start, err1 := strconv.Atoi(startStr)
	end, err2 := strconv.Atoi(endStr)
	if err1 != nil || err2 != nil || start > end || start < 0 {
		return nil
	}
	// Cap the expanded count at 1000 to guard against a malformed range
	// producing a pathological number of URLs.
	if count := end - start + 1; count > 1000 {
		return nil
	}
	urls := make([]string, 0, end-start+1)
	for n := start; n <= end; n++ {
		urls = append(urls, prefix+strconv.Itoa(n))
	}
	return urls
}

// NormalizeDumpURL converts a relative dump image path (e.g.
// "digital/video/118abw00013/118abw00013pl") into an absolute DMM CDN URL with
// a .jpg extension. Paths that are already absolute URLs, or that already carry
// an image extension, are returned unchanged.
func NormalizeDumpURL(rel string) string {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return ""
	}
	if strings.HasPrefix(rel, "http://") || strings.HasPrefix(rel, "https://") {
		return rel
	}
	if !strings.HasSuffix(rel, ".jpg") && !strings.HasSuffix(rel, ".jpeg") {
		rel += ".jpg"
	}
	return dmmCDNBase + rel
}

// normalizeDVDID normalizes a display dvd_id for index matching: uppercase,
// strip hyphens and all Unicode whitespace. This mirrors the r18.dev scraper's
// own dvd_id normalization so lookups align with how IDs are stored on import.
func normalizeDVDID(id string) string {
	id = strings.ToUpper(id)
	id = strings.ReplaceAll(id, "-", "")
	id = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, id)
	return id
}

var dumpPaddedMarkerRegex = regexp.MustCompile(`^((?:T28|[A-Z]+))([0-9]+)([EZ]?)(HD|H|AI)$`)

var dumpMarkerTailRegex = regexp.MustCompile(`^((?:T28|[A-Z]+)[0-9]+[EZ]?)(H|AI)$`)

var dumpMarkerSeriesRegex = regexp.MustCompile(`^(?:[0-9]*(?:T28|[A-Z]+))$`)
var dumpMarkerPinnedTailRegex = regexp.MustCompile(`^([0-9]+)([EZ]?)(HD|H|AI)$`)

// dumpMarkerDisplayKey retains the display-series boundary that normalized
// DVD keys erase. It mirrors the source's prefix-free compact T/T28 grammar;
// a separated series always wins, and only number-leading zeros are padding.
func dumpMarkerDisplayKey(id string) string {
	upper := strings.ToUpper(strings.TrimSpace(id))
	parts := strings.FieldsFunc(upper, func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || unicode.IsSpace(r)
	})
	var series, number, suffix, marker string
	if len(parts) >= 2 && dumpMarkerSeriesRegex.MatchString(parts[0]) {
		if m := dumpMarkerPinnedTailRegex.FindStringSubmatch(strings.Join(parts[1:], "")); m != nil {
			series, number, suffix, marker = parts[0], m[1], m[2], m[3]
		}
	}
	if series == "" {
		m := dumpPaddedMarkerRegex.FindStringSubmatch(strings.Join(parts, ""))
		if m == nil {
			return ""
		}
		series, number, suffix, marker = m[1], m[2], m[3], m[4]
		if series == "T28" && (len(number) == 3 || len(number) == 4) {
			series, number = "T", "28"+number
		}
	}
	if marker == "HD" {
		marker = "H"
	}
	number = strings.TrimLeft(number, "0")
	if number == "" {
		number = "0"
	}
	return series + "/" + number + "/" + suffix + "/" + marker
}

var dumpRawMarkerRegex = regexp.MustCompile(`^(\d*)((?:t28|[a-z]+))(\d+)([ez]?)(hd|ai|h)$`)

// dumpRawMarkerEqual preserves established padding aliases without turning
// candidate expansion into ownership of another catalog/channel or marker.
func dumpRawMarkerEqual(left, right string) bool {
	if strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right)) {
		return true
	}
	key := dumpRawMarkerKey(left)
	return key != "" && key == dumpRawMarkerKey(right)
}

func dumpRawMarkerKey(id string) string {
	raw := strings.ToLower(strings.TrimSpace(id))
	channel := ""
	if len(raw) > 2 && raw[1] == '_' {
		channel, raw = raw[:2], raw[2:]
	}
	m := dumpRawMarkerRegex.FindStringSubmatch(raw)
	if m == nil {
		return ""
	}
	series, number := m[2], m[3]
	if series == t28Series {
		if m[1] != "" {
			// The same fixture-proven boundary as the source guard. Unknown
			// prefixes cannot decide which digits are series versus number.
			switch channel + m[1] {
			case "9", "1", channel003Prefix:
			default:
				return ""
			}
		} else if len(number) == 3 || len(number) == 4 {
			series, number = "t", "28"+number
		}
	}
	return channel + m[1] + "/" + series + "/" + strings.TrimLeft(number, "0") + "/" + m[4] + "/" + m[5]
}

// isRawDumpMarkerQuery follows the marker candidate helper's literal-CID
// contract. Raw ownership is independent of any DVD ID stored on that row.
func isRawDumpMarkerQuery(id string) bool {
	raw := strings.ToLower(strings.TrimSpace(id))
	parts := remasterMarkerTailRgx.FindStringSubmatch(raw)
	if parts == nil {
		return false
	}
	if underscoreContentIDRegex.FindString(parts[1]) == parts[1] {
		return true
	}
	if strings.ContainsAny(strings.Join(strings.Fields(raw), " "), "-_. ") {
		return false
	}
	return (looksLikeContentID(raw) && contentIDFullRegex.MatchString(raw)) || wideNumericCIDRegex.MatchString(raw)
}

// dumpNormKeys returns all dvd_id_norm variants to probe for a query id: the
// direct normalization plus, for canonical remaster ids (…156H), the stored
// display-spelling form (…156HD) so dump rows recorded as "RCT-156-HD" hit.
func dumpNormKeys(id string) []string {
	n := normalizeDVDID(id)
	compacted := strings.NewReplacer(".", "", "_", "").Replace(n)
	if dumpPaddedMarkerRegex.MatchString(compacted) {
		n = compacted
	}
	if n == "" {
		return nil
	}
	keys := []string{n}
	if m := dumpPaddedMarkerRegex.FindStringSubmatch(n); m != nil {
		number := strings.TrimLeft(m[2], "0")
		if number == "" {
			number = "0"
		}
		for _, digits := range []string{number, strings.Repeat("0", max(0, 3-len(number))) + number} {
			key := m[1] + digits + m[3] + m[4]
			if key != n && key != keys[len(keys)-1] {
				keys = append(keys, key)
			}
		}
	}
	for _, key := range append([]string(nil), keys...) {
		if m := dumpMarkerTailRegex.FindStringSubmatch(key); m != nil {
			if m[2] == "H" {
				keys = append(keys, m[1]+"HD")
			}
			// AI is already the stored form in both spellings (DV-818AI /
			// DV-818-AI normalize to the same key), so nothing to add.
		}
	}
	return keys
}
