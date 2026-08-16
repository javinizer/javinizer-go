package config

import (
	"encoding/json"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestCovFinal_MaxRetriesJSONError(t *testing.T) {
	s := &ScrapersConfig{Overrides: map[string]*models.ScraperSettings{}}
	raw := map[string]json.RawMessage{
		"max_retries": json.RawMessage(`"not_a_number"`),
	}
	ss := &models.ScraperSettings{}
	err := s.applyJSONAliases(raw, ss)
	assert.Error(t, err)
}
