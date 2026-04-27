package libredmm

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/configutil"
)

type LibreDMMConfig struct {
	config.BaseScraperConfig `yaml:",inline"`
	BaseURL                  string `yaml:"base_url" json:"base_url"`
}

func (c *LibreDMMConfig) ValidateConfig(sc *config.ScraperSettings) error {
	if err := config.ValidateCommonSettings("libredmm", sc); err != nil {
		return err
	}
	if err := configutil.ValidateHTTPBaseURL("libredmm.base_url", sc.BaseURL); err != nil {
		return err
	}
	return nil
}
