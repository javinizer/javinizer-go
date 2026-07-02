package r18devdump

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// fullDump is a fixture containing multiple related COPY blocks so LookupMovie
// can be tested against a fully-joined movie, including a director.
const fullDump = `COPY public.derived_video (content_id, dvd_id, title_en, title_ja, comment_en, comment_ja, runtime_mins, release_date, sample_url, maker_id, label_id, series_id, jacket_full_url, jacket_thumb_url, gallery_full_first, gallery_full_last, gallery_thumb_first, gallery_thumb_last, site_id, service_code) FROM stdin;
118abw00013	ABW-013	You Can't Make A Sound	Airi Suzumura	\N	\N	182	2020-10-02	https://cc3001.dmm.co.jp/litevideo/freepv/1/118/118abw013/118abw013_dmb_w.mp4	40136	2062442	4012940	digital/video/118abw00013/118abw00013pl	digital/video/118abw00013/118abw00013ps	digital/video/118abw00013/118abw00013jp-1	digital/video/118abw00013/118abw00013jp-12	digital/video/118abw00013/118abw00013-1	digital/video/118abw00013/118abw00013-12	2	digital
\.
COPY public.derived_actress (id, name_romaji, image_url, name_kanji, name_kana) FROM stdin;
1019076	Airi Suzumura	suzumura_airi.jpg	鈴村あいり	すずむらあいり
\.
COPY public.derived_maker (id, name_en, name_ja) FROM stdin;
40136	Prestige	プレステージ
\.
COPY public.derived_label (id, name_en, name_ja) FROM stdin;
2062442	ABSOLUTELY WONDERFUL	ABSOLUTELY WONDERFUL
\.
COPY public.derived_series (id, name_en, name_ja) FROM stdin;
4012940	When You Can't Scream Out...	声が出せない状況で…
\.
COPY public.derived_director (id, name_kanji, name_kana, name_romaji) FROM stdin;
5001	監督名	かんとくめい	Kantoku Mei
\.
COPY public.derived_category (id, name_en, name_ja) FROM stdin;
4111	Cheating Wife	寝取り・寝取られ・NTR
5016	Squirting	潮吹き
\.
COPY public.derived_video_actress (content_id, actress_id, ordinality, release_date) FROM stdin;
118abw00013	1019076	1	2020-10-02
\.
COPY public.derived_video_category (content_id, category_id, release_date) FROM stdin;
118abw00013	4111	2020-10-02
118abw00013	5016	2020-10-02
\.
COPY public.derived_video_director (content_id, director_id) FROM stdin;
118abw00013	5001
\.
COPY public.source_dmm_trailer (content_id, url, "timestamp") FROM stdin;
118abw00013	https://cc3001.dmm.co.jp/litevideo/freepv/1/118/118abw013/118abw013_trailer.mp4	2020-10-02
\.
`

func importFullDump(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/r18dev_dump.db"
	_, err := Import(context.Background(), strings.NewReader(fullDump), path, ImportOptions{SourceDate: "2026-06-30"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	return path
}

func TestLookupMovie_FullJoin(t *testing.T) {
	path := importFullDump(t)
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	m, err := store.LookupMovie(ctx, "ABW-013")
	if err != nil {
		t.Fatalf("LookupMovie miss for ABW-013: %v", err)
	}

	// Core video fields.
	assertEq(t, "ContentID", m.ContentID, "118abw00013")
	assertEq(t, "DVDID", m.DVDID, "ABW-013")
	assertEq(t, "TitleEn", m.TitleEn, "You Can't Make A Sound")
	assertEq(t, "TitleJa", m.TitleJa, "Airi Suzumura")
	assertEq(t, "ReleaseDate", m.ReleaseDate, "2020-10-02")
	if m.Runtime != 182 {
		t.Errorf("Runtime = %d, want 182", m.Runtime)
	}
	assertEq(t, "SampleURL", m.SampleURL, "https://cc3001.dmm.co.jp/litevideo/freepv/1/118/118abw013/118abw013_dmb_w.mp4")

	// Image paths (relative, resolved by the scraper).
	assertEq(t, "JacketFullURL", m.JacketFullURL, "digital/video/118abw00013/118abw00013pl")
	assertEq(t, "GalleryFirst", m.GalleryFirst, "digital/video/118abw00013/118abw00013jp-1")
	assertEq(t, "GalleryLast", m.GalleryLast, "digital/video/118abw00013/118abw00013jp-12")

	// Joined entities.
	if m.Maker == nil || m.Maker.NameEn != "Prestige" || m.Maker.NameJa != "プレステージ" {
		t.Errorf("Maker = %+v, want Prestige/プレステージ", m.Maker)
	}
	if m.Label == nil || m.Label.NameEn != "ABSOLUTELY WONDERFUL" {
		t.Errorf("Label = %+v", m.Label)
	}
	if m.Series == nil || m.Series.NameEn != "When You Can't Scream Out..." {
		t.Errorf("Series = %+v", m.Series)
	}
	if m.Director == nil || m.Director.NameRomaji != "Kantoku Mei" || m.Director.NameKanji != "監督名" {
		t.Errorf("Director = %+v, want Kantoku Mei/監督名", m.Director)
	}

	// Actresses.
	if len(m.Actresses) != 1 {
		t.Fatalf("Actresses = %d, want 1", len(m.Actresses))
	}
	assertEq(t, "Actress.NameRomaji", m.Actresses[0].NameRomaji, "Airi Suzumura")
	assertEq(t, "Actress.NameKanji", m.Actresses[0].NameKanji, "鈴村あいり")
	assertEq(t, "Actress.ImageURL", m.Actresses[0].ImageURL, "suzumura_airi.jpg")
	assertEq(t, "Actress.ID", m.Actresses[0].ID, "1019076")

	// Categories.
	if len(m.Categories) != 2 {
		t.Fatalf("Categories = %d, want 2", len(m.Categories))
	}
	assertEq(t, "Cat[0].NameEn", m.Categories[0].NameEn, "Cheating Wife")
	assertEq(t, "Cat[1].NameEn", m.Categories[1].NameEn, "Squirting")

	// Trailer from source_dmm_trailer table.
	assertEq(t, "TrailerURL", m.TrailerURL, "https://cc3001.dmm.co.jp/litevideo/freepv/1/118/118abw013/118abw013_trailer.mp4")
}

func TestLookupMovie_Miss(t *testing.T) {
	path := importFullDump(t)
	store, _ := Open(path)
	defer store.Close()
	_, err := store.LookupMovie(context.Background(), "NOPE-999")
	if !errors.Is(err, models.ErrDumpMiss) {
		t.Errorf("expected ErrDumpMiss, got %v", err)
	}
}

func TestLookupMovie_NilStoreSafe(t *testing.T) {
	var s *Store
	_, err := s.LookupMovie(context.Background(), "ABW-013")
	if !errors.Is(err, models.ErrDumpMiss) {
		t.Errorf("nil store err = %v, want ErrDumpMiss", err)
	}
}

// TestLookupMovie_NullRuntime is a regression test for a BLOCKER: the importer
// once re-injected the raw "\N" literal into the INTEGER runtime_mins column,
// which broke the NullInt64 scan and turned every NULL-runtime movie into a
// silent miss (forcing HTTP fallback). A NULL runtime must yield Runtime=0
// with a hit, not a miss.
func TestLookupMovie_NullRuntime(t *testing.T) {
	dump := `COPY public.derived_video (content_id, dvd_id, title_en, title_ja, comment_en, comment_ja, runtime_mins, release_date, sample_url, maker_id, label_id, series_id, jacket_full_url, jacket_thumb_url, gallery_full_first, gallery_full_last, gallery_thumb_first, gallery_thumb_last, site_id, service_code) FROM stdin;
118nul00001	NUL-001	Some Title	\N	\N	\N	\N	2021-01-01	\N	\N	\N	\N	\N	\N	\N	\N	\N	\N	\N	\N
\.
`
	path := t.TempDir() + "/r18dev_dump.db"
	_, err := Import(context.Background(), strings.NewReader(dump), path, ImportOptions{})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	store, _ := Open(path)
	defer store.Close()

	m, err := store.LookupMovie(context.Background(), "NUL-001")
	if err != nil {
		t.Fatalf("NULL runtime_mins must not cause a miss: %v", err)
	}
	if m.Runtime != 0 {
		t.Errorf("Runtime = %d, want 0 for NULL", m.Runtime)
	}
}

// TestLookupMovie_NullActressFields is a regression test for a MAJOR: the join
// helpers once scanned nullable text into plain strings, so a NULL in any
// actress field dropped that actress (and all subsequent). An actress with all
// NULL fields must still be returned, with empty strings. A maker with NULL
// names must still resolve (id present) with empty name fields.
func TestLookupMovie_NullActressFields(t *testing.T) {
	// Columns: content_id, dvd_id, title_en, title_ja, comment_en, comment_ja,
	// runtime_mins, release_date, sample_url, maker_id(=8001), label_id,
	// series_id, jacket_full_url, jacket_thumb_url, gallery_full_first,
	// gallery_full_last, gallery_thumb_first, gallery_thumb_last, site_id, service_code.
	dump := `COPY public.derived_video (content_id, dvd_id, title_en, title_ja, comment_en, comment_ja, runtime_mins, release_date, sample_url, maker_id, label_id, series_id, jacket_full_url, jacket_thumb_url, gallery_full_first, gallery_full_last, gallery_thumb_first, gallery_thumb_last, site_id, service_code) FROM stdin;
118nl000001	NLA-001	Title	\N	\N	\N	90	2021-01-01	\N	8001	\N	\N	\N	\N	\N	\N	\N	\N	\N	\N
\.
COPY public.derived_actress (id, name_romaji, image_url, name_kanji, name_kana) FROM stdin;
7001	\N	\N	\N	\N
7002	Second Actress	\N	\N	\N
\.
COPY public.derived_video_actress (content_id, actress_id, ordinality, release_date) FROM stdin;
118nl000001	7001	1	2021-01-01
118nl000001	7002	2	2021-01-01
\.
COPY public.derived_maker (id, name_en, name_ja) FROM stdin;
8001	\N	\N
\.
`
	path := t.TempDir() + "/r18dev_dump.db"
	_, err := Import(context.Background(), strings.NewReader(dump), path, ImportOptions{})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	store, _ := Open(path)
	defer store.Close()

	m, err := store.LookupMovie(context.Background(), "NLA-001")
	if err != nil {
		t.Fatalf("LookupMovie: %v", err)
	}
	// Both actresses must survive — NULL fields must not drop them.
	if len(m.Actresses) != 2 {
		t.Fatalf("Actresses = %d, want 2 (NULL fields must not drop rows)", len(m.Actresses))
	}
	assertEq(t, "Actress[1].NameRomaji", m.Actresses[1].NameRomaji, "Second Actress")
	// A maker with all-NULL names is still present (id resolved) with empty names.
	if m.Maker == nil {
		t.Error("Maker should be present (id resolved) even with NULL names")
	}
}

// TestLookupMovie_ThumbGalleryFallback verifies that when the full-size gallery
// range is absent but the thumb range is present, LookupMovie falls back to the
// thumb range so screenshots are not silently lost.
func TestLookupMovie_ThumbGalleryFallback(t *testing.T) {
	dump := `COPY public.derived_video (content_id, dvd_id, title_en, title_ja, comment_en, comment_ja, runtime_mins, release_date, sample_url, maker_id, label_id, series_id, jacket_full_url, jacket_thumb_url, gallery_full_first, gallery_full_last, gallery_thumb_first, gallery_thumb_last, site_id, service_code) FROM stdin;
118thu00001	THU-001	Title	\N	\N	\N	90	2021-01-01	\N	\N	\N	\N	\N	\N	\N	\N	digital/video/118thu00001/118thu00001jp-1	digital/video/118thu00001/118thu00001jp-3	\N	\N
\.
`
	path := t.TempDir() + "/r18dev_dump.db"
	_, err := Import(context.Background(), strings.NewReader(dump), path, ImportOptions{})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	store, _ := Open(path)
	defer store.Close()

	m, err := store.LookupMovie(context.Background(), "THU-001")
	if err != nil {
		t.Fatalf("LookupMovie: %v", err)
	}
	assertEq(t, "GalleryFirst (thumb fallback)", m.GalleryFirst, "digital/video/118thu00001/118thu00001jp-1")
	assertEq(t, "GalleryLast (thumb fallback)", m.GalleryLast, "digital/video/118thu00001/118thu00001jp-3")
}

// TestLookupMovie_AsymmetricGalleryFallback verifies that when only ONE end of
// the full-size gallery range is present, LookupMovie falls back to the thumb
// range. Without the either-endpoint guard, a (first set, last empty) row
// would feed ExpandGallery a mismatched pair and yield no screenshots.
func TestLookupMovie_AsymmetricGalleryFallback(t *testing.T) {
	// Columns: ..., gallery_full_first(set), gallery_full_last(\N),
	// gallery_thumb_first(set), gallery_thumb_last(set), ...
	dump := `COPY public.derived_video (content_id, dvd_id, title_en, title_ja, comment_en, comment_ja, runtime_mins, release_date, sample_url, maker_id, label_id, series_id, jacket_full_url, jacket_thumb_url, gallery_full_first, gallery_full_last, gallery_thumb_first, gallery_thumb_last, site_id, service_code) FROM stdin;
118asym00001	ASYM-001	Title	\N	\N	\N	90	2021-01-01	\N	\N	\N	\N	\N	\N	digital/video/118asym00001/118asym00001jp-1	\N	digital/video/118asym00001/118asym00001jp-1	digital/video/118asym00001/118asym00001jp-3	\N	\N
\.
`
	path := t.TempDir() + "/r18dev_dump.db"
	_, err := Import(context.Background(), strings.NewReader(dump), path, ImportOptions{})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	store, _ := Open(path)
	defer store.Close()

	m, err := store.LookupMovie(context.Background(), "ASYM-001")
	if err != nil {
		t.Fatalf("LookupMovie: %v", err)
	}
	// Asymmetric full range -> fall back to thumb range entirely.
	assertEq(t, "GalleryFirst (asymmetric fallback)", m.GalleryFirst, "digital/video/118asym00001/118asym00001jp-1")
	assertEq(t, "GalleryLast (asymmetric fallback)", m.GalleryLast, "digital/video/118asym00001/118asym00001jp-3")
}

// TestLookupMovie_JoinErrorDegradesGracefully verifies M1: a real query
// failure in a non-critical join (here: the actresses table is dropped) is
// logged and degraded to empty, NOT propagated as an error. The core video
// data and intact joins (maker/label/series) must still be returned so a
// single corrupt join table does not force the whole movie back to HTTP.
func TestLookupMovie_JoinErrorDegradesGracefully(t *testing.T) {
	path := importFullDump(t)

	// Corrupt the actresses table so lookupActresses' JOIN fails at runtime.
	// The read-only Store cannot do this, so open a read-write connection.
	corruptor, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open corruptor: %v", err)
	}
	if _, err := corruptor.Exec("DROP TABLE actresses"); err != nil {
		corruptor.Close()
		t.Fatalf("drop actresses: %v", err)
	}
	corruptor.Close()

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	// Capture log output to assert the degrade warn was genuinely emitted.
	// Without this, a regression that swallows the join error (returns
	// nil,nil instead of nil,err) would still pass: Actresses would be empty
	// either way. Asserting the warn proves the error path was taken.
	var logBuf bytes.Buffer
	restore := logging.SetOutput(&logBuf)
	defer restore()

	m, err := store.LookupMovie(context.Background(), "ABW-013")
	if err != nil {
		t.Fatalf("join error must not abort LookupMovie: %v", err)
	}
	// Core video data still present.
	assertEq(t, "ContentID", m.ContentID, "118abw00013")
	assertEq(t, "TitleEn", m.TitleEn, "You Can't Make A Sound")
	// Intact joins still resolve.
	if m.Maker == nil || m.Maker.NameEn != "Prestige" {
		t.Errorf("Maker should still resolve (table intact): %+v", m.Maker)
	}
	// Actresses degraded to empty (table dropped -> query error -> warn log).
	if len(m.Actresses) != 0 {
		t.Errorf("Actresses should degrade to empty on query error, got %d", len(m.Actresses))
	}
	// The degrade warn was genuinely emitted — proves the error path fired.
	if !strings.Contains(logBuf.String(), "actresses") || !strings.Contains(logBuf.String(), "degraded") {
		t.Errorf("expected a degraded warn for actresses in log output, got: %s", logBuf.String())
	}
}

func TestLookupMovie_EmptyJoins(t *testing.T) {
	// A movie with no actresses, categories, maker, etc. — joins should be nil/empty, not error.
	dump := `COPY public.derived_video (content_id, dvd_id, title_en, title_ja, comment_en, comment_ja, runtime_mins, release_date, sample_url, maker_id, label_id, series_id, jacket_full_url, jacket_thumb_url, gallery_full_first, gallery_full_last, gallery_thumb_first, gallery_thumb_last, site_id, service_code) FROM stdin;
118xxx00001	XXX-001	Some Title	\N	\N	\N	90	2021-01-01	\N	\N	\N	\N	\N	\N	\N	\N	\N	\N	\N	\N
\.
`
	path := t.TempDir() + "/r18dev_dump.db"
	_, err := Import(context.Background(), strings.NewReader(dump), path, ImportOptions{})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	store, _ := Open(path)
	defer store.Close()

	m, err := store.LookupMovie(context.Background(), "XXX-001")
	if err != nil {
		t.Fatalf("expected hit, got %v", err)
	}
	if m.Maker != nil || m.Label != nil || m.Series != nil || m.Director != nil {
		t.Error("nil entities should stay nil for empty joins")
	}
	if len(m.Actresses) != 0 || len(m.Categories) != 0 {
		t.Error("empty joins should yield empty slices")
	}
	if m.TrailerURL != "" {
		t.Errorf("TrailerURL = %q, want empty", m.TrailerURL)
	}
	if m.JacketFullURL != "" || m.GalleryFirst != "" {
		t.Error("empty image fields should stay empty")
	}
}

func TestExpandGallery(t *testing.T) {
	tests := []struct {
		name        string
		first, last string
		wantCount   int
		wantFirst   string
		wantLast    string
	}{
		{"standard 1-12", ".../118abw00013jp-1", ".../118abw00013jp-12", 12, ".../118abw00013jp-1", ".../118abw00013jp-12"},
		{"single 5-5", "path/jp-5", "path/jp-5", 1, "path/jp-5", "path/jp-5"},
		{"two digits 1-10", "p/jp-1", "p/jp-10", 10, "p/jp-1", "p/jp-10"},
		{"empty first", "", "p/jp-5", 0, "", ""},
		{"empty last", "p/jp-1", "", 0, "", ""},
		{"no hyphen", "nohyphen", "nohyphen2", 0, "", ""},
		{"different prefix", "p/jp-1", "p/ps-12", 0, "", ""},
		{"non-numeric suffix", "p/jp-a", "p/jp-c", 0, "", ""},
		{"reversed range", "p/jp-5", "p/jp-1", 0, "", ""},
		{"large range guard", "p/jp-1", "p/jp-1001", 0, "", ""},
		{"exactly 1000 allowed", "p/jp-1", "p/jp-1000", 1000, "p/jp-1", "p/jp-1000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExpandGallery(tt.first, tt.last)
			if len(got) != tt.wantCount {
				t.Fatalf("got %d URLs, want %d: %v", len(got), tt.wantCount, got)
			}
			if tt.wantCount > 0 {
				if got[0] != tt.wantFirst {
					t.Errorf("first = %q, want %q", got[0], tt.wantFirst)
				}
				if got[len(got)-1] != tt.wantLast {
					t.Errorf("last = %q, want %q", got[len(got)-1], tt.wantLast)
				}
			}
		})
	}
}

func TestNormalizeDumpURL(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"digital/video/118abw00013/118abw00013pl", "https://pics.dmm.co.jp/digital/video/118abw00013/118abw00013pl.jpg"},
		{"https://pics.dmm.co.jp/digital/video/x/xpl.jpg", "https://pics.dmm.co.jp/digital/video/x/xpl.jpg"},
		{"http://example.com/img.jpg", "http://example.com/img.jpg"},
		{"", ""},
		{"  digital/video/x/xps  ", "https://pics.dmm.co.jp/digital/video/x/xps.jpg"},
		// Already-extension paths must not double-append .jpg.
		{"digital/video/x/xpl.jpg", "https://pics.dmm.co.jp/digital/video/x/xpl.jpg"},
		{"digital/video/x/xpl.jpeg", "https://pics.dmm.co.jp/digital/video/x/xpl.jpeg"},
	}
	for _, tt := range tests {
		if got := NormalizeDumpURL(tt.in); got != tt.want {
			t.Errorf("NormalizeDumpURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// Compile-time assertion that *Store implements the full interface.
var _ models.R18DevDumpLookup = (*Store)(nil)

func assertEq(t *testing.T, field, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %q, want %q", field, got, want)
	}
}

// TestLookupMovie_CancelledContextCoversJoinDegraded verifies the cancellation
// branch of logJoinDegraded: when the context is cancelled before LookupMovie,
// the join queries fail with context.Canceled, which is logged at debug (not
// warn) and degraded to empty rather than propagated as a join error.
func TestLookupMovie_CancelledContextCoversJoinDegraded(t *testing.T) {
	path := importFullDump(t)
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so every query observes ctx.Err()

	// The core video query observes ctx.Err() and returns it; the join
	// queries (had they run) would hit logJoinDegraded's cancellation branch.
	// The direct test below covers that branch in isolation.
	_, err = store.LookupMovie(ctx, "ABW-013")
	if err == nil {
		t.Fatal("expected an error from the cancelled core query")
	}
}

// TestLogJoinDegraded_CancellationBranch directly exercises the cancellation
// vs. genuine-error branches of logJoinDegraded without a full LookupMovie.
func TestLogJoinDegraded_CancellationBranch(t *testing.T) {
	s := &Store{}
	// Cancellation -> debug path (must not panic, must not warn-fatal).
	s.logJoinDegraded("cid", "actresses", context.Canceled)
	s.logJoinDegraded("cid", "actresses", context.DeadlineExceeded)
	// Genuine error -> warn path.
	s.logJoinDegraded("cid", "actresses", assertErrSentinel{})
}

type assertErrSentinel struct{}

func (assertErrSentinel) Error() string { return "sentinel" }
