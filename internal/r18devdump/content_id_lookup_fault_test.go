package r18devdump

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Each connection fails the selected query, allowing later-phase errors to
// prove they propagate instead of becoming misses or falling back to another ID.
type casingFaultDriver struct {
	failAt  int
	queries *int
}
type casingFaultConn struct{ casingFaultDriver }

func (d *casingFaultDriver) Open(string) (driver.Conn, error) { return &casingFaultConn{*d}, nil }
func (*casingFaultConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unused prepare")
}
func (*casingFaultConn) Close() error              { return nil }
func (*casingFaultConn) Begin() (driver.Tx, error) { return nil, errors.New("unused transaction") }
func (c *casingFaultConn) QueryContext(_ context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	*c.queries++
	if *c.queries == c.failAt {
		return nil, errors.New("synthetic casing query failure")
	}
	return &faultyRows{mode: "empty"}, nil
}
func casingFaultStore(t *testing.T, failAt int) *Store {
	t.Helper()
	queries := 0
	name := fmt.Sprintf("casing-%s-%d", strings.ReplaceAll(t.Name(), "/", "-"), failAt)
	sql.Register(name, &casingFaultDriver{failAt: failAt, queries: &queries})
	db, err := sql.Open(name, "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return &Store{db: db}
}
func TestContentIDLookupFaults(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		t.Run(fmt.Sprintf("direct/%d", failAt), func(t *testing.T) {
			_, err := casingFaultStore(t, failAt).LookupByContentID(context.Background(), "MiXeD")
			require.ErrorContains(t, err, "synthetic casing query failure")
		})
		t.Run(fmt.Sprintf("movie/%d", failAt), func(t *testing.T) {
			_, err := casingFaultStore(t, failAt).LookupMovie(context.Background(), "IPX-535")
			require.ErrorContains(t, err, "synthetic casing query failure")
		})
	}
	for _, tc := range []struct {
		name, id string
		failAt   int
	}{{"literal", "-", 1}, {"folded", "-", 2}, {"candidate", "-", 3}, {"dvd", "IPX-535", 3}} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := casingFaultStore(t, tc.failAt).MatchByDisplayID(context.Background(), tc.id)
			require.ErrorContains(t, err, "synthetic casing query failure")
		})
	}
	for _, mode := range []string{"badcols", "failingErr"} {
		t.Run(mode, func(t *testing.T) {
			s := newFaultyStore(t, mode)
			defer s.Close()
			_, err := s.queryContentIDs(context.Background(), []string{"Mixed"}, true)
			require.Error(t, err)
		})
	}
}
func TestContentIDLookupHelpers(t *testing.T) {
	assert.Equal(t, "abc-ß-İ-K-01", foldContentID("AbC-ß-İ-K-01"))
	assert.True(t, ContentIDsEqualFold("AbC001", "abc001"))
	assert.False(t, ContentIDsEqualFold("K001", "k001"), "alias equality is ASCII-only")
	var s *Store
	rows, err := s.queryContentIDs(context.Background(), nil, true)
	require.NoError(t, err)
	assert.Nil(t, rows)
	row, ok := selectContentID([]dumpDVDRow{{contentID: "AbC"}, {contentID: "abc"}}, "AbC", true)
	assert.True(t, ok)
	assert.Equal(t, "AbC", row.contentID)
	_, ok = selectContentID([]dumpDVDRow{{contentID: "AbC"}, {contentID: "abc"}}, "ABC", true)
	assert.False(t, ok)
}
