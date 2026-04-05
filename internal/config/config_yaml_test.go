package config_test

import (
	"os"
	"path/filepath"
	"testing"

	config "github.com/javinizer/javinizer-go/internal/config"
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
)

func TestConfigYAMLLoadAndRoundTrip(t *testing.T) {
	repoRoot := filepath.Join("..", "..", "configs", "config.yaml")

	cfg, err := config.Load(repoRoot)
	if err != nil {
		t.Fatalf("Failed to load config.yaml: %v", err)
	}

	if cfg.Scrapers.Priority == nil || len(cfg.Scrapers.Priority) == 0 {
		t.Error("Scrapers.Priority should not be empty")
	}

	expectedScrapers := []string{"r18dev", "dmm", "mgstage", "javlibrary", "javdb", "javbus", "jav321", "tokyohot", "aventertainment", "dlgetchu", "libredmm", "caribbeancom", "fc2", "javstash"}
	for _, scraper := range expectedScrapers {
		if _, ok := cfg.Scrapers.Overrides[scraper]; !ok {
			t.Errorf("Scraper %q not found in Overrides", scraper)
		}
	}

	tmpDir := t.TempDir()
	tmpPath := filepath.Join(tmpDir, "config.yaml")
	err = config.Save(cfg, tmpPath)
	if err != nil {
		t.Fatalf("Failed to save config to temp file: %v", err)
	}

	reloaded, err := config.Load(tmpPath)
	if err != nil {
		t.Fatalf("Failed to load re-saved config: %v", err)
	}

	if len(reloaded.Scrapers.Priority) != len(cfg.Scrapers.Priority) {
		t.Errorf("Priority length mismatch after round-trip: got %d, want %d",
			len(reloaded.Scrapers.Priority), len(cfg.Scrapers.Priority))
	}

	for _, scraper := range expectedScrapers {
		if _, ok := reloaded.Scrapers.Overrides[scraper]; !ok {
			t.Errorf("Scraper %q not found in Overrides after round-trip", scraper)
		}
	}
}

func TestConfigYAMLScraperFlareSolverr(t *testing.T) {
	repoRoot := filepath.Join("..", "..", "configs", "config.yaml")

	cfg, err := config.Load(repoRoot)
	if err != nil {
		t.Fatalf("Failed to load config.yaml: %v", err)
	}

	scrapersWithFlareSolverr := []string{"r18dev", "dmm", "mgstage", "javlibrary", "javdb", "javbus", "jav321", "tokyohot", "aventertainment", "dlgetchu", "libredmm", "caribbeancom", "fc2"}

	for _, scraper := range scrapersWithFlareSolverr {
		scraperCfg, ok := cfg.Scrapers.Overrides[scraper]
		if !ok {
			t.Errorf("Scraper %q not found in Overrides", scraper)
			continue
		}
		if scraperCfg == nil {
			t.Errorf("Scraper %q config is nil", scraper)
			continue
		}
		t.Logf("%s: UseFlareSolverr=%v", scraper, scraperCfg.UseFlareSolverr)
	}
}

func TestGeneratedConfigLoadable(t *testing.T) {
	repoRoot := filepath.Join("..", "..", "configs", "config.yaml")

	if _, err := os.Stat(repoRoot); os.IsNotExist(err) {
		t.Skip("configs/config.yaml not found, skipping integration test")
	}

	cfg, err := config.Load(repoRoot)
	if err != nil {
		t.Fatalf("configs/config.yaml is not loadable: %v", err)
	}

	for name, scraperCfg := range cfg.Scrapers.Overrides {
		if scraperCfg == nil {
			t.Errorf("Scraper %q has nil config (possible unmarshal issue)", name)
		}
	}

	for name, scraperCfg := range cfg.Scrapers.Overrides {
		if scraperCfg != nil {
			_ = scraperCfg.UseFlareSolverr
			t.Logf("%s: UseFlareSolverr=%v", name, scraperCfg.UseFlareSolverr)
		}
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("configs/config.yaml validation failed: %v", err)
	}
}
