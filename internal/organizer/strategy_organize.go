package organizer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
)

// operationLocks serializes destination-touching operations per destination so a
// concurrent batch worker cannot slip a file between the existence check and the move
// (TOCTOU). Entries are ref-counted: a lock stays registered while any goroutine waits
// on it and is removed only once the last reference drops, which is race-free (unlike
// try-lock delete flows that can unlink an entry with a waiter or a replacement).
// Known residual (centralized hardening tracked separately as #224): keys are
// lexically cleaned only, so symlink/case aliases of one destination can acquire
// different locks.
type destFileLock struct {
	mu   sync.Mutex
	refs int
}

type destFileLocker struct {
	mu    sync.Mutex
	items map[string]*destFileLock
}

var operationLocks destFileLocker

func withDestFileLock(path string, fn func() error) error {
	key := filepath.Clean(path)
	operationLocks.mu.Lock()
	if operationLocks.items == nil {
		operationLocks.items = make(map[string]*destFileLock)
	}
	l := operationLocks.items[key]
	if l == nil {
		l = &destFileLock{}
		operationLocks.items[key] = l
	}
	l.refs++
	operationLocks.mu.Unlock()

	l.mu.Lock()
	defer func() {
		l.mu.Unlock()
		operationLocks.mu.Lock()
		l.refs--
		if l.refs == 0 {
			if operationLocks.items[key] == l {
				delete(operationLocks.items, key)
			}
		}
		operationLocks.mu.Unlock()
	}()
	return fn()
}

// dirOperationLocks coordinate directory-scope operations: a directory RENAME
// (in-place organize) takes the exclusive lock, while any child-file operation inside
// that directory takes the shared lock. Shared locks let unrelated copies into one
// directory proceed concurrently; a rename (or its rollback) waits until all child
// writes have drained, so it can never drag a freshly landed file to a path its owner
// no longer expects. Entries are ref-counted exactly like operationLocks.
type destDirLock struct {
	mu   sync.RWMutex
	refs int
}

type destDirLocker struct {
	mu    sync.Mutex
	items map[string]*destDirLock
}

var dirOperationLocks destDirLocker

func withDestDirLock(dir string, exclusive bool, fn func() error) error {
	key := filepath.Clean(dir)
	dirOperationLocks.mu.Lock()
	if dirOperationLocks.items == nil {
		dirOperationLocks.items = make(map[string]*destDirLock)
	}
	l := dirOperationLocks.items[key]
	if l == nil {
		l = &destDirLock{}
		dirOperationLocks.items[key] = l
	}
	l.refs++
	dirOperationLocks.mu.Unlock()

	if exclusive {
		l.mu.Lock()
	} else {
		l.mu.RLock()
	}
	defer func() {
		if exclusive {
			l.mu.Unlock()
		} else {
			l.mu.RUnlock()
		}
		dirOperationLocks.mu.Lock()
		l.refs--
		if l.refs == 0 && dirOperationLocks.items[key] == l {
			delete(dirOperationLocks.items, key)
		}
		dirOperationLocks.mu.Unlock()
	}()
	return fn()
}

// withDestDirExclusiveLock serializes a directory-rename operation against every
// child write already inside that directory and vice versa.
func withDestDirExclusiveLock(dir string, fn func() error) error {
	return withDestDirLock(dir, true, fn)
}

// withDestDirSharedLock lets independent child writes into one directory run in
// parallel while excluding a concurrent rename of the directory itself.
func withDestDirSharedLock(dir string, fn func() error) error {
	return withDestDirLock(dir, false, fn)
}

// refuseExistingDestination enforces no-clobber at execution time with lexical-self vs
// hardlink-alias distinction: identical lexical paths (./file vs file) are identical=true —
// operators must not touch the destination at all; a DIFFERENT path to the same inode
// (hardlink alias) is sameInode=true and may no-op when its output type is satisfied.
// A destination symlink object (even dangling) or an existing directory always conflicts;
// so does a different file. Same-directory ENTRY aliases (path reaching the identical
// directory entry through lexically distinct routes like symlinks in ancestors or case
// folding on a case-insensitive FS) are identical=true so they're never removed.
// Identity is evaluated with no-follow Lstat on both sides; the source's own symlink
// status is Mint vital — a source symlink pointing at the destination's regular inode
// is NOT an alias (it doesn't share that destination's name-bearing link).
func refuseExistingDestination(fs afero.Fs, src, dst string) (identical, sameInode bool, err error) {
	if filepath.Clean(src) == filepath.Clean(dst) {
		return true, true, nil
	}
	var lstatDst, lstatSrc os.FileInfo
	var dstErr, srcErr error
	// dstLstat/srcLstat record whether the filesystem actually performed an Lstat.
	// Symlink checks are trustworthy only when true; when false the values are Stat-based.
	var dstLstat, srcLstat bool
	if lst, ok := fs.(afero.Lstater); ok {
		var didDst, didSrc bool
		lstatDst, didDst, dstErr = lst.LstatIfPossible(dst)
		lstatSrc, didSrc, srcErr = lst.LstatIfPossible(src)
		dstLstat, srcLstat = didDst, didSrc
	} else {
		lstatDst, dstErr = fs.Stat(dst)
		lstatSrc, srcErr = fs.Stat(src)
	}
	// A filesystem whose Stat follows links reports a DANGLING destination symlink as
	// absent. Whenever the destination came from a following lookup (no Lstater, or
	// LstatIfPossible reporting didLstat=false), probe ReadlinkIfPossible so an
	// unauthorized operation never silently replaces a symlink object.
	if dstErr != nil && errors.Is(dstErr, os.ErrNotExist) && !dstLstat && symlinkObjectExists(fs, dst) {
		return false, false, fmt.Errorf("file already exists at destination (refusing to overwrite): %s", dst)
	}
	if dstErr == nil {
		if dstLstat && lstatDst.Mode()&os.ModeSymlink != 0 {
			return false, false, fmt.Errorf("file already exists at destination (refusing to overwrite): %s", dst)
		}
		if lstatDst.IsDir() {
			return false, false, fmt.Errorf("file already exists at destination (refusing to overwrite): %s", dst)
		}
		if srcErr != nil || (srcLstat && lstatSrc.Mode()&os.ModeSymlink != 0) || !os.SameFile(lstatSrc, lstatDst) {
			return false, false, fmt.Errorf("file already exists at destination (refusing to overwrite): %s", dst)
		}
		return false, true, nil
	}
	if !errors.Is(dstErr, os.ErrNotExist) {
		return false, false, fmt.Errorf("failed to check destination: %w", dstErr)
	}
	return false, false, nil
}

// symlinkObjectExists probes path specifically for a symlink OBJECT (including dangling
// ones) on filesystems whose Stat follows links — where a dangling symlink otherwise
// masquerades as not-exists.
func symlinkObjectExists(fs afero.Fs, path string) bool {
	lr, ok := fs.(afero.LinkReader)
	if !ok {
		return false
	}
	_, err := lr.ReadlinkIfPossible(path)
	return err == nil
}

// pathExistsBestEffort reports whether path names any directory entry — file, directory,
// or symlink object (even dangling) — regardless of whether the filesystem supports a
// true Lstat; a Stat-following lookup alone would miss a dangling symlink.
func pathExistsBestEffort(fs afero.Fs, path string) (bool, error) {
	if lst, ok := fs.(afero.Lstater); ok {
		_, didLstat, err := lst.LstatIfPossible(path)
		switch {
		case err == nil:
			return true, nil
		case !errors.Is(err, os.ErrNotExist):
			return false, err
		case didLstat:
			// true Lstat miss: genuinely absent — no fallback probe needed
			return false, nil
		}
		// didLstat=false: fs fell back to a link-following Stat; a dangling symlink
		// would hide here, so probe readlink below.
	} else if _, err := fs.Stat(path); err == nil {
		return true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	return symlinkObjectExists(fs, path), nil
}

type organizeStrategy struct {
	fs             afero.Fs
	config         *Config
	templateEngine template.EngineInterface
	linker         linker
}

var _ OperationStrategy = (*organizeStrategy)(nil)

func newOrganizeStrategy(fs afero.Fs, cfg *Config, engine template.EngineInterface, linker linker) *organizeStrategy {
	if engine == nil {
		engine = template.NewEngine()
	}
	if linker == nil {
		linker = OSLinker{}
	}
	return &organizeStrategy{
		fs:             fs,
		config:         cfg,
		templateEngine: engine,
		linker:         linker,
	}
}

func (s *organizeStrategy) Plan(match models.FileMatchInfo, movie *models.Movie, destDir string, forceUpdate bool) (*OrganizePlan, error) {
	pc := buildPlanContext(s.config, s.templateEngine, movie, match)
	if pc.Err != nil {
		return nil, pc.Err
	}

	subfolderParts := make([]string, 0, len(s.config.SubfolderFormat))
	for _, subfolderTemplate := range s.config.SubfolderFormat {
		subfolderName, err := s.templateEngine.Execute(subfolderTemplate, pc.Ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to generate subfolder from template '%s': %w", subfolderTemplate, err)
		}
		subfolderName = template.SanitizeFolderPath(subfolderName)
		if subfolderName != "" {
			subfolderParts = append(subfolderParts, subfolderName)
		}
	}

	pathParts := []string{destDir}
	pathParts = append(pathParts, subfolderParts...)
	overheadBase := filepath.Join(pathParts...)
	// Use a placeholder folder + filename to compute actual separator overhead
	fullOverhead := filepath.Join(overheadBase, "X", pc.FileName)
	overheadBytes := len(fullOverhead) - 1
	folderMaxBytes := 0
	if s.config.MaxPathLength > 0 && overheadBytes < s.config.MaxPathLength {
		folderMaxBytes = s.config.MaxPathLength - overheadBytes
	}
	if s.config.MaxPathLength > 0 && folderMaxBytes <= 0 {
		return nil, fmt.Errorf("path validation failed: destination directory and filename overhead (%d bytes) already exceeds max_path_length (%d); reduce the destination path or increase max_path_length", overheadBytes, s.config.MaxPathLength)
	}

	folderName := pc.FolderName
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

	pathParts = append(pathParts, folderName)
	targetDir := filepath.Join(pathParts...)
	targetPath := filepath.Join(targetDir, pc.FileName)

	if s.config.MaxPathLength > 0 {
		if err := s.templateEngine.ValidatePathLength(targetPath, s.config.MaxPathLength); err != nil {
			return nil, fmt.Errorf("path validation failed: %w", err)
		}
	}

	willMove := filepath.ToSlash(match.Path) != filepath.ToSlash(targetPath)

	conflicts := checkTargetConflict(s.fs, match.Path, targetPath, forceUpdate, willMove)

	var subfolderPath string
	if len(subfolderParts) > 0 {
		subfolderPath = filepath.Join(subfolderParts...)
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
		InPlace:             false,
		OldDir:              "",
		IsDedicated:         false,
		SkipInPlaceReason:   "organize mode - always move to destination",
		FolderName:          folderName,
		SubfolderPath:       subfolderPath,
		BaseFileName:        resolveBaseFileName(s.config, s.templateEngine, movie, match),
		PreserveSourcePath:  false,
		RenameFolder:        false,
		strategy:            strategyOrganize,
		executeStrategy:     s,
		moveFiles:           true,
		overwriteAuthorized: forceUpdate,
	}, nil
}

func (s *organizeStrategy) Execute(plan *OrganizePlan) (*OrganizeResult, error) {
	result := &OrganizeResult{
		OriginalPath:           plan.SourcePath,
		NewPath:                plan.TargetPath,
		FolderPath:             plan.TargetDir,
		FileName:               plan.TargetFile,
		Moved:                  false,
		ShouldGenerateMetadata: true,
	}

	// No-op: source already at target, nothing to do
	if !plan.WillMove {
		return result, nil
	}

	// Move path: moveFiles=true (default) — rename source to target
	if plan.moveFiles {
		move := func() error {
			if !plan.overwriteAuthorized {
				identical, sameIn, err := refuseExistingDestination(s.fs, plan.SourcePath, plan.TargetPath)
				if err != nil {
					return err
				}
				if identical || sameIn {
					return nil // same path or same file — nothing to move
				}
			}

			if err := s.fs.MkdirAll(plan.TargetDir, config.DirPerm); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}

			return fsutil.MoveFileFs(s.fs, plan.SourcePath, plan.TargetPath)
		}

		// Shared dir lock: concurrent organizes into one directory proceed in parallel
		// (only the per-file lock serializes same-file collisions), while an in-place
		// directory rename elsewhere drains us before it may move the directory.
		err := withDestDirSharedLock(plan.TargetDir, func() error {
			return withDestFileLock(plan.TargetPath, move)
		})
		if err != nil {
			result.Error = err
			return result, result.Error
		}

		result.Moved = true
		return result, nil
	}

	// Copy/link path (absorbed from CopyWithLinkMode)
	if len(plan.Conflicts) > 0 {
		result.Error = fmt.Errorf("conflicts detected: %s", strings.Join(plan.Conflicts, "; "))
		return result, result.Error
	}

	result.ShouldGenerateMetadata = true

	if !plan.LinkMode.IsValid() {
		result.Error = fmt.Errorf("unsupported link mode %q", plan.LinkMode)
		return result, result.Error
	}

	// Every destination-touching step runs under the destination lock: unauthorized
	// paths guard inside it (a plain copy would otherwise overwrite a late-created file),
	// and authorized Remove+link work must serialize against concurrent guarded calls.
	// The shared parent-directory lock keeps concurrent copies into the same directory
	// parallel while an in-place directory rename (exclusive holder) drains us first.
	err := withDestDirSharedLock(plan.TargetDir, func() error {
		return withDestFileLock(plan.TargetPath, func() error {
			dstSameInode := false
			dstLexicalSelf := false
			// Always classify: lexical-self aliases must never be removed (any authorization
			// mode), and unauthorized operations refuse any different-file destination.
			{
				self, sameIn, err := refuseExistingDestination(s.fs, plan.SourcePath, plan.TargetPath)
				if err != nil {
					if !plan.overwriteAuthorized {
						return err
					}
					// Authorized mode: classification failures are benign (overwrite intended).
				} else {
					dstLexicalSelf = self
					dstSameInode = sameIn
				}
			}

			if err := s.fs.MkdirAll(plan.TargetDir, config.DirPerm); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}

			// Remove an existing target ONLY for an authorized replacement. Unauthorized
			// paths never remove (their conversion output of choice must be won via
			// refusal-and-fail). A lexical self-path is also never removed.
			// Same-inode hardlinks under unauthorized mode are idempotent (returned early).
			if plan.LinkMode != LinkModeNone && !dstLexicalSelf && plan.overwriteAuthorized {
				if err := s.fs.Remove(plan.TargetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("failed to prepare target path for link: %w", err)
				}
			}

			if dstLexicalSelf || (dstSameInode && !plan.overwriteAuthorized && plan.LinkMode == LinkModeHard) {
				return nil // self-path or already-satisfied hardlink output — idempotent
			}

			switch plan.LinkMode {
			case LinkModeHard:
				if err := s.linker.hardlink(plan.SourcePath, plan.TargetPath); err != nil {
					if errors.Is(err, syscall.EXDEV) {
						return fmt.Errorf("failed to create hard link (source and destination must be on the same filesystem): %w", err)
					}
					if errors.Is(err, os.ErrPermission) {
						return fmt.Errorf("failed to create hard link (permission denied): %w", err)
					}
					return fmt.Errorf("failed to create hard link: %w", err)
				}
			case LinkModeSoft:
				linkTarget := plan.SourcePath
				if !filepath.IsAbs(linkTarget) {
					abs, err := filepath.Abs(linkTarget)
					if err != nil {
						return fmt.Errorf("failed to resolve source path for symlink: %w", err)
					}
					linkTarget = abs
				}
				if err := s.linker.symlink(linkTarget, plan.TargetPath); err != nil {
					if errors.Is(err, os.ErrPermission) {
						return fmt.Errorf("failed to create soft link%s: %w", softLinkPermDeniedHint, err)
					}
					return fmt.Errorf("failed to create soft link: %w", err)
				}
			default:
				if dstSameInode && !plan.overwriteAuthorized {
					return nil
				}
				if err := s.linker.copyFile(s.fs, plan.SourcePath, plan.TargetPath); err != nil {
					return fmt.Errorf("failed to copy file: %w", err)
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
	result.ShouldGenerateMetadata = true

	return result, nil
}
