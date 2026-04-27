package javdb

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/configutil"
)

type JavDBConfig struct {
	config.BaseScraperConfig `yaml:",inline"`
	BaseURL                  string `yaml:"base_url" json:"base_url"`
}

func (c *JavDBConfig) ValidateConfig(sc *config.ScraperSettings) error {
	if err := config.ValidateCommonSettings("javdb", sc); err != nil {
		return err
	}
	if err := configutil.ValidateHTTPBaseURL("javdb.base_url", sc.BaseURL); err != nil {
		return err
	}
	return nil
}
