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

// Collect forwards the user limit into the lister query window: a small
// --limit must never materialize the full ~250k-row dump.
func TestCollectPassesUserLimitIntoLister(t *testing.T) {
	requiredLimit := -1
	src := NewFromLister(func(_ context.Context, limit int) ([]models.DumpActress, error) {
		requiredLimit = limit
		return fakeActresses("1"), nil
	})
	require.NoError(t, src.Collect(context.Background(), actresscache.SourceOptions{
		Limit:   7,
		Workers: 1,
	}, func(actresscache.Candidate) error { return nil }))
	assert.Equal(t, 7, requiredLimit)
}

// User-limit semantics on the source: windowing marks the enumeration
// truncated; the store-level adapter (RegisterR18Dev) still enforces
// MaxScanRows as the outer safety cap.
func TestCollectWindowsAtUserLimit(t *testing.T) {
	src := NewFromLister(func(context.Context, int) ([]models.DumpActress, error) {
		return fakeActresses("1", "2", "3"), nil
	})
	emitted := 0
	completed := false
	err := src.Collect(context.Background(), actresscache.SourceOptions{
		Limit:        2,
		Workers:      1,
		MarkComplete: func() { completed = true },
	}, func(actresscache.Candidate) error {
		emitted++
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 2, emitted)
	assert.False(t, completed, "truncated enumeration must not declare completion")
}

func TestCollectFullEnumerationMarksComplete(t *testing.T) {
	src := NewFromLister(func(context.Context, int) ([]models.DumpActress, error) {
		return fakeActresses("1", "2"), nil
	})
	completed := false
	err := src.Collect(context.Background(), actresscache.SourceOptions{
		Workers:      1,
		MarkComplete: func() { completed = true },
	}, func(actresscache.Candidate) error { return nil })
	require.NoError(t, err)
	assert.True(t, completed)
}
