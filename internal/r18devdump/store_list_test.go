package r18devdump

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListActressesSuccessAndQueryFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actresses.db")
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE actresses (id TEXT, name_romaji TEXT, image_url TEXT, name_kanji TEXT, name_kana TEXT);
		INSERT INTO actresses VALUES ('42', 'Yui Hatano', 'image.jpg', '波多野結衣', 'はたのゆい');`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	store, err := Open(path)
	require.NoError(t, err)
	rows, err := store.ListActresses(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "42", rows[0].ID)
	assert.Equal(t, "Yui Hatano", rows[0].NameRomaji)
	require.NoError(t, store.Close())

	_, err = store.ListActresses(context.Background())
	require.ErrorContains(t, err, "list actresses")
}
