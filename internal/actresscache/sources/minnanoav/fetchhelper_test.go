package minnanoavsource

import "github.com/javinizer/javinizer-go/internal/actresscache"

// mustFetch unwraps a fetcher construction result, panicking on failure
// (test-only template.Must-style helper).
func mustFetch(f *actresscache.Fetcher, err error) *actresscache.Fetcher {
	if err != nil {
		panic(err)
	}
	return f
}
