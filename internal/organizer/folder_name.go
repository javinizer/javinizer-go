package organizer

import (
	"fmt"
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/template"
)

// folderNameBudget counts bytes, including separators and any nested subfolders
// already included in baseDir. Zero means unlimited or exhausted; callers retain
// their strategy-specific exhausted-budget errors.
func folderNameBudget(baseDir, fileName string, maxPathLength int) (budget, overhead int) {
	overhead = len(filepath.Join(baseDir, "X", fileName)) - len("X")
	if maxPathLength > 0 && overhead < maxPathLength {
		budget = maxPathLength - overhead
	}
	return budget, overhead
}

// renderBudgetedFolderName preserves the initial template result when unlimited.
// The initial Execute remains in buildPlanContext, even for unused folder names.
func renderBudgetedFolderName(engine template.EngineInterface, format string, pc planContext, movieID string, maxBytes int) (string, error) {
	if maxBytes <= 0 {
		return pc.FolderName, nil
	}
	name, err := engine.ExecuteWithMaxBytes(format, pc.Ctx, maxBytes)
	if err != nil {
		return "", fmt.Errorf("failed to generate folder name: %w", err)
	}
	return sanitizeFolderNameWithFallback(name, movieID), nil
}

func sanitizeFolderNameWithFallback(name, movieID string) string {
	name = template.SanitizeFolderPath(name)
	if name == "" {
		name = template.SanitizeFolderPath(movieID)
		if name == "" {
			name = folderFallbackUnknown
		}
	}
	return name
}
