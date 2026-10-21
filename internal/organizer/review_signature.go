package organizer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
		RenderOptions   any
	}{FolderFormat: cfg.FolderFormat, FileFormat: cfg.FileFormat, SubfolderFormat: cfg.SubfolderFormat, MaxTitleLength: cfg.MaxTitleLength, MaxPathLength: cfg.MaxPathLength, RenameFile: cfg.RenameFile, Strategy: p.strategy, RenderOptions: cfg.templateRenderOptions()}
	body, err := json.Marshal(naming)
	if err != nil {
		return "unavailable"
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}
