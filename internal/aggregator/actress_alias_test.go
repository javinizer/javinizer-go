package aggregator

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActressAliasConversion(t *testing.T) {
	// Create in-memory database
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  "file::memory:?cache=shared",
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"r18dev", "dmm"},
			},
			ActressDatabase: config.ActressDatabaseConfig{
				Enabled:      true,
				ConvertAlias: true,
			},
		},
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev", "dmm"},
		},
	}

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)

	// Create alias repository and add test aliases
	aliasRepo := database.NewActressAliasRepository(db)

	// Alias: "Yui Hatano" -> "Hatano Yui"
	err = aliasRepo.Create(context.TODO(), &models.ActressAlias{
		AliasName:     "Yui Hatano",
		CanonicalName: "Hatano Yui",
	})
	require.NoError(t, err)

	// Alias: "Jun Amamiya" -> "Amamiya Jun"
	err = aliasRepo.Create(context.TODO(), &models.ActressAlias{
		AliasName:     "Jun Amamiya",
		CanonicalName: "Amamiya Jun",
	})
	require.NoError(t, err)

	// Alias: Japanese name conversion
	err = aliasRepo.Create(context.TODO(), &models.ActressAlias{
		AliasName:     "波多野結衣",
		CanonicalName: "はたのゆい",
	})
	require.NoError(t, err)

	// Create alias resolver for ActressMerger tests
	resolver := newAliasResolverWithCache(MetadataConfigFromApp(&cfg.Metadata), nil, map[string]string{
		"Yui Hatano":  "Hatano Yui",
		"Jun Amamiya": "Amamiya Jun",
		"波多野結衣":       "はたのゆい",
	})

	t.Run("Convert FirstName LastName alias", func(t *testing.T) {
		merger := newActressMerger()
		sources := []actressSource{
			{
				Source: "r18dev",
				Actresses: []models.ActressInfo{
					{
						FirstName: "Yui",
						LastName:  "Hatano",
					},
				},
			},
		}
		opts := actressMergeOptions{
			Priority:      []string{"r18dev"},
			AliasResolver: resolver,
		}

		actresses := merger.Merge(sources, opts)

		require.Len(t, actresses, 1)
		// Should be converted from "Yui Hatano" to "Hatano Yui"
		// After conversion: LastName="Hatano" (family), FirstName="Yui" (given)
		assert.Equal(t, "Yui", actresses[0].FirstName)
		assert.Equal(t, "Hatano", actresses[0].LastName)
		// Most importantly: FullName() should return canonical "Hatano Yui"
		assert.Equal(t, "Hatano Yui", actresses[0].FullName())
	})

	t.Run("Convert Japanese name alias", func(t *testing.T) {
		merger := newActressMerger()
		sources := []actressSource{
			{
				Source: "r18dev",
				Actresses: []models.ActressInfo{
					{
						JapaneseName: "波多野結衣",
					},
				},
			},
		}
		opts := actressMergeOptions{
			Priority:      []string{"r18dev"},
			AliasResolver: resolver,
		}

		actresses := merger.Merge(sources, opts)

		require.Len(t, actresses, 1)
		// Japanese name should be converted
		assert.Equal(t, "はたのゆい", actresses[0].JapaneseName)
	})

	t.Run("No conversion when alias not found", func(t *testing.T) {
		merger := newActressMerger()
		sources := []actressSource{
			{
				Source: "r18dev",
				Actresses: []models.ActressInfo{
					{
						FirstName: "Unknown",
						LastName:  "Actress",
					},
				},
			},
		}
		opts := actressMergeOptions{
			Priority:      []string{"r18dev"},
			AliasResolver: resolver,
		}

		actresses := merger.Merge(sources, opts)

		require.Len(t, actresses, 1)
		// Should remain unchanged
		assert.Equal(t, "Unknown", actresses[0].FirstName)
		assert.Equal(t, "Actress", actresses[0].LastName)
	})

	t.Run("Conversion disabled", func(t *testing.T) {
		resolverNoConvert := newAliasResolverWithCache(&MetadataConfig{
			ActressDatabase: actressDatabaseConfigView{
				Enabled:      true,
				ConvertAlias: false,
			},
		}, nil, map[string]string{
			"Yui Hatano": "Hatano Yui",
		})

		merger := newActressMerger()
		sources := []actressSource{
			{
				Source: "r18dev",
				Actresses: []models.ActressInfo{
					{
						FirstName: "Yui",
						LastName:  "Hatano",
					},
				},
			},
		}
		opts := actressMergeOptions{
			Priority:      []string{"r18dev"},
			AliasResolver: resolverNoConvert,
		}

		actresses := merger.Merge(sources, opts)

		require.Len(t, actresses, 1)
		// Should NOT be converted
		assert.Equal(t, "Yui", actresses[0].FirstName)
		assert.Equal(t, "Hatano", actresses[0].LastName)
	})

	t.Run("Conversion disabled when actress database is disabled", func(t *testing.T) {
		resolverNoDB := newAliasResolverWithCache(&MetadataConfig{
			ActressDatabase: actressDatabaseConfigView{
				Enabled:      false,
				ConvertAlias: true,
			},
		}, nil, map[string]string{
			"Yui Hatano": "Hatano Yui",
		})

		merger := newActressMerger()
		sources := []actressSource{
			{
				Source: "r18dev",
				Actresses: []models.ActressInfo{
					{
						FirstName: "Yui",
						LastName:  "Hatano",
					},
				},
			},
		}
		opts := actressMergeOptions{
			Priority:      []string{"r18dev"},
			AliasResolver: resolverNoDB,
		}

		actresses := merger.Merge(sources, opts)
		require.Len(t, actresses, 1)

		// Should NOT be converted because actress_database.enabled=false.
		assert.Equal(t, "Yui", actresses[0].FirstName)
		assert.Equal(t, "Hatano", actresses[0].LastName)
		assert.Equal(t, "Hatano Yui", actresses[0].FullName())
	})

	t.Run("Multiple actresses with mixed conversion", func(t *testing.T) {
		merger := newActressMerger()
		sources := []actressSource{
			{
				Source: "r18dev",
				Actresses: []models.ActressInfo{
					{
						FirstName: "Yui",
						LastName:  "Hatano",
					},
					{
						FirstName: "Unknown",
						LastName:  "Actress",
					},
					{
						FirstName: "Jun",
						LastName:  "Amamiya",
					},
				},
			},
		}
		opts := actressMergeOptions{
			Priority:      []string{"r18dev"},
			AliasResolver: resolver,
		}

		actresses := merger.Merge(sources, opts)

		require.Len(t, actresses, 3)

		// Find each actress and verify conversion using FullName()
		for _, actress := range actresses {
			fullName := actress.FullName()
			switch fullName {
			case "Hatano Yui":
				// Converted from "Yui Hatano" to canonical "Hatano Yui"
				assert.Equal(t, "Yui", actress.FirstName)
				assert.Equal(t, "Hatano", actress.LastName)
			case "Actress Unknown":
				// Not converted (no alias) - FullName() returns LastName + " " + FirstName
				assert.Equal(t, "Unknown", actress.FirstName)
				assert.Equal(t, "Actress", actress.LastName)
			case "Amamiya Jun":
				// Converted from "Jun Amamiya" to canonical "Amamiya Jun"
				assert.Equal(t, "Jun", actress.FirstName)
				assert.Equal(t, "Amamiya", actress.LastName)
			}
		}
	})
}

func TestActressAliasWithAggregate(t *testing.T) {
	// Full integration test with Aggregate() method
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  "file::memory:?cache=shared",
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"r18dev"},
			},
			ActressDatabase: config.ActressDatabaseConfig{
				Enabled:      true,
				ConvertAlias: true,
			},
		},
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
	}

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)

	// Add aliases
	aliasRepo := database.NewActressAliasRepository(db)
	err = aliasRepo.Create(context.TODO(), &models.ActressAlias{
		AliasName:     "Yui Hatano",
		CanonicalName: "Hatano Yui",
	})
	require.NoError(t, err)

	agg := newAggregatorWithRepos(testConfigFromAppConfig(cfg),
		database.NewGenreReplacementRepository(db),
		database.NewWordReplacementRepository(db),
		database.NewActressAliasRepository(db),
	)

	results := []*models.ScraperResult{
		{
			Source: "r18dev",
			ID:     "IPX-001",
			Title:  "Test Movie",
			Actresses: []models.ActressInfo{
				{
					FirstName: "Yui",
					LastName:  "Hatano",
				},
			},
		},
	}

	movie, _, err := agg.Aggregate(results)

	require.NotNil(t, movie)
	require.Len(t, movie.Actresses, 1)

	// Verify alias conversion happened - "Yui Hatano" -> "Hatano Yui"
	assert.Equal(t, "Yui", movie.Actresses[0].FirstName)
	assert.Equal(t, "Hatano", movie.Actresses[0].LastName)
	// Most importantly: FullName() should return canonical form
	assert.Equal(t, "Hatano Yui", movie.Actresses[0].FullName())
}
