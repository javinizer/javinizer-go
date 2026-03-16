package nfo

import (
	"os"
	"testing"

	"github.com/spf13/afero"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReverseFormatNFO_Integration(t *testing.T) {
	// Create test NFO file
	nfoContent := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<movie>
    <title>Test Movie</title>
    <id>TEST-001</id>
    <actor>
        <name>Yui Hatano</name>
        <altname>波多野結衣</altname>
        <thumb>https://example.com/thumb.jpg</thumb>
    </actor>
</movie>`

	testFile := "/tmp/test_reverse_format_nfo.xml"
	err := os.WriteFile(testFile, []byte(nfoContent), 0644)
	require.NoError(t, err, "Should create test file")
	defer func() { _ = os.Remove(testFile) }()

	// Parse the reverse format NFO
	result, err := ParseNFO(afero.NewOsFs(), testFile)
	require.NoError(t, err, "Should parse NFO successfully")
	require.NotNil(t, result)
	require.NotNil(t, result.Movie)

	movie := result.Movie
	require.Len(t, movie.Actresses, 1, "Should have 1 actress")

	actress := movie.Actresses[0]

	// Verify reverse format: Name=Romanized, AltName=Japanese
	assert.Equal(t, "Yui", actress.FirstName, "FirstName from romanized Name field")
	assert.Equal(t, "Hatano", actress.LastName, "LastName from romanized Name field")
	assert.Equal(t, "波多野結衣", actress.JapaneseName, "JapaneseName from AltName field (reverse format)")
	assert.Equal(t, "https://example.com/thumb.jpg", actress.ThumbURL, "ThumbURL preserved")

	t.Logf("✅ Reverse format parsed correctly:")
	t.Logf("   Romanized: %s %s (from <name>)", actress.FirstName, actress.LastName)
	t.Logf("   Japanese:  %s (from <altname>)", actress.JapaneseName)
}
