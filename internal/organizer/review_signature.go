package organizer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/javinizer/javinizer-go/internal/template"
)

// ReviewNamingSignature fingerprints the naming configuration used by the plan.
func (p *OrganizePlan) ReviewNamingSignature() string {
	var cfg *Config
	switch strategy := p.executeStrategy.(type) {
	case *organizeStrategy:
		cfg = strategy.config
	case *inPlaceStrategy:
		cfg = strategy.config
	case *inPlaceNoRenameFolderStrategy:
		cfg = strategy.config
	}
	if cfg == nil {
		return ""
	}
	naming := struct {
		FolderFormat    string
		FileFormat      string
		SubfolderFormat []string
		MaxTitleLength  int
		MaxPathLength   int
		RenameFile      bool
		Strategy        strategyType
		RenderOptions   template.RenderOptions
	}{FolderFormat: cfg.FolderFormat, FileFormat: cfg.FileFormat, SubfolderFormat: cfg.SubfolderFormat, MaxTitleLength: cfg.MaxTitleLength, MaxPathLength: cfg.MaxPathLength, RenameFile: cfg.RenameFile, Strategy: p.strategy, RenderOptions: cfg.templateRenderOptions()}
	body, _ := json.Marshal(naming)
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}
