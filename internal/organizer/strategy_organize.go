package organizer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
)

// authorizedOverwriteWarning renders the force-overwrite audit crumb: an
// overwrite-authorized terminal leg replaced a bytes-bearing destination.
// Wired lanes (all keyed to PUBLISH-BOUND occupancy evidence — the publish's
// own bound replacement signal (organize/in-place moves, organize copy,
// inner rename via the fsutil DestReplaced verbs, PR #249 codex P2) or the
// occupancy proven at adjacency with the destructive Remove for the link
// install lane — never to plan-time or classify-time state): organize move,
// organize copy, organize link install (Remove+link replaces resident bytes
// at the destination), the in-place strategy's inner file rename and
// non-rename file move, and the in-place-norenamefolder file rename. Composition
// follows the authorized-duplicate warning (duplicates.go) so the whole
// OrganizeResult.Warnings pipeline carries it verbatim — the CLI prints it
// per-file next to the dup warnings, the eventlog persists one organize warn
// entry, and the worker's history writer folds it into the organize history
// metadata.
func authorizedOverwriteWarning(targetPath string) string {
	return fmt.Sprintf("overwrite authorized: replaced existing destination %s", targetPath)
}

// Destination locking is unified on ONE process-wide registry,
// fsutil.SharedDestLocks (#224 phase D): every organizer-reachable terminal
// operation — file moves/copies/links, subtitle installs, MkdirAll directory
// creation, and in-place directory renames — serializes per destination with
// the downloader's install paths and the history reverter, not just with
// other organizer legs. The registry folds key case/separator spelling and
// refcounts entries to zero-GC; locks remain an intra-process ordering aid —
// the atomic no-replace publish syscalls stay the cross-process safety net.
// File destinations ride the shared file tier; the directory tier is
// key-namespaced inside the same registry (fsutil dirLockTierSuffix), so the
// universal holding pattern — ancestor DIRECTORY before descendant FILE — can
// never re-acquire a key the goroutine already holds, even for degenerate
// plans whose cleaned TargetPath equals TargetDir (sync.RWMutex has no
// re-entrancy).
func withDestFileLock(path string, fn func() error) error {
	release := fsutil.SharedDestLocks().Acquire(path)
	defer release()
	return fn()
}

// withDestDirExclusiveLock serializes a directory-rename operation against every
// child write already inside that directory and vice versa.
func withDestDirExclusiveLock(dir string, fn func() error) error {
	release := fsutil.SharedDestLocks().AcquireDirExclusive(dir)
	defer release()
	return fn()
}

// withDestDirSharedLock lets independent child writes into one directory run in
// parallel while excluding a concurrent rename of the directory itself.
func withDestDirSharedLock(dir string, fn func() error) error {
	release := fsutil.SharedDestLocks().AcquireDirShared(dir)
	defer release()
	return fn()
}

// refuseExistingDestination classifies destination state at execution time with lexical-self vs
// #224: this remains the CLASSIFICATION authority (no-op/conflict kinds); the
// no-clobber guarantee itself is carried syscall-atomically by the fsutil
// no-replace composites on every unauthorized terminal leg — a foreign writer
// landing after this classification is refused at publish, not overwritten.
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
// classifyExistingDestination inspects the destination with kind information so
// authorization decisions can be scoped per-kind (from #224 Phase C). It is the
// single classification authority; refuseExistingDestination renders its
// refusal sentence for unauthorized flows; authorized flows consult its kinds
// (symlink/dir always conflict)
// Lanes: identical (lexical self — never touched), sameInode (hardlink
// alias — no-op), occupied+kind, or unoccupied.
// destinationClassification is the four-way outcome of looking at the
// destination: identical (lexical-self — never touch), sameInode (hardlink
// alias — no-op), Conflict (with Kind), or unoccupied (Err==nil,
// Conflict==nil). Struct return avoids the (conflict != nil, err == nil)
// shape nilerr hates.
type destinationClassification struct {
	Identical bool
	SameInode bool
	Conflict  *PlanConflict
	Err       error
}

func classifyExistingDestination(fs afero.Fs, src, dst string) destinationClassification {
	if filepath.Clean(src) == filepath.Clean(dst) {
		return destinationClassification{Identical: true, SameInode: true}
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
		return destinationClassification{Conflict: &PlanConflict{Path: dst, Kind: ConflictSymlink}}
	}
	if dstErr == nil {
		if lstatDst.Mode()&os.ModeSymlink != 0 {
			return destinationClassification{Conflict: &PlanConflict{Path: dst, Kind: ConflictSymlink}}
		}
		// A following Stat succeeded but never truly lstat'd the object — a
		// VMSyed/fs boundary could misreport a live symlink as a plain file.
		// Probe for one when no true Lstat happened (#224 codex P2).
		if !dstLstat && symlinkObjectExists(fs, dst) {
			return destinationClassification{Conflict: &PlanConflict{Path: dst, Kind: ConflictSymlink}}
		}
		if lstatDst.IsDir() {
			return destinationClassification{Conflict: &PlanConflict{Path: dst, Kind: ConflictDirectory}}
		}
		if srcErr != nil || (srcLstat && lstatSrc.Mode()&os.ModeSymlink != 0) || !os.SameFile(lstatSrc, lstatDst) {
			return destinationClassification{Conflict: &PlanConflict{Path: dst, Kind: ConflictFile}}
		}
		return destinationClassification{SameInode: true}
	}
	if !errors.Is(dstErr, os.ErrNotExist) {
		return destinationClassification{Err: fmt.Errorf("failed to check destination: %w", dstErr)}
	}
	return destinationClassification{}
}

// refuseExistingDestination is the unauthorized-lane classifier: identical
// (never touch), sameInode means alias no-op, and conflicts map onto the Kind
// taxonomy (classifyExistingDestination).
func refuseExistingDestination(fs afero.Fs, src, dst string) (identical, sameInode bool, err error) {
	c := classifyExistingDestination(fs, src, dst)
	if c.Err != nil {
		return false, false, c.Err
	}
	if c.Conflict != nil {
		return false, false, fmt.Errorf("file already exists at destination (refusing to overwrite): %s", c.Conflict.Path)
	}
	return c.Identical, c.SameInode, nil
}

// mapNoReplaceRefusal translates the fsutil no-replace failure classes into the
// organizer's existing failure vocabulary (#224): occupancy becomes the same
// conflict the classifier reports, while a volume that cannot express an
// atomic no-replace publish surfaces as a DISTINCT infrastructure refusal —
// never as a content conflict.
// destMaybeLstat probes dst without ever following links where the fs can,
// and says whether the probing was truly no-follow. When didLstat=false the
// info came from a link-following Stat (wrapping non-Lister or Listers whose
// deferred Lstat gave up), so a caller CANNOT tell a live symlink from a
// regular file from info alone — it must probe via readlink separately.
func destMaybeLstat(fs afero.Fs, dst string) (os.FileInfo, bool, error) {
	if lst, ok := fs.(afero.Lstater); ok {
		info, did, lerr := lst.LstatIfPossible(dst)
		return info, !did, lerr
	}
	info, err := fs.Stat(dst)
	return info, true, err
}

func mapNoReplaceRefusal(err error, dst string) error {
	switch {
	// Completed-first (#224 P2): a cleanup-refusal error carries publish
	// refusal classes transitively (collision/unsupported inside rmErr); it
	// must NEVER be reported as a pre-publish refusal — the destination was
	// already published and the source is preserved.
	case fsutil.PublishCompleted(err):
		return fmt.Errorf("published to destination but source cleanup refused (both files preserved): %s: %w", dst, err)
	case errors.Is(err, fsutil.ErrPublishNoReplaceUnsupported):
		return fmt.Errorf("destination volume cannot express an atomic no-clobber write (no-replace unsupported): %s: %w", dst, err)
	case errors.Is(err, fsutil.ErrPublishCollision):
		return fmt.Errorf("file already exists at destination (refusing to overwrite): %s", dst)
	default:
		return err
	}
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
				folderName = folderFallbackUnknown
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
	// Target dir exists as a regular FILE today — nothing can be created under
	// it, and organizing must surface this as a directory conflict through
	// plan conflicts (not a deep MkdirAll failure) (#224 task 3.3).
	if willMove {
		if fi, statErr := s.fs.Stat(targetDir); statErr == nil && !fi.IsDir() {
			conflicts = append(conflicts, PlanConflict{Path: targetDir, Kind: ConflictDirectory})
		}
	}

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
		// overwroteOccupiedDest records that THIS execution replaced a
		// bytes-bearing destination the authorization suppressed, keyed to
		// the PUBLISH-BOUND replacement signal of the move's own publish
		// (PR #249 codex P2): an occupant vacated before the publish never
		// crumbs, an occupant planted before the publish always does — even
		// inside the classify → publish window under held (process-local)
		// locks. The no-op (identical / same-inode) and refused lanes never
		// set it, and a failed publish discards it by returning before the
		// warning.
		overwroteOccupiedDest := false
		move := func() error {
			if plan.overwriteAuthorized {
				// Authorized: still classify (#224 Phase C) — symlink/dir dests
				// refuse regardless of authorization; file dests replace; self
				// and same-inode stay no-ops even here. The classification
				// carries NO crumb evidence anymore: the publish's own bound
				// replacement signal keys it below.
				identical, sameIn, err := classifyAuthorizedDestination(s.fs, plan.SourcePath, plan.TargetPath)
				if err != nil {
					return err
				}
				if identical || sameIn {
					return nil
				}
			} else {
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

			if !plan.overwriteAuthorized {
				// #224: the atomic no-replace composite — a foreign writer
				// claiming the name after classification conflicts atomically
				// instead of being replaced by the rename inside the window.
				if err := fsutil.MoveFileNoReplace(s.fs, plan.SourcePath, plan.TargetPath); err != nil {
					return mapNoReplaceRefusal(err, plan.TargetPath)
				}
				return nil
			}
			// Publish-bound crumb: the move's own publish reports whether an
			// occupied destination not aliasing the source was displaced AT the
			// publish instant — the same-device leg probes at rename adjacency,
			// the cross-device fallback inherits the staged publish's bound
			// signal — so a foreign plant/vacate after the classification above
			// can neither forge nor hide it.
			replaced, mErr := fsutil.MoveFileFsDestReplaced(s.fs, plan.SourcePath, plan.TargetPath)
			if mErr != nil {
				// Partial-publish ambiguity (PR #249 codex P2 follow-up — F1): the
				// typed ErrPublishCompleted leg means the cross-device publish
				// LANDED and only the source cleanup refused; fsutil carries its
				// displaced-occupancy answer through that result, so the audit crumb
				// must not be dropped with the error — Apply journals the
				// publish-completed ambiguity as an event nonetheless, and its
				// replacement evidence rides the FAILED result below. Every other
				// failure leg discarded nothing because nothing landed (fsutil
				// answers replaced=false there).
				if fsutil.PublishCompleted(mErr) && replaced {
					overwroteOccupiedDest = true
				}
				return mErr
			}
			overwroteOccupiedDest = replaced
			return nil
		}

		// Shared dir lock: concurrent organizes into one directory proceed in parallel
		// (only the per-file lock serializes same-file collisions), while an in-place
		// directory rename elsewhere drains us before it may move the directory.
		err := withDestDirSharedLock(plan.TargetDir, func() error {
			return withDestFileLock(plan.TargetPath, move)
		})
		if err != nil {
			result.Error = err
			// The publish-completed failure keeps its crumb: the destination
			// really carries this operation's bytes — displaced resident bytes
			// included — so the audit warning rides the FAILED result's Warnings
			// for the console/history/eventlog consumers. Moved stays false (the
			// source is retained): this is never journaled as a clean
			// move/replace — the revert ledger keeps its single partial-publish
			// row (CompleteFailed), not a completed-operation row alongside it.
			if overwroteOccupiedDest {
				result.Warnings = append(result.Warnings, authorizedOverwriteWarning(plan.TargetPath))
			}
			return result, result.Error
		}

		result.Moved = true
		// Force-overwrite audit crumb: the replace actually landed — keep the
		// resident bytes' replacement visible to every audit consumer.
		if overwroteOccupiedDest {
			result.Warnings = append(result.Warnings, authorizedOverwriteWarning(plan.TargetPath))
		}
		return result, nil
	}

	// Copy/link path (absorbed from CopyWithLinkMode)
	if len(plan.Conflicts) > 0 {
		result.Error = fmt.Errorf("conflicts detected: %s", joinPlanConflictPaths(plan.Conflicts))
		return result, result.Error
	}

	result.ShouldGenerateMetadata = true

	if !plan.LinkMode.IsValid() {
		result.Error = fmt.Errorf("unsupported link mode %q", plan.LinkMode)
		return result, result.Error
	}

	// overwroteOccupiedDest records that THIS execution replaced a
	// bytes-bearing destination the authorization suppressed (same audit
	// contract as the move lane): the copy lane keys it to the publish-bound
	// replacement signal of its own staged publish (PR #249 codex P2), the
	// link lane to occupancy proven at adjacency with its Remove+install —
	// never to plan-time state. No-op and refused lanes never set it, and a
	// failed install discards it by returning before the warning.
	overwroteOccupiedDest := false
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

			// Remove an existing target ONLY for an authorized replacement of a
			// REGULAR FILE — symlinks, directories, and everything else at the
			// destination are always refused (#224). Gated on IsRegular.
			linkInstallOccupant := false
			if plan.LinkMode != LinkModeNone && !dstLexicalSelf && plan.overwriteAuthorized {
				var linfo os.FileInfo
				var lerr error
				if lst, ok := s.fs.(afero.Lstater); ok {
					linfo, _, lerr = lst.LstatIfPossible(plan.TargetPath)
				} else {
					linfo, lerr = s.fs.Stat(plan.TargetPath)
				}
				if lerr != nil {
					if !os.IsNotExist(lerr) {
						return fmt.Errorf("failed to inspect target before link install: %w", lerr)
					}
				} else if !linfo.Mode().IsRegular() {
					// Anything not a regular file (directory, symlink object,
					// device…) at the destination must not be removed before
					// installing the link output (#224 hole 1).
					return fmt.Errorf("destination is not a regular file (cannot authorize-over): %s", plan.TargetPath)
				}
				// Force-overwrite audit crumb (link lane): an authorized link
				// install REPLACES resident bytes AT THE DESTINATION — the
				// Remove below discards a foreign occupant's entry (plus its
				// bytes when it held the last link). The crumb keys on the
				// REMOVE'S OWN OUTCOME — the destructive publish itself (PR #249
				// codex P2, same publish-bound discipline as the copy/move
				// lanes): nil means THIS lane physically unlinked a resident
				// entry; a tolerated NotExist means the name was already vacant
				// (nothing displaced — no crumb even though the probe above saw
				// an occupant), and a plant inside the probe → Remove window
				// that got unlinked always crumbs. Same-inode alias entries
				// (removing one destroys nothing) stay excluded.
				rmErr := s.fs.Remove(plan.TargetPath)
				if rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
					return fmt.Errorf("failed to prepare target path for link: %w", rmErr)
				}
				linkInstallOccupant = rmErr == nil && !dstSameInode
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
				// Authorized link install delivered: a foreign occupant's bytes
				// were replaced at the destination.
				overwroteOccupiedDest = linkInstallOccupant
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
				// Same audit contract as the hard-link leg above.
				overwroteOccupiedDest = linkInstallOccupant
			default:
				if dstSameInode && !plan.overwriteAuthorized {
					return nil
				}
				if !plan.overwriteAuthorized {
					// #224: copy leg is atomically no-clobbering too.
					if err := fsutil.CopyFileNoReplace(s.fs, plan.SourcePath, plan.TargetPath); err != nil {
						return mapNoReplaceRefusal(fmt.Errorf("failed to copy file: %w", err), plan.TargetPath)
					}
					return nil
				}
				// Authorized copy lane: the regular-file gate is also needed here —
				// a symlink/directory destination must refuse regardless of
				// authorization (#224 codex P1). dstLexicalSelf never reaches here
				// (refused at classification). The gate remains a REFUSAL policy
				// only: the crumb evidence left classify-time probing for the
				// publish-bound signal below (PR #249 codex P2) — the staged copy
				// can stream for arbitrary length between this classification and
				// the publish, so any occupancy answer captured here can be raced
				// stale by a foreign plant/vacate (a false or suppressed crumb).
				if plan.overwriteAuthorized && !dstLexicalSelf {
					linfo, followed, lerr := destMaybeLstat(s.fs, plan.TargetPath)
					if lerr != nil && !errors.Is(lerr, os.ErrNotExist) {
						return fmt.Errorf("failed to inspect target before copy: %w", lerr)
					}
					if linfo != nil {
						// A link-following Stat on no-Lstat wrappers reads through a live
						// symlink as a regular file; when "followed" we probe via readlink,
						// and non-regular detectors just refuse.
						if followed && symlinkObjectExists(s.fs, plan.TargetPath) {
							return fmt.Errorf("destination is not a regular file (cannot authorize-over): %s", plan.TargetPath)
						}
						if !linfo.Mode().IsRegular() {
							return fmt.Errorf("destination is not a regular file (cannot authorize-over): %s", plan.TargetPath)
						}
					}
				}
				destReplaced, cerr := s.linker.copyFile(s.fs, plan.SourcePath, plan.TargetPath)
				if cerr != nil {
					return fmt.Errorf("failed to copy file: %w", cerr)
				}
				// Authorized copy leg delivered: the staged publish reports —
				// bound to the replace-rename itself, alias-excluded against the
				// source it streamed — whether resident bytes were displaced at
				// the publish instant (crumb == PHYSICAL occupancy).
				overwroteOccupiedDest = destReplaced
			}
			return nil
		})
	})
	if err != nil {
		result.Error = err
		return result, result.Error
	}

	result.Moved = true
	// Force-overwrite audit crumb: the replace actually landed — keep the
	// resident bytes' replacement visible to every audit consumer.
	if overwroteOccupiedDest {
		result.Warnings = append(result.Warnings, authorizedOverwriteWarning(plan.TargetPath))
	}
	result.ShouldGenerateMetadata = true

	return result, nil
}
