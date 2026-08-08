package r18devdump

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListActressesLimitCapsRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actresses.db")
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE actresses (id TEXT, name_romaji TEXT, image_url TEXT, name_kanji TEXT, name_kana TEXT);
		INSERT INTO actresses VALUES ('1','A ichi','a.jpg','',''),('2','B ni','b.jpg','',''),('3','C san','c.jpg','','');`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	store, err := Open(path)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	capped, err := store.ListActressesLimit(context.Background(), 2)
	require.NoError(t, err)
	require.Len(t, capped, 2, "LIMIT must be pushed into the query")
	assert.Equal(t, "1", capped[0].ID)

	// limit <= 0 means uncapped (SQLite LIMIT -1).
	all, err := store.ListActressesLimit(context.Background(), 0)
	require.NoError(t, err)
	require.Len(t, all, 3)
}
