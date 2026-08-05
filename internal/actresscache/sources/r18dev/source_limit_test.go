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

// Collect has no limit argument on the seam path: the store-level adapter
// (RegisterR18Dev) enforces MaxScanRows; here we pin user-limit semantics.
func TestCollectWindowsAtUserLimit(t *testing.T) {
	src := NewFromLister(func(context.Context) ([]models.DumpActress, error) {
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
	src := NewFromLister(func(context.Context) ([]models.DumpActress, error) {
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
