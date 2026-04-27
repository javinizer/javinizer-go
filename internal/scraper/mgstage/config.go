package mgstage

import (
	"github.com/javinizer/javinizer-go/internal/config"
)

type MGStageConfig struct {
	config.BaseScraperConfig `yaml:",inline"`
}

func (c *MGStageConfig) ValidateConfig(sc *config.ScraperSettings) error {
	return config.ValidateCommonSettings("mgstage", sc)
}
