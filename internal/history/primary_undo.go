package history

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
)

// Fault seams operate outside the SQL merge callback. Tests use only synthetic
// media, and can reopen SQLite and a fresh Reverter at each durable boundary.
var primaryUndoPublish = primaryUndoPublishNoReplace
var primaryUndoRemove = func(fs afero.Fs, path string) error { return fs.Remove(path) }
var primaryUndoRandom io.Reader = rand.Reader
var primaryUndoSyncParent = primaryUndoSyncDir
var primaryUndoPrepareStage = fsutil.SyncStagedMetadata

const primaryUndoPlannedState = "planned"

// Admission requires the intersection of fd-scoped staging metadata, a native
// mutation witness, and the POSIX directory-sync/no-replace recovery primitives.
// In particular, Solaris/illumos have a witness but stagedHandleChtimes returns
// ENOSYS. Reject before creating a journal or stage instead of rotating a full
// copy on every retry. Windows retains its separate, stricter admission refusal.
func primaryUndoDurabilitySupportedOn(goos string) error {
	switch goos {
	case "linux", "darwin", "dragonfly", "freebsd", "netbsd", "openbsd":
		return nil
	default:
		return fmt.Errorf("cross-volume primary Undo on %s requires manual recovery: native metadata and recovery durability are not supported; retain source, stage, and history", goos)
	}
}

func primaryUndoToken() (string, error) {
	var token [16]byte
	if _, err := io.ReadFull(primaryUndoRandom, token[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(token[:]), nil
}

func primaryUndoIdentity(file afero.File) (models.PrimaryUndoIdentity, os.FileInfo, error) {
	info, err := file.Stat()
	if err != nil {
		return models.PrimaryUndoIdentity{}, nil, err
	}
	if info == nil || !info.Mode().IsRegular() {
		return models.PrimaryUndoIdentity{}, nil, fmt.Errorf("primary Undo requires a regular file")
	}
	dev, ino, fileID, strong := primaryUndoObjectKey(file, info)
	if !strong {
		return models.PrimaryUndoIdentity{}, nil, fmt.Errorf("primary Undo requires a strong native file identity")
	}
	return models.PrimaryUndoIdentity{Strong: true, Device: dev, Inode: ino, FileID128: fileID, Size: info.Size(), ModTime: info.ModTime().UnixNano(), Mode: uint32(info.Mode().Perm())}, info, nil
}

func primaryUndoSameObject(a, b models.PrimaryUndoIdentity) bool {
	return a.Strong && b.Strong && a.Device == b.Device && a.Inode == b.Inode && a.FileID128 == b.FileID128
}

func primaryUndoOpenPinned(fs afero.Fs, path string, expected models.PrimaryUndoIdentity, content bool) (afero.File, error) {
	info, err := lstatRestoreSource(fs, path)
	if err != nil {
		return nil, err
	}
	if info == nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("primary Undo path %s is not a regular file", path)
	}
	file, err := restoreOpenReplacementSource(fs, path)
	if err != nil {
		return nil, err
	}
	found, _, err := primaryUndoIdentity(file)
	if err == nil && (!primaryUndoSameObject(found, expected) || (content && found != expected)) {
		err = fmt.Errorf("primary Undo refuses changed object at %s", path)
	}
	if err != nil {
		_ = file.Close() // Read-only pin; retain the ownership/admission error.
		return nil, err
	}
	return file, nil
}

type primaryUndoChangeWitness struct{ Seconds, Nanoseconds int64 }

func primaryUndoVerify(fs afero.Fs, path string, expected models.PrimaryUndoIdentity, digest string) error {
	_, err := primaryUndoVerifyWitness(fs, path, expected, digest)
	return err
}

func primaryUndoVerifyWitness(fs afero.Fs, path string, expected models.PrimaryUndoIdentity, digest string) (primaryUndoChangeWitness, error) {
	empty := primaryUndoChangeWitness{}
	file, err := primaryUndoOpenPinned(fs, path, expected, true)
	if err != nil {
		return empty, err
	}
	defer func() { _ = file.Close() }() // Read-only hash handle; no writes to flush.
	witness, err := primaryUndoObserveChange(file)
	if err != nil {
		return empty, err
	}
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return empty, err
	}
	after, _, err := primaryUndoIdentity(file)
	if err != nil {
		return empty, err
	}
	afterWitness, err := primaryUndoObserveChange(file)
	if err != nil {
		return empty, err
	}
	if after != expected || afterWitness != witness || digest == "" || hex.EncodeToString(sum.Sum(nil)) != digest {
		return empty, fmt.Errorf("primary Undo content changed at %s", path)
	}
	named, err := primaryUndoOpenPinned(fs, path, expected, true)
	if err != nil {
		return empty, err
	}
	namedWitness, witnessErr := primaryUndoObserveChange(named)
	closeErr := named.Close()
	if witnessErr != nil || closeErr != nil {
		return empty, errors.Join(witnessErr, closeErr)
	}
	if namedWitness != witness {
		return empty, fmt.Errorf("primary Undo content changed after hashing %s", path)
	}
	return witness, nil
}

func primaryUndoRecheckWitness(fs afero.Fs, path string, expected models.PrimaryUndoIdentity, witness primaryUndoChangeWitness) error {
	file, err := primaryUndoOpenPinned(fs, path, expected, true)
	if err != nil {
		return err
	}
	current, witnessErr := primaryUndoObserveChange(file)
	closeErr := file.Close()
	if witnessErr != nil || closeErr != nil {
		return errors.Join(witnessErr, closeErr)
	}
	if current != witness {
		return fmt.Errorf("primary Undo restored destination changed after hashing")
	}
	return nil
}

func primaryUndoWrite(ctx context.Context, repo database.BatchFileOperationRepositoryInterface, op *models.BatchFileOperation, expected, next *models.PrimaryUndo) error {
	var raw string
	err := repo.UpdateJournalInTx(database.WithDurableJournal(ctx), op.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		ledger, err := models.ParseGeneratedFiles(current.GeneratedFiles)
		if err != nil {
			return ledger, false, err
		}
		if ledger.PrimaryResident || ledger.DirectoryMove != nil {
			return ledger, false, fmt.Errorf("conflicting resident and primary Undo ownership; manual recovery required")
		}
		if current.RevertStatus != models.RevertStatusApplied && current.RevertStatus != models.RevertStatusFailed {
			return ledger, false, fmt.Errorf("primary Undo operation is no longer retryable")
		}
		if !reflect.DeepEqual(ledger.PrimaryUndo, expected) {
			return ledger, false, fmt.Errorf("primary Undo journal changed concurrently")
		}
		saved := *next
		ledger.PrimaryUndo = &saved
		raw = models.MarshalLedgerJSON(ledger)
		return ledger, true, nil
	})
	if err != nil {
		return fmt.Errorf("persist primary Undo %s: %w", next.State, err)
	}
	op.GeneratedFiles = raw
	return nil
}

func primaryUndoState(ctx context.Context, repo database.BatchFileOperationRepositoryInterface, op *models.BatchFileOperation, j *models.PrimaryUndo, state string) error {
	next := *j
	next.State = state
	if err := primaryUndoWrite(ctx, repo, op, j, &next); err != nil {
		return err
	}
	*j = next
	return nil
}

// beginPrimaryUndo is entered only after an ordinary move's native rename
// reported EXDEV. No source bytes are consumed before the published copy and
// its ownership proof are durably journaled.
func beginPrimaryUndo(ctx context.Context, fs afero.Fs, repo database.BatchFileOperationRepositoryInterface, op *models.BatchFileOperation) error {
	if err := primaryUndoDurabilitySupported(); err != nil {
		return err
	}
	if op.InPlaceRenamed || op.OperationType != models.OperationTypeMove {
		return fmt.Errorf("cross-volume primary Undo does not support this operation")
	}
	if _, err := lstatRestoreSource(fs, op.OriginalPath); !os.IsNotExist(err) {
		return fmt.Errorf("primary Undo destination is occupied or unreadable: %s", op.OriginalPath)
	}
	source, err := restoreOpenReplacementSource(fs, op.NewPath)
	if err != nil {
		return err
	}
	identity, _, err := primaryUndoIdentity(source)
	_ = source.Close() // Read-only identity probe; no writes to flush.
	if err != nil {
		return err
	}
	token, err := primaryUndoToken()
	if err != nil {
		return err
	}
	j := &models.PrimaryUndo{Token: token, State: primaryUndoPlannedState, Source: op.NewPath, Destination: op.OriginalPath, Stage: op.OriginalPath + ".undo." + token, SourceIdentity: identity}
	if err := primaryUndoWrite(ctx, repo, op, nil, j); err != nil {
		return err
	}
	return resumePrimaryUndo(ctx, fs, repo, op, j)
}

func primaryUndoStagePath(j *models.PrimaryUndo, path string) bool {
	prefix := j.Destination + ".undo." + j.Token
	if path == prefix {
		return true
	}
	if !strings.HasPrefix(path, prefix+".") {
		return false
	}
	token, err := hex.DecodeString(strings.TrimPrefix(path, prefix+"."))
	return err == nil && len(token) == 16
}

func clonePrimaryUndo(j *models.PrimaryUndo) models.PrimaryUndo {
	next := *j
	next.SupersededStages = append([]models.PrimaryUndoStage(nil), j.SupersededStages...)
	return next
}

func rotatePrimaryUndoStage(ctx context.Context, repo database.BatchFileOperationRepositoryInterface, op *models.BatchFileOperation, j *models.PrimaryUndo) error {
	if !j.StageIdentity.Strong {
		return fmt.Errorf("unadmitted primary Undo stage requires manual recovery")
	}
	token, err := primaryUndoToken()
	if err != nil {
		return err
	}
	next := clonePrimaryUndo(j)
	next.SupersededStages = append(next.SupersededStages, models.PrimaryUndoStage{Path: j.Stage, Identity: j.StageIdentity})
	next.Stage = j.Destination + ".undo." + j.Token + "." + token
	next.StageIdentity = models.PrimaryUndoIdentity{}
	next.SHA256 = ""
	next.State = primaryUndoPlannedState
	if err := primaryUndoWrite(ctx, repo, op, j, &next); err != nil {
		return err
	}
	*j = next
	return nil
}

func validatePrimaryUndo(op *models.BatchFileOperation, j *models.PrimaryUndo) error {
	token, err := hex.DecodeString(j.Token)
	if err != nil || len(token) != 16 || j.Source != op.NewPath || j.Destination != op.OriginalPath || !primaryUndoStagePath(j, j.Stage) || !j.SourceIdentity.Strong || op.InPlaceRenamed || op.OperationType != models.OperationTypeMove {
		return fmt.Errorf("invalid primary Undo recovery journal; manual recovery required")
	}
	for _, stage := range j.SupersededStages {
		if !primaryUndoStagePath(j, stage.Path) || stage.Path == j.Stage || !stage.Identity.Strong {
			return fmt.Errorf("invalid superseded primary Undo stage")
		}
		for _, path := range []string{stage.CleanupFrom, stage.Cleanup} {
			if path == "" || path == stage.Path {
				continue
			}
			prefix := stage.Path + ".undo-remove."
			if !strings.HasPrefix(path, prefix) {
				return fmt.Errorf("invalid superseded-stage cleanup path")
			}
			token, err := hex.DecodeString(strings.TrimPrefix(path, prefix))
			if err != nil || len(token) != 16 {
				return fmt.Errorf("invalid superseded-stage cleanup token")
			}
		}
	}
	for _, path := range []string{j.Cleanup, j.CleanupFrom} {
		if path == "" || path == j.Source {
			continue
		}
		prefix := j.Source + ".undo-remove."
		if len(path) != len(prefix)+32 || path[:len(prefix)] != prefix {
			return fmt.Errorf("invalid primary Undo cleanup path")
		}
		if token, err := hex.DecodeString(path[len(prefix):]); err != nil || len(token) != 16 {
			return fmt.Errorf("invalid primary Undo cleanup token")
		}
	}
	return nil
}

func resumePrimaryUndo(ctx context.Context, fs afero.Fs, repo database.BatchFileOperationRepositoryInterface, op *models.BatchFileOperation, j *models.PrimaryUndo) error {
	if err := primaryUndoDurabilitySupported(); err != nil {
		return err
	}
	if err := validatePrimaryUndo(op, j); err != nil {
		return err
	}
	if j.State == "copying" {
		// A previous process may still have this generation open for writes.
		// Never reuse its inode: rotate by CAS, retaining its ownership, so
		// every stale producer can affect only an unpublished old generation.
		if err := rotatePrimaryUndoStage(ctx, repo, op, j); err != nil {
			return err
		}
	}
	switch j.State {
	case primaryUndoPlannedState:
		if _, err := lstatRestoreSource(fs, j.Destination); !os.IsNotExist(err) {
			return fmt.Errorf("primary Undo destination is occupied or unreadable: %s", j.Destination)
		}
		if err := copyPrimaryUndo(ctx, fs, repo, op, j); err != nil {
			return err
		}
		fallthrough
	case "sealed":
		if _, err := lstatRestoreSource(fs, j.Destination); os.IsNotExist(err) {
			if err := primaryUndoVerify(fs, j.Stage, j.StageIdentity, j.SHA256); err != nil {
				return err
			}
			// The one sealed inode is published exactly once; no helper may restage
			// it to an unjournaled identity. A substitution retains the library copy.
			pubErr := primaryUndoPublish(fs, j.Stage, j.Destination)
			if pubErr != nil && !errors.Is(pubErr, fsutil.ErrPublishCompleted) {
				return fmt.Errorf("publish primary Undo: %w", pubErr)
			}
		} else if err != nil {
			return err
		}
		if err := primaryUndoVerify(fs, j.Destination, j.StageIdentity, j.SHA256); err != nil {
			return err
		}
		if _, err := lstatRestoreSource(fs, j.Stage); !os.IsNotExist(err) {
			return fmt.Errorf("primary Undo stage remains after publication; retained for manual recovery: %s", j.Stage)
		}
		if err := primaryUndoSyncParent(j.Destination); err != nil {
			return err
		}
		if err := primaryUndoState(ctx, repo, op, j, "published"); err != nil {
			return err
		}
		fallthrough
	case "published", "unlinking":
		if err := cleanupPrimaryUndo(ctx, fs, repo, op, j); err != nil {
			return err
		}
		fallthrough
	case "primary_removed":
		if err := cleanupSupersededPrimaryUndo(ctx, fs, repo, op, j); err != nil {
			return err
		}
		fallthrough
	case "done":
		return primaryUndoVerify(fs, j.Destination, j.StageIdentity, j.SHA256)
	default:
		return fmt.Errorf("unknown primary Undo recovery state %q", j.State)
	}
}

func copyPrimaryUndo(ctx context.Context, fs afero.Fs, repo database.BatchFileOperationRepositoryInterface, op *models.BatchFileOperation, j *models.PrimaryUndo) error {
	source, err := primaryUndoOpenPinned(fs, j.Source, j.SourceIdentity, true)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }() // Read-only source pin; no writes to flush.
	_, sourceInfo, err := primaryUndoIdentity(source)
	if err != nil {
		return err
	}
	if j.State != primaryUndoPlannedState {
		return fmt.Errorf("primary Undo copy requires a fresh planned generation")
	}
	stage, err := fs.OpenFile(j.Stage, os.O_CREATE|os.O_EXCL|os.O_RDWR, sourceInfo.Mode().Perm())
	if err != nil {
		return fmt.Errorf("create primary Undo stage (an existing unsealed name requires manual recovery): %w", err)
	}
	// Error-path cleanup only; the writable stage's successful Close is checked
	// explicitly below before the sealed journal can authorize publication.
	defer func() { _ = stage.Close() }()
	identity, _, err := primaryUndoIdentity(stage)
	if err != nil {
		return err
	}
	if err := primaryUndoSyncParent(j.Stage); err != nil {
		return err
	}
	next := clonePrimaryUndo(j)
	next.State = "copying"
	next.StageIdentity = identity
	if err := primaryUndoWrite(ctx, repo, op, j, &next); err != nil {
		return err
	}
	*j = next
	sourceWitness, err := primaryUndoObserveChange(source)
	if err != nil {
		return err
	}
	hash := sha256.New()
	n, err := io.CopyBuffer(stage, io.TeeReader(source, hash), make([]byte, 256*1024))
	if err != nil {
		return err
	}
	after, _, err := primaryUndoIdentity(source)
	if err != nil {
		return err
	}
	afterWitness, err := primaryUndoObserveChange(source)
	if err != nil {
		return err
	}
	if after != j.SourceIdentity || afterWitness != sourceWitness || n != j.SourceIdentity.Size {
		return fmt.Errorf("primary Undo source changed while copying")
	}
	if err := primaryUndoPrepareStage(fs, stage, sourceInfo); err != nil {
		return err
	}
	identity, _, err = primaryUndoIdentity(stage)
	if err != nil {
		return err
	}
	next = clonePrimaryUndo(j)
	next.State = "sealed"
	next.StageIdentity = identity
	next.SHA256 = hex.EncodeToString(hash.Sum(nil))
	if err := stage.Close(); err != nil {
		return err
	}
	if err := primaryUndoWrite(ctx, repo, op, j, &next); err != nil {
		return err
	}
	*j = next
	return nil
}

func cleanupPrimaryUndo(ctx context.Context, fs afero.Fs, repo database.BatchFileOperationRepositoryInterface, op *models.BatchFileOperation, j *models.PrimaryUndo) error {
	if err := primaryUndoVerify(fs, j.Destination, j.StageIdentity, j.SHA256); err != nil {
		return err
	}
	location := j.Source
	if j.State == "unlinking" {
		// A different process may still act on this committed claim. Never
		// replace its endpoints or take ownership from a restarted process:
		// even apparently absent endpoints do not fence a delayed producer.
		return fmt.Errorf("interrupted primary Undo cleanup claim requires manual recovery; retain source %s, claim %s, and history", j.CleanupFrom, j.Cleanup)
	}
	if j.State != "published" {
		return fmt.Errorf("invalid primary Undo cleanup state %q", j.State)
	}
	if err := primaryUndoVerify(fs, location, j.SourceIdentity, j.SHA256); err != nil {
		return err
	}
	token, err := primaryUndoToken()
	if err != nil {
		return err
	}
	next := *j
	next.State = "unlinking"
	next.CleanupFrom = location
	next.Cleanup = j.Source + ".undo-remove." + token
	if err := primaryUndoWrite(ctx, repo, op, j, &next); err != nil {
		return err
	}
	*j = next
	destinationWitness, err := primaryUndoVerifyWitness(fs, j.Destination, j.StageIdentity, j.SHA256)
	if err != nil {
		return err
	}
	// The source's FINAL full hash follows every long SQL/destination proof.
	// Native change-time checks detect same-size, mtime-restamped edits.
	if err := primaryUndoVerify(fs, location, j.SourceIdentity, j.SHA256); err != nil {
		return err
	}
	// The only unlink addresses this unpredictable, durably recorded terminal.
	// Only this live producer may use its newly committed claim. A restarted
	// process retains that claim for manual recovery rather than superseding
	// an endpoint a delayed producer may still use. No hop is unjournaled.
	if err := primaryUndoPublish(fs, location, j.Cleanup); err != nil {
		return fmt.Errorf("take primary Undo cleanup claim: %w", err)
	}
	// Rename changes native change time, so a pre-take witness cannot prove
	// content afterwards. Rehash the claimed source and capture its post-take
	// witness: same-size, mtime-restamped edits during the take must survive.
	sourceWitness, err := primaryUndoVerifyWitness(fs, j.Cleanup, j.SourceIdentity, j.SHA256)
	if err != nil {
		return err
	}
	if err := primaryUndoRecheckWitness(fs, j.Destination, j.StageIdentity, destinationWitness); err != nil {
		return err
	}
	if _, err := lstatRestoreSource(fs, location); !os.IsNotExist(err) {
		return fmt.Errorf("primary Undo cleanup source reoccupied or unreadable; terminal retained")
	}
	// No long read or journal write follows this final source witness check.
	// This is not an atomic conditional unlink: an unconstrained writer can
	// still modify an already-open object after the check and before removal.
	if err := primaryUndoRecheckWitness(fs, j.Cleanup, j.SourceIdentity, sourceWitness); err != nil {
		return err
	}
	if err := primaryUndoRemove(fs, j.Cleanup); err != nil {
		return fmt.Errorf("remove primary Undo cleanup claim: %w", err)
	}
	if err := primaryUndoSyncParent(j.Cleanup); err != nil {
		return err
	}
	return primaryUndoState(ctx, repo, op, j, "primary_removed")
}

func cleanupSupersededPrimaryUndo(ctx context.Context, fs afero.Fs, repo database.BatchFileOperationRepositoryInterface, op *models.BatchFileOperation, j *models.PrimaryUndo) error {
	for len(j.SupersededStages) > 0 {
		if err := primaryUndoVerify(fs, j.Destination, j.StageIdentity, j.SHA256); err != nil {
			return err
		}
		stage := j.SupersededStages[0]
		location := stage.Path
		if stage.Cleanup != "" {
			return fmt.Errorf("interrupted superseded-stage cleanup claim requires manual recovery; retain stage %s, claim %s, and history", stage.CleanupFrom, stage.Cleanup)
		}
		owned, err := primaryUndoOpenPinned(fs, location, stage.Identity, false)
		if err != nil {
			return err
		}
		if err := owned.Close(); err != nil {
			return err
		}
		token, err := primaryUndoToken()
		if err != nil {
			return err
		}
		stage.CleanupFrom = location
		stage.Cleanup = stage.Path + ".undo-remove." + token
		next := clonePrimaryUndo(j)
		next.SupersededStages[0] = stage
		if err := primaryUndoWrite(ctx, repo, op, j, &next); err != nil {
			return err
		}
		*j = next
		destinationWitness, err := primaryUndoVerifyWitness(fs, j.Destination, j.StageIdentity, j.SHA256)
		if err != nil {
			return err
		}
		// An old producer may still be writing this EXCLUSIVELY CREATED
		// scratch inode. It can never publish after losing its generation
		// CAS. Ownership uses its strong object key, never mutable size/time
		// or equal bytes. No old generation is ever opened for writes here.
		if err := primaryUndoPublish(fs, location, stage.Cleanup); err != nil {
			return err
		}
		owned, err = primaryUndoOpenPinned(fs, stage.Cleanup, stage.Identity, false)
		if err != nil {
			return err
		}
		if err := owned.Close(); err != nil {
			return err
		}
		if err := primaryUndoRecheckWitness(fs, j.Destination, j.StageIdentity, destinationWitness); err != nil {
			return err
		}
		if err := primaryUndoRemove(fs, stage.Cleanup); err != nil {
			return err
		}
		if err := primaryUndoSyncParent(stage.Cleanup); err != nil {
			return err
		}
		next = clonePrimaryUndo(j)
		next.SupersededStages = append([]models.PrimaryUndoStage(nil), next.SupersededStages[1:]...)
		if err := primaryUndoWrite(ctx, repo, op, j, &next); err != nil {
			return err
		}
		*j = next
	}
	return primaryUndoState(ctx, repo, op, j, "done")
}

// A stale producer may lose its generation CAS after another process has
// completed Undo. Report that caller's failure without overwriting the
// winner's terminal RevertStatus. The journal itself remains retryable.
func primaryUndoFailure(op *models.BatchFileOperation, err error) *RevertFileResult {
	return &RevertFileResult{OperationID: op.ID, MovieID: op.MovieID, OriginalPath: op.OriginalPath, NewPath: op.NewPath, Outcome: models.RevertOutcomeFailed, Reason: models.RevertReasonUnexpectedPathState, Error: err.Error()}
}
