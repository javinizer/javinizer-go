package scraper

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	_ "github.com/javinizer/javinizer-go/internal/scraper/aventertainment"
	_ "github.com/javinizer/javinizer-go/internal/scraper/caribbeancom"
	_ "github.com/javinizer/javinizer-go/internal/scraper/dlgetchu"
	_ "github.com/javinizer/javinizer-go/internal/scraper/dmm"
	_ "github.com/javinizer/javinizer-go/internal/scraper/fc2"
	_ "github.com/javinizer/javinizer-go/internal/scraper/jav321"
	_ "github.com/javinizer/javinizer-go/internal/scraper/javbus"
	_ "github.com/javinizer/javinizer-go/internal/scraper/javdb"
	_ "github.com/javinizer/javinizer-go/internal/scraper/javlibrary"
	_ "github.com/javinizer/javinizer-go/internal/scraper/javstash"
	_ "github.com/javinizer/javinizer-go/internal/scraper/libredmm"
	_ "github.com/javinizer/javinizer-go/internal/scraper/mgstage"
	_ "github.com/javinizer/javinizer-go/internal/scraper/r18dev"
	_ "github.com/javinizer/javinizer-go/internal/scraper/tokyohot"

	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var expectedScraperNames = []string{
	"r18dev",
	"javbus",
	"dmm",
	"mgstage",
	"fc2",
	"javdb",
	"jav321",
	"caribbeancom",
	"dlgetchu",
	"libredmm",
	"tokyohot",
	"javlibrary",
	"javstash",
	"aventertainment",
}

func TestAllScrapersRegistered(t *testing.T) {
	for _, name := range expectedScraperNames {
		t.Run(name+"_registered", func(t *testing.T) {
			constructor := GetScraperConstructors()[name]
			assert.NotNil(t, constructor, "scraper %q should have a registered constructor", name)

			validator := scraperutil.GetValidator(name)
			assert.NotNil(t, validator, "scraper %q should have a registered validator", name)

			factory := scraperutil.GetConfigFactory(name)
			assert.NotNil(t, factory, "scraper %q should have a registered config factory", name)

			flatten := scraperutil.GetFlattenFunc(name)
			assert.NotNil(t, flatten, "scraper %q should have a registered flatten func", name)
		})
	}
}

func TestAllScrapersHaveDefaults(t *testing.T) {
	defaults := GetRegisteredDefaults()
	for _, name := range expectedScraperNames {
		t.Run(name+"_defaults", func(t *testing.T) {
			_, exists := defaults[name]
			assert.True(t, exists, "scraper %q should have registered defaults", name)
		})
	}
}

func TestAllScrapersHaveOptions(t *testing.T) {
	for _, name := range expectedScraperNames {
		t.Run(name+"_options", func(t *testing.T) {
			_, exists := scraperutil.GetScraperOptions(name)
			assert.True(t, exists, "scraper %q should have registered options", name)
		})
	}
}

func TestNoHttpclientGoFilesRemain(t *testing.T) {
	_, filename, _, _ := runtime.Caller(0)
	scraperDir := filepath.Dir(filename)

	httpclientFiles := []string{}
	for _, name := range expectedScraperNames {
		httpclientPath := filepath.Join(scraperDir, name, "httpclient.go")
		if _, err := os.Stat(httpclientPath); err == nil {
			httpclientFiles = append(httpclientFiles, httpclientPath)
		}
	}

	assert.Empty(t, httpclientFiles, "no httpclient.go files should remain in any scraper directory; found: %v", httpclientFiles)
}

func TestAllScrapersConfigFactoryProducesValidConfig(t *testing.T) {
	for _, name := range expectedScraperNames {
		t.Run(name+"_config_factory", func(t *testing.T) {
			factory := scraperutil.GetConfigFactory(name)
			require.NotNil(t, factory, "scraper %q should have config factory", name)

			cfg := factory()
			assert.NotNil(t, cfg, "scraper %q config factory should produce non-nil config", name)

			type enabledChecker interface {
				IsEnabled() bool
			}
			if checker, ok := cfg.(enabledChecker); ok {
				assert.False(t, checker.IsEnabled(), "default config for %q should have IsEnabled=false", name)
			}
		})
	}
}

func TestAllScrapersFlattenFuncProducesSettings(t *testing.T) {
	for _, name := range expectedScraperNames {
		t.Run(name+"_flatten_produces_settings", func(t *testing.T) {
			fn := scraperutil.GetFlattenFunc(name)
			require.NotNil(t, fn, "scraper %q should have flatten func", name)

			factory := scraperutil.GetConfigFactory(name)
			require.NotNil(t, factory, "scraper %q should have config factory", name)

			cfg := factory()
			result := fn(cfg)
			assert.NotNil(t, result, "flatten func for %q should produce non-nil result with default config", name)
		})
	}
}
