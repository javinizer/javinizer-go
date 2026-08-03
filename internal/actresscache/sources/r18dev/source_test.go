package r18devsource

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/r18devdump"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectReadsActressesFromDump(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r18dev.db")
	dump := "COPY public.derived_actress (id, name_romaji, image_url, name_kanji, name_kana) FROM stdin;\n12345\tYamada Hanako\tfoo.jpg\t山田花子\tやまだはなこ\n\\.\n"
	_, err := r18devdump.Import(context.Background(), strings.NewReader(dump), path, r18devdump.ImportOptions{})
	require.NoError(t, err)

	var got []actresscache.Candidate
	err = New().Collect(context.Background(), actresscache.SourceOptions{R18DevDumpPath: path}, func(candidate actresscache.Candidate) error {
		got = append(got, candidate)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "r18dev:actress:12345", got[0].Key)
	require.Equal(t, 12345, got[0].DMMID)
	require.Equal(t, "Yamada", got[0].FirstName)
	require.Equal(t, "Hanako", got[0].LastName)
	require.Equal(t, "山田花子", got[0].JapaneseName)
	require.Equal(t, "https://pics.dmm.co.jp/mono/actjpgs/foo.jpg", got[0].ThumbURL)
}

func TestCollectRequiresDumpPath(t *testing.T) {
	err := New().Collect(context.Background(), actresscache.SourceOptions{}, func(actresscache.Candidate) error { return nil })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "r18dev source requires")
}

func TestCollectSupportsParameterAndLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r18dev.db")
	dump := `COPY public.derived_actress (id, name_romaji, image_url, name_kanji, name_kana) FROM stdin;
12345	Yamada Hanako	foo.jpg	山田花子	やまだはなこ
67890	Suzuki Taro	bar.jpg	鈴木太郎	すずきたろう
\.
`
	_, err := r18devdump.Import(context.Background(), strings.NewReader(dump), path, r18devdump.ImportOptions{})
	require.NoError(t, err)
	count := 0
	err = New().Collect(context.Background(), actresscache.SourceOptions{Parameters: map[string]string{"r18dev.dump": path}, Limit: 1}, func(actresscache.Candidate) error { count++; return nil })
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestCollectUsesWorkers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r18dev.db")
	dump := `COPY public.derived_actress (id, name_romaji, image_url, name_kanji, name_kana) FROM stdin;
1	One Hanako	one.jpg	一花子	
2	Two Hanako	two.jpg	二花子	
3	Three Hanako	three.jpg	三花子	
4	Four Hanako	four.jpg	四花子	
\.
`
	_, err := r18devdump.Import(context.Background(), strings.NewReader(dump), path, r18devdump.ImportOptions{})
	require.NoError(t, err)
	var mu sync.Mutex
	active, maximum := 0, 0
	err = New().Collect(context.Background(), actresscache.SourceOptions{R18DevDumpPath: path, Workers: 4}, func(actresscache.Candidate) error {
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
func TestNameAndThumbnailFallbacks(t *testing.T) {
	first, last := splitRomajiName("One")
	assert.Equal(t, "One", first)
	assert.Empty(t, last)
	assert.Equal(t, "https://pics.dmm.co.jp/mono/actjpgs/foo.jpg", thumbnailURL(models.DumpActress{ImageURL: "foo.jpg"}))
	assert.Equal(t, "https://example.test/photo", thumbnailURL(models.DumpActress{ImageURL: "https://example.test/photo"}))
	assert.Equal(t, "https://pics.dmm.co.jp/mono/actjpgs/hanako_yamada.jpg", thumbnailURL(models.DumpActress{NameRomaji: "Yamada Hanako"}))
}
