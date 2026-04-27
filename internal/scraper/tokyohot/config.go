package tokyohot

import (
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/configutil"
)

type TokyoHotConfig struct {
	config.BaseScraperConfig `yaml:",inline"`
	Language                 string `yaml:"language" json:"language"`
	BaseURL                  string `yaml:"base_url" json:"base_url"`
}

func (c *TokyoHotConfig) ValidateConfig(sc *config.ScraperSettings) error {
	if err := config.ValidateCommonSettings("tokyohot", sc); err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(sc.Language)) {
	case "", "en", "ja", "zh":
	default:
		return fmt.Errorf("tokyohot: language must be 'en', 'ja', or 'zh', got %q", sc.Language)
	}
	if err := configutil.ValidateHTTPBaseURL("tokyohot.base_url", sc.BaseURL); err != nil {
		return err
	}
	return nil
}
