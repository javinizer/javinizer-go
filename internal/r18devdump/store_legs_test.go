package r18devdump

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3" // register the sqlite3 driver for sql.Open
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListActressesScanErrorLeg(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scan.db")
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	_, err = db.Exec(`CREATE TABLE actresses (id TEXT, name_romaji TEXT, image_url TEXT, name_kanji TEXT, name_kana TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO actresses VALUES ('1','a','b','c','d')`)
	require.NoError(t, err)
	sentinel := errors.New("scan boom")
	original := scanActressRow
	defer func() { scanActressRow = original }()
	scanActressRow = func(*sql.Rows, ...any) error { return sentinel }
	_, err = (&Store{db: db}).ListActresses(context.Background())
	require.ErrorIs(t, err, sentinel)
	assert.Contains(t, err.Error(), "scan actress")
}

func TestListActressesIterateErrorLeg(t *testing.T) {
	path := filepath.Join(t.TempDir(), "iter.db")
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TABLE base_rows (id TEXT, name_romaji TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO base_rows VALUES ('1','ok'),('2','ok')`); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE VIEW actresses AS SELECT id, name_romaji, CASE id WHEN '1' THEN json_extract('{bad json','$.a') ELSE 'x' END AS image_url, '' AS name_kanji, '' AS name_kana FROM base_rows`)
	require.NoError(t, err)
	_, err = (&Store{db: db}).ListActresses(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "iterate actresses")
	assert.Error(t, err)
}

func TestListActressesQueryErrorLeg(t *testing.T) {
	path := filepath.Join(t.TempDir(), "q.db")
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	_, err = (&Store{db: db}).ListActresses(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "list actresses")
}
