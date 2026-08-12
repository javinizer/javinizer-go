package r18devdump

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedDumpFullCols(t *testing.T, rows string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "r18dev_dump.db")
	dump := "COPY public.derived_video (content_id, dvd_id, release_date, service_code) FROM stdin;\n" + rows + "\n\\.\n"
	_, err := Import(context.Background(), strings.NewReader(dump), path, ImportOptions{SourceDate: "2026-04-28"})
	require.NoError(t, err)
	return path
}

const matchFixture = "118ipx00535\tIPX-535\t2019-01-01\tdigital\n" +
	"lulu00441\t\\N\t2026-07-03\tdigital\n" +
	"lulu441\t\\N\t2026-07-07\tmono"

func TestMatchByDisplayID_NormHitSingleMatch(t *testing.T) {
	store, err := Open(seedDumpFullCols(t, matchFixture))
	require.NoError(t, err)
	defer store.Close()

	matches, err := store.MatchByDisplayID(context.Background(), "IPX-535")
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, "118ipx00535", matches[0].ContentID)
	assert.Equal(t, "IPX-535", matches[0].DVDID)
	assert.Equal(t, "2019-01-01", matches[0].ReleaseDate)
	assert.Equal(t, "digital", matches[0].ServiceCode)

	// Normalization input styles all land on the same single match.
	for _, q := range []string{"ipx-535", "IPX535", " ipx 535 "} {
		m, err := store.MatchByDisplayID(context.Background(), q)
		require.NoError(t, err)
		require.Len(t, m, 1)
		assert.Equal(t, "118ipx00535", m[0].ContentID)
	}
}

func TestMatchByDisplayID_CandidateExpansionOrderedMultiMatch(t *testing.T) {
	store, err := Open(seedDumpFullCols(t, matchFixture))
	require.NoError(t, err)
	defer store.Close()

	matches, err := store.MatchByDisplayID(context.Background(), "LULU-441")
	require.NoError(t, err)
	require.Len(t, matches, 2)
	assert.Equal(t, "lulu00441", matches[0].ContentID, "canonical zero-padded candidate first")
	assert.Equal(t, "lulu441", matches[1].ContentID)
	assert.Equal(t, "digital", matches[0].ServiceCode)
	assert.Equal(t, "mono", matches[1].ServiceCode)
	assert.Equal(t, "", matches[0].DVDID)
}

func TestMatchByDisplayID_DirectContentIDInputExpandsToo(t *testing.T) {
	store, err := Open(seedDumpFullCols(t, matchFixture))
	require.NoError(t, err)
	defer store.Close()

	matches, err := store.MatchByDisplayID(context.Background(), "lulu00441")
	require.NoError(t, err)
	require.Len(t, matches, 2)
	assert.Equal(t, "lulu00441", matches[0].ContentID)
}

func TestMatchByDisplayID_ExactContentIDRowLeadsCandidates(t *testing.T) {
	store, err := Open(seedDumpFullCols(t, matchFixture))
	require.NoError(t, err)
	defer store.Close()

	// A literal content_id query surfaces its own row first, even when a
	// canonical zero-padded sibling exists in the same candidate set.
	matches, err := store.MatchByDisplayID(context.Background(), "lulu441")
	require.NoError(t, err)
	require.Len(t, matches, 2)
	assert.Equal(t, "lulu441", matches[0].ContentID, "exact content_id row first")
	assert.Equal(t, "lulu00441", matches[1].ContentID, "canonical variant follows")

	// Dash-containing display queries keep canonical-first ordering.
	matches, err = store.MatchByDisplayID(context.Background(), "LULU-441")
	require.NoError(t, err)
	require.Equal(t, "lulu00441", matches[0].ContentID)
}

// --- fault-injection coverage for defensive error branches ---

// faultyRows is a driver.Rows whose behavior is controlled by mode sentinels:
// empty (no rows), oneRow (one valid row), badcols (columns mismatch Scan),
// and failingErr (rows.Err() non-nil after iteration).
type faultyRows struct {
	mode  string
	dirty bool
}

func (r *faultyRows) Columns() []string {
	if r.mode == "badcols" {
		return []string{"content_id", "dvd_id"}
	}
	return []string{"content_id", "dvd_id", "release_date", "service_code"}
}

func (r *faultyRows) Close() error { return nil }

func (r *faultyRows) Next(dest []driver.Value) error {
	if (r.mode == "oneRow" || r.mode == "badcols" || r.mode == "failingErr") && !r.dirty {
		r.dirty = true
		dest[0] = "118ipx00535"
		dest[1] = "IPX-535"
		if r.mode != "badcols" {
			dest[2] = "2013-03-01"
			dest[3] = "digital"
		}
		return nil
	}
	if r.mode == "failingErr" {
		return errors.New("simulated iteration failure")
	}
	return io.EOF
}

func (r *faultyRows) Err() error { return nil }

type faultyConn struct{ mode string }

func (c *faultyConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("no prepare") }
func (c *faultyConn) Close() error                        { return nil }
func (c *faultyConn) Begin() (driver.Tx, error)           { return nil, errors.New("no tx") }
func (c *faultyConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(query, "IN (") {
		return &faultyRows{mode: c.mode}, nil
	}
	return &faultyRows{mode: "empty"}, nil
}

type faultyDriver struct{ mode string }

func (d *faultyDriver) Open(string) (driver.Conn, error) { return &faultyConn{mode: d.mode}, nil }

func newFaultyStore(t *testing.T, mode string) *Store {
	t.Helper()
	name := "faulty-" + mode + "-" + strings.ReplaceAll(t.Name(), "/", "_")
	sql.Register(name, &faultyDriver{mode: mode})
	db, err := sql.Open(name, "")
	require.NoError(t, err)
	return &Store{db: db, path: ":faulty:"}
}

func TestMatchByDisplayID_CandidateScanError(t *testing.T) {
	s := newFaultyStore(t, "badcols")
	defer func() { _ = s.Close() }()
	_, err := s.MatchByDisplayID(context.Background(), "IPX-535")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scan")
}

func TestMatchByDisplayID_CandidateRowsErr(t *testing.T) {
	s := newFaultyStore(t, "failingErr")
	defer func() { _ = s.Close() }()
	_, err := s.MatchByDisplayID(context.Background(), "IPX-535")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "candidate lookup failed")
}

func TestMatchByDisplayID_NormQueryErrorPropagated(t *testing.T) {
	path := seedDump(t, "118ipx00535\tIPX-535")
	corruptor, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	_, err = corruptor.Exec("DROP TABLE videos; CREATE TABLE videos (content_id TEXT PRIMARY KEY, dvd_id_norm TEXT)")
	require.NoError(t, err)
	require.NoError(t, corruptor.Close())

	store, err := Open(path)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	_, err = store.MatchByDisplayID(context.Background(), "IPX-535")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dvd_id lookup failed")
}

func TestMatchByDisplayID_ClosedDBCandidateError(t *testing.T) {
	store, err := Open(seedDump(t, "118ipx00535\tIPX-535"))
	require.NoError(t, err)
	require.NoError(t, store.Close())
	// "-" normalizes to an empty norm (norm branch skipped) and yields no
	// generated candidates, so the probe set is just the exact-ID probe; the
	// closed DB must surface a real error rather than a bare miss.
	_, err = store.MatchByDisplayID(context.Background(), "-")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lookup failed")
}

func TestMatchByDisplayID_MissAndRejects(t *testing.T) {
	store, err := Open(seedDumpFullCols(t, matchFixture))
	require.NoError(t, err)
	defer store.Close()

	_, err = store.MatchByDisplayID(context.Background(), "NOPE-999")
	assert.True(t, errors.Is(err, models.ErrDumpMiss))
	assert.False(t, errors.Is(err, models.ErrDumpNoDVDID))

	_, err = store.MatchByDisplayID(context.Background(), "")
	assert.True(t, errors.Is(err, models.ErrDumpMiss))

	var nilStore *Store
	_, err = nilStore.MatchByDisplayID(context.Background(), "IPX-535")
	assert.True(t, errors.Is(err, models.ErrDumpMiss))
}
