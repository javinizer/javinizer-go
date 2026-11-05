package r18devdump

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateSidecar_UnreadableLogicalKeyPreservesDumpAndRecovers(t *testing.T) {
	path := importFixture(t)
	alterFixture(t, path,
		`INSERT INTO actresses (id, name_romaji) VALUES ('actress-1', 'Example Actress')`,
		`INSERT INTO video_actresses (content_id, actress_id) VALUES ('118ipx00535', 'actress-1')`)

	healthy, err := ValidateSidecar(t.Context(), path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, healthy.Close()) })
	wantMovie, err := healthy.LookupMovie(t.Context(), "IPX-535")
	require.NoError(t, err)
	require.NotNil(t, wantMovie)
	require.Len(t, wantMovie.Actresses, 1)
	wantStats, err := healthy.Stats(t.Context())
	require.NoError(t, err)
	require.NoError(t, healthy.Close())

	// The view has every required column and prepares normally. Evaluating
	// its logical key fails in SQLite, so validation must reject it before
	// publishing a Store instead of treating an unreadable key as valid.
	alterFixture(t, path,
		`ALTER TABLE actresses RENAME TO logical_key_fixture_actresses`,
		`CREATE VIEW actresses AS
			SELECT abs(-9223372036854775808) AS id, name_romaji,
				image_url, name_kanji, name_kana FROM logical_key_fixture_actresses`)
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	rejected, err := ValidateSidecar(t.Context(), path)
	if rejected != nil {
		t.Cleanup(func() { require.NoError(t, rejected.Close()) })
	}
	require.ErrorIs(t, err, ErrDumpInvalid)
	require.Nil(t, rejected, "a failed validation must not return a usable Store")
	require.Contains(t, err.Error(), "actresses logical-key probe")
	require.Contains(t, err.Error(), "integer overflow")
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after, "validation must leave the rejected sidecar unchanged")

	// Rejected sidecars must be replaceable immediately. In particular,
	// Windows cannot rename the database while SQLite retains its handle.
	moved := path + ".rejected"
	require.NoError(t, os.Rename(path, moved))
	require.NoError(t, os.Rename(moved, path))
	alterFixture(t, path,
		`DROP VIEW actresses`,
		`ALTER TABLE logical_key_fixture_actresses RENAME TO actresses`)

	recovered, err := ValidateSidecar(t.Context(), path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, recovered.Close()) })
	gotMovie, err := recovered.LookupMovie(t.Context(), "IPX-535")
	require.NoError(t, err)
	require.Equal(t, wantMovie, gotMovie, "repair must preserve the original movie and actress")
	gotStats, err := recovered.Stats(t.Context())
	require.NoError(t, err)
	require.Equal(t, wantStats, gotStats, "validation must preserve row count and provenance")
}
