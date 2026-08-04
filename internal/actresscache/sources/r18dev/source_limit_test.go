package r18devsource

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeActresses(ids ...string) []models.DumpActress {
	out := make([]models.DumpActress, 0, len(ids))
	for _, id := range ids {
		out = append(out, models.DumpActress{ID: id, NameRomaji: "Test" + id + " Actress"})
	}
	return out
}

func TestCollectPassesScanCapToLister(t *testing.T) {
	t.Run("no user limit scans up to the hard cap", func(t *testing.T) {
		var gotLimit int
		src := NewFromLister(func(_ context.Context, limit int) ([]models.DumpActress, error) {
			gotLimit = limit
			return fakeActresses("1"), nil
		})
		err := src.Collect(context.Background(), actresscache.SourceOptions{Workers: 1}, func(actresscache.Candidate) error { return nil })
		require.NoError(t, err)
		assert.Equal(t, maxScanRows, gotLimit)
	})

	t.Run("user limit below the cap wins", func(t *testing.T) {
		var gotLimit int
		src := NewFromLister(func(_ context.Context, limit int) ([]models.DumpActress, error) {
			gotLimit = limit
			return fakeActresses("1"), nil
		})
		err := src.Collect(context.Background(), actresscache.SourceOptions{Limit: 5, Workers: 1}, func(actresscache.Candidate) error { return nil })
		require.NoError(t, err)
		assert.Equal(t, 5, gotLimit)
	})

	t.Run("user limit above the cap is clamped", func(t *testing.T) {
		var gotLimit int
		src := NewFromLister(func(_ context.Context, limit int) ([]models.DumpActress, error) {
			gotLimit = limit
			return fakeActresses("1"), nil
		})
		err := src.Collect(context.Background(), actresscache.SourceOptions{Limit: maxScanRows + 1, Workers: 1}, func(actresscache.Candidate) error { return nil })
		require.NoError(t, err)
		assert.Equal(t, maxScanRows, gotLimit)
	})
}

// A custom lister that ignores the limit argument must still not flood the
// pipeline: Collect truncates defensively.
func TestCollectTruncatesIgnoringLister(t *testing.T) {
	src := NewFromLister(func(_ context.Context, _ int) ([]models.DumpActress, error) {
		return fakeActresses("1", "2", "3"), nil
	})
	emitted := 0
	err := src.Collect(context.Background(), actresscache.SourceOptions{Limit: 2, Workers: 1}, func(actresscache.Candidate) error {
		emitted++
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 2, emitted)
}
