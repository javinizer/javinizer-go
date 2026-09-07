package organizer

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
)

type inPlaceNoRenameFolderStrategy struct {
	fs             afero.Fs
	config         *Config
	templateEngine template.EngineInterface
}

var _ OperationStrategy = (*inPlaceNoRenameFolderStrategy)(nil)

func newInPlaceNoRenameFolderStrategy(fs afero.Fs, cfg *Config, _ matcher.MatcherInterface, engine template.EngineInterface) *inPlaceNoRenameFolderStrategy {
	if engine == nil {
		engine = template.NewEngine()
	}
	return &inPlaceNoRenameFolderStrategy{
		fs:             fs,
		config:         cfg,
		templateEngine: engine,
	}
}

func (s *inPlaceNoRenameFolderStrategy) Plan(match models.FileMatchInfo, movie *models.Movie, destDir string, forceUpdate bool) (*OrganizePlan, error) {
	pc := buildPlanContext(s.config, s.templateEngine, movie, match)
	if pc.Err != nil {
		return nil, pc.Err
	}

	sourceDir := filepath.Dir(match.Path)
	targetDir := sourceDir
	targetPath := filepath.Join(targetDir, pc.FileName)
	willMove := filepath.ToSlash(match.Path) != filepath.ToSlash(targetPath)

	if s.config.MaxPathLength > 0 && len(targetPath) > s.config.MaxPathLength {
		excess := len(targetPath) - s.config.MaxPathLength
		ext := match.Extension
		currentNameLen := len(pc.FileName) - len(ext)
		if currentNameLen > excess && currentNameLen-excess > 0 {
			baseName := s.templateEngine.TruncateTitleBytes(strings.TrimSuffix(pc.FileName, ext), currentNameLen-excess)
			if baseName != "" {
				pc.FileName = template.SanitizeFilename(baseName) + ext
				targetPath = filepath.Join(targetDir, pc.FileName)
				willMove = filepath.ToSlash(match.Path) != filepath.ToSlash(targetPath)
			}
		}
	}

	if s.config.MaxPathLength > 0 {
		if err := s.templateEngine.ValidatePathLength(targetPath, s.config.MaxPathLength); err != nil {
			return nil, fmt.Errorf("path validation failed: %w", err)
		}
	}

	conflicts := checkTargetConflict(s.fs, match.Path, targetPath, forceUpdate, willMove)

	return &OrganizePlan{
		Match:               match,
		Movie:               movie,
		SourcePath:          match.Path,
		TargetDir:           targetDir,
		TargetFile:          pc.FileName,
		TargetPath:          targetPath,
		WillMove:            willMove,
		Conflicts:           conflicts,
		InPlace:             false,
		OldDir:              "",
		IsDedicated:         false,
		SkipInPlaceReason:   "in-place-norenamefolder mode - file rename only",
		FolderName:          "",
		SubfolderPath:       "",
		BaseFileName:        resolveBaseFileName(s.config, s.templateEngine, movie, match),
		PreserveSourcePath:  true,
		RenameFolder:        false,
		strategy:            strategyInPlaceNoRenameFolder,
		executeStrategy:     s,
		moveFiles:           true,
		overwriteAuthorized: forceUpdate,
	}, nil
}

func (s *inPlaceNoRenameFolderStrategy) Execute(plan *OrganizePlan) (*OrganizeResult, error) {
	result := &OrganizeResult{
		OriginalPath:           plan.SourcePath,
		NewPath:                plan.TargetPath,
		FolderPath:             plan.TargetDir,
		FileName:               plan.TargetFile,
		Moved:                  false,
		ShouldGenerateMetadata: true,
	}

	// overwroteOccupiedDest records that THIS execution replaced a
	// bytes-bearing destination the authorization suppressed — keyed to the
	// PUBLISH-BOUND replacement signal of the move's own publish (PR #249
	// codex P2): no-op and refused lanes never set it, and a
	// replacement-free failure answers false exactly like them — while a
	// failed publish that STILL displaced resident bytes keeps the crumb ON
	// the FAILED result (see foldMovePublishCrumb).
	overwroteOccupiedDest := false
	// Shared parent-directory lock + target-file lock (dir before file): an in-place
	// rename elsewhere drains shared holders before moving the directory, so this move
	// never lands inside a renamed (possibly about-to-rollback) directory — while
	// concurrent copies into the same directory stay parallel.
	err := withDestDirSharedLock(plan.TargetDir, func() error {
		return withDestFileLock(plan.TargetPath, func() error {
			if plan.overwriteAuthorized {
				identical, sameIn, err := classifyAuthorizedDestination(s.fs, plan.SourcePath, plan.TargetPath)
				if err != nil {
					return err
				}
				if identical || sameIn {
					return nil
				}
			} else {
				lexicalSelf, sameIn, err := refuseExistingDestination(s.fs, plan.SourcePath, plan.TargetPath)
				if err != nil {
					return err
				}
				if lexicalSelf || sameIn {
					return nil
				}
			}
			if !plan.overwriteAuthorized {
				if err := fsutil.MoveFileNoReplace(s.fs, plan.SourcePath, plan.TargetPath); err != nil {
					return mapNoReplaceRefusal(err, plan.TargetPath)
				}
				return nil
			}
			// Publish-bound crumb: the move's own publish reports whether
			// resident bytes were displaced AT the publish instant; the
			// retained crumb folds on the displacement answer ALONE, failure
			// included (see foldMovePublishCrumb).
			replaced, mErr := moveFileDestReplaced(s.fs, plan.SourcePath, plan.TargetPath)
			foldMovePublishCrumb(&overwroteOccupiedDest, replaced)
			if mErr != nil {
				return mErr
			}
			return nil
		})
	})
	if err != nil {
		result.Error = fmt.Errorf("failed to rename file: %w", err)
		// A failed publish that displaced resident bytes keeps its crumb on
		// the FAILED result (every failed-with-displacement class — see
		// foldMovePublishCrumb); Moved stays false, so the failure
		// journals ONCE as the failed-and-retained row, never
		// double-journaled as a clean replace.
		if overwroteOccupiedDest {
			result.Warnings = append(result.Warnings, authorizedOverwriteWarning(plan.TargetPath))
		}
		return result, result.Error
	}

	result.Moved = true
	// Force-overwrite audit crumb: the replace actually landed.
	if overwroteOccupiedDest {
		result.Warnings = append(result.Warnings, authorizedOverwriteWarning(plan.TargetPath))
	}

	return result, nil
}
