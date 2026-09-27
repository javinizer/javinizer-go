package r18dev

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A T-28123-HD query must not accept a lookup whose explicit DVD ID is the
// conflicting T28-123-HD: compact foldDisplay erases the separator-pinned
// T/T28 boundary, so markerVariationAccept compares parsed identity tuples.
func TestMarkerVariationAcceptTupleComparison(t *testing.T) {
	body := func(dvdid string) []byte {
		return []byte(`{"content_id":"t28123h","dvd_id":"` + dvdid + `"}`)
	}

	assert.False(t, markerVariationAccept(
		[]byte(`{"content_id":"t28123h","dvd_id":"T28-123-HD"}`), "T-28123-HD", "h", "t"))
	assert.True(t, markerVariationAccept(
		[]byte(`{"content_id":"t28123h","dvd_id":"T-28123-HD"}`), "T-28123-HD", "h", "t"))
	_ = body
}
