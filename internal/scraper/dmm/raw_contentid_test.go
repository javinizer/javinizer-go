package dmm

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveContentID_RawContentIDInput reproduces a bug where passing a raw
// content_id (e.g. "118abf030") as manual input fails to match DMM search
// results. The DMM search finds cid=118abf030, extractContentIDCandidates
// cleans it to "abf030" (prefix stripped), but matchIDs only contained
// ["118abf030", "abf30", "abf00030"] — none of which equal "abf030".
//
// The fix adds the prefix-stripped search query to matchIDs.
func TestResolveContentID_RawContentIDInput(t *testing.T) {
	dbCfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := database.New(&database.Config{Type: dbCfg.Database.Type, DSN: dbCfg.Database.DSN, LogLevel: dbCfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	repo := database.NewContentIDMappingRepository(db)
	settings := createTestSettings(true, nil)
	scraper := newScraper(&settings, testGlobalProxy, testGlobalFlareSolverr, dmmOptions{ScrapeActress: false, Browser: models.BrowserConfig{Enabled: false, Timeout: 30}, ContentIDRepo: repo})

	transport := &searchVariationRoundTripper{
		responseByQuery: map[string]string{
			"118abf030": `<html><body>
				<a href="/digital/videoa/-/detail/=/cid=118abf030/">ABF-030 result</a>
			</body></html>`,
		},
	}
	scraper.client.SetTransport(transport)

	contentID, err := scraper.ResolveContentID("118abf030")
	require.NoError(t, err)
	assert.Equal(t, "abf030", contentID)
}
