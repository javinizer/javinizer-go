package scraper

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

func TestRegisterAll(t *testing.T) {
	registry := scraperutil.NewScraperRegistry()
	RegisterAll(registry)
}
