package r18devdump

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"database/sql"
	_ "github.com/mattn/go-sqlite3"

	"github.com/javinizer/javinizer-go/internal/models"
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
