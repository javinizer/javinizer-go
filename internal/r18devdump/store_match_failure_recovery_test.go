package r18devdump

import (
	"database/sql"
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestMatchByDisplayIDDatabaseFailuresDoNotBecomeMisses(t *testing.T) {
	for _, tc := range []struct {
		name, query, install, restore, stage, cause string
		wantIDs                                     []string
	}{
		{
			name:    "normalized DVD lookup",
			query:   "IPX-535",
			install: `ALTER TABLE videos RENAME COLUMN dvd_id_norm TO fixture_dvd_id_norm`,
			restore: `ALTER TABLE videos RENAME COLUMN fixture_dvd_id_norm TO dvd_id_norm`,
			stage:   "dump dvd_id lookup failed for",
			cause:   "no such column: dvd_id_norm",
			wantIDs: []string{"118ipx00535"},
		},
		{
			name:  "expanded content candidates",
			query: "LULU-441",
			install: `ALTER TABLE videos RENAME TO match_fixture_videos;
				CREATE VIEW videos AS SELECT content_id, dvd_id, dvd_id_norm,
					CASE WHEN content_id = 'lulu00441' THEN abs(-9223372036854775808) ELSE release_date END AS release_date,
					service_code FROM match_fixture_videos`,
			restore: `DROP VIEW videos; ALTER TABLE match_fixture_videos RENAME TO videos`,
			stage:   `dump candidate lookup failed for "LULU-441": dump content_id lookup failed:`,
			cause:   "integer overflow",
			wantIDs: []string{"lulu00441", "lulu441"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := seedDumpFullCols(t, matchFixture)
			healthyStore, err := Open(path)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, healthyStore.Close()) })
			healthy, err := healthyStore.MatchByDisplayID(t.Context(), tc.query)
			require.NoError(t, err)
			var ids []string
			for _, match := range healthy {
				ids = append(ids, match.ContentID)
			}
			require.Equal(t, tc.wantIDs, ids)
			require.NoError(t, healthyStore.Close())

			writer, err := sql.Open("sqlite3", path)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, writer.Close()) })
			_, err = writer.Exec(tc.install)
			require.NoError(t, err)
			before, err := os.ReadFile(path)
			require.NoError(t, err)

			// Open's public readiness check accepts the sidecar. The literal
			// content-ID probe is healthy but empty. Each fault therefore
			// belongs to its later matching stage, not initial admission.
			store, err := Open(path)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, store.Close()) })
			matches, err := store.MatchByDisplayID(t.Context(), tc.query)
			require.Nil(t, matches, "do not publish partial matching evidence")
			require.ErrorContains(t, err, tc.stage)
			require.ErrorContains(t, err, tc.cause)
			require.NotErrorIs(t, err, models.ErrDumpMiss)
			require.NotErrorIs(t, err, models.ErrDumpNoDVDID)
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, before, after, "read-only matching must not repair the installed schema fault")

			_, err = writer.Exec(tc.restore)
			require.NoError(t, err)
			recovered, err := store.MatchByDisplayID(t.Context(), tc.query)
			require.NoError(t, err)
			require.Equal(t, healthy, recovered, "same store recovers complete ordered matches after restoring only the owned fault")
		})
	}
}
