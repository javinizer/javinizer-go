package dmm

import (
	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

// Display extraction: label rows with too few cells or an empty normalized
// value never produce identities.
func TestExtractDisplayID_DisplayBranches(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<html><body><table>` +
		`<tr><td>品番：</td></tr>` +
		`<tr><td>品番：</td><td>ーーー</td></tr>` +
		`<tr><td>品番：</td><td>DV-818AI</td></tr>` +
		`</table></body></html>`))
	require.NoError(t, err)
	assert.Equal(t, "dv-818ai", extractDisplayID(doc))
}
