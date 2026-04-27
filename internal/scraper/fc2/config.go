package fc2

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/configutil"
)

type FC2Config struct {
	config.BaseScraperConfig `yaml:",inline"`
	BaseURL                  string `yaml:"base_url" json:"base_url"`
}

func (c *FC2Config) ValidateConfig(sc *config.ScraperSettings) error {
	if err := config.ValidateCommonSettings("fc2", sc); err != nil {
		return err
	}
	if err := configutil.ValidateHTTPBaseURL("fc2.base_url", sc.BaseURL); err != nil {
		return err
	}
	return nil
}
