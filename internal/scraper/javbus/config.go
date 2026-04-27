package javbus

import (
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/configutil"
)

type JavBusConfig struct {
	config.BaseScraperConfig `yaml:",inline"`
	Language                 string `yaml:"language" json:"language"`
	BaseURL                  string `yaml:"base_url" json:"base_url"`
}

func (c *JavBusConfig) ValidateConfig(sc *config.ScraperSettings) error {
	if err := config.ValidateCommonSettings("javbus", sc); err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(sc.Language)) {
	case "", "en":
	case "ja":
	case "zh":
	default:
		return fmt.Errorf("javbus: language must be 'en', 'ja', or 'zh', got %q", sc.Language)
	}
	if err := configutil.ValidateHTTPBaseURL("javbus.base_url", sc.BaseURL); err != nil {
		return err
	}
	return nil
}
