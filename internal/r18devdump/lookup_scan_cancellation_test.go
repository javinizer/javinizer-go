package r18devdump

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// Import through the normal schema, with rows deliberately inserted out of
// display order and nullable fields alongside populated fields.
const scanCancellationDump = `COPY public.derived_video (content_id, dvd_id) FROM stdin;
scan00001	SCAN-001
\.
COPY public.derived_actress (id, name_romaji, image_url, name_kanji, name_kana) FROM stdin;
300	Third	third.jpg	三	さん
100	\N	\N	\N	\N
200	First	first.jpg	一	いち
\.
COPY public.derived_video_actress (content_id, actress_id, ordinality) FROM stdin;
scan00001	300	\N
scan00001	100	2
scan00001	200	1
\.
COPY public.derived_category (id, name_en, name_ja) FROM stdin;
30	Third category	三
10	First category	一
20	\N	\N
\.
COPY public.derived_video_category (content_id, category_id) FROM stdin;
scan00001	30
scan00001	10
scan00001	20
\.
`

func TestLookupJoinScanCancellationDiscardsPartialsAndRecovers(t *testing.T) {
	path := t.TempDir() + "/dump.db"
	_, err := Import(t.Context(), strings.NewReader(scanCancellationDump), path, ImportOptions{})
	require.NoError(t, err)

	tests := []struct {
		name     string
		lookup   func(*Store, context.Context, string) (any, error)
		withScan func(*Store, context.Context, string, func(*sql.Rows, ...any) error) (any, error)
		want     any
	}{
		{
			name: "actress",
			lookup: func(s *Store, ctx context.Context, id string) (any, error) {
				return s.lookupActresses(ctx, id)
			},
			withScan: func(s *Store, ctx context.Context, id string, scan func(*sql.Rows, ...any) error) (any, error) {
				return s.lookupActressesWithScan(ctx, id, scan)
			},
			want: []models.DumpActress{
				{ID: "200", NameRomaji: "First", ImageURL: "first.jpg", NameKanji: "一", NameKana: "いち"},
				{ID: "100"},
				{ID: "300", NameRomaji: "Third", ImageURL: "third.jpg", NameKanji: "三", NameKana: "さん"},
			},
		},
		{
			name: "category",
			lookup: func(s *Store, ctx context.Context, id string) (any, error) {
				return s.lookupCategories(ctx, id)
			},
			withScan: func(s *Store, ctx context.Context, id string, scan func(*sql.Rows, ...any) error) (any, error) {
				return s.lookupCategoriesWithScan(ctx, id, scan)
			},
			want: []models.DumpNamedEntity{
				{NameEn: "First category", NameJa: "一"},
				{},
				{NameEn: "Third category", NameJa: "三"},
			},
		},
	}
	for _, tt := range tests {
		for _, cancelAt := range []int{1, 2} {
			t.Run(fmt.Sprintf("%s/row_%d", tt.name, cancelAt), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					store, err := OpenContext(t.Context(), path)
					require.NoError(t, err)
					t.Cleanup(func() { require.NoError(t, store.Close()) })
					store.db.SetMaxOpenConns(1)

					healthy, err := tt.lookup(store, t.Context(), "scan00001")
					require.NoError(t, err)
					require.Equal(t, tt.want, healthy, "preserve ordering, every field and SQL NULL values")

					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					calls, successfulScans := 0, 0
					var scanErr error
					got, err := tt.withScan(store, ctx, "scan00001", func(rows *sql.Rows, dest ...any) error {
						calls++
						if calls == cancelAt {
							require.Equal(t, 1, store.db.Stats().InUse)
							// Next has returned a real SQLite row. Cancellation can
							// close it before Scan acquires its separate rows lock.
							// Wait for database/sql's cancellation goroutine, not a
							// guessed delay or an explicit test-side Rows.Close.
							cancel()
							synctest.Wait()
							require.Equal(t, 0, store.db.Stats().InUse, "cancellation releases the sole connection")
							require.ErrorIs(t, rows.Err(), context.Canceled)
						}
						actualErr := rows.Scan(dest...)
						if calls == cancelAt {
							scanErr = actualErr
							require.ErrorIs(t, actualErr, context.Canceled, "the real Scan must supply the failure")
						} else {
							require.NoError(t, actualErr)
							successfulScans++
						}
						return actualErr
					})
					require.Equal(t, cancelAt, calls, "stop at the failing scan")
					require.Equal(t, cancelAt-1, successfulScans)
					require.Nil(t, got, "discard all earlier rows, not a partial or empty slice")
					require.ErrorIs(t, err, context.Canceled)
					require.ErrorIs(t, err, scanErr)
					require.EqualError(t, err, fmt.Sprintf("scan %s for %q: context canceled", tt.name, "scan00001"))
					require.Equal(t, 0, store.db.Stats().InUse)

					// The same store and sole connection remain usable with a
					// fresh context; canceled rows must not poison a later lookup.
					recovered, err := tt.lookup(store, t.Context(), "scan00001")
					require.NoError(t, err)
					require.Equal(t, tt.want, recovered)
					missing, err := tt.lookup(store, t.Context(), "missing00001")
					require.NoError(t, err)
					require.Nil(t, missing)
					require.Equal(t, 0, store.db.Stats().InUse)
				})
			})
		}
	}
}
