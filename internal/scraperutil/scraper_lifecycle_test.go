package scraperutil_test

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

type closingScraper struct {
	mockScraper
	closes atomic.Int32
	err    error
}

func (s *closingScraper) Close() error { s.closes.Add(1); return s.err }

func TestRegistryCloseAllOnceEvenOnError(t *testing.T) {
	r := scraperutil.NewScraperRegistry()
	want := errors.New("cleanup failed")
	a := &closingScraper{mockScraper: mockScraper{name: "a"}, err: want}
	b := &closingScraper{mockScraper: mockScraper{name: "b"}}
	r.RegisterInstance(a)
	r.RegisterInstance(b)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if !errors.Is(r.Close(), want) {
				t.Error("lost cleanup failure")
			}
		})
	}
	wg.Wait()
	require.EqualValues(t, 1, a.closes.Load())
	require.EqualValues(t, 1, b.closes.Load())
	var nilRegistry *scraperutil.ScraperRegistry
	require.NoError(t, nilRegistry.Close())
}
