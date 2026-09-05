package organizer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
)

var videoExtensions = map[string]bool{
	".mp4": true, ".mkv": true, ".avi": true, ".wmv": true,
	".flv": true, ".mov": true, ".m4v": true, ".webm": true,
	".mpg": true, ".mpeg": true, ".m2ts": true, ".ts": true,
}

// resolveFileName generates the target filename from the template, falling back
// to the match ID (then original filename) when sanitization produces an empty string.
// This prevents creating paths like "/dest/.mkv" when template fields are all empty.
func resolveFileName(cfg *Config, engine template.EngineInterface, ctx *template.Context, match models.FileMatchInfo) (string, error) {
	fileName, err := engine.Execute(cfg.FileFormat, ctx)
	if err != nil {
		return "", fmt.Errorf("failed to generate file name: %w", err)
	}

	fileName = template.SanitizeFilename(fileName)

	if fileName == "" {
		if match.MovieID != "" {
			fileName = template.SanitizeFilename(match.MovieID)
		}
		if fileName == "" {
			fileName = template.SanitizeFilename(strings.TrimSuffix(match.Name, match.Extension))
		}
		if fileName == "" && match.Path != "" {
			fileName = template.SanitizeFilename(strings.TrimSuffix(filepath.Base(match.Path), match.Extension))
		}
		if fileName == "" {
			fileName = "file"
		}
		logging.Warnf("[%s] Template produced empty filename after sanitization, falling back to %q", match.MovieID, fileName)
	}

	fileName = fileName + match.Extension
	return fileName, nil
}

func resolveBaseFileName(cfg *Config, engine template.EngineInterface, movie *models.Movie, match models.FileMatchInfo) string {
	if cfg.RenameFile {
		baseCtx := template.NewContextFromMovie(movie)
		baseCtx.GroupActress = cfg.GroupActress
		baseCtx.GroupActressMin = cfg.GroupActressMin
		baseCtx.GroupActressName = cfg.GroupActressName
		baseCtx.GroupUnknownActressName = cfg.GroupUnknownActressName
		baseCtx.UnknownActressMode = cfg.UnknownActressMode
		baseCtx.FirstNameOrder = cfg.FirstNameOrder
		baseCtx.ActressLanguageJa = cfg.ActressLanguageJA
		baseCtx.ActressDelimiter = cfg.ActressDelimiter
		applyTitleTruncation(engine, baseCtx, cfg.MaxTitleLength)

		rendered, err := engine.Execute(cfg.FileFormat, baseCtx)
		if err == nil {
			sanitized := template.SanitizeFilename(rendered)
			if sanitized != "" {
				return sanitized
			}
		}
		if match.MovieID != "" {
			if sanitized := template.SanitizeFilename(match.MovieID); sanitized != "" {
				return sanitized
			}
		}
		if name := template.SanitizeFilename(strings.TrimSuffix(match.Name, match.Extension)); name != "" {
			return name
		}
		if match.Path != "" {
			if name := template.SanitizeFilename(strings.TrimSuffix(filepath.Base(match.Path), match.Extension)); name != "" {
				return name
			}
		}
		return "file"
	}
	base := template.SanitizeFilename(strings.TrimSuffix(match.Name, match.Extension))
	if base != "" {
		return base
	}
	if match.Path != "" {
		if pathBase := template.SanitizeFilename(strings.TrimSuffix(filepath.Base(match.Path), match.Extension)); pathBase != "" {
			return pathBase
		}
	}
	if match.MovieID != "" {
		if sanitized := template.SanitizeFilename(match.MovieID); sanitized != "" {
			return sanitized
		}
	}
	return "file"
}

func applyTitleTruncation(engine template.EngineInterface, ctx *template.Context, maxLen int) {
	if maxLen <= 0 {
		return
	}
	ctx.Title = engine.TruncateTitle(ctx.Title, maxLen)
	ctx.OriginalTitle = engine.TruncateTitle(ctx.OriginalTitle, maxLen)
	for lang, tr := range ctx.Translations {
		tr.Title = engine.TruncateTitle(tr.Title, maxLen)
		tr.OriginalTitle = engine.TruncateTitle(tr.OriginalTitle, maxLen)
		ctx.Translations[lang] = tr
	}
}

// checkTargetConflict classifies the destination with no-follow Lstat so
// dangling symlinks are seen (they vanish under a following Stat). Force
// update suppresses ONLY ConflictFile — directories and symlinks are always
// recorded. Idempotency: lexical self and same-inode aliases are not
// conflicts.
func checkTargetConflict(fs afero.Fs, sourcePath, targetPath string, forceUpdate, willMove bool) []PlanConflict {
	conflicts := make([]PlanConflict, 0)
	if !willMove {
		return conflicts
	}
	var target os.FileInfo
	var targetErr error
	if lst, ok := fs.(afero.Lstater); ok {
		if info, did, e := lst.LstatIfPossible(targetPath); e == nil && did {
			target, targetErr = info, nil
		} else {
			target, targetErr = fs.Stat(targetPath)
			// n.b.: when no true Lstat happened and the target is a dangling
			// symlink this Stat misreads it as missing; symlinkObjectExists
			// (below, available in-package) closes that hole.
		}
	} else {
		target, targetErr = fs.Stat(targetPath)
	}
	if targetErr != nil {
		// Dangling-symlink destination: Stat misses it but the object exists. A
		// symlink is never authorizable-over, so this kind is unconditional (#224).
		if symlinkObjectExists(fs, targetPath) {
			conflicts = append(conflicts, PlanConflict{Path: targetPath, Kind: ConflictSymlink})
		}
		return conflicts
	}
	// A live symlink object at the destination is never renamed-over safely —
	// a fallback Stat returns didLstat=false only when no Lstat was performed,
	// so confirm via readlink before declaring it a regular file.
	if target.Mode()&os.ModeSymlink != 0 || symlinkObjectExists(fs, targetPath) {
		conflicts = append(conflicts, PlanConflict{Path: targetPath, Kind: ConflictSymlink})
		return conflicts
	}
	if target.IsDir() {
		conflicts = append(conflicts, PlanConflict{Path: targetPath, Kind: ConflictDirectory})
		return conflicts
	}
	// Same-inode alias of the source is not a conflict (idempotent no-op).
	sourceStat, sourceErr := fs.Stat(sourcePath)
	if sourceErr == nil && os.SameFile(sourceStat, target) {
		return conflicts
	}
	if !forceUpdate {
		conflicts = append(conflicts, PlanConflict{Path: targetPath, Kind: ConflictFile})
	}
	return conflicts
}

type planContext struct {
	Ctx        *template.Context
	FileName   string
	FolderName string
	Err        error
}

func buildPlanContext(cfg *Config, engine template.EngineInterface, movie *models.Movie, match models.FileMatchInfo) planContext {
	ctx := template.NewContextFromMovie(movie)
	ctx.GroupActress = cfg.GroupActress
	ctx.GroupActressMin = cfg.GroupActressMin
	ctx.GroupActressName = cfg.GroupActressName
	ctx.GroupUnknownActressName = cfg.GroupUnknownActressName
	ctx.UnknownActressMode = cfg.UnknownActressMode
	ctx.FirstNameOrder = cfg.FirstNameOrder
	ctx.ActressLanguageJa = cfg.ActressLanguageJA
	ctx.ActressDelimiter = cfg.ActressDelimiter

	applyTitleTruncation(engine, ctx, cfg.MaxTitleLength)

	ctx.PartNumber = match.PartNumber
	ctx.PartSuffix = match.PartSuffix
	ctx.IsMultiPart = match.IsMultiPart

	var fileName string
	var err error
	if cfg.RenameFile {
		fileName, err = resolveFileName(cfg, engine, ctx, match)
		if err != nil {
			return planContext{Err: err}
		}
	} else {
		fileName = match.Name
		if fileName == "" && match.Path != "" {
			fileName = filepath.Base(match.Path)
		}
	}

	var folderName string
	folderName, err = engine.Execute(cfg.FolderFormat, ctx)
	if err != nil {
		return planContext{Err: fmt.Errorf("failed to generate folder name: %w", err)}
	}

	folderName = template.SanitizeFolderPath(folderName)
	if folderName == "" {
		folderName = template.SanitizeFolderPath(match.MovieID)
		if folderName == "" {
			folderName = folderFallbackUnknown
		}
	}

	return planContext{
		Ctx:        ctx,
		FileName:   fileName,
		FolderName: folderName,
	}
}

// OrganizeCmd carries all parameters for the single-method Organize seam.
// Per Phase 48: replaces the fixed-sequence Plan→ValidatePlan→Execute/Copy
// protocol with one command struct.
type OrganizeCmd struct {
	Match       models.FileMatchInfo
	Movie       *models.Movie
	DestDir     string
	ForceUpdate bool
	MoveFiles   bool     // true = move files, false = copy/link
	LinkMode    LinkMode // Only relevant when MoveFiles=false
	DryRun      bool
	// OperationMode overrides the organizer config's mode for this command.
	// When non-empty, plan() resolves the strategy from this mode instead of
	// o.config.OperationMode, so a per-request override (e.g. the API's
	// OperationModeOverride) reaches ResolveStrategy even when the organizer
	// config still carries the global default. Empty = use config mode.
	OperationMode   operationmode.OperationMode
	ForceRenameFile bool
	// DuplicateTracker shares intra-batch duplicate-destination preflight
	// across the plans of one batch run (#224 phase E). Nil disables
	// detection (single-file callers have no batch to collide with).
	DuplicateTracker *DuplicateTracker
}

// OrganizerInterface is the single-method seam for file organization.
// Per Phase 48: Plan/ValidatePlan/Execute are internal implementation details
// of the Organize method — callers invoke one method instead of a fixed sequence.
type OrganizerInterface interface {
	Organize(ctx context.Context, cmd OrganizeCmd) (*OrganizeResult, error)
}

var _ OrganizerInterface = (*Organizer)(nil)

// Organizer handles file organization (moving/renaming)
type Organizer struct {
	fs              afero.Fs
	config          *Config
	templateEngine  template.EngineInterface
	subtitleHandler *subtitleHandler
	matcher         matcher.MatcherInterface
	linker          linker
}

// NewOrganizer creates a new file organizer
func NewOrganizer(fs afero.Fs, cfg *Config, engine template.EngineInterface, m matcher.MatcherInterface) *Organizer {
	if engine == nil {
		engine = template.NewEngine()
	}
	return &Organizer{
		fs:              fs,
		config:          cfg,
		templateEngine:  engine,
		subtitleHandler: newSubtitleHandler(fs, cfg.SubtitleExtensions),
		matcher:         m,
		linker:          OSLinker{},
	}
}

// OrganizeResult represents the result of organizing a file
type OrganizeResult struct {
	OriginalPath string
	NewPath      string
	FolderPath   string
	FileName     string
	Moved        bool
	Error        error
	// Warnings carries non-fatal per-file advisories an authorized run must
	// not silently drop (#224 phase E): authorized intra-batch duplicates land
	// here so the worker history rows and the API eventlog can persist them.
	Warnings []string
	// DuplicateSkipped is true when an authorized intra-batch duplicate was
	// demoted to a warning and execution skipped (codex P1, PR #241): nothing
	// moved for this file, so NewPath names the winner's SHARED destination
	// for display/history only. Revert journaling treats the result as a true
	// no-op — NO primary-move record is persisted for it, leaving the winner's
	// operation row the sole revert subject of the shared bytes (a revert of
	// the skipped loser must never rename the winner's video).
	DuplicateSkipped bool
	// PrePublication marks a strategy-execute failure whose error proves the
	// destination never received this file's bytes (codex PR #241 batch-2 F1):
	// every organize publish composite carries fsutil.PublishCompleted on its
	// post-publish leg, so an execute error WITHOUT that class failed
	// pre-publication — nothing the destination could hold belongs to this
	// file. The result's NewPath/FolderPath name the INTENDED target only;
	// under duplicate ownership that may be a SHARED destination a promoted
	// claimant later publishes, so revert journaling must ignore all target
	// fields and finalize the row completed-noop exactly like a
	// DuplicateSkipped result — reverting the failed owner must never treat
	// another claimant's published bytes as this owner's moved primary.
	// In-place directory-rename failures are marked on the identical rule
	// when nothing SURVIVED on disk (no rename happened, or its rollback
	// landed); only a surviving rename (rollback refused) stays journaled —
	// that mutation is publication-equivalent and its claim settles (codex
	// P1, PR #241).
	PrePublication         bool
	Subtitles              []SubtitleResult
	InPlaceRenamed         bool   // Whether an in-place directory rename SURVIVES on disk (rename happened and was never rolled back)
	OldDirectoryPath       string // Original directory path (for updating subsequent file paths)
	NewDirectoryPath       string // New directory path after in-place rename
	ShouldGenerateMetadata bool   // Whether NFO/media should be generated for this result
}

// SubtitleResult records the outcome for one matched subtitle file: planned
// (dry run), skipped (skip-on-exists), error, or installed — with the mode
// distinction carried on the embedded SubtitleMove (Moved vs Copied, #224 E).
type SubtitleResult struct {
	models.SubtitleMove
	Skipped bool
	Planned bool
	Error   error
}

// strategyType is an internal enum identifying the operation strategy.
// It is unexported — external consumers use behavior flags on OrganizePlan instead.
type strategyType int

const (
	strategyOrganize strategyType = iota
	strategyInPlace
	strategyInPlaceNoRenameFolder
	strategyMetadataArtwork
)

// OrganizePlan represents a planned file organization operation.
type OrganizePlan struct {
	Match             models.FileMatchInfo
	Movie             *models.Movie
	SourcePath        string
	TargetDir         string
	TargetFile        string
	TargetPath        string
	WillMove          bool
	Conflicts         []PlanConflict
	InPlace           bool
	OldDir            string
	IsDedicated       bool
	SkipInPlaceReason string
	FolderName        string
	SubfolderPath     string
	BaseFileName      string

	// Behavior flags for external consumers (e.g., preview orchestrator).
	// These replace direct StrategyType comparisons.
	PreserveSourcePath bool // true = metadata-artwork or in-place-norenamefolder: keep files in original directory
	RenameFolder       bool // true = in-place with dedicated folder: rename the folder, not just the file

	strategy        strategyType
	executeStrategy OperationStrategy
	LinkMode        LinkMode
	moveFiles       bool // true = move (rename); false = copy/link — determines which branch strategy.Execute takes
	// overwriteAuthorized is true when the caller explicitly authorized replacing an existing
	// destination (cmd.ForceUpdate). When false, move execution refuses to replace a file that
	// exists at the target even if it appeared after plan-time conflict checks (TOCTOU guard).
	overwriteAuthorized bool
}

// Plan creates an organization plan without executing it
func (o *Organizer) resolveStrategy() OperationStrategy {
	return ResolveStrategy(o.fs, o.config, o.matcher, o.templateEngine)
}

// ResolveStrategy returns the operation strategy selected by the configured operation mode.
func ResolveStrategy(fs afero.Fs, cfg *Config, m matcher.MatcherInterface, engine template.EngineInterface) OperationStrategy {
	switch cfg.OperationMode {
	case operationmode.OperationModeOrganize:
		return newOrganizeStrategy(fs, cfg, engine, OSLinker{})
	case operationmode.OperationModeInPlace:
		return newInPlaceStrategy(fs, cfg, m, engine)
	case operationmode.OperationModeInPlaceNoRenameFolder:
		return newInPlaceNoRenameFolderStrategy(fs, cfg, m, engine)
	case operationmode.OperationModeMetadataArtwork, operationmode.OperationModePreview:
		return newMetadataArtworkStrategy(fs, cfg)
	default:
		return newOrganizeStrategy(fs, cfg, engine, OSLinker{})
	}
}

func (o *Organizer) strategyFromType(st strategyType) OperationStrategy {
	switch st {
	case strategyInPlace:
		return newInPlaceStrategy(o.fs, o.config, o.matcher, o.templateEngine)
	case strategyInPlaceNoRenameFolder:
		return newInPlaceNoRenameFolderStrategy(o.fs, o.config, o.matcher, o.templateEngine)
	case strategyMetadataArtwork:
		return newMetadataArtworkStrategy(o.fs, o.config)
	default:
		return newOrganizeStrategy(o.fs, o.config, o.templateEngine, o.linker)
	}
}

func (o *Organizer) plan(match models.FileMatchInfo, movie *models.Movie, destDir string, forceUpdate bool, modeOverride operationmode.OperationMode) (*OrganizePlan, error) {
	strategy := o.resolveStrategy()
	// Honor a per-command mode override so a per-request selection (e.g. the
	// API's OperationModeOverride) reaches ResolveStrategy instead of being
	// shadowed by the global mode baked into o.config at factory time.
	// Shallow-copy the config with the override mode so the resolved strategy
	// and its Plan() see the requested mode (e.g. in-place strategies branch
	// on config.OperationMode). Skip the copy when the override is empty or
	// matches the config — the common no-override path stays unchanged.
	if modeOverride != "" && modeOverride != o.config.OperationMode {
		overrideCfg := *o.config
		overrideCfg.OperationMode = modeOverride
		strategy = ResolveStrategy(o.fs, &overrideCfg, o.matcher, o.templateEngine)
	}
	return strategy.Plan(match, movie, destDir, forceUpdate)
}

// execute executes an organization plan
//
//nolint:unused
func (o *Organizer) execute(plan *OrganizePlan) (*OrganizeResult, error) {
	result := &OrganizeResult{
		OriginalPath:           plan.SourcePath,
		NewPath:                plan.TargetPath,
		FolderPath:             plan.TargetDir,
		FileName:               plan.TargetFile,
		Moved:                  false,
		ShouldGenerateMetadata: false,
	}

	if len(plan.Conflicts) > 0 {
		result.Error = fmt.Errorf("conflicts detected: %s", joinPlanConflictPaths(plan.Conflicts))
		return result, result.Error
	}

	if !plan.WillMove {
		result.ShouldGenerateMetadata = true
		o.handleSubtitles(plan, result, subtitleInstall{})
		return result, nil
	}

	strategy := plan.executeStrategy
	if strategy == nil {
		strategy = o.strategyFromType(plan.strategy)
	}

	strategyResult, err := strategy.Execute(plan)
	if err != nil {
		return strategyResult, err
	}

	if o.config.MoveSubtitles {
		o.handleSubtitles(plan, strategyResult, subtitleMoveInstall)
	}

	return strategyResult, nil
}

func (o *Organizer) subtitleFileInfo(plan *OrganizePlan) models.FileMatchInfo {
	fileInfoForSubtitles := models.FileMatchInfo{
		Path:      plan.Match.Path,
		Name:      plan.Match.Name,
		Extension: plan.Match.Extension,
		Size:      plan.Match.Size,
		ModTime:   plan.Match.ModTime,
	}
	if plan.InPlace {
		fileInfoForSubtitles.Path = plan.TargetPath
		oldFileName := plan.Match.Name
		if oldFileName == "" && plan.Match.Path != "" {
			oldFileName = filepath.Base(plan.Match.Path)
		}
		if oldFileName != "" && oldFileName != plan.TargetFile {
			fileInfoForSubtitles.Path = filepath.Join(plan.TargetDir, oldFileName)
		}
	}
	return fileInfoForSubtitles
}

// subtitleInstall selects how handleSubtitles delivers subtitle files. A nil
// op plans only (no filesystem changes); a non-nil op installs through one of
// the fsutil no-replace composites, and copied records the mode distinction in
// results (#224 phase E): a copy install retains the source (revert deletes
// the installed copy), a move install does not (revert moves it back).
type subtitleInstall struct {
	op     func(afero.Fs, string, string) error
	copied bool
}

var (
	subtitleMoveInstall = subtitleInstall{op: fsutil.MoveFileNoReplace}
	subtitleCopyInstall = subtitleInstall{op: fsutil.CopyFileNoReplace, copied: true}
)

func (o *Organizer) handleSubtitles(plan *OrganizePlan, result *OrganizeResult, install subtitleInstall) {
	subtitles := o.subtitleHandler.FindSubtitles(o.subtitleFileInfo(plan))
	if len(subtitles) == 0 {
		return
	}

	subtitleResults := make([]SubtitleResult, len(subtitles))
	for i, subtitle := range subtitles {
		videoNameWithoutExt := strings.TrimSuffix(plan.TargetFile, filepath.Ext(plan.TargetFile))
		newSubtitleName := o.subtitleHandler.generateSubtitleFileName(
			videoNameWithoutExt,
			subtitle.Language,
			subtitle.Extension,
		)
		newPath := filepath.Join(plan.TargetDir, newSubtitleName)

		if install.op == nil {
			subtitleResults[i] = SubtitleResult{
				SubtitleMove: models.SubtitleMove{
					OriginalPath: subtitle.OriginalPath,
					NewPath:      newPath,
				},
				Planned: true,
			}
			continue
		}

		sr := SubtitleResult{
			SubtitleMove: models.SubtitleMove{
				OriginalPath: subtitle.OriginalPath,
				NewPath:      newPath,
			},
		}

		// Shared parent-directory lock + per-subtitle file lock: child writes stay
		// parallel per file, but a directory rename elsewhere drains us before moving.
		err := withDestDirSharedLock(plan.TargetDir, func() error {
			return withDestFileLock(newPath, func() error {
				// pathExistsBestEffort also sees dangling symlink objects: a filesystem whose
				// Stat follows links (no true Lstat) would otherwise let this op replace one.
				exists, statErr := pathExistsBestEffort(o.fs, newPath)
				if statErr != nil {
					return fmt.Errorf("failed to check subtitle destination: %w", statErr)
				}
				if exists {
					sr.Skipped = true
					return nil
				}
				err := install.op(o.fs, subtitle.OriginalPath, newPath)
				if err != nil && fsutil.PublishCompleted(err) {
					// Post-publish cleanup refusal (#224 codex P2): bytes at the
					// destination AND the source retained — an ambiguous delivery,
					// NOT a clean move. Recording Moved here would let revert
					// journaling (workflow/revert_log.go MoveBack, executed by
					// history/reverter.go) overwrite the retained source's newer
					// contents with a stale copy. Record the ambiguity as an
					// error slot: never Moved, never Skipped.
					return fmt.Errorf("subtitle published but source cleanup refused — both copies retained (%w)", err)
				}
				if err != nil && fsutil.PublishRefusal(err) {
					// #224: subtitle destinations accept the publish-refusal classes
					// as a skip — occupancy (a foreign subtitle won the name inside
					// the check→publish window: same outcome as the pre-check) and
					// no-replace-unsupported volumes (the subtitle is simply not
					// delivered; nothing was installed that could have overwritten
					// the foreign bytes).
					sr.Skipped = true
					return nil
				}
				return err
			})
		})
		if err != nil {
			sr.Error = fmt.Errorf("failed to handle subtitle: %w", err)
		} else if !sr.Skipped {
			if install.copied {
				sr.Copied = true
			} else {
				sr.Moved = true
			}
		}

		subtitleResults[i] = sr
	}
	result.Subtitles = subtitleResults
}

// withForceRename returns the planner that honors a per-command force-rename
// request: when the config disables RenameFile but the command demands it,
// planning runs against a clone with RenameFile enabled. Shared by Organize
// and PlanOrganize so primed claims and executed plans always derive from the
// identical planner selection (#240 finding A).
func (o *Organizer) withForceRename(forceRename bool) *Organizer {
	if forceRename && o.config != nil && !o.config.RenameFile {
		clone := *o
		cfg := *o.config
		cfg.RenameFile = true
		clone.config = &cfg
		return &clone
	}
	return o
}

// PlanOrganize computes cmd's organization plan WITHOUT validating,
// registering duplicate preflight, or executing anything — the read-only
// planning seam the apply phase calls exactly once per sorted batch item
// before worker fan-out, to prime deterministic duplicate ownership (#240
// finding A). Planner selection is shared with Organize, so a primed claim
// always matches the plan the item's worker later computes.
func (o *Organizer) PlanOrganize(ctx context.Context, cmd OrganizeCmd) (*OrganizePlan, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return o.withForceRename(cmd.ForceRenameFile).plan(cmd.Match, cmd.Movie, cmd.DestDir, cmd.ForceUpdate, cmd.OperationMode)
}

// PlanSourceExists reports whether plan's source file is still present on
// the organizer's filesystem (codex r2 P2): the duplicate priming leg MUST
// NOT claim a canonical key for a plan that cannot execute — a primed owner
// whose source already vanished at priming time would hold the key for the
// whole run, blocking every valid later claimant. Read-only: one Stat
// against the same fs Organize validates and executes on. Non-missing Stat
// errors leave the claim decision to the later validation/execution pass,
// mirroring validatePlan's IsNotExist-only rule.
func (o *Organizer) PlanSourceExists(plan *OrganizePlan) bool {
	_, err := o.fs.Stat(plan.SourcePath)
	return !os.IsNotExist(err)
}

// Organize is the single-method seam that plans, validates, and executes
// file organization in one call. Per Phase 48: Plan/ValidatePlan/Execute
// are internal implementation details — callers use Organize instead of a
// fixed sequence.
func (o *Organizer) Organize(ctx context.Context, cmd OrganizeCmd) (*OrganizeResult, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	plan, err := o.withForceRename(cmd.ForceRenameFile).plan(cmd.Match, cmd.Movie, cmd.DestDir, cmd.ForceUpdate, cmd.OperationMode)
	if err != nil {
		return nil, err
	}

	// codex P2 (PR #241): claim close-out on panic — an owner panicking
	// ANYWHERE after planning (mid-observe, mid-execute, subtitle install)
	// must release its canonical key so waiting claimants promote instead of
	// deadlocking behind a dead owner. The re-panic preserves the worker
	// boundary's withFileRecovery per-file failure semantics; the claim
	// release itself is the tracker's failure terminal (no-op for losers and
	// on owner-check mismatch).
	defer func() {
		if r := recover(); r != nil {
			cmd.DuplicateTracker.release(plan)
			panic(r)
		}
	}()

	// Intra-batch duplicate preflight (#224 phase E): plan-only grouping by
	// proven-equal canonical destination keys. Unauthorized duplicates join
	// plan.Conflicts and short-circuit through the identical failure pipeline;
	// authorized duplicates return as persisted per-file warnings and skip
	// execution — only the primed winner's bytes land (#240 finding A).
	dupWarnings, dupSkip := applyDuplicatePreflight(ctx, plan, cmd.DuplicateTracker, cmd.ForceUpdate)

	// codex P2 (PR #241 F2) promotion/execute boundary recheck: the preflight
	// wait honors the caller's context, so a deadline or batch cancel can land
	// AFTER this plan claimed or was promoted onto its key. Recheck before ANY
	// further filesystem work (validation reads included): an aborted owner
	// returns the context error — matching the entry guard's outcome — and
	// releases its key so the next claimant promotes instead of blocking
	// behind a corpse. Non-owners release nothing, so cancelled waiters pass
	// through harmlessly.
	if err := ctx.Err(); err != nil {
		cmd.DuplicateTracker.release(plan)
		return nil, err
	}

	// codex P2 (PR #241 F1): a stationary resident's no-op execute cannot
	// surface its ghost — ForceUpdate skips validatePlan entirely, and even a
	// normal-mode no-op short-circuits nothing it can fail — so a parked
	// claim whose source vanished in the priming→worker gap releases HERE in
	// both modes, never settling the key and skipping every gated mover into
	// an empty destination. validatePlan's IsNotExist-only rule below already
	// excludes the same class when validation runs; this check is the
	// force-mode mirror at exactly one Stat.
	if !plan.WillMove && plan.TargetPath != "" {
		if _, err := o.fs.Stat(plan.SourcePath); os.IsNotExist(err) {
			cmd.DuplicateTracker.release(plan)
			return nil, fmt.Errorf("organization validation failed: [source file does not exist: %s]", plan.SourcePath)
		}
	}

	if !cmd.ForceUpdate {
		if issues := o.validatePlan(plan); len(issues) > 0 {
			// codex r2 P2: a plan the run's duplicate tracker primed as owner
			// must not keep its canonical key when it cannot execute — most
			// acutely the winner whose source vanished between priming and
			// apply. Releasing lets the next valid claimant's observe fall
			// through instead of dying on the stale owner's claim. Losers
			// (whose validation failure lists their ConflictDuplicate) release
			// nothing — release matches only the recorded owner. A STATIONARY
			// resident failing here (its source vanished in the priming→worker
			// gap) likewise releases its own parked claim (codex P2, PR #241
			// F1), so the ghost key frees/promotes instead of sealing the
			// destination for the rest of the run.
			cmd.DuplicateTracker.release(plan)
			return nil, fmt.Errorf("organization validation failed: %v", issues)
		}
	}

	// Propagate link mode to plan for strategy consumption.
	// When MoveFiles=true, LinkMode stays LinkModeNone (zero value) — strategy does a move.
	// When MoveFiles=false, LinkMode is set from the command — strategy does copy/link.
	plan.moveFiles = cmd.MoveFiles
	if !cmd.MoveFiles {
		plan.LinkMode = cmd.LinkMode
	}

	// Check for conflicts before executing
	if len(plan.Conflicts) > 0 {
		// codex r2 P2: same release rule on the conflict terminal (reached
		// under ForceUpdate, which skips validatePlan) — an inexecutable
		// primed owner frees its key for the next valid claimant. A loser's
		// ConflictDuplicate never matches the owner key, so this is a no-op
		// for every duplicate-skipped plan.
		cmd.DuplicateTracker.release(plan)
		return nil, fmt.Errorf("conflicts detected: %s", joinPlanConflictPaths(plan.Conflicts))
	}

	// Dry-run: return early with planned result (no filesystem changes).
	// The owner still settles its claim (codex P2, PR #241): the dry-run
	// outcome is terminal, and waiting claimants must resolve now.
	if cmd.DryRun {
		cmd.DuplicateTracker.settle(plan)
		return &OrganizeResult{
			OriginalPath:           plan.SourcePath,
			NewPath:                plan.TargetPath,
			FolderPath:             plan.TargetDir,
			FileName:               plan.TargetFile,
			Moved:                  false,
			Warnings:               dupWarnings,
			ShouldGenerateMetadata: true,
		}, nil
	}

	// Authorized intra-batch duplicate (#240 finding A): demoted to a
	// persisted warning above, it must NOT execute — pre-priming both sources
	// raced onto the same destination with lock order deciding the surviving
	// bytes. Skipping guarantees only the deterministic winner publishes.
	if dupSkip {
		return &OrganizeResult{
			OriginalPath:           plan.SourcePath,
			NewPath:                plan.TargetPath,
			FolderPath:             plan.TargetDir,
			FileName:               plan.TargetFile,
			Moved:                  false,
			Warnings:               dupWarnings,
			DuplicateSkipped:       true,
			ShouldGenerateMetadata: true,
		}, nil
	}

	strategy := plan.executeStrategy
	if strategy == nil {
		strategy = o.strategyFromType(plan.strategy)
	}

	strategyResult, err := strategy.Execute(plan)
	if err != nil {
		if fsutil.PublishCompleted(err) {
			// codex P1 (PR #241): PARTIAL publish — the cross-device move's
			// publish leg landed this owner's bytes at the destination and
			// only the source cleanup refused (the typed ErrPublishCompleted
			// class every fsutil move composite carries on that leg). The
			// destination is therefore the owner's terminal outcome, exactly
			// like a clean move: SETTLE the claim (never release). Releasing
			// would promote a waiting claimant onto an occupied destination —
			// under ForceUpdate that claimant would overwrite the published
			// bytes and the failed owner's revert row would then aim the
			// winner's bytes at the old owner's source path. Settling keeps
			// the waiter's duplicate verdict unchanged (conflict /
			// authorized-skip) and the shared destination byte-owned by the
			// row that actually published it.
			cmd.DuplicateTracker.settle(plan)
			return strategyResult, err
		}
		// codex r2 P2: PRE-publication failure (the disappeared-source
		// failure surfaces HERE under ForceUpdate, validation skipped, and
		// every refusal/ambiguity that left the destination untouched): the
		// primed owner's claim is released on the identical rule so the
		// next valid claimant can still land its bytes on the destination.
		// codex P1 (PR #241): in-place plans are MUTATION-aware instead of
		// blanket-exempt — the strategy's rollback seams report honestly
		// whether a directory rename SURVIVED on disk (InPlaceRenamed stands
		// only when the rename was never rolled back). An in-place failure
		// with nothing surviving (source dir vanished post-priming, or an
		// inner-rename refusal whose rollback landed) is exactly the
		// pre-publication class: journal no target fields, release the claim
		// — a promoted claimant's renamed directory is never this failed
		// row's revert subject. A SURVIVING rename (rollback refused) is
		// publication-equivalent for claim purposes — the destination name
		// physically changed to this owner's target — so the claim SETTLES,
		// never releases: a waiting claimant keeps its duplicate verdict
		// instead of publishing into the directory the failed owner still
		// owns, and the settled row's journal names where the bytes actually
		// went (the strategy re-points NewPath/FileName at the surviving
		// location), making its revert an exact-inverse unwind.
		if plan.InPlace && strategyResult != nil && strategyResult.InPlaceRenamed {
			cmd.DuplicateTracker.settle(plan)
			return strategyResult, err
		}
		if strategyResult != nil {
			strategyResult.PrePublication = true
		}
		cmd.DuplicateTracker.release(plan)
		return strategyResult, err
	}
	strategyResult.Warnings = append(strategyResult.Warnings, dupWarnings...)

	// Terminal success for the duplicate tracker (codex P2, PR #241): the
	// claim stays owned, so already-waiting claimants wake to their unchanged
	// duplicate verdict instead of blocking behind this owner's subtitle work.
	cmd.DuplicateTracker.settle(plan)

	// Subtitle handling is centralized here — applies to both move and copy/link paths.
	// Authorization never reaches subtitle destinations: both entry points
	// install strictly skip-on-exists through the no-replace composites.
	if cmd.MoveFiles && o.config.MoveSubtitles {
		o.handleSubtitles(plan, strategyResult, subtitleMoveInstall)
	} else if !cmd.MoveFiles && o.config.MoveSubtitles {
		o.handleSubtitles(plan, strategyResult, subtitleCopyInstall)
	}

	return strategyResult, nil
}

// validatePlan checks if a plan is valid and safe to execute
func (o *Organizer) validatePlan(plan *OrganizePlan) []string {
	issues := make([]string, 0)

	// Check for conflicts
	for _, c := range plan.Conflicts {
		issues = append(issues, c.String()) // String() = bare path
	}

	// Check source exists
	if _, err := o.fs.Stat(plan.SourcePath); os.IsNotExist(err) {
		issues = append(issues, fmt.Sprintf("source file does not exist: %s", plan.SourcePath))
	}

	// Check folder name is not empty
	if plan.TargetDir == "" || plan.TargetFile == "" {
		issues = append(issues, "target directory or filename is empty")
	}

	// Check for invalid characters in paths
	if strings.Contains(plan.TargetPath, "//") {
		issues = append(issues, "target path contains double slashes")
	}

	return issues
}
