package nfo

import (
	"encoding/xml"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddGenericRole(t *testing.T) {
	tests := []struct {
		name             string
		addGenericRole   bool
		expectedRoleText string
	}{
		{
			name:             "Config enabled",
			addGenericRole:   true,
			expectedRoleText: "Actress",
		},
		{
			name:             "Config disabled",
			addGenericRole:   false,
			expectedRoleText: "", // Empty role
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				ActorFirstNameOrder: true,
				AddGenericRole:      tt.addGenericRole,
			}
			gen := NewGenerator(cfg)

			movie := &models.Movie{
				ID:    "IPX-001",
				Title: "Test Movie",
				Actresses: []models.Actress{
					{FirstName: "Yui", LastName: "Hatano"},
					{FirstName: "Tsubasa", LastName: "Amami"},
				},
			}

			nfo := gen.MovieToNFO(movie, "")

			require.Len(t, nfo.Actors, 2)
			assert.Equal(t, "Yui Hatano", nfo.Actors[0].Name)
			assert.Equal(t, tt.expectedRoleText, nfo.Actors[0].Role)
			assert.Equal(t, "Tsubasa Amami", nfo.Actors[1].Name)
			assert.Equal(t, tt.expectedRoleText, nfo.Actors[1].Role)
		})
	}
}

func TestAltNameRole(t *testing.T) {
	tests := []struct {
		name         string
		altNameRole  bool
		japaneseName string
		expectedRole string
	}{
		{
			name:         "Config enabled with Japanese name",
			altNameRole:  true,
			japaneseName: "波多野結衣",
			expectedRole: "波多野結衣",
		},
		{
			name:         "Config disabled",
			altNameRole:  false,
			japaneseName: "波多野結衣",
			expectedRole: "", // Empty role
		},
		{
			name:         "Config enabled but no Japanese name",
			altNameRole:  true,
			japaneseName: "",
			expectedRole: "", // Empty role
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				ActorFirstNameOrder: true,
				AltNameRole:         tt.altNameRole,
			}
			gen := NewGenerator(cfg)

			movie := &models.Movie{
				ID:    "IPX-001",
				Title: "Test Movie",
				Actresses: []models.Actress{
					{
						FirstName:    "Yui",
						LastName:     "Hatano",
						JapaneseName: tt.japaneseName,
					},
				},
			}

			nfo := gen.MovieToNFO(movie, "")

			require.Len(t, nfo.Actors, 1)
			assert.Equal(t, "Yui Hatano", nfo.Actors[0].Name)
			assert.Equal(t, tt.expectedRole, nfo.Actors[0].Role)
		})
	}
}

func TestBothRoleOptions(t *testing.T) {
	t.Run("AltNameRole takes precedence over AddGenericRole", func(t *testing.T) {
		cfg := &Config{
			ActorFirstNameOrder: true,
			AddGenericRole:      true, // This should be overridden
			AltNameRole:         true,
		}
		gen := NewGenerator(cfg)

		movie := &models.Movie{
			ID:    "IPX-001",
			Title: "Test Movie",
			Actresses: []models.Actress{
				{
					FirstName:    "Yui",
					LastName:     "Hatano",
					JapaneseName: "波多野結衣",
				},
			},
		}

		nfo := gen.MovieToNFO(movie, "")

		require.Len(t, nfo.Actors, 1)
		assert.Equal(t, "Yui Hatano", nfo.Actors[0].Name)
		assert.Equal(t, "波多野結衣", nfo.Actors[0].Role, "AltNameRole should take precedence")
	})

	t.Run("AddGenericRole used when no Japanese name", func(t *testing.T) {
		cfg := &Config{
			ActorFirstNameOrder: true,
			AddGenericRole:      true,
			AltNameRole:         true, // Enabled but no Japanese name available
		}
		gen := NewGenerator(cfg)

		movie := &models.Movie{
			ID:    "IPX-001",
			Title: "Test Movie",
			Actresses: []models.Actress{
				{
					FirstName:    "Yui",
					LastName:     "Hatano",
					JapaneseName: "", // No Japanese name
				},
			},
		}

		nfo := gen.MovieToNFO(movie, "")

		require.Len(t, nfo.Actors, 1)
		assert.Equal(t, "Yui Hatano", nfo.Actors[0].Name)
		assert.Equal(t, "Actress", nfo.Actors[0].Role, "Should fall back to generic role")
	})
}

func TestRoleInXML(t *testing.T) {
	cfg := &Config{
		ActorFirstNameOrder: true,
		AddGenericRole:      true,
	}
	gen := NewGenerator(cfg)

	movie := &models.Movie{
		ID:    "IPX-001",
		Title: "Test Movie",
		Actresses: []models.Actress{
			{FirstName: "Yui", LastName: "Hatano"},
		},
	}

	nfo := gen.MovieToNFO(movie, "")

	// Marshal to XML
	xmlData, err := xml.MarshalIndent(nfo, "", "  ")
	require.NoError(t, err)

	xmlStr := string(xmlData)

	// Verify role appears in XML
	assert.True(t, strings.Contains(xmlStr, "<role>Actress</role>"), "XML should contain role element")
	assert.True(t, strings.Contains(xmlStr, "<name>Yui Hatano</name>"), "XML should contain actress name")
}

func TestAltNameRoleInXML(t *testing.T) {
	cfg := &Config{
		ActorFirstNameOrder: true,
		AltNameRole:         true,
	}
	gen := NewGenerator(cfg)

	movie := &models.Movie{
		ID:    "IPX-001",
		Title: "Test Movie",
		Actresses: []models.Actress{
			{
				FirstName:    "Yui",
				LastName:     "Hatano",
				JapaneseName: "波多野結衣",
			},
		},
	}

	nfo := gen.MovieToNFO(movie, "")

	// Marshal to XML
	xmlData, err := xml.MarshalIndent(nfo, "", "  ")
	require.NoError(t, err)

	xmlStr := string(xmlData)

	// Verify Japanese name in role field
	assert.True(t, strings.Contains(xmlStr, "<role>波多野結衣</role>"), "XML should contain Japanese name in role")
	assert.True(t, strings.Contains(xmlStr, "<name>Yui Hatano</name>"), "XML should contain English name")
}
