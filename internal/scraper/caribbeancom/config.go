package caribbeancom

import (
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/configutil"
)

type CaribbeancomConfig struct {
	config.BaseScraperConfig `yaml:",inline"`
	Language                 string `yaml:"language" json:"language"`
	BaseURL                  string `yaml:"base_url" json:"base_url"`
}

func (c *CaribbeancomConfig) ValidateConfig(sc *config.ScraperSettings) error {
	if err := config.ValidateCommonSettings("caribbeancom", sc); err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(sc.Language)) {
	case "", "ja", "en":
	default:
		return fmt.Errorf("caribbeancom: language must be 'ja' or 'en', got %q", sc.Language)
	}
	if err := configutil.ValidateHTTPBaseURL("caribbeancom.base_url", sc.BaseURL); err != nil {
		return err
	}
	return nil
}
