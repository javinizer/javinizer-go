package r18devdump

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

type legacyQueryRecorder struct {
	mu      sync.Mutex
	queries []string
}

func (r *legacyQueryRecorder) add(q string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queries = append(r.queries, q)
}
func (r *legacyQueryRecorder) take() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	q := r.queries
	r.queries = nil
	return q
}

type legacyRecordingDriver struct{ recorder *legacyQueryRecorder }
type legacyRecordingConn struct {
	*sqlite3.SQLiteConn
	recorder *legacyQueryRecorder
}

func (d *legacyRecordingDriver) Open(name string) (driver.Conn, error) {
	c, err := (&sqlite3.SQLiteDriver{}).Open(name)
	if err != nil {
		return nil, err
	}
	return &legacyRecordingConn{c.(*sqlite3.SQLiteConn), d.recorder}, nil
}
func (c *legacyRecordingConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	c.recorder.add(q)
	return c.SQLiteConn.QueryContext(ctx, q, args)
}

var legacyDriverSequence atomic.Uint64

func recordedLegacyStore(t *testing.T, rows string) (*Store, *legacyQueryRecorder, string) {
	t.Helper()
	path := seedDump(t, rows)
	// Recreate the exact old content-only key shape; the other columns remain
	// identical. Neither legacy index is new or mutated by a read-only lookup.
	ddl := strings.Replace(tableSchema[derivedVideoTable].create, "CREATE TABLE videos (", "CREATE TABLE videos_old (", 1)
	ddl = strings.Replace(ddl, "PRIMARY KEY (content_id, service_code)", "PRIMARY KEY (content_id)", 1)
	alterFixture(t, path, ddl, "INSERT INTO videos_old SELECT * FROM videos", "DROP TABLE videos", "ALTER TABLE videos_old RENAME TO videos", "CREATE INDEX idx_videos_dvd_id_norm ON videos(dvd_id_norm)")
	recorder := &legacyQueryRecorder{}
	name := fmt.Sprintf("legacy-recording-%d", legacyDriverSequence.Add(1))
	sql.Register(name, &legacyRecordingDriver{recorder})
	db, err := sql.Open(name, "file:"+path+"?mode=ro")
	require.NoError(t, err)
	store := &Store{db: db, path: path}
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	return store, recorder, path
}

func TestLegacyContentIDLookup_IndexedHitsAndMisses(t *testing.T) {
	store, recorder, path := recordedLegacyStore(t, "abc00001\tABC-001\nabc00002\tABC-002\nAbC00003\t\\N")
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	for _, tc := range []struct{ query, want string }{{"ABC-001", "abc00001"}, {"ABC00003", "AbC00003"}, {"NOTHERE-999", ""}} {
		t.Run(tc.query, func(t *testing.T) {
			recorder.take()
			matches, err := store.MatchByDisplayID(context.Background(), tc.query)
			if tc.want == "" {
				require.ErrorIs(t, err, models.ErrDumpMiss)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.want, matches[0].ContentID)
			}
			queries := recorder.take()
			for _, q := range queries {
				require.NotContains(t, q, "COLLATE NOCASE IN", "ordinary legacy lookups must not scan videos")
			}
			if tc.query == "ABC-001" {
				for _, q := range queries {
					require.NotContains(t, q, ">= ?", "DVD hit should precede folded alias work")
				}
			}
		})
	}
	rows, err := store.db.Query("EXPLAIN QUERY PLAN "+contentIDAliasSeekSQL, "ABC00001", "abc00001")
	require.NoError(t, err)
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		require.NoError(t, rows.Scan(&id, &parent, &unused, &detail))
		plan = append(plan, detail)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	require.Contains(t, strings.Join(plan, "\n"), "SEARCH videos USING COVERING INDEX sqlite_autoindex_videos_1")
	require.NotContains(t, strings.Join(plan, "\n"), "SCAN")
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after, "read-only lookup must not migrate the sidecar")
}

func TestLegacyContentIDLookup_IdentityAndServiceParity(t *testing.T) {
	store, _, _ := recordedLegacyStore(t, "AbC00001\tFIRST-001\nabc00001\tSECOND-002\nÄbC00002\tTHIRD-003\näbc00002\tFOURTH-004")
	for _, tc := range []struct{ id, want string }{{"AbC00001", "FIRST-001"}, {"abc00001", "SECOND-002"}, {"ABC00001", ""}, {"ÄBC00002", "THIRD-003"}, {"äBC00002", "FOURTH-004"}} {
		got, err := store.LookupByContentID(context.Background(), tc.id)
		if tc.want == "" {
			require.ErrorIs(t, err, models.ErrDumpMiss)
		} else {
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		}
	}
	// Composite sidecars can also omit the additive folded index. Ambiguity
	// must count all services, not merely the first raw identity found by a seek.
	path := seedDump(t, "AbC00001\tFIRST-001")
	alterFixture(t, path, "DROP INDEX idx_videos_content_id_fold", "INSERT INTO videos(content_id,dvd_id,dvd_id_norm,service_code) VALUES('AbC00001','OTHER-002','OTHER002','rental')")
	variants, err := Open(path)
	require.NoError(t, err)
	defer variants.Close()
	_, err = variants.LookupByContentID(context.Background(), "ABC00001")
	require.ErrorIs(t, err, models.ErrDumpMiss)
}

func TestLegacyContentIDLookup_SeekBudgetFallsBackWithoutPartialIdentity(t *testing.T) {
	// Every casing exists at the wrong numeric suffix. Enumerating all possible
	// casings would explode with input length even though there are no aliases.
	var fixture strings.Builder
	for i := 0; i < 256; i++ {
		b := []byte("abcdefgh00002")
		for j := 0; j < 8; j++ {
			if i&(1<<j) != 0 {
				b[j] -= 32
			}
		}
		fmt.Fprintf(&fixture, "%s\t\\N\n", b)
	}
	store, recorder, _ := recordedLegacyStore(t, strings.TrimSuffix(fixture.String(), "\n"))
	got, err := store.queryContentIDs(context.Background(), []string{"abcdefgh00001", "ABCDefgh00002"}, true)
	require.NoError(t, err)
	require.Len(t, got, 256, "fallback must return all rows, not a partially collected case bucket")
	seeks, scans := 0, 0
	for _, q := range recorder.take() {
		if q == contentIDAliasSeekSQL {
			seeks++
		}
		if strings.Contains(q, "COLLATE NOCASE IN") {
			scans++
		}
	}
	require.Equal(t, contentIDAliasSeeksPerID+extraContentIDAliasSeeks, seeks)
	require.Equal(t, 1, scans)
}

func TestNextContentIDAlias_BruteForceOracle(t *testing.T) {
	r := rand.New(rand.NewSource(715))
	alphabet := []byte{'a', 'B', 'c', '0', '_', 0x80, 0xff}
	for n := 0; n < 300; n++ {
		raw := make([]byte, r.Intn(8))
		for i := range raw {
			raw[i] = alphabet[r.Intn(len(alphabet))]
		}
		high := foldContentID(string(raw))
		low := []byte(high)
		for i, c := range low {
			if c >= 'a' && c <= 'z' {
				low[i] -= 32
			}
		}
		variants := []string{string(low)}
		for i := range low {
			if low[i] != high[i] {
				old := append([]string(nil), variants...)
				for _, v := range old {
					b := []byte(v)
					b[i] = high[i]
					variants = append(variants, string(b))
				}
			}
		}
		sort.Strings(variants)
		bounds := append([]string{"", string(raw), string(raw) + "!"}, variants...)
		for i := 0; i < 8; i++ {
			b := make([]byte, r.Intn(10))
			for j := range b {
				b[j] = byte(r.Intn(256))
			}
			bounds = append(bounds, string(b))
		}
		for _, after := range bounds {
			idx := sort.Search(len(variants), func(i int) bool { return variants[i] > after })
			got, ok := nextContentIDAlias(string(low), high, after)
			require.Equal(t, idx < len(variants), ok, "raw=%q after=%q", raw, after)
			if ok {
				require.Equal(t, variants[idx], got, "raw=%q after=%q", raw, after)
			}
		}
	}
	require.Equal(t, "abc\xff", foldContentID("AbC\xff"), "invalid UTF-8 is not rewritten by SQLite NOCASE")
}

func TestLegacyContentIDLookup_IndexCapability(t *testing.T) {
	for _, tc := range []struct {
		name, ddl string
		want      bool
	}{
		{"none", "", false},
		{"named_but_binary", "CREATE INDEX idx_videos_content_id_fold ON videos(content_id)", false},
		{"partial", "CREATE INDEX idx_videos_content_id_fold ON videos(content_id COLLATE NOCASE) WHERE dvd_id IS NOT NULL", false},
		{"expression", "CREATE INDEX idx_videos_content_id_fold ON videos(lower(content_id))", false},
		{"wrong_leading_key", "CREATE INDEX alternative ON videos(dvd_id,content_id COLLATE NOCASE)", false},
		{"rtrim", "CREATE INDEX alternative ON videos(content_id COLLATE RTRIM)", false},
		{"different_name", "CREATE INDEX alternative ON videos(content_id COLLATE NOCASE)", true},
		{"composite_descending", "CREATE INDEX alternative ON videos(content_id COLLATE nocase DESC,service_code)", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, recorder, path := recordedLegacyStore(t, "AbC00001\tFIRST-001")
			if tc.ddl != "" {
				alterFixture(t, path, tc.ddl)
			}
			got, err := store.hasContentFoldIndex(context.Background())
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			recorder.take()
			again, err := store.hasContentFoldIndex(context.Background())
			require.NoError(t, err)
			require.Equal(t, got, again)
			require.Empty(t, recorder.take(), "successful capability check is cached")
			dvd, err := store.LookupByContentID(context.Background(), "ABC00001")
			require.NoError(t, err)
			require.Equal(t, "FIRST-001", dvd)
		})
	}
}

func TestLegacyContentIDLookup_CanceledProbeDoesNotPoisonCache(t *testing.T) {
	store, _, _ := recordedLegacyStore(t, "AbC00001\tFIRST-001")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := store.hasContentFoldIndex(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, store.contentIndexKnown)
	indexed, err := store.hasContentFoldIndex(context.Background())
	require.NoError(t, err)
	require.False(t, indexed)
	require.True(t, store.contentIndexKnown)
	_, err = store.queryContentIDAliases(ctx, []string{"ABC00001"})
	require.ErrorIs(t, err, context.Canceled)
	dvd, err := store.LookupByContentID(context.Background(), "ABC00001")
	require.NoError(t, err)
	require.Equal(t, "FIRST-001", dvd)
}

func TestLegacyContentIDLookup_EmbeddedNULUsesSQLiteSemantics(t *testing.T) {
	// Imported/validated IDs reject controls; malformed/copied stores and caller
	// input still must not change SQLite's historical NOCASE semantics.
	store, recorder, path := recordedLegacyStore(t, "valid00001\tFIRST-001")
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO videos(content_id,dvd_id) VALUES(?,?)", "AbC\x00x", "SECOND-002")
	require.NoError(t, err)
	require.NoError(t, db.Close())
	rows, err := store.queryContentIDs(context.Background(), []string{"abc\x00z"}, true)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "AbC\x00x", rows[0].contentID)
	for _, q := range recorder.take() {
		require.NotEqual(t, contentIDAliasSeekSQL, q)
	}
}

func TestLegacyContentIDLookup_CanceledCallerDoesNotWaitForOtherProbe(t *testing.T) {
	store, _, _ := recordedLegacyStore(t, "AbC00001\tFIRST-001")
	store.db.SetMaxOpenConns(1)
	held, err := store.db.Conn(context.Background())
	require.NoError(t, err)
	defer held.Close()
	first := make(chan error, 1)
	go func() { _, err := store.hasContentFoldIndex(context.Background()); first <- err }()
	require.Eventually(t, func() bool { return store.db.Stats().WaitCount > 0 }, time.Second, time.Millisecond, "first probe must be waiting for the held connection")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	second := make(chan error, 1)
	go func() { _, err := store.hasContentFoldIndex(ctx); second <- err }()
	select {
	case err := <-second:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Error("canceled probe waited for an unrelated database operation")
	}
	require.NoError(t, held.Close())
	require.NoError(t, <-first)
}

type legacyIndexFaultDriver struct{ mode string }
type legacyIndexFaultConn struct{ mode string }
type legacyIndexFaultRows struct {
	mode    string
	emitted bool
}

func (d legacyIndexFaultDriver) Open(string) (driver.Conn, error) {
	return &legacyIndexFaultConn{d.mode}, nil
}
func (*legacyIndexFaultConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unused prepare")
}
func (*legacyIndexFaultConn) Close() error              { return nil }
func (*legacyIndexFaultConn) Begin() (driver.Tx, error) { return nil, errors.New("unused transaction") }
func (c *legacyIndexFaultConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &legacyIndexFaultRows{mode: c.mode}, nil
}
func (*legacyIndexFaultRows) Columns() []string { return []string{"found"} }
func (*legacyIndexFaultRows) Close() error      { return nil }
func (r *legacyIndexFaultRows) Next(dest []driver.Value) error {
	if !r.emitted {
		r.emitted = true
		if r.mode == "scan" {
			dest[0] = "not an integer"
		} else {
			dest[0] = int64(1)
		}
		return nil
	}
	if r.mode == "iterate" {
		return errors.New("index iteration failed")
	}
	return io.EOF
}
func TestLegacyContentIDLookup_ProbeScanAndIterationFailureNotCached(t *testing.T) {
	for _, mode := range []string{"scan", "iterate"} {
		t.Run(mode, func(t *testing.T) {
			name := fmt.Sprintf("legacy-index-fault-%d", legacyDriverSequence.Add(1))
			sql.Register(name, legacyIndexFaultDriver{mode})
			db, err := sql.Open(name, "")
			require.NoError(t, err)
			defer db.Close()
			store := &Store{db: db}
			_, err = store.hasContentFoldIndex(context.Background())
			require.ErrorContains(t, err, "index probe failed")
			require.False(t, store.contentIndexKnown)
		})
	}
}

func TestLegacyContentIDLookup_WidestCandidateMissDoesNotScan(t *testing.T) {
	var fixture strings.Builder
	// Real widest known display expansion. Put surrounding catalog rows in
	// every prefix so seeks must skip existing ranges, not just an empty DB.
	for _, prefix := range ContentIDPrefixLookup["avop"] {
		fmt.Fprintf(&fixture, "%savop00001\tAVOP-001\n%savop00999\tAVOP-999\n", prefix, prefix)
	}
	store, recorder, _ := recordedLegacyStore(t, strings.TrimSuffix(fixture.String(), "\n"))
	matches, err := store.MatchByDisplayID(context.Background(), "AVOP-555")
	require.ErrorIs(t, err, models.ErrDumpMiss)
	require.Empty(t, matches)
	for _, q := range recorder.take() {
		require.NotContains(t, q, "COLLATE NOCASE IN", "wide supported display misses must not trigger a scan")
	}
}

func TestLegacyContentIDLookup_DuplicateCandidatesShareBudget(t *testing.T) {
	store, recorder, _ := recordedLegacyStore(t, "AbC00001\tFIRST-001")
	ids := make([]string, 200)
	for i := range ids {
		ids[i] = "ABC00001"
		if i%2 == 0 {
			ids[i] = "abc00001"
		}
	}
	matches, err := store.queryContentIDs(context.Background(), ids, true)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.Equal(t, "AbC00001", matches[0].contentID)
	seeks := 0
	for _, q := range recorder.take() {
		if q == contentIDAliasSeekSQL {
			seeks++
		}
		require.NotContains(t, q, "COLLATE NOCASE IN")
	}
	require.LessOrEqual(t, seeks, contentIDAliasSeeksPerID, "duplicate aliases must not restart their lookup")
}
