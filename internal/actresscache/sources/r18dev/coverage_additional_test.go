package r18devsource

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/r18devdump"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func importActressDump(t *testing.T, rows string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "r18dev.db")
	dump := "COPY public.derived_actress (id, name_romaji, image_url, name_kanji, name_kana) FROM stdin;\n" + rows + "\\.\n"
	_, err := r18devdump.Import(context.Background(), strings.NewReader(dump), path, r18devdump.ImportOptions{})
	require.NoError(t, err)
	return path
}

func TestCollectSupportsGenericDumpParameterAndLifecycleCallbacks(t *testing.T) {
	path := importActressDump(t, "123\tOne Person\tone.jpg\t一人\t\n456\tTwo Person\ttwo.jpg\t二人\t\n")
	var seen []string
	complete := false
	var emitted []actresscache.Candidate
	err := New().Collect(context.Background(), actresscache.SourceOptions{
		Parameters:   map[string]string{"dump": path},
		MarkSeen:     func(key string) { seen = append(seen, key) },
		ShouldSkip:   func(key string) bool { return strings.HasSuffix(key, ":123") },
		MarkComplete: func() { complete = true },
	}, func(candidate actresscache.Candidate) error {
		emitted = append(emitted, candidate)
		return nil
	})
	require.NoError(t, err)
	assert.Len(t, seen, 2)
	require.Len(t, emitted, 1)
	assert.Equal(t, "456", emitted[0].SourceID)
	assert.True(t, complete)
}

func TestCollectReturnsOpenAndQueryErrors(t *testing.T) {
	t.Run("open", func(t *testing.T) {
		err := New().Collect(context.Background(), actresscache.SourceOptions{R18DevDumpPath: filepath.Join(t.TempDir(), "missing.db")}, func(actresscache.Candidate) error { return nil })
		require.ErrorContains(t, err, "open r18.dev dump")
	})
	t.Run("query", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "empty.db")
		require.NoError(t, os.WriteFile(path, nil, 0o644))
		err := New().Collect(context.Background(), actresscache.SourceOptions{R18DevDumpPath: path}, func(actresscache.Candidate) error { return nil })
		require.ErrorContains(t, err, "list actresses")
	})
}

func TestCollectReturnsEmitterError(t *testing.T) {
	path := importActressDump(t, "123\tOne Person\tone.jpg\t一人\t\n456\tTwo Person\ttwo.jpg\t二人\t\n789\tThree Person\tthree.jpg\t三人\t\n")
	want := errors.New("emit failed")
	err := New().Collect(context.Background(), actresscache.SourceOptions{R18DevDumpPath: path, Workers: 1}, func(actresscache.Candidate) error { return want })
	require.ErrorIs(t, err, want)
}

func TestCollectSkipsRowsWithoutIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty-id.db")
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE actresses (id TEXT, name_romaji TEXT, image_url TEXT, name_kanji TEXT, name_kana TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO actresses VALUES ('', 'Nobody', '', '', '')`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	count := 0
	err = New().Collect(context.Background(), actresscache.SourceOptions{R18DevDumpPath: path}, func(actresscache.Candidate) error { count++; return nil })
	require.NoError(t, err)
	assert.Zero(t, count)
}

func TestCollectPropagatesCanceledListContext(t *testing.T) {
	path := importActressDump(t, "123\tOne Person\tone.jpg\t一人\t\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := New().Collect(ctx, actresscache.SourceOptions{R18DevDumpPath: path}, func(actresscache.Candidate) error { return nil })
	require.Error(t, err)
}

func TestR18DevHelpersCoverEmptyAndSanitizedNames(t *testing.T) {
	first, last := splitRomajiName("   ")
	assert.Empty(t, first)
	assert.Empty(t, last)
	assert.Empty(t, thumbnailURL(models.DumpActress{}))
	assert.Empty(t, thumbnailURL(models.DumpActress{NameRomaji: "!!!"}))
	assert.Equal(t, "http://example.test/a.jpg", thumbnailURL(models.DumpActress{ImageURL: "http://example.test/a.jpg"}))
	assert.Equal(t, dmmActressImageBase+"nested/a.jpg", thumbnailURL(models.DumpActress{ImageURL: "/nested/a.jpg"}))
	assert.Equal(t, dmmActressImageBase+"a_b1.jpg", thumbnailURL(models.DumpActress{NameRomaji: "B1 A-"}))
}
