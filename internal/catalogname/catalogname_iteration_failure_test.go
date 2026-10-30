package catalogname

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadIterationFailureRejectsPartialSnapshotAndRecovers(t *testing.T) {
	for _, tc := range []struct {
		name, install, restore, errorPrefix string
	}{
		{
			name: "actresses",
			install: `ALTER TABLE actresses RENAME TO iteration_fixture_actresses;
				CREATE VIEW actresses AS SELECT id,
					CASE WHEN id = 2 THEN abs(-9223372036854775808) ELSE first_name END AS first_name,
					last_name, japanese_name, origin, verified, ambiguity_quarantined, aliases
					FROM iteration_fixture_actresses ORDER BY id`,
			restore: `DROP VIEW actresses;
				ALTER TABLE iteration_fixture_actresses RENAME TO actresses`,
			errorPrefix: "read catalog actresses: ",
		},
		{
			name: "aliases",
			install: `ALTER TABLE actress_aliases RENAME TO iteration_fixture_aliases;
				CREATE VIEW actress_aliases AS SELECT alias_name,
					CASE WHEN rowid = 2 THEN abs(-9223372036854775808) ELSE canonical_name END AS canonical_name
					FROM iteration_fixture_aliases ORDER BY rowid`,
			restore: `DROP VIEW actress_aliases;
				ALTER TABLE iteration_fixture_aliases RENAME TO actress_aliases`,
			errorPrefix: "read catalog aliases: ",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "catalog.db")
			db := fixture(t, path)
			_, err := db.Exec(`INSERT INTO actresses VALUES (2,'Other','Person','','import',1,0,'');
				INSERT INTO actress_aliases VALUES ('Stage Name','Hatano Yui'), ('Second Stage','Other Person')`)
			require.NoError(t, err)
			healthy, err := Load(t.Context(), path)
			require.NoError(t, err)
			first := healthy.Resolve("Hatano Yui")
			second := healthy.Resolve("Other Person")
			require.True(t, first.Resolved)
			require.True(t, second.Resolved)

			// These owned schema faults prepare successfully but fail during
			// real SQLite row evaluation. Alias failure happens after all
			// actress records have been read, so no partial trusted snapshot
			// may escape when the remaining ownership evidence is unreadable.
			_, err = db.Exec(tc.install)
			require.NoError(t, err)
			before, err := os.ReadFile(path)
			require.NoError(t, err)
			snapshot, err := Load(t.Context(), path)
			require.Nil(t, snapshot)
			require.EqualError(t, err, tc.errorPrefix+"integer overflow")
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, before, after, "Load must not repair or mutate the fixture's installed schema fault")
			require.Equal(t, first, healthy.Resolve("Hatano Yui"), "earlier detached snapshot remains usable")
			require.Equal(t, second, healthy.Resolve("Other Person"))

			// Restoring only the owned fault also proves failed Load released
			// its read transaction; a fresh load can again trust complete data.
			_, err = db.Exec(tc.restore)
			require.NoError(t, err)
			recovered, err := Load(t.Context(), path)
			require.NoError(t, err)
			require.Equal(t, first, recovered.Resolve("Hatano Yui"))
			require.Equal(t, second, recovered.Resolve("Other Person"))
			require.Equal(t, healthy.Resolve("Stage Name"), recovered.Resolve("Stage Name"))
			require.Equal(t, healthy.Resolve("Second Stage"), recovered.Resolve("Second Stage"))
		})
	}
}
