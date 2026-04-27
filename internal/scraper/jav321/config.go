package jav321

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/configutil"
)

type Jav321Config struct {
	config.BaseScraperConfig `yaml:",inline"`
	BaseURL                  string `yaml:"base_url" json:"base_url"`
}

func (c *Jav321Config) ValidateConfig(sc *config.ScraperSettings) error {
	if err := config.ValidateCommonSettings("jav321", sc); err != nil {
		return err
	}
	if err := configutil.ValidateHTTPBaseURL("jav321.base_url", sc.BaseURL); err != nil {
		return err
	}
	return nil
}
