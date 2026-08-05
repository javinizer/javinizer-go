package legacycsvsource

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The row key must key on identity only: a changed thumbnail keeps the key,
// so transient failures on new content can never strand the old journal entry
// and prune the actress out of a build.
func TestCandidateKeyStableAcrossThumbnailChanges(t *testing.T) {
	cols := csvColumns{fullName: 0, lastName: 1, firstName: 2, japaneseName: 3, thumbURL: 4, alias: 5}
	base := []string{"First Last", "Last", "First", "姓 名", "https://cdn.example/old.jpg", "Alias"}
	changedThumb := []string{"First Last", "Last", "First", "姓 名", "https://cdn.example/new.jpg", "Alias"}
	a := candidateFromRow(base, cols, "https://dump.example/actresses.csv", 7)
	b := candidateFromRow(changedThumb, cols, "https://dump.example/actresses.csv", 7)
	assert.Equal(t, a.Key, b.Key, "thumbnail changes must not mint a new identity key")

	changedName := []string{"First Renamed", "Last", "First", "姓 名", "https://cdn.example/old.jpg", "Alias"}
	c := candidateFromRow(changedName, cols, "https://dump.example/actresses.csv", 7)
	assert.NotEqual(t, a.Key, c.Key, "identity changes still mint distinct keys")
}
