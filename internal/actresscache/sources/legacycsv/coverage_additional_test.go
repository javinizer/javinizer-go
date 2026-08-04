package legacycsvsource

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectReportsEmptyHeader(t *testing.T) {
	err := New().Collect(context.Background(), actresscache.SourceOptions{
		Parameters: map[string]string{"legacy.csv": writeCSV(t, "")},
	}, func(actresscache.Candidate) error { return nil })
	require.ErrorContains(t, err, "header")
}

func TestCollectMarksRowsAndCompletionAndIgnoresBlankThumbnails(t *testing.T) {
	path := writeCSV(t, "FullName,ThumbUrl\nNo Photo,\nYamada Hanako,https://example.test/photo.jpg\n")
	var seen []string
	complete := false
	var got actresscache.Candidate
	err := New().Collect(context.Background(), actresscache.SourceOptions{
		Parameters:   map[string]string{"legacy.csv": path},
		MarkSeen:     func(key string) { seen = append(seen, key) },
		MarkComplete: func() { complete = true },
	}, func(candidate actresscache.Candidate) error {
		got = candidate
		return nil
	})
	require.NoError(t, err)
	require.Len(t, seen, 1)
	assert.True(t, complete)
	assert.Equal(t, "Hanako", got.FirstName)
	assert.Equal(t, "Yamada", got.LastName)
}

func TestCollectReturnsEmitError(t *testing.T) {
	path := writeCSV(t, "FullName,ThumbUrl\nOne,https://example.test/one.jpg\n")
	want := errors.New("emit failed")
	err := New().Collect(context.Background(), actresscache.SourceOptions{
		Parameters: map[string]string{"legacy.csv": path}, Workers: 2,
	}, func(actresscache.Candidate) error { return want })
	require.ErrorIs(t, err, want)
}

type cancelOnDoneContext struct {
	context.Context
	once     sync.Once
	done     chan struct{}
	canceled bool
}

func (c *cancelOnDoneContext) Done() <-chan struct{} {
	c.once.Do(func() {
		c.canceled = true
		close(c.done)
	})
	return c.done
}

func (c *cancelOnDoneContext) Err() error {
	if c.canceled {
		return context.Canceled
	}
	return nil
}

func TestCollectObservesCancellationBeforeFirstRow(t *testing.T) {
	ctx := &cancelOnDoneContext{Context: context.Background(), done: make(chan struct{})}
	err := New().Collect(ctx, actresscache.SourceOptions{
		Parameters: map[string]string{"legacy.csv": writeCSV(t, "FullName,ThumbUrl\nOne,https://example.test/one.jpg\n")},
	}, func(actresscache.Candidate) error { return nil })
	require.ErrorIs(t, err, context.Canceled)
}

func TestCollectObservesCancellationWhileReading(t *testing.T) {
	path := writeCSV(t, "FullName,ThumbUrl\nOne,https://example.test/one.jpg\nTwo,https://example.test/two.jpg\n")
	ctx, cancel := context.WithCancel(context.Background())
	var once sync.Once
	err := New().Collect(ctx, actresscache.SourceOptions{
		Parameters: map[string]string{"legacy.csv": path},
		MarkSeen:   func(string) { once.Do(cancel) },
	}, func(actresscache.Candidate) error { return nil })
	require.ErrorIs(t, err, context.Canceled)
}

func TestCollectRejectsDirectoryAsCSV(t *testing.T) {
	path := filepath.Join(t.TempDir(), "directory")
	require.NoError(t, os.Mkdir(path, 0o755))
	err := New().Collect(context.Background(), actresscache.SourceOptions{
		Parameters: map[string]string{"legacy.csv": path},
	}, func(actresscache.Candidate) error { return nil })
	require.ErrorContains(t, err, "header")
}

func TestCandidateFromRowHandlesShortAndMultiwordNames(t *testing.T) {
	candidate := candidateFromRow([]string{"Yamada Hana Ko"}, columnsForTest(), "source.csv", 9)
	assert.Equal(t, "Yamada", candidate.LastName)
	assert.Equal(t, "Hana Ko", candidate.FirstName)
	assert.Empty(t, candidate.ThumbURL)
	assert.Equal(t, "9", candidate.SourceID)
}

func TestCollectLimitTruncationSkipsMarkComplete(t *testing.T) {
	path := writeCSV(t, "FullName,ThumbUrl\nOne,https://example.test/one.jpg\nTwo,https://example.test/two.jpg\nThree,https://example.test/three.jpg\n")
	complete := false
	err := New().Collect(context.Background(), actresscache.SourceOptions{
		Parameters:   map[string]string{"legacy.csv": path},
		Limit:        2,
		MarkComplete: func() { complete = true },
	}, func(actresscache.Candidate) error { return nil })
	require.NoError(t, err)
	assert.False(t, complete, "limit-truncated enumeration is not complete")
}
