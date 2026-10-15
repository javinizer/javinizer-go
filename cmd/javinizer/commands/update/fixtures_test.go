package update_test

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/testutil"
)

func createOfflineUpdateConfig(t *testing.T, withMedia bool, customize ...func(*config.Config)) (string, *testutil.LocalScraperFixture) {
	t.Helper()
	var fixture *testutil.LocalScraperFixture
	path, _ := testutil.CreateTestConfig(t, func(cfg *config.Config) {
		fixture = testutil.ConfigureLocalScraper(t, cfg, withMedia)
		for _, apply := range customize {
			apply(cfg)
		}
	})
	return path, fixture
}
