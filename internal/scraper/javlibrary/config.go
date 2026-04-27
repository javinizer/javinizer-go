package javlibrary

import (
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/configutil"
)

type JavLibraryConfig struct {
	config.BaseScraperConfig `yaml:",inline"`
	Language                 string            `yaml:"language" json:"language"`
	BaseURL                  string            `yaml:"base_url" json:"base_url"`
	Cookies                  map[string]string `yaml:"cookies,omitempty" json:"cookies,omitempty"`
}

func (c *JavLibraryConfig) ValidateConfig(sc *config.ScraperSettings) error {
	if err := config.ValidateCommonSettings("javlibrary", sc); err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(sc.Language)) {
	case "", "en", "ja", "cn", "tw":
	default:
		return fmt.Errorf("javlibrary: language must be 'en', 'ja', 'cn', or 'tw', got %q", sc.Language)
	}
	if err := configutil.ValidateHTTPBaseURL("javlibrary.base_url", sc.BaseURL); err != nil {
		return err
	}
	return nil
}
