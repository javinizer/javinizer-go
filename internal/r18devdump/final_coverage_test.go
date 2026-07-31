package r18devdump

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestOpenReportsDriverOpenError(t *testing.T) {
	old := openSQL
	openSQL = func(string, string) (*sql.DB, error) { return nil, errors.New("open failed") }
	t.Cleanup(func() { openSQL = old })
	_, err := Open("ignored")
	require.ErrorContains(t, err, "open dump db")
}

func TestListActressesScanAndIterationErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows *sqlmock.Rows
		want string
	}{
		{"scan", sqlmock.NewRows([]string{"id"}).AddRow("1"), "scan actress"},
		{"iteration", sqlmock.NewRows([]string{"id", "name_romaji", "image_url", "name_kanji", "name_kana"}).AddRow("1", "A", "", "", "").RowError(0, errors.New("row failed")), "iterate actresses"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			mock.ExpectQuery("SELECT id, name_romaji").WillReturnRows(tc.rows)
			_, err = (&Store{db: db}).ListActresses(t.Context())
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestLookupActressesAndCategoriesRowErrors(t *testing.T) {
	tests := []struct {
		name  string
		rows  *sqlmock.Rows
		call  func(*Store) error
		match string
	}{
		{
			name:  "actress scan",
			rows:  sqlmock.NewRows([]string{"id"}).AddRow("1"),
			call:  func(s *Store) error { _, err := s.lookupActresses(context.Background(), "id"); return err },
			match: "scan actress",
		},
		{
			name:  "actress iteration",
			rows:  sqlmock.NewRows([]string{"id", "name_romaji", "image_url", "name_kanji", "name_kana"}).AddRow("1", "A", "", "", "").RowError(0, errors.New("row failed")),
			call:  func(s *Store) error { _, err := s.lookupActresses(context.Background(), "id"); return err },
			match: "iterate actresses",
		},
		{
			name:  "category scan",
			rows:  sqlmock.NewRows([]string{"name_en"}).AddRow("A"),
			call:  func(s *Store) error { _, err := s.lookupCategories(context.Background(), "id"); return err },
			match: "scan category",
		},
		{
			name:  "category iteration",
			rows:  sqlmock.NewRows([]string{"name_en", "name_ja"}).AddRow("A", "").RowError(0, errors.New("row failed")),
			call:  func(s *Store) error { _, err := s.lookupCategories(context.Background(), "id"); return err },
			match: "iterate categories",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			mock.ExpectQuery("SELECT").WillReturnRows(tc.rows)
			err = tc.call(&Store{db: db})
			require.ErrorContains(t, err, tc.match)
		})
	}
}
