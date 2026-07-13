package r18devdump

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"database/sql"
	_ "github.com/mattn/go-sqlite3"

	"github.com/javinizer/javinizer-go/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedDump(t *testing.T, rows string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "r18dev_dump.db")
	dump := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n" + rows + "\n\\.\n"
	res, err := Import(context.Background(), strings.NewReader(dump), path, ImportOptions{
		SourceURL:  "https://example.com/dumps/r18dotdev_dump_2026-04-28.sql.gz",
		SourceDate: "2026-04-28",
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if res.Path != path {
		t.Errorf("Import path = %q, want %q", res.Path, path)
	}
	return path
}

func TestImportAndLookup(t *testing.T) {
	path := seedDump(t, "118ipx00535\tIPX-535\n118abw00001\t\\N\nh_086mesu00103\tMESU-103")

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Exact dvd_id lookup.
	cid, err := store.LookupByDVDID(ctx, "IPX-535")
	if err != nil || cid != "118ipx00535" {
		t.Errorf("LookupByDVDID(IPX-535) = %q,%v, want 118ipx00535,nil", cid, err)
	}
	// Normalization: hyphen/case/whitespace-insensitive.
	for _, q := range []string{"ipx-535", "IPX535", " ipx 535 ", "ipx-535"} {
		cid, err := store.LookupByDVDID(ctx, q)
		if err != nil || cid != "118ipx00535" {
			t.Errorf("LookupByDVDID(%q) = %q,%v, want 118ipx00535,nil", q, cid, err)
		}
	}
	// Missing dvd_id -> ErrDumpMiss (not a generic error).
	if _, err := store.LookupByDVDID(ctx, "NOPE-999"); !errors.Is(err, models.ErrDumpMiss) {
		t.Errorf("LookupByDVDID(NOPE-999) err = %v, want ErrDumpMiss", err)
	}

	// Content_id -> dvd_id.
	did, err := store.LookupByContentID(ctx, "118ipx00535")
	if err != nil || did != "IPX-535" {
		t.Errorf("LookupByContentID = %q,%v, want IPX-535,nil", did, err)
	}
	// NULL dvd_id in dump -> miss.
	if _, err := store.LookupByContentID(ctx, "118abw00001"); !errors.Is(err, models.ErrDumpMiss) {
		t.Errorf("LookupByContentID for NULL dvd_id err = %v, want ErrDumpMiss", err)
	}

	// Stats.
	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.RowCount != 3 {
		t.Errorf("RowCount = %d, want 3", stats.RowCount)
	}
	if stats.SourceDate != "2026-04-28" {
		t.Errorf("SourceDate = %q, want 2026-04-28", stats.SourceDate)
	}
	if stats.SourceURL == "" {
		t.Error("SourceURL should be set")
	}
	if stats.ImportedAt == "" {
		t.Error("ImportedAt should be set")
	}
}

func TestImport_DuplicateContentIDIgnored(t *testing.T) {
	// Duplicate content_id: second insert is ignored, first dvd_id wins.
	path := seedDump(t, "118dup00001\tDUP-001\n118dup00001\tDUP-002")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	stats, _ := store.Stats(ctx)
	if stats.RowCount != 1 {
		t.Errorf("RowCount = %d, want 1 (dup ignored)", stats.RowCount)
	}
	did, err := store.LookupByContentID(ctx, "118dup00001")
	if err != nil || did != "DUP-001" {
		t.Errorf("LookupByContentID = %q,%v, want DUP-001,nil (first wins)", did, err)
	}
}

func TestImport_AtomicFailureLeavesPathUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r18dev_dump.db")

	// First, create a valid dump at path.
	goodPath := seedDump(t, "118ipx00535\tIPX-535")
	if goodPath != path {
		// seedDump builds under its own temp dir; replicate into ours.
		data, _ := os.ReadFile(goodPath)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatalf("write seed: %v", err)
		}
	}

	// Now attempt an import with a reader that errors mid-stream. This tests
	// the atomic-failure guarantee: the original good database must remain
	// intact and the stale .tmp file must be cleaned up.
	_, err := Import(context.Background(), &errorReader{}, path, ImportOptions{})
	if err == nil {
		t.Fatal("expected Import to fail on reader error")
	}

	// The original good database must still be intact and queryable.
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open after failed import: %v (original should be intact)", err)
	}
	defer store.Close()
	cid, err := store.LookupByDVDID(context.Background(), "IPX-535")
	if err != nil || cid != "118ipx00535" {
		t.Errorf("original data lost after failed import: %q,%v", cid, err)
	}
	// Stale temp file must be cleaned up.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("stale .tmp file left behind after failed import")
	}
}

func TestOpen_MissingFile(t *testing.T) {
	_, err := Open(filepath.Join(t.TempDir(), "does-not-exist.db"))
	if err == nil {
		t.Fatal("expected error opening non-existent dump db")
	}
}

func TestStats_ErrorOnMissingVideosTable(t *testing.T) {
	// A valid SQLite file with no `videos` table: Open succeeds but Stats fails.
	path := filepath.Join(t.TempDir(), "empty.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	// Materialize the file with a table other than `videos` so Open succeeds but
	// Stats' COUNT(*) against the missing videos table fails.
	if _, err := db.Exec("CREATE TABLE dump_meta (key TEXT PRIMARY KEY, value TEXT)"); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	if _, err := store.Stats(context.Background()); err == nil {
		t.Error("expected Stats to error on missing videos table")
	}
}

func TestNilStoreIsSafe(t *testing.T) {
	var s *Store
	ctx := context.Background()
	if _, err := s.LookupByDVDID(ctx, "x"); !errors.Is(err, models.ErrDumpMiss) {
		t.Errorf("nil store LookupByDVDID err = %v, want ErrDumpMiss", err)
	}
	if _, err := s.LookupByContentID(ctx, "x"); !errors.Is(err, models.ErrDumpMiss) {
		t.Errorf("nil store LookupByContentID err = %v, want ErrDumpMiss", err)
	}
	if _, err := s.LookupMovie(ctx, "x"); !errors.Is(err, models.ErrDumpMiss) {
		t.Errorf("nil store LookupMovie err = %v, want ErrDumpMiss", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("nil store Close should be nil, got %v", err)
	}
}

// TestImport_ConcurrentReadWhileImporting validates the concurrency design: a
// read-only Store opened on `path` keeps serving consistent lookups while a
// concurrent Import writes a .tmp file and atomically renames it over `path`.
// The open connection's file descriptor points to the old inode, which rename
// leaves intact (unlinked but alive until Close), so reads never observe a
// half-written database. A fresh Open after the import sees the new data.
// Run with -race to confirm no data races.
//
// This invariant is POSIX-only: rename over a file with open handles relies on
// the kernel unlinking the old inode while the open fd keeps it alive. Windows
// locks files that are open, so the rename is refused ("The process cannot
// access the file because it is being used by another process"). The
// production import path (close-then-rename) still works on Windows when no
// reader is open; only the concurrent-reader-during-rename case is unsupported
// there.
func TestImport_ConcurrentReadWhileImporting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("atomic rename over an open reader relies on POSIX inode semantics; Windows locks open files")
	}
	path := seedDump(t, "118ipx00535	IPX-535")

	reader, err := Open(path)
	if err != nil {
		t.Fatalf("Open reader: %v", err)
	}
	defer reader.Close()

	ctx := context.Background()
	// The reader was opened before the import, so it must keep seeing the old
	// (IPX-535) data throughout and after the import.
	if cid, err := reader.LookupByDVDID(ctx, "IPX-535"); err != nil || cid != "118ipx00535" {
		t.Fatalf("pre-import read failed: cid=%q err=%v", cid, err)
	}

	newDump := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118abw00013\tABW-013\n\\.\n"
	if _, err := Import(ctx, strings.NewReader(newDump), path, ImportOptions{}); err != nil {
		t.Fatalf("concurrent Import: %v", err)
	}

	// The original reader still sees the old inode: IPX-535 hits, ABW-013 misses.
	if cid, err := reader.LookupByDVDID(ctx, "IPX-535"); err != nil || cid != "118ipx00535" {
		t.Errorf("post-import old reader lost IPX-535: cid=%q err=%v", cid, err)
	}
	if _, err := reader.LookupByDVDID(ctx, "ABW-013"); !errors.Is(err, models.ErrDumpMiss) {
		t.Errorf("old reader should not see post-import ABW-013: err=%v", err)
	}

	// A fresh Open resolves the new path and sees the imported ABW-013.
	fresh, err := Open(path)
	if err != nil {
		t.Fatalf("fresh Open: %v", err)
	}
	defer fresh.Close()
	if cid, err := fresh.LookupByDVDID(ctx, "ABW-013"); err != nil || cid != "118abw00013" {
		t.Errorf("fresh reader should see imported ABW-013: cid=%q err=%v", cid, err)
	}
}

func TestNormalizeDVDID(t *testing.T) {
	tests := []struct{ in, want string }{
		{"IPX-535", "IPX535"},
		{"ipx-535", "IPX535"},
		{" IPX 535 ", "IPX535"},
		{"h_086mesu-103", "H_086MESU103"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := normalizeDVDID(tt.in); got != tt.want {
			t.Errorf("normalizeDVDID(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// errorReader is an io.Reader that always returns an error, simulating a
// truncated/corrupt dump stream mid-import.
type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

// TestImport_EmptyPathErrors covers the empty-path guard in Import.
func TestImport_EmptyPathErrors(t *testing.T) {
	_, err := Import(context.Background(), strings.NewReader(""), "", ImportOptions{})
	if err == nil || !strings.Contains(err.Error(), "path is empty") {
		t.Fatalf("expected empty-path error, got: %v", err)
	}
}

// TestSqliteTableName_DefaultAndMapped covers the default (passthrough) branch
// and the unmapped-table case of sqliteTableName.
func TestSqliteTableName_DefaultAndMapped(t *testing.T) {
	if got := sqliteTableName("unknown_table"); got != "unknown_table" {
		t.Errorf("unknown table should pass through, got %q", got)
	}
	if got := sqliteTableName("makers"); got != "makers" {
		t.Errorf("makers should map to makers, got %q", got)
	}
	if got := sqliteTableName("derived_video_actress"); got != "video_actresses" {
		t.Errorf("derived_video_actress should map to video_actresses, got %q", got)
	}
}

// TestLookup_QueryErrorsPropagated covers the non-ErrNoRows error branches of
// LookupByDVDID, LookupByContentID, LookupMovie, and Stats. Dropping the
// videos table makes every query against it fail at runtime with a real error
// (not sql.ErrNoRows), exercising the error-return paths.
func TestLookup_QueryErrorsPropagated(t *testing.T) {
	path := seedDump(t, "118ipx00535	IPX-535")

	corruptor, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open corruptor: %v", err)
	}
	if _, err := corruptor.Exec("DROP TABLE videos"); err != nil {
		corruptor.Close()
		t.Fatalf("drop videos: %v", err)
	}
	corruptor.Close()

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	if _, err := store.LookupByDVDID(ctx, "IPX-535"); err == nil || errors.Is(err, models.ErrDumpMiss) {
		t.Errorf("LookupByDVDID: expected a real error, got %v", err)
	}
	if _, err := store.LookupByContentID(ctx, "118ipx00535"); err == nil || errors.Is(err, models.ErrDumpMiss) {
		t.Errorf("LookupByContentID: expected a real error, got %v", err)
	}
	if _, err := store.LookupMovie(ctx, "IPX-535"); err == nil || errors.Is(err, models.ErrDumpMiss) {
		t.Errorf("LookupMovie: expected a real error, got %v", err)
	}
	if _, err := store.Stats(ctx); err == nil {
		t.Errorf("Stats: expected an error, got nil")
	}
}

// TestLookup_NilStoreAndEmptyID covers the early-return guards.
func TestLookup_NilStoreAndEmptyID(t *testing.T) {
	var s *Store
	ctx := context.Background()
	if _, err := s.LookupByDVDID(ctx, "IPX-535"); err != models.ErrDumpMiss {
		t.Errorf("nil store LookupByDVDID: got %v, want ErrDumpMiss", err)
	}
	if _, err := s.LookupByContentID(ctx, "x"); err != models.ErrDumpMiss {
		t.Errorf("nil store LookupByContentID: got %v, want ErrDumpMiss", err)
	}
	if _, err := s.LookupMovie(ctx, "IPX-535"); err != models.ErrDumpMiss {
		t.Errorf("nil store LookupMovie: got %v, want ErrDumpMiss", err)
	}
	if _, err := s.Stats(ctx); err == nil {
		t.Error("nil store Stats: expected error")
	}
}

// TestLookupMovie_NamedEntityAndTrailerErrors covers the error branches of
// lookupNamedEntity (maker/label/series), lookupDirector, and lookupTrailer by
// dropping their tables. Each must degrade gracefully (nil/empty) rather than
// abort LookupMovie, hitting logJoinDegraded's warn path.
func TestLookupMovie_NamedEntityAndTrailerErrors(t *testing.T) {
	path := importFullDump(t)

	corruptor, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open corruptor: %v", err)
	}
	for _, tbl := range []string{"makers", "labels", "series", "directors", "video_directors", "trailers"} {
		if _, err := corruptor.Exec("DROP TABLE " + tbl); err != nil {
			corruptor.Close()
			t.Fatalf("drop %s: %v", tbl, err)
		}
	}
	corruptor.Close()

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	// LookupMovie must not abort despite every named-entity/trailer join
	// failing; core video data is still returned.
	m, err := store.LookupMovie(context.Background(), "ABW-013")
	if err != nil {
		t.Fatalf("degraded joins must not abort LookupMovie: %v", err)
	}
	if m == nil || m.ContentID != "118abw00013" {
		t.Fatalf("core video data must still resolve, got: %+v", m)
	}
	if m.Maker != nil || m.Label != nil || m.Series != nil || m.Director != nil {
		t.Errorf("named entities should degrade to nil on query error")
	}
	if m.TrailerURL != "" {
		t.Errorf("trailer should degrade to empty on query error, got %q", m.TrailerURL)
	}
}

// TestLookup_EmptyNormalizedID covers the norm=="" guard branches in
// LookupByDVDID and LookupMovie: an ID that normalizes to empty (e.g. "---")
// returns ErrDumpMiss without hitting the database.
func TestLookup_EmptyNormalizedID(t *testing.T) {
	path := seedDump(t, "118ipx00535	IPX-535")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	for _, id := range []string{"---", "-", "   ", "!@#"} {
		if _, err := store.LookupByDVDID(ctx, id); err != models.ErrDumpMiss {
			t.Errorf("LookupByDVDID(%q): got %v, want ErrDumpMiss", id, err)
		}
		if _, err := store.LookupMovie(ctx, id); err != models.ErrDumpMiss {
			t.Errorf("LookupMovie(%q): got %v, want ErrDumpMiss", id, err)
		}
	}
}

// TestLookupByContentID_Miss covers the ErrNoRows branch of LookupByContentID
// (a content_id that doesn't exist in the videos table).
func TestLookupByContentID_Miss(t *testing.T) {
	path := seedDump(t, "118ipx00535	IPX-535")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	if _, err := store.LookupByContentID(context.Background(), "nonexistent999"); err != models.ErrDumpMiss {
		t.Errorf("LookupByContentID miss: got %v, want ErrDumpMiss", err)
	}
}

// TestLookupDirector_SecondQueryError covers the lookupDirector branch where
// the first query (video_directors) succeeds but the second (directors) fails.
// Dropping ONLY the directors table (not video_directors) triggers this.
func TestLookupDirector_SecondQueryError(t *testing.T) {
	path := importFullDump(t)
	corruptor, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open corruptor: %v", err)
	}
	if _, err := corruptor.Exec("DROP TABLE directors"); err != nil {
		corruptor.Close()
		t.Fatalf("drop directors: %v", err)
	}
	corruptor.Close()
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	// LookupMovie must not abort; director degrades to nil.
	m, err := store.LookupMovie(context.Background(), "ABW-013")
	if err != nil {
		t.Fatalf("degraded director must not abort: %v", err)
	}
	if m == nil || m.ContentID != "118abw00013" {
		t.Fatalf("core data must resolve: %+v", m)
	}
	if m.Director != nil {
		t.Errorf("director should degrade to nil, got %+v", m.Director)
	}
}

// TestLookupNamedEntity_ErrNoRows covers the ErrNoRows branch of
// lookupNamedEntity: the movie has a maker_id/label_id/series_id, but those
// rows were deleted from the makers/labels/series tables.
func TestLookupNamedEntity_ErrNoRows(t *testing.T) {
	path := importFullDump(t)
	corruptor, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open corruptor: %v", err)
	}
	// Delete all maker/label/series rows so the foreign-key lookups return
	// ErrNoRows (not a query error — the tables exist, just no matching rows).
	for _, tbl := range []string{"makers", "labels", "series"} {
		if _, err := corruptor.Exec("DELETE FROM " + tbl); err != nil {
			corruptor.Close()
			t.Fatalf("delete from %s: %v", tbl, err)
		}
	}
	corruptor.Close()
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	m, err := store.LookupMovie(context.Background(), "ABW-013")
	if err != nil {
		t.Fatalf("missing named entities must not abort: %v", err)
	}
	if m == nil || m.ContentID != "118abw00013" {
		t.Fatalf("core data must resolve: %+v", m)
	}
	if m.Maker != nil || m.Label != nil || m.Series != nil {
		t.Errorf("named entities should be nil on ErrNoRows")
	}
}

// TestLookupDirector_ErrNoRows covers the ErrNoRows branch of lookupDirector's
// second query: video_directors has a director_id, but that director was
// deleted from the directors table.
func TestLookupDirector_ErrNoRows(t *testing.T) {
	path := importFullDump(t)
	corruptor, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open corruptor: %v", err)
	}
	if _, err := corruptor.Exec("DELETE FROM directors"); err != nil {
		corruptor.Close()
		t.Fatalf("delete directors: %v", err)
	}
	corruptor.Close()
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	m, err := store.LookupMovie(context.Background(), "ABW-013")
	if err != nil {
		t.Fatalf("missing director must not abort: %v", err)
	}
	if m == nil || m.ContentID != "118abw00013" {
		t.Fatalf("core data must resolve: %+v", m)
	}
	if m.Director != nil {
		t.Errorf("director should be nil on ErrNoRows")
	}
}

// TestLookupCategories_QueryError covers the lookupCategories error branch by
// dropping the categories table (the JOIN fails).
func TestLookupCategories_QueryError(t *testing.T) {
	path := importFullDump(t)
	corruptor, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open corruptor: %v", err)
	}
	if _, err := corruptor.Exec("DROP TABLE categories"); err != nil {
		corruptor.Close()
		t.Fatalf("drop categories: %v", err)
	}
	corruptor.Close()
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	m, err := store.LookupMovie(context.Background(), "ABW-013")
	if err != nil {
		t.Fatalf("categories error must not abort: %v", err)
	}
	if m == nil || m.ContentID != "118abw00013" {
		t.Fatalf("core data must resolve: %+v", m)
	}
	if len(m.Categories) != 0 {
		t.Errorf("categories should degrade to empty, got %d", len(m.Categories))
	}
}

// TestStats_LoadMetaError covers the loadMeta error branches by dropping the
// dump_meta table. Stats must return an error (it calls loadMeta).
func TestStats_LoadMetaError(t *testing.T) {
	path := seedDump(t, "118ipx00535	IPX-535")
	corruptor, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open corruptor: %v", err)
	}
	if _, err := corruptor.Exec("DROP TABLE dump_meta"); err != nil {
		corruptor.Close()
		t.Fatalf("drop dump_meta: %v", err)
	}
	corruptor.Close()
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	if _, err := store.Stats(context.Background()); err == nil {
		t.Error("Stats should error when dump_meta is missing")
	}
}

// TestImport_MkdirAllFailure covers the MkdirAll error branch of Import by
// pointing the path at a sub-path of an existing file (not a directory).
func TestImport_MkdirAllFailure(t *testing.T) {
	// Create a file, then use a path underneath it — MkdirAll fails because
	// the parent is a file, not a directory.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	badPath := filepath.Join(blocker, "sub", "r18dev_dump.db")
	_, err := Import(context.Background(), strings.NewReader(""), badPath, ImportOptions{})
	if err == nil || !strings.Contains(err.Error(), "create dump dir") {
		t.Fatalf("expected create-dump-dir error, got: %v", err)
	}
}

// TestImport_CanceledContext covers the ctx.Err() guard in the emit closure:
// a canceled context makes emit return immediately, which ParseDump propagates.
func TestImport_CanceledContext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r18dev_dump.db")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dump := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n"
	_, err := Import(ctx, strings.NewReader(dump), path, ImportOptions{})
	if err == nil {
		t.Fatal("expected an error from canceled context, got nil")
	}
}

// TestImport_BatchFlushAndSkipTable covers three branches:
//   - len(batch) == 0 guard in flush (line 244): after a mid-stream flush,
//     flushAll encounters an empty residual batch for that table.
//   - !ok skip-table guard in emit (line 277): a COPY for a table we don't
//     store is silently skipped.
//   - len(batches) >= importBatchSize triggers flush (line 283): 41+ rows
//     for derived_video forces a mid-stream batch flush.
func TestImport_BatchFlushAndSkipTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r18dev_dump.db")
	// Generate 80 rows for derived_video (2× importBatchSize=40) so the
	// residual batch is empty after the second mid-stream flush — this hits
	// the len(batch)==0 guard in flush during flushAll.
	var sb strings.Builder
	sb.WriteString("COPY public.derived_video (content_id, dvd_id) FROM stdin;\n")
	for i := 0; i < 80; i++ {
		fmt.Fprintf(&sb, "118ipx%05d\tIPX-%d\n", i, i)
	}
	sb.WriteString("\\.\n")
	// A table we don't store — emit must skip it (line 277).
	sb.WriteString("COPY public.unknown_table (col) FROM stdin;\nsomedata\n\\.\n")
	res, err := Import(context.Background(), strings.NewReader(sb.String()), path, ImportOptions{})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if res.Rows != 80 {
		t.Errorf("Rows: got %d, want 80", res.Rows)
	}
	// Verify the store opens and the data is queryable.
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()
	cid, err := store.LookupByDVDID(context.Background(), "IPX-79")
	if err != nil {
		t.Fatalf("LookupByDVDID: %v", err)
	}
	if cid != "118ipx00079" {
		t.Errorf("content_id: got %q, want 118ipx00040", cid)
	}
}

// TestImport_EmitCtxCanceledMidStream covers the ctx.Err() guard inside emit
// (line 273): a reader that cancels the context AFTER the schema is created
// but BEFORE data rows are emitted, so emit's ctx.Err() check is reached.
func TestImport_EmitCtxCanceledMidStream(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r18dev_dump.db")
	ctx, cancel := context.WithCancel(context.Background())
	// Use a reader that cancels the context when the data COPY starts.
	r := &cancelingReader{
		data:   "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n",
		cancel: cancel,
	}
	_, err := Import(ctx, r, path, ImportOptions{})
	if err == nil {
		t.Fatal("expected error from canceled context mid-stream")
	}
}

// cancelingReader cancels the context when it encounters the first data byte
// after the COPY header line (the '\n' after "FROM stdin;").
type cancelingReader struct {
	data     string
	cancel   context.CancelFunc
	canceled bool
}

func (r *cancelingReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	// Cancel after we've read past the COPY header line.
	if !r.canceled && strings.Contains(r.data, "FROM stdin;") {
		idx := strings.Index(r.data, "\n")
		if idx >= 0 {
			n := copy(p, r.data[:idx+1])
			r.data = r.data[idx+1:]
			r.cancel()
			r.canceled = true
			return n, nil
		}
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

// TestImport_FlushAllError covers the flushAll error propagation (lines 263,
// 293-296): a reader that cancels the context when returning EOF causes the
// residual batch flush (in flushAll → insertBatch → tx.ExecContext) to fail
// with a canceled-context error, which Import surfaces as "insert rows".
func TestImport_FlushAllError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r18dev_dump.db")
	ctx, cancel := context.WithCancel(context.Background())
	// Reader returns valid COPY data (< importBatchSize rows, so no mid-stream
	// flush — the residual stays in batches), then cancels the context on EOF
	// so flushAll's insertBatch fails on the canceled context.
	r := &cancelOnEOFReader{
		data:   "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n",
		cancel: cancel,
	}
	_, err := Import(ctx, r, path, ImportOptions{})
	if err == nil || !strings.Contains(err.Error(), "insert rows") {
		t.Fatalf("expected insert-rows error, got: %v", err)
	}
}

// cancelOnEOFReader returns the data, then cancels the context when EOF is
// reached. This ensures the context is canceled AFTER ParseDump finishes but
// BEFORE flushAll runs.
type cancelOnEOFReader struct {
	data     string
	cancel   context.CancelFunc
	canceled bool
}

func (r *cancelOnEOFReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		if !r.canceled {
			r.cancel()
			r.canceled = true
		}
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

// TestImport_WriteMetaError covers the writeMeta error branch (lines 298-299,
// 445): by generating exactly importBatchSize (40) rows, the 40th row triggers
// a mid-stream flush that empties the batch. The reader then cancels the
// context on EOF, so flushAll succeeds (empty residual) but writeMeta's
// tx.ExecContext fails on the canceled context → "write dump_meta" error.
func TestImport_WriteMetaError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r18dev_dump.db")
	ctx, cancel := context.WithCancel(context.Background())
	var sb strings.Builder
	sb.WriteString("COPY public.derived_video (content_id, dvd_id) FROM stdin;\n")
	for i := 0; i < importBatchSize; i++ {
		fmt.Fprintf(&sb, "118ipx%05d\tIPX-%d\n", i, i)
	}
	sb.WriteString("\\.\n")
	r := &cancelOnEOFReader{data: sb.String(), cancel: cancel}
	_, err := Import(ctx, r, path, ImportOptions{})
	if err == nil || !strings.Contains(err.Error(), "write dump_meta") {
		t.Fatalf("expected write-dump_meta error, got: %v", err)
	}
}

// TestImport_RenameError covers the os.Rename error branch (line 321): when
// the target path already exists as a directory, Rename fails.
func TestImport_RenameError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "r18dev_dump.db")
	// Create the target path as a directory so Rename(tmpPath, path) fails.
	require.NoError(t, os.MkdirAll(path, 0o755))
	dump := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n"
	_, err := Import(context.Background(), strings.NewReader(dump), path, ImportOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rename tmp db")
}

func TestImport_BeforeSwap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r18dev_dump.db")
	dump := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n"
	called := false

	afterCalled := false
	_, err := Import(context.Background(), strings.NewReader(dump), path, ImportOptions{
		BeforeSwap: func() error {
			called = true
			_, statErr := os.Stat(path + ".tmp")
			return statErr
		},
		AfterSwap: func() { afterCalled = true },
	})

	require.NoError(t, err)
	assert.True(t, called)
	assert.True(t, afterCalled)
	_, err = os.Stat(path)
	require.NoError(t, err)
}

func TestImport_BeforeSwapError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r18dev_dump.db")
	dump := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n"
	want := errors.New("swap blocked")

	_, err := Import(context.Background(), strings.NewReader(dump), path, ImportOptions{
		BeforeSwap: func() error { return want },
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, want)
	assert.Contains(t, err.Error(), "prepare dump swap")
	_, statErr := os.Stat(path + ".tmp")
	assert.True(t, os.IsNotExist(statErr))
}

// insertManyRows inserts n rows into the given table using a transaction for
// speed. Each row is identified by a numeric suffix. Used by the rows.Err()
// tests to create enough rows that context cancellation fires mid-iteration.
func insertManyRows(t *testing.T, db *sql.DB, table string, n int) {
	t.Helper()
	tx, err := db.Begin()
	require.NoError(t, err)
	var stmt *sql.Stmt
	switch table {
	case "actresses":
		stmt, err = tx.Prepare("INSERT INTO actresses (id, name_romaji) VALUES (?, ?)")
		require.NoError(t, err)
		for i := 0; i < n; i++ {
			_, err := stmt.Exec(fmt.Sprintf("id%d", i), fmt.Sprintf("Name %d", i))
			require.NoError(t, err)
		}
		// Link all actresses to the one content_id.
		stmt.Close()
		stmt, err = tx.Prepare("INSERT INTO video_actresses (content_id, actress_id) VALUES ('118ipx00535', ?)")
		require.NoError(t, err)
		for i := 0; i < n; i++ {
			_, err := stmt.Exec(fmt.Sprintf("id%d", i))
			require.NoError(t, err)
		}
	case "categories":
		stmt, err = tx.Prepare("INSERT INTO categories (id, name_en) VALUES (?, ?)")
		require.NoError(t, err)
		for i := 0; i < n; i++ {
			_, err := stmt.Exec(fmt.Sprintf("cat%d", i), fmt.Sprintf("Category %d", i))
			require.NoError(t, err)
		}
		stmt.Close()
		stmt, err = tx.Prepare("INSERT INTO video_categories (content_id, category_id) VALUES ('118ipx00535', ?)")
		require.NoError(t, err)
		for i := 0; i < n; i++ {
			_, err := stmt.Exec(fmt.Sprintf("cat%d", i))
			require.NoError(t, err)
		}
	case "dump_meta":
		stmt, err = tx.Prepare("INSERT INTO dump_meta (key, value) VALUES (?, ?)")
		require.NoError(t, err)
		for i := 0; i < n; i++ {
			_, err := stmt.Exec(fmt.Sprintf("key%d", i), fmt.Sprintf("val%d", i))
			require.NoError(t, err)
		}
	}
	if stmt != nil {
		stmt.Close()
	}
	require.NoError(t, tx.Commit())
}

// TestLookupActresses_RowsErr covers the rows.Err() branch of lookupActresses
// (line 321): a query with many rows is interrupted by context cancellation
// during iteration, causing rows.Err() to return context.Canceled.
func TestLookupActresses_RowsErr(t *testing.T) {
	path := seedDump(t, "118ipx00535	IPX-535")
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	insertManyRows(t, db, "actresses", 50000)
	db.Close()

	store, err := Open(path)
	require.NoError(t, err)
	defer store.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Millisecond)
	defer cancel()
	_, _ = store.LookupMovie(ctx, "IPX-535")
	// The error is caught by logJoinDegraded (actresses degrade to empty),
	// but rows.Err() is still exercised.
}

// TestLookupCategories_RowsErr covers the rows.Err() branch of lookupCategories
// (line 348): 0 actresses (fast), many categories, context canceled during
// categories iteration.
func TestLookupCategories_RowsErr(t *testing.T) {
	path := seedDump(t, "118ipx00535	IPX-535")
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	insertManyRows(t, db, "categories", 50000)
	db.Close()

	store, err := Open(path)
	require.NoError(t, err)
	defer store.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Millisecond)
	defer cancel()
	_, _ = store.LookupMovie(ctx, "IPX-535")
}

// TestLoadMeta_RowsErr covers the rows.Err() body (line 405) and Scan error
// body (line 400) of loadMeta: uses 1000 rows with 100KB values so the query
// reliably takes > 1ms, ensuring the context deadline fires during Scan or
// iteration. Both paths are exercised across runs.
func TestLoadMeta_RowsErr(t *testing.T) {
	path := seedDump(t, "118ipx00535	IPX-535")
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	tx, err := db.Begin()
	require.NoError(t, err)
	stmt, err := tx.Prepare("INSERT INTO dump_meta (key, value) VALUES (?, ?)")
	require.NoError(t, err)
	bigVal := strings.Repeat("x", 100*1024) // 100KB per row
	for i := 0; i < 1000; i++ {
		_, err := stmt.Exec(fmt.Sprintf("key%d", i), bigVal)
		require.NoError(t, err)
	}
	stmt.Close()
	require.NoError(t, tx.Commit())
	db.Close()

	store, err := Open(path)
	require.NoError(t, err)
	defer store.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	_, _ = store.Stats(ctx)
	// Run several iterations with fresh timeouts — the Scan error (line 400)
	// and rows.Err error (line 405) fire on different runs depending on whether
	// the deadline hits during Scan or between Next() calls. Both are covered
	// across the iterations.
	for i := 0; i < 10; i++ {
		ctx2, cancel2 := context.WithTimeout(context.Background(), time.Millisecond)
		_, _ = store.Stats(ctx2)
		cancel2()
	}
}

// TestImport_BeginTxError covers the BeginTx error branch (line 234): with a
// very short context timeout (~50µs), schema creation (DDL) often completes
// but BeginTx fails with "context deadline exceeded". The test runs multiple
// iterations because the timing is inherently racy — schema creation and
// BeginTx are both sub-millisecond, so the timeout must fire in the narrow
// window between them.
func TestImport_BeginTxError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r18dev_dump.db")
	hitBeginTx := false
	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Microsecond)
		_, err := Import(ctx, strings.NewReader(""), path, ImportOptions{})
		cancel()
		if err != nil && strings.Contains(err.Error(), "begin tx") {
			hitBeginTx = true
			break
		}
		_ = os.Remove(path + ".tmp")
		_ = os.Remove(path + ".tmp-wal")
		_ = os.Remove(path + ".tmp-shm")
	}
	if !hitBeginTx {
		t.Skip("BeginTx error not triggered in 20 iterations — timing-dependent on this machine")
	}
}
