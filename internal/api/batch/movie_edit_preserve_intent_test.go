package batch

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestPreserveCachedActressesHonoursExplicitThumbnailIntent(t *testing.T) {
	baseline := &models.Movie{Actresses: []models.Actress{
		{ID: 1, FirstName: "Yui", LastName: "Hatano", ThumbURL: "https://old.test/thumb.jpg"},
	}}

	echo := &models.Movie{Actresses: []models.Actress{
		{ID: 1, FirstName: "Yui", LastName: "Hatano", ThumbURL: "https://old.test/thumb.jpg"},
	}}
	assert.True(t, shouldPreserveCachedActresses(echo, baseline), "an unchanged echo keeps cached actresses")

	explicit := &models.Movie{Actresses: []models.Actress{
		{ID: 1, FirstName: "Yui", LastName: "Hatano", ThumbURL: "https://old.test/thumb.jpg", ThumbEdited: true},
	}}
	assert.False(t, shouldPreserveCachedActresses(explicit, baseline), "an explicit thumbnail edit must take the guarded path")
}
