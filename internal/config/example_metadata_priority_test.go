package config_test

import (
	"path/filepath"
	"testing"

	config "github.com/javinizer/javinizer-go/internal/config"
)

// metadataFieldsShippedByConfig lists every per-field priority key that the
// Web UI / aggregator consults. A fresh `javinizer init` config must ship each
// of these as ABSENT or an empty [] so the field inherits `scrapers.priority`
// (World A semantics) instead of locking into a stale hardcoded list.
//
// Regression guard for issue #105: the embedded config.yaml.example previously
// shipped every field as [dmm, r18dev, libredmm], which (a) excluded scrapers
// the user later enabled (e.g. mgstage) and (b) caused persist failures for
// titles only found on those excluded scrapers ("content_id is required when
// using ContentID as primary key").
var metadataFieldsShippedByConfig = []string{
	"id", "content_id", "title", "original_title", "description",
	"release_date", "runtime", "actress", "genre", "director",
	"maker", "label", "series", "rating", "cover_url",
	"poster_url", "screenshot_url", "trailer_url",
}

// TestExampleConfigShipsInheritedFieldPriorities verifies the embedded example
// config (what `javinizer init` writes verbatim) does NOT lock any per-field
// metadata priority into a non-empty override. Every field must be ABSENT or
// empty [] so it inherits `scrapers.priority` and automatically picks up
// scrapers the user enables later.
func TestExampleConfigShipsInheritedFieldPriorities(t *testing.T) {
	examplePath := filepath.Join("..", "..", "configs", "config.yaml.example")

	cfg, err := config.Load(examplePath)
	if err != nil {
		t.Fatalf("failed to load config.yaml.example: %v", err)
	}

	for _, field := range metadataFieldsShippedByConfig {
		override := cfg.Metadata.Priority.PerFieldOverride(field)
		if len(override) > 0 {
			t.Errorf(
				"metadata.priority.%s ships as %v — a non-empty per-field override locks the "+
					"field out of inheriting scrapers.priority (issue #105). Ship [] or omit the key.",
				field, override,
			)
		}
	}
}
