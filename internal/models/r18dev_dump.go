package models

import (
	"context"
	"errors"
)

// ErrDumpMiss is returned by R18DevDumpLookup lookups when the queried ID is
// genuinely absent from the cached dump — as opposed to a real database error
// (corrupt file, schema drift, I/O failure), which is returned as a wrapped
// non-sentinel error. Callers distinguish the two with errors.Is(err,
// ErrDumpMiss): a miss falls back to HTTP silently, while a real error is
// logged at warn so a degraded dump does not silently revert to rate-limit-
// prone HTTP.
var ErrDumpMiss = errors.New("r18.dev dump: id not found")

// DumpStats describes a locally cached r18.dev database dump.
type DumpStats struct {
	RowCount   int64  `json:"row_count"`
	SourceURL  string `json:"source_url"`
	SourceDate string `json:"source_date"`
	ImportedAt string `json:"imported_at"`
	Path       string `json:"path"`
}

// DumpNamedEntity is a maker, label, series, or category with bilingual names.
type DumpNamedEntity struct {
	NameEn string
	NameJa string
}

// DumpDirector is a director with kanji/kana/romaji names.
type DumpDirector struct {
	NameKanji  string
	NameKana   string
	NameRomaji string
}

// DumpActress is an actress record from the dump.
type DumpActress struct {
	ID         string
	NameRomaji string
	ImageURL   string
	NameKanji  string
	NameKana   string
}

// DumpMovie is a fully-resolved movie record from the local r18.dev dump. It
// carries every field the r18.dev API would return, so the scraper can build a
// complete ScraperResult locally with zero HTTP to r18.dev.
//
// Image/gallery fields store the relative paths exactly as they appear in the
// dump (e.g. "digital/video/118abw00013/118abw00013pl"); the scraper resolves
// these to absolute DMM CDN URLs at result-construction time.
type DumpMovie struct {
	ContentID      string
	DVDID          string
	TitleEn        string
	TitleJa        string
	CommentEn      string
	CommentJa      string
	Runtime        int
	ReleaseDate    string
	SampleURL      string
	JacketFullURL  string
	JacketThumbURL string
	GalleryFirst   string
	GalleryLast    string
	SiteID         string
	ServiceCode    string

	Maker      *DumpNamedEntity
	Label      *DumpNamedEntity
	Series     *DumpNamedEntity
	Director   *DumpDirector
	Actresses  []DumpActress
	Categories []DumpNamedEntity
	TrailerURL string
}

// R18DevDumpLookup provides local content_id <-> dvd_id resolution and full
// movie metadata from a cached r18.dev database dump. Implementations are
// backed by a sidecar SQLite database populated by `javinizer dump download`.
//
// When the dump is present, the r18.dev scraper consults LookupMovie first and
// returns a complete ScraperResult with zero HTTP to r18.dev. On a miss, it
// falls back to live HTTP resolution. When the dump is absent, the scraper
// behaves exactly as it would without this interface.
//
// Every lookup distinguishes a genuine miss (ErrDumpMiss) from a real database
// error (any other wrapped error). This lets the scraper log a degraded dump
// at warn instead of silently masking a corrupt sidecar as an endless string
// of misses that quietly revert to HTTP.
type R18DevDumpLookup interface {
	// LookupByDVDID resolves a display dvd_id (e.g. "IPX-535") to its DMM
	// content_id (e.g. "118ipx00535"). Returns ("", ErrDumpMiss) on a miss.
	LookupByDVDID(ctx context.Context, dvdID string) (contentID string, err error)

	// LookupByContentID resolves a DMM content_id back to its display dvd_id.
	// Returns ("", ErrDumpMiss) on a miss or when the dump's dvd_id is NULL.
	LookupByContentID(ctx context.Context, contentID string) (dvdID string, err error)

	// LookupMovie resolves a display dvd_id to a fully-populated DumpMovie —
	// titles, descriptions, runtime, release date, cover/poster/gallery URLs,
	// actresses, maker, label, series, director, categories, and trailer. This
	// is the zero-HTTP fast path: when it hits, the scraper needs no r18.dev
	// API call at all. Returns (nil, ErrDumpMiss) on a miss.
	LookupMovie(ctx context.Context, dvdID string) (*DumpMovie, error)

	// Stats reports metadata about the cached dump for diagnostics and the
	// `javinizer dump status` command.
	Stats(ctx context.Context) (DumpStats, error)
}
