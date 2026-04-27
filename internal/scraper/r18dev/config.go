package r18dev

import (
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
)

type R18DevConfig struct {
	config.BaseScraperConfig `yaml:",inline"`
	Language                 string `yaml:"language" json:"language"`
}

func (c *R18DevConfig) ValidateConfig(sc *config.ScraperSettings) error {
	if err := config.ValidateCommonSettings("r18dev", sc); err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(sc.Language)) {
	case "", "en":
	case "ja":
	default:
		return fmt.Errorf("r18dev: language must be 'en' or 'ja', got %q", sc.Language)
	}
	return nil
}
