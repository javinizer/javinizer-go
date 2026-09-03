package organizer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
)

type inPlaceStrategy struct {
	fs             afero.Fs
	config         *Config
	templateEngine template.EngineInterface
	matcher        matcher.MatcherInterface
}

var _ OperationStrategy = (*inPlaceStrategy)(nil)

func newInPlaceStrategy(fs afero.Fs, cfg *Config, m matcher.MatcherInterface, engine template.EngineInterface) *inPlaceStrategy {
	if engine == nil {
		engine = template.NewEngine()
	}
	return &inPlaceStrategy{
		fs:             fs,
		config:         cfg,
		templateEngine: engine,
		matcher:        m,
	}
}

func (s *inPlaceStrategy) isDedicatedFolder(dir string, id string, m matcher.MatcherInterface) (bool, error) {
	entries, err := afero.ReadDir(s.fs, dir)
	if err != nil {
		// Propagate the directory-read error instead of treating an unreadable
		// source folder as "not dedicated" (which could misclassify it and allow
		// an invalid move plan).
		return false, fmt.Errorf("failed to read source directory %q: %w", dir, err)
	}

	videoCount := 0
	matchingCount := 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		ext := strings.ToLower(filepath.Ext(entry.Name()))

		if !videoExtensions[ext] {
			continue
		}

		videoCount++

		matchedID := m.MatchString(entry.Name())
		if matchedID == id || strings.Contains(strings.ToUpper(entry.Name()), strings.ToUpper(id)) {
			matchingCount++
		}
	}

	return videoCount > 0 && videoCount == matchingCount, nil
}

func (s *inPlaceStrategy) Plan(match models.FileMatchInfo, movie *models.Movie, destDir string, forceUpdate bool) (*OrganizePlan, error) {
	pc := buildPlanContext(s.config, s.templateEngine, movie, match)
	if pc.Err != nil {
		return nil, pc.Err
	}

	sourceDir := filepath.Dir(match.Path)
	parentDir := filepath.Dir(sourceDir)

	var targetDir string
	targetPath := ""
	willMove := false

	inPlace := false
	oldDir := ""
	isDedicated := false
	skipInPlaceReason := ""

	if s.matcher != nil {
		var err error
		isDedicated, err = s.isDedicatedFolder(sourceDir, match.MovieID, s.matcher)
		if err != nil {
			return nil, err
		}
		_ = isDedicated
	} else {
		skipInPlaceReason = "matcher not set"
	}

	// Compute the budgeted folder name only when the folder will be used.
	// For non-dedicated in-place folders, the folder name is never used.
	folderName := pc.FolderName
	folderWillRename := s.config.OperationMode == operationmode.OperationModeOrganize || isDedicated
	if folderWillRename {
		// In in-place mode, the target is under the source directory tree,
		// so destDir is irrelevant for overhead calculation.
		baseDir := parentDir
		if s.config.OperationMode == operationmode.OperationModeOrganize && !isDedicated {
			baseDir = destDir
		}
		fullOverhead := filepath.Join(baseDir, "X", pc.FileName)
		overheadBytes := len(fullOverhead) - len("X")
		folderMaxBytes := 0
		if s.config.MaxPathLength > 0 && overheadBytes < s.config.MaxPathLength {
			folderMaxBytes = s.config.MaxPathLength - overheadBytes
		}
		if s.config.MaxPathLength > 0 && folderMaxBytes <= 0 {
			return nil, fmt.Errorf("path validation failed: directory and filename overhead (%d bytes) already exceeds max_path_length (%d); reduce the path or increase max_path_length", overheadBytes, s.config.MaxPathLength)
		}
		if folderMaxBytes > 0 {
			var err error
			folderName, err = s.templateEngine.ExecuteWithMaxBytes(s.config.FolderFormat, pc.Ctx, folderMaxBytes)
			if err != nil {
				return nil, fmt.Errorf("failed to generate folder name: %w", err)
			}
			folderName = template.SanitizeFolderPath(folderName)
			if folderName == "" {
				folderName = template.SanitizeFolderPath(match.MovieID)
				if folderName == "" {
					folderName = "unknown"
				}
			}
		}
	}

	if s.matcher != nil && isDedicated {
		currentFolderName := filepath.Base(sourceDir)
		if currentFolderName != folderName {
			inPlace = true
			oldDir = sourceDir
			targetDir = filepath.Join(filepath.Dir(sourceDir), folderName)
			targetPath = filepath.Join(targetDir, pc.FileName)
			willMove = true
		} else {
			skipInPlaceReason = "folder already has correct name"
		}
	} else if s.matcher == nil {
		skipInPlaceReason = "matcher not set"
	} else {
		skipInPlaceReason = "folder contains mixed IDs"
	}

	if !inPlace && s.config.OperationMode == operationmode.OperationModeOrganize {
		pathParts := []string{destDir}
		if folderName != "" {
			pathParts = append(pathParts, folderName)
		}
		targetDir = filepath.Join(pathParts...)
		targetPath = filepath.Join(targetDir, pc.FileName)
		willMove = filepath.ToSlash(match.Path) != filepath.ToSlash(targetPath)
	} else if !inPlace {
		targetDir = sourceDir
		targetPath = filepath.Join(targetDir, pc.FileName)
		willMove = filepath.ToSlash(match.Path) != filepath.ToSlash(targetPath)
	}

	if s.config.MaxPathLength > 0 {
		if err := s.templateEngine.ValidatePathLength(targetPath, s.config.MaxPathLength); err != nil {
			return nil, fmt.Errorf("path validation failed: %w", err)
		}
	}

	conflicts := checkTargetConflict(s.fs, match.Path, targetPath, forceUpdate, willMove)
	if inPlace && !forceUpdate {
		if stat, err := s.fs.Stat(targetDir); err == nil {
			oldStat, oldErr := s.fs.Stat(oldDir)
			if oldErr != nil {
				conflicts = append(conflicts, targetDir)
			} else if !os.SameFile(oldStat, stat) {
				conflicts = append(conflicts, targetDir)
			}
		}
	}

	return &OrganizePlan{
		Match:               match,
		Movie:               movie,
		SourcePath:          match.Path,
		TargetDir:           targetDir,
		TargetFile:          pc.FileName,
		TargetPath:          targetPath,
		WillMove:            willMove,
		Conflicts:           conflicts,
		InPlace:             inPlace,
		OldDir:              oldDir,
		IsDedicated:         isDedicated,
		SkipInPlaceReason:   skipInPlaceReason,
		FolderName:          folderName,
		SubfolderPath:       "",
		BaseFileName:        resolveBaseFileName(s.config, s.templateEngine, movie, match),
		PreserveSourcePath:  false,
		RenameFolder:        inPlace,
		strategy:            strategyInPlace,
		executeStrategy:     s,
		moveFiles:           true,
		overwriteAuthorized: forceUpdate,
	}, nil
}

func (s *inPlaceStrategy) Execute(plan *OrganizePlan) (*OrganizeResult, error) {
	result := &OrganizeResult{
		OriginalPath:           plan.SourcePath,
		NewPath:                plan.TargetPath,
		FolderPath:             plan.TargetDir,
		FileName:               plan.TargetFile,
		Moved:                  false,
		ShouldGenerateMetadata: true,
	}

	if plan.InPlace {
		// The WHOLE directory sequence — stat, renames, inner file step, and any rollback —
		// runs while holding the EXCLUSIVE TargetDir lock plus the inner TargetPath file
		// lock, acquired up front (dir before file): a sibling worker holding only the
		// child path's shared dir lock can never write a file between the directory
		// rename and the inner file step, so rollback can never displace a sibling's
		// entry. Unrelated writes into OTHER directories stay fully parallel.
		err := withDestDirExclusiveLock(plan.TargetDir, func() error {
			return withDestFileLock(plan.TargetPath, func() error {
				info, err := s.fs.Stat(plan.OldDir)
				if err != nil {
					return fmt.Errorf("failed to stat old directory: %w", err)
				}
				if !info.IsDir() {
					return fmt.Errorf("old path is not a directory: %s", plan.OldDir)
				}

				dirExists := false
				if lst, ok := s.fs.(afero.Lstater); ok {
					info, didLstat, statErr := lst.LstatIfPossible(plan.TargetDir)
					switch {
					case statErr == nil:
						dirExists = true
						// didLstat=false means info came from a link-following Stat: re-probe with
						// readlink so a non-dangling symlink (possibly pointing at OldDir, which
						// would pass the os.SameFile check below) is not treated as the real dir.
						if info.Mode()&os.ModeSymlink != 0 || (!didLstat && symlinkObjectExists(s.fs, plan.TargetDir)) {
							return fmt.Errorf("target directory is a symlink: %s", plan.TargetDir)
						}
					case !errors.Is(statErr, os.ErrNotExist):
						return fmt.Errorf("failed to check target directory: %w", statErr)
					case !didLstat && symlinkObjectExists(s.fs, plan.TargetDir):
						// link-following Stat fallback would hide a dangling symlink object
						return fmt.Errorf("target directory is a symlink: %s", plan.TargetDir)
					}
				} else if _, statErr := s.fs.Stat(plan.TargetDir); statErr == nil {
					if symlinkObjectExists(s.fs, plan.TargetDir) {
						return fmt.Errorf("target directory is a symlink: %s", plan.TargetDir)
					}
					dirExists = true
				} else if !errors.Is(statErr, os.ErrNotExist) {
					return fmt.Errorf("failed to check target directory: %w", statErr)
				} else if symlinkObjectExists(s.fs, plan.TargetDir) {
					return fmt.Errorf("target directory is a symlink: %s", plan.TargetDir)
				}
				if dirExists {
					oldInfo, oldErr := s.fs.Stat(plan.OldDir)
					sameDir := false
					if oldErr == nil {
						newInfo, newErr := s.fs.Stat(plan.TargetDir)
						// Same directory reached through another name (alias/symlink): falling
						// through is the no-op case — anything else is a real conflict.
						sameDir = newErr == nil && os.SameFile(oldInfo, newInfo)
					}
					if !sameDir {
						return fmt.Errorf("target directory already exists: %s", plan.TargetDir)
					}
				}

				if err := s.fs.Rename(plan.OldDir, plan.TargetDir); err != nil {
					return fmt.Errorf("failed to rename directory: %w", err)
				}

				result.InPlaceRenamed = true
				result.OldDirectoryPath = plan.OldDir
				result.NewDirectoryPath = plan.TargetDir

				oldFileName := plan.Match.Name
				if oldFileName == "" {
					oldFileName = filepath.Base(plan.SourcePath)
				}
				currentFilePath := filepath.Join(plan.TargetDir, oldFileName)
				if currentFilePath != plan.TargetPath {
					// Both destination locks are already held (see above), so no sibling can slip a
					// file into the renamed directory at plan.TargetPath between the directory
					// rename and this inner step — refuse-then-rename with rollback is atomic.
					if !plan.overwriteAuthorized {
						lexicalSelf, sameIn, e2 := refuseExistingDestination(s.fs, currentFilePath, plan.TargetPath)
						if e2 != nil {
							if rb := s.fs.Rename(plan.TargetDir, plan.OldDir); rb != nil {
								logging.Errorf("[in-place] Failed to rollback directory rename %s → %s: %v", plan.TargetDir, plan.OldDir, rb)
							}
							return e2
						}
						if lexicalSelf || sameIn {
							return nil // inner target names the same file — nothing to rename
						}
					}
					if err := s.fs.Rename(currentFilePath, plan.TargetPath); err != nil {
						if rb := s.fs.Rename(plan.TargetDir, plan.OldDir); rb != nil {
							logging.Errorf("[in-place] Failed to rollback directory rename %s → %s: %v", plan.TargetDir, plan.OldDir, rb)
						}
						return fmt.Errorf("failed to rename file after directory rename: %w", err)
					}
				}
				return nil
			})
		})
		if err != nil {
			result.Error = err
			return result, result.Error
		}
		result.Moved = true
		return result, nil
	} else {
		// Shared parent-directory lock + target-file lock (dir before file): an in-place
		// directory rename elsewhere drains shared holders before it may move the
		// directory, so this move can never land inside a renamed (possibly
		// about-to-rollback) directory.
		err := withDestDirSharedLock(plan.TargetDir, func() error {
			return withDestFileLock(plan.TargetPath, func() error {
				if !plan.overwriteAuthorized {
					lexicalSelf, sameIn, err := refuseExistingDestination(s.fs, plan.SourcePath, plan.TargetPath)
					if err != nil {
						return err
					}
					if lexicalSelf || sameIn {
						return nil
					}
				}
				if err := s.fs.MkdirAll(plan.TargetDir, config.DirPerm); err != nil {
					return fmt.Errorf("failed to create directory: %w", err)
				}
				return fsutil.MoveFileFs(s.fs, plan.SourcePath, plan.TargetPath)
			})
		})
		if err != nil {
			result.Error = fmt.Errorf("failed to move file: %w", err)
			return result, result.Error
		}

		result.Moved = true
	}

	return result, nil
}
