package dlgetchu

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/configutil"
)

type DLGetchuConfig struct {
	config.BaseScraperConfig `yaml:",inline"`
	BaseURL                  string `yaml:"base_url" json:"base_url"`
}

func (c *DLGetchuConfig) ValidateConfig(sc *config.ScraperSettings) error {
	if err := config.ValidateCommonSettings("dlgetchu", sc); err != nil {
		return err
	}
	if err := configutil.ValidateHTTPBaseURL("dlgetchu.base_url", sc.BaseURL); err != nil {
		return err
	}
	return nil
}
