package r18devdump

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"

	_ "github.com/mattn/go-sqlite3"
)

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
}

// Open opens a read-only connection to the dump sidecar database. Returns an
// error if the file does not exist or is not a valid dump database; callers
// should treat that as "dump not available" and fall back to HTTP resolution.
func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?mode=ro&_busy_timeout=5000", path)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open dump db: %w", err)
	}
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping dump db: %w", err)
	}
	return &Store{db: db, path: path}, nil
}

// Close releases the underlying database connection.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// LookupByDVDID resolves a display dvd_id (e.g. "IPX-535") to its DMM
// content_id. The query is matched against a normalized dvd_id column
// (uppercase, hyphens and whitespace stripped), so "IPX-535", "ipx535", and
// " IPX 535 " all resolve identically. Returns models.ErrDumpMiss on a miss.
func (s *Store) LookupByDVDID(ctx context.Context, dvdID string) (string, error) {
	if s == nil || dvdID == "" {
		return "", models.ErrDumpMiss
	}
	norm := normalizeDVDID(dvdID)
	if norm == "" {
		return "", models.ErrDumpMiss
	}
	var contentID string
	err := s.db.QueryRowContext(ctx,
		"SELECT content_id FROM videos WHERE dvd_id_norm = ? ORDER BY content_id LIMIT 1",
		norm,
	).Scan(&contentID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", models.ErrDumpMiss
	}
	if err != nil {
		return "", fmt.Errorf("dump lookup by dvd_id %q: %w", dvdID, err)
	}
	return contentID, nil
}

// LookupByContentID resolves a DMM content_id back to its display dvd_id.
// Returns models.ErrDumpMiss when the content_id is absent or its dvd_id is
// NULL.
func (s *Store) LookupByContentID(ctx context.Context, contentID string) (string, error) {
	if s == nil || contentID == "" {
		return "", models.ErrDumpMiss
	}
	var dvdID sql.NullString
	err := s.db.QueryRowContext(ctx,
		"SELECT dvd_id FROM videos WHERE content_id = ? LIMIT 1",
		strings.ToLower(contentID),
	).Scan(&dvdID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", models.ErrDumpMiss
	}
	if err != nil {
		return "", fmt.Errorf("dump lookup by content_id %q: %w", contentID, err)
	}
	if !dvdID.Valid || dvdID.String == "" {
		return "", models.ErrDumpMiss
	}
	return dvdID.String, nil
}

// LookupMovie resolves a display dvd_id to a fully-populated DumpMovie with all
// related entities joined. This is the zero-HTTP fast path for the r18.dev
// scraper: when it hits, no r18.dev API call is needed at all. Returns
// models.ErrDumpMiss on a miss; any other wrapped error indicates a degraded
// dump (corrupt file, schema drift, I/O failure) that the caller should log
// before falling back to HTTP.
func (s *Store) LookupMovie(ctx context.Context, dvdID string) (*models.DumpMovie, error) {
	if s == nil || dvdID == "" {
		return nil, models.ErrDumpMiss
	}
	norm := normalizeDVDID(dvdID)
	if norm == "" {
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
	err := s.db.QueryRowContext(ctx, `SELECT
		content_id, dvd_id, title_en, title_ja, comment_en, comment_ja,
		runtime_mins, release_date, sample_url,
		maker_id, label_id, series_id,
		jacket_full_url, jacket_thumb_url,
		gallery_full_first, gallery_full_last,
		gallery_thumb_first, gallery_thumb_last,
		site_id, service_code
		FROM videos WHERE dvd_id_norm = ? ORDER BY content_id LIMIT 1`, norm,
	).Scan(
		&m.ContentID, &dvdIDCol, &titleEn, &titleJa, &commentEn, &commentJa,
		&runtime, &releaseDate, &sampleURL,
		&makerID, &labelID, &seriesID,
		&jacketFull, &jacketThumb,
		&galleryFirst, &galleryLast,
		&thumbFirst, &thumbLast,
		&siteID, &serviceCode,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, models.ErrDumpMiss
	}
	if err != nil {
		return nil, fmt.Errorf("dump lookup movie %q: %w", dvdID, err)
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
