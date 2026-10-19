package r18devdump

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func collatedProvenanceFixture(t *testing.T, collation string) string {
	t.Helper()
	path := importFixture(t)
	alterFixture(t, path,
		"CREATE TABLE dump_meta_collated (key TEXT COLLATE "+collation+" PRIMARY KEY, value TEXT)",
		"INSERT INTO dump_meta_collated SELECT key, value FROM dump_meta",
		"DROP TABLE dump_meta",
		"ALTER TABLE dump_meta_collated RENAME TO dump_meta",
		"UPDATE dump_meta SET value = '2026-09-20' WHERE key = 'source_date'",
		"UPDATE dump_meta SET value = '2026-09-27T00:00:00Z' WHERE key = 'imported_at'")
	return path
}

func TestValidateSidecar_ProvenanceKeysRequireExactSpelling(t *testing.T) {
	for _, collation := range []string{"BINARY", "NOCASE", "RTRIM"} {
		for _, key := range []string{"source_url", "source_date", "imported_at"} {
			t.Run(collation+"/"+key, func(t *testing.T) {
				path := collatedProvenanceFixture(t, collation)
				replacement := strings.ToUpper(key)
				if collation == "RTRIM" {
					replacement = key + " "
				}
				alterFixture(t, path, fmt.Sprintf("UPDATE dump_meta SET key = '%s' WHERE key = '%s'", replacement, key))
				store, err := ValidateSidecar(context.Background(), path)
				if store != nil {
					stats, statsErr := store.Stats(context.Background())
					t.Logf("accepted noncanonical %q: stats=%+v, err=%v", replacement, stats, statsErr)
					require.NoError(t, store.Close())
				}
				require.ErrorIs(t, err, ErrDumpInvalid)
				require.Nil(t, store)
				require.Contains(t, err.Error(), "missing required provenance keys")
			})
		}
	}
}

func TestValidateSidecar_CollatedCanonicalProvenanceAccepted(t *testing.T) {
	for _, collation := range []string{"BINARY", "NOCASE", "RTRIM"} {
		t.Run(collation, func(t *testing.T) {
			path := collatedProvenanceFixture(t, collation)
			alterFixture(t, path, "INSERT INTO dump_meta VALUES ('future_metadata', 'extra')")
			store, err := ValidateSidecar(context.Background(), path)
			require.NoError(t, err)
			defer store.Close()
			stats, err := store.Stats(context.Background())
			require.NoError(t, err)
			require.Equal(t, "https://example/dumps/r18dotdev_dump_2026-09-20.sql.gz", stats.SourceURL)
			require.Equal(t, "2026-09-20", stats.SourceDate)
			require.Equal(t, "2026-09-27T00:00:00Z", stats.ImportedAt)
		})
	}
}
