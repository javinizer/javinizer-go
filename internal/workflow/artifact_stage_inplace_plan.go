package workflow

import (
	"fmt"
	"path/filepath"
	"strings"
)

func (s *artifactStage) setInPlaceFinalRoot(dir string) error {
	parent := filepath.Dir(filepath.Dir(s.sourcePath))
	rel, err := filepath.Rel(parent, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("staged path escapes staging area: %s", dir)
	}
	s.finalRoot = dir
	return nil
}
