package legacycsvsource

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeCSV(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "jvThumbs.csv")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestCollectParsesLegacyCSV(t *testing.T) {
	path := writeCSV(t, "\ufeffFullName,LastName,FirstName,JapaneseName,ThumbUrl,Alias\nYamada Hanako,Yamada,Hanako,山田花子,https://example.test/hanako.jpg,旧名|Alias\nAika,,,AIKA,https://example.test/aika.jpg,\n")
	var got []actresscache.Candidate
	err := New().Collect(context.Background(), actresscache.SourceOptions{Parameters: map[string]string{"legacy.csv": path}}, func(candidate actresscache.Candidate) error {
		got = append(got, candidate)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, sourceName, got[0].Source)
	assert.Equal(t, "Hanako", got[0].FirstName)
	assert.Equal(t, "Yamada", got[0].LastName)
	assert.Equal(t, "山田花子", got[0].JapaneseName)
	assert.Equal(t, []string{"旧名", "Alias"}, got[0].Aliases)
	assert.Contains(t, got[0].SourceURL, "#row=2")
	assert.Equal(t, "Aika", got[1].FirstName)
}

func TestCollectSupportsLimitAndResumeSkip(t *testing.T) {
	path := writeCSV(t, "FullName,LastName,FirstName,JapaneseName,ThumbUrl,Alias\nOne,,One,一,https://example.test/1.jpg,\nTwo,,Two,二,https://example.test/2.jpg,\n")
	count := 0
	err := New().Collect(context.Background(), actresscache.SourceOptions{Parameters: map[string]string{"jvthumbs.csv": path}, Limit: 1}, func(actresscache.Candidate) error {
		count++
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	err = New().Collect(context.Background(), actresscache.SourceOptions{Parameters: map[string]string{"legacy.csv": path}, ShouldSkip: func(string) bool { return true }}, func(actresscache.Candidate) error {
		count++
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestCollectUsesWorkers(t *testing.T) {
	path := writeCSV(t, "FullName,ThumbUrl\nOne,https://example.test/1.jpg\nTwo,https://example.test/2.jpg\nThree,https://example.test/3.jpg\nFour,https://example.test/4.jpg\n")
	var mu sync.Mutex
	active, maximum := 0, 0
	err := New().Collect(context.Background(), actresscache.SourceOptions{Parameters: map[string]string{"legacy.csv": path}, Workers: 4}, func(actresscache.Candidate) error {
		mu.Lock()
		active++
		if active > maximum {
			maximum = active
		}
		mu.Unlock()
		time.Sleep(25 * time.Millisecond)
		mu.Lock()
		active--
		mu.Unlock()
		return nil
	})
	require.NoError(t, err)
	assert.Greater(t, maximum, 1)
}

func TestCollectRejectsInvalidSetup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := New().Collect(ctx, actresscache.SourceOptions{}, func(actresscache.Candidate) error { return nil })
	require.ErrorIs(t, err, context.Canceled)
	err = New().Collect(context.Background(), actresscache.SourceOptions{}, func(actresscache.Candidate) error { return nil })
	assert.Contains(t, err.Error(), "requires")
	err = New().Collect(context.Background(), actresscache.SourceOptions{Parameters: map[string]string{"legacy.csv": filepath.Join(t.TempDir(), "missing.csv")}}, func(actresscache.Candidate) error { return nil })
	assert.Contains(t, err.Error(), "open legacy thumbnail CSV")
}

func TestCollectRejectsInvalidHeadersAndRows(t *testing.T) {
	cases := []struct {
		name string
		csv  string
		want string
	}{
		{"no names", "ThumbUrl\nhttps://example.test/a.jpg\n", "no actress name"},
		{"no thumbnail", "JapaneseName\n花子\n", "no ThumbUrl"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := New().Collect(context.Background(), actresscache.SourceOptions{Parameters: map[string]string{"legacy.csv": writeCSV(t, tc.csv)}}, func(actresscache.Candidate) error { return nil })
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestCollectAcceptsUnquotedLegacyURL(t *testing.T) {
	path := writeCSV(t, "JapaneseName,ThumbUrl\n花子,http://example.test/photo.jpg\n")
	count := 0
	err := New().Collect(context.Background(), actresscache.SourceOptions{Parameters: map[string]string{"legacy.csv": path}}, func(candidate actresscache.Candidate) error {
		count++
		assert.Equal(t, "http://example.test/photo.jpg", candidate.ThumbURL)
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestLegacyHelpers(t *testing.T) {
	assert.Equal(t, "japanesename", normalizeHeader(" Japanese_Name "))
	columns := findColumns([]string{"FullName", "ThumbURL", "Aliases"})
	assert.Equal(t, 0, columns.fullName)
	assert.Equal(t, 1, columns.thumbURL)
	assert.Equal(t, 2, columns.alias)
	assert.Equal(t, []string{"one", "two"}, splitAliases("one| two||"))
	candidate := candidateFromRow([]string{"Aika", "", "", "AIKA", "https://example.test/aika.jpg", ""}, columnsForTest(), "file:///tmp/a.csv", 2)
	assert.Equal(t, "Aika", candidate.FirstName)
	assert.NotEmpty(t, candidate.Key)
}

func columnsForTest() csvColumns {
	return csvColumns{fullName: 0, lastName: 1, firstName: 2, japaneseName: 3, thumbURL: 4, alias: 5}
}
