package nfo_test

import (
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseNFO_InvalidDatePreservesMetadataWithWarning(t *testing.T) {
	fs := afero.NewMemMapFs()
	const path = "IPX-535.nfo"
	const content = `<movie><id>IPX-535</id><title>IPX-535</title><director>NFO Director</director><runtime>90</runtime><releasedate>not-a-date</releasedate></movie>`
	require.NoError(t, afero.WriteFile(fs, path, []byte(content), 0600))

	result, err := nfo.ParseNFO(fs, path)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Movie)
	assert.Equal(t, path, result.Source)
	assert.Equal(t, "IPX-535", result.NFOTitle)
	assert.Equal(t, "IPX-535", result.Movie.ID)
	assert.Equal(t, "IPX-535", result.Movie.ContentID)
	assert.Equal(t, "IPX-535", result.Movie.Title)
	assert.Equal(t, "NFO Director", result.Movie.Director)
	assert.Equal(t, 90, result.Movie.Runtime)
	assert.Nil(t, result.Movie.ReleaseDate)
	require.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0], `failed to parse date "not-a-date"`)
	unchanged, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.Equal(t, content, string(unchanged))
}

func TestParseNFO_BoundedXMLDocument(t *testing.T) {
	const limit = 1 << 20 // The public parser contract limits input to 1 MiB.
	const document = `<movie><id>IPX-535</id><title>Bounded title</title><runtime>90</runtime></movie>`
	for _, tt := range []struct {
		name    string
		padding int
		wantErr bool
	}{
		{"document ends below limit", limit - len(document) - 1, false},
		{"document ends at limit", limit - len(document), false},
		{"document starts beyond limit", limit, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			const path = "padded.nfo"
			// Leading XML whitespace is valid. In the oversized case the bounded
			// reader reaches EOF before the root, although the actual file is a
			// complete document. An empty file would not test this size boundary.
			content := strings.Repeat(" ", tt.padding) + document
			require.NoError(t, afero.WriteFile(fs, path, []byte(content), 0600))
			result, err := nfo.ParseNFO(fs, path)
			if tt.wantErr {
				require.EqualError(t, err, "NFO file exceeds maximum size of 1048576 bytes")
				assert.Nil(t, result, "do not return partial metadata for a truncated document")
				return
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotNil(t, result.Movie)
			assert.Empty(t, result.Warnings)
			assert.Equal(t, "IPX-535", result.Movie.ID)
			assert.Equal(t, "Bounded title", result.Movie.Title)
			assert.Equal(t, 90, result.Movie.Runtime)
		})
	}
}
