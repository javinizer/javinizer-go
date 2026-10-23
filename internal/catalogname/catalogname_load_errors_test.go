package catalogname

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadPersistedAliasesOnlyBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	db := fixture(t, path)
	_, err := db.Exec(`INSERT INTO actress_aliases VALUES ('Stage Name','Hatano Yui'), ('  ','Hatano Yui')`)
	require.NoError(t, err)
	snapshot, err := Load(context.Background(), path)
	require.NoError(t, err)
	require.Equal(t, "alias_name_mismatch", snapshot.Resolve("Stage Name").Reason)
	require.True(t, snapshot.Resolve("Hatano Yui").Resolved)
	_, err = db.Exec(`INSERT INTO actresses VALUES (2,'Other','Person','','import',1,0,''); INSERT INTO actress_aliases VALUES ('Yui Hatano','Person Other')`)
	require.NoError(t, err)
	snapshot, err = Load(context.Background(), path)
	require.NoError(t, err)
	require.Equal(t, "ambiguous", snapshot.Resolve("Hatano Yui").Reason)
}

func TestLoadCanceledContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	fixture(t, path)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	snapshot, err := Load(ctx, path)
	require.Nil(t, snapshot)
	require.ErrorIs(t, err, context.Canceled)
}

func TestLoadInvalidCatalogDataWithoutWrites(t *testing.T) {
	for _, tc := range []struct{ name, mutation, errorText string }{
		{"invalid verification value", `UPDATE actresses SET verified='not-a-boolean'`, "read catalog actress:"},
		{"missing curated alias table", `DROP TABLE actress_aliases`, "read curated catalog aliases"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "catalog.db")
			db := fixture(t, path)
			_, err := db.Exec(tc.mutation)
			require.NoError(t, err)
			before, err := os.ReadFile(path)
			require.NoError(t, err)
			snapshot, err := Load(context.Background(), path)
			require.Nil(t, snapshot)
			require.ErrorContains(t, err, tc.errorText)
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

func TestLoadRejectsNULPath(t *testing.T) {
	snapshot, err := Load(context.Background(), "catalog\x00.db")
	require.Nil(t, snapshot)
	require.ErrorContains(t, err, "ordinary existing SQLite file")
}
