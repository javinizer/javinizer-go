package r18devdump

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedDumpFullCols(t *testing.T, rows string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "r18dev_dump.db")
	dump := "COPY public.derived_video (content_id, dvd_id, release_date, service_code) FROM stdin;\n" + rows + "\n\\.\n"
	_, err := Import(context.Background(), strings.NewReader(dump), path, ImportOptions{SourceDate: "2026-04-28"})
	require.NoError(t, err)
	return path
}

const matchFixture = "118ipx00535\tIPX-535\t2019-01-01\tdigital\n" +
	"lulu00441\t\\N\t2026-07-03\tdigital\n" +
	"lulu441\t\\N\t2026-07-07\tmono"

func TestMatchByDisplayID_NormHitSingleMatch(t *testing.T) {
	store, err := Open(seedDumpFullCols(t, matchFixture))
	require.NoError(t, err)
	defer store.Close()

	matches, err := store.MatchByDisplayID(context.Background(), "IPX-535")
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, "118ipx00535", matches[0].ContentID)
	assert.Equal(t, "IPX-535", matches[0].DVDID)
	assert.Equal(t, "2019-01-01", matches[0].ReleaseDate)
	assert.Equal(t, "digital", matches[0].ServiceCode)

	// Normalization input styles all land on the same single match.
	for _, q := range []string{"ipx-535", "IPX535", " ipx 535 "} {
		m, err := store.MatchByDisplayID(context.Background(), q)
		require.NoError(t, err)
		require.Len(t, m, 1)
		assert.Equal(t, "118ipx00535", m[0].ContentID)
	}
}

func TestMatchByDisplayID_CandidateExpansionOrderedMultiMatch(t *testing.T) {
	store, err := Open(seedDumpFullCols(t, matchFixture))
	require.NoError(t, err)
	defer store.Close()

	matches, err := store.MatchByDisplayID(context.Background(), "LULU-441")
	require.NoError(t, err)
	require.Len(t, matches, 2)
	assert.Equal(t, "lulu00441", matches[0].ContentID, "canonical zero-padded candidate first")
	assert.Equal(t, "lulu441", matches[1].ContentID)
	assert.Equal(t, "digital", matches[0].ServiceCode)
	assert.Equal(t, "mono", matches[1].ServiceCode)
	assert.Equal(t, "", matches[0].DVDID)
}

func TestMatchByDisplayID_DirectContentIDInputExpandsToo(t *testing.T) {
	store, err := Open(seedDumpFullCols(t, matchFixture))
	require.NoError(t, err)
	defer store.Close()

	matches, err := store.MatchByDisplayID(context.Background(), "lulu00441")
	require.NoError(t, err)
	require.Len(t, matches, 2)
	assert.Equal(t, "lulu00441", matches[0].ContentID)
}

func TestMatchByDisplayID_ExactContentIDRowLeadsCandidates(t *testing.T) {
	store, err := Open(seedDumpFullCols(t, matchFixture))
	require.NoError(t, err)
	defer store.Close()

	// A literal content_id query surfaces its own row first, even when a
	// canonical zero-padded sibling exists in the same candidate set.
	matches, err := store.MatchByDisplayID(context.Background(), "lulu441")
	require.NoError(t, err)
	require.Len(t, matches, 2)
	assert.Equal(t, "lulu441", matches[0].ContentID, "exact content_id row first")
	assert.Equal(t, "lulu00441", matches[1].ContentID, "canonical variant follows")

	// Dash-containing display queries keep canonical-first ordering.
	matches, err = store.MatchByDisplayID(context.Background(), "LULU-441")
	require.NoError(t, err)
	require.Equal(t, "lulu00441", matches[0].ContentID)
}

func TestMatchByDisplayID_MissAndRejects(t *testing.T) {
	store, err := Open(seedDumpFullCols(t, matchFixture))
	require.NoError(t, err)
	defer store.Close()

	_, err = store.MatchByDisplayID(context.Background(), "NOPE-999")
	assert.True(t, errors.Is(err, models.ErrDumpMiss))
	assert.False(t, errors.Is(err, models.ErrDumpNoDVDID))

	_, err = store.MatchByDisplayID(context.Background(), "")
	assert.True(t, errors.Is(err, models.ErrDumpMiss))

	var nilStore *Store
	_, err = nilStore.MatchByDisplayID(context.Background(), "IPX-535")
	assert.True(t, errors.Is(err, models.ErrDumpMiss))
}
