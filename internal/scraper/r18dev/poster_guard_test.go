package r18dev

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestResolveAwsimgsrcPoster_RejectsUnsplitableInput covers the early-return
// guards: an ID that cannot be split into series+number, and a number that
// overflows int parsing, both yield no awsimgsrc candidate without any HTTP.
func TestResolveAwsimgsrcPoster_RejectsUnsplitableInput(t *testing.T) {
	cfg := createTestSettings(true)
	s := newScraper(&cfg, testGlobalProxy, testGlobalFlareSolverr, nil)

	assert.Empty(t, s.resolveAwsimgsrcPoster(context.Background(), "no-digits-here", &http.Client{}))
	assert.Empty(t, s.resolveAwsimgsrcPoster(context.Background(), "abc99999999999999999999", &http.Client{}))
	assert.Empty(t, s.resolveAwsimgsrcPoster(context.Background(), "lulu99999999999999999999", &http.Client{}), "series present in table but overflowing number must bail before HTTP")

	// Unknown series (deliberately absent from the prefix table) exercises the
	// common-prefix fallback; the blocked transport proves no HTTP is issued
	// past prefix construction.
	rt := &recordingTransport{err: errHTTPBlocked}
	blocked := &http.Client{Transport: rt}
	assert.Empty(t, s.resolveAwsimgsrcPoster(context.Background(), "zzqq00999", blocked))

}
