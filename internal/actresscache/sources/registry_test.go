package sources

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/r18devdump"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterAllProvidesDeterministicSources(t *testing.T) {
	registry := actresscache.NewRegistry()
	RegisterAll(registry)
	assert.Equal(t, []string{"legacy-jvthumbs", "minnanoav"}, registry.Names())
	minnano, ok := registry.Create("minnanoav")
	require.True(t, ok)
	assert.Equal(t, "minnanoav", minnano.Name())
	_, ok = registry.Create("missing")
	assert.False(t, ok)
}

func TestRegisterR18Dev(t *testing.T) {
	t.Run("missing dump errors", func(t *testing.T) {
		_, err := RegisterR18Dev(actresscache.NewRegistry(), filepath.Join(t.TempDir(), "missing.db"))
		require.ErrorContains(t, err, "open r18.dev dump")
	})

	t.Run("registers lister-backed source", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "r18dev.db")
		dump := "COPY public.derived_actress (id, name_romaji, image_url, name_kanji, name_kana) FROM stdin;\n12345\tYamada Hanako\tfoo.jpg\t山田花子\tやまだはなこ\n\\.\n"
		_, err := r18devdump.Import(context.Background(), strings.NewReader(dump), path, r18devdump.ImportOptions{})
		require.NoError(t, err)
		registry := actresscache.NewRegistry()
		closer, err := RegisterR18Dev(registry, path)
		require.NoError(t, err)
		defer func() { _ = closer.Close() }()
		assert.Contains(t, registry.Names(), "r18dev")
		source, ok := registry.Create("r18dev")
		require.True(t, ok)
		var got []actresscache.Candidate
		require.NoError(t, source.Collect(context.Background(), actresscache.SourceOptions{}, func(c actresscache.Candidate) error { got = append(got, c); return nil }))
		assert.Len(t, got, 1)
	})
}

func TestR18DevBoundedLister(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r18dev.db")
	dump := "COPY public.derived_actress (id, name_romaji, image_url, name_kanji, name_kana) FROM stdin;\n1	A One\ta.jpg\t\t\n2	B Two\tb.jpg\t\t\n\\.\n"
	_, err := r18devdump.Import(context.Background(), strings.NewReader(dump), path, r18devdump.ImportOptions{})
	require.NoError(t, err)
	store, err := r18devdump.Open(path)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	// Under the cap the full table passes through.
	lister := r18DevBoundedLister(store, 2)
	got, err := lister(context.Background())
	require.NoError(t, err)
	assert.Len(t, got, 2)

	// At cap-1 the scan errors loudly (no truncated cache handed to the builder).
	lister = r18DevBoundedLister(store, 1)
	_, err = lister(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scan safety cap")
}

func TestR18DevBoundedListerStoreError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r18dev.db")
	dump := "COPY public.derived_actress (id, name_romaji, image_url, name_kanji, name_kana) FROM stdin;\n1\tA One\ta.jpg\t\t\t\n\\.\n"
	_, err := r18devdump.Import(context.Background(), strings.NewReader(dump), path, r18devdump.ImportOptions{})
	require.NoError(t, err)
	store, err := r18devdump.Open(path)
	require.NoError(t, err)
	require.NoError(t, store.Close())
	_, err = r18DevBoundedLister(store, 2)(context.Background())
	require.Error(t, err, "closed dump store must surface the lister error")
}
