package r18dev

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGuardRemasterResult_RawFold(t *testing.T) {
	res := &models.ScraperResult{ID: "RCT-156HD", ContentID: "1rct00156hd"}
	out, err := guardRemasterResult("1rct00156hd", res)
	require.NoError(t, err)
	assert.Equal(t, "RCT-156H", out.ID)
	assert.Equal(t, "1rct00156hd", out.ContentID)

	wide := &models.ScraperResult{ID: "ABC-123456HD", ContentID: "1abc123456h"}
	outWide, err := guardRemasterResult("1abc123456h", wide)
	require.NoError(t, err)
	assert.Equal(t, "ABC-123456H", outWide.ID)

	assert.True(t, cidMatchesMarker("1abc123456h", "h", "abc"))
	assert.True(t, cidMatchesMarker("436abc123456h", "h", "abc"))
	assert.False(t, cidMatchesMarker("1abc123456h", "h", "xyz"))
}
