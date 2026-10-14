package history

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
)

func directoryIdentityMatches(fs afero.Fs, path string, info os.FileInfo, move *models.DirectoryMove) bool {
	if info == nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	dev, ino, ok := fsutil.BoundObjectIdentity(fs, path, info)
	return ok && dev == move.Device && ino == move.Inode
}

// A directory inverse is independent of primary filenames. Only the admitted
// directory object can authorize recovery, including when both aliases vanished.
func (r *Reverter) residentDirectoryLocation(move *models.DirectoryMove) (string, error) {
	lst, ok := r.fs.(afero.Lstater)
	if !ok {
		return "", fmt.Errorf("directory recovery requires no-follow lookup")
	}
	var found string
	for _, path := range append([]string{move.Original}, move.Locations...) {
		info, noFollow, err := lst.LstatIfPossible(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || !noFollow {
			return "", fmt.Errorf("directory recovery cannot inspect %s: %v", path, err)
		}
		if directoryIdentityMatches(r.fs, path, info, move) {
			found = path
			continue
		}
		// Reservation files can remain from a pre-take crash; they are not the
		// directory. An occupied original always blocks compensation.
		if path == move.Original {
			return "", fmt.Errorf("directory recovery destination conflict: %s", path)
		}
	}
	if found == "" {
		return "", fmt.Errorf("directory recovery admitted object is missing")
	}
	return found, nil
}

func (r *Reverter) restoreResidentDirectory(ctx context.Context, op *models.BatchFileOperation, move *models.DirectoryMove) error {
	source, err := r.residentDirectoryLocation(move)
	if err != nil || source == move.Original {
		return err
	}
	scratch := filepath.Join(filepath.Dir(source), ".javinizer-directory-"+rand.Text())
	// Record the recovery scratch name before the filesystem can consume the
	// directory. Interrupted Undo is recoverable through the same identity.
	release := fsutil.SharedJournalLocks().Acquire(strconv.Itoa(int(op.ID)))
	err = r.batchFileOpRepo.UpdateJournalInTx(ctx, op.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		ledger, err := models.ParseGeneratedFiles(current.GeneratedFiles)
		if err != nil {
			return ledger, false, err
		}
		if ledger.DirectoryMove == nil {
			return ledger, false, fmt.Errorf("directory recovery journal disappeared")
		}
		ledger.DirectoryMove.Locations = append(ledger.DirectoryMove.Locations, scratch)
		return ledger, true, nil
	})
	release()
	if err != nil {
		return err
	}
	claimFile, err := r.fs.OpenFile(scratch, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	claim, statErr := claimFile.Stat()
	closeErr := claimFile.Close()
	if claim != nil {
		defer func() { _ = fsutil.UnlinkVerified(r.fs, scratch, claim) }()
	}
	if err := errors.Join(statErr, closeErr); err != nil {
		return err
	}
	hold, err := fsutil.TakeAside(fsutil.TakeAsideSpec{FS: r.fs, Src: source, Scratch: scratch, Claim: claim, Prove: func(info os.FileInfo) error {
		if !directoryIdentityMatches(r.fs, scratch, info, move) {
			return fmt.Errorf("directory recovery identity changed")
		}
		return nil
	}})
	if err != nil {
		return err
	}
	if err := fsutil.PublishNoReplace(r.fs, scratch, move.Original); err != nil {
		return errors.Join(err, hold.Restore())
	}
	info, noFollow, err := r.fs.(afero.Lstater).LstatIfPossible(move.Original)
	if err != nil || !noFollow || !directoryIdentityMatches(r.fs, move.Original, info, move) {
		return fmt.Errorf("restored directory no longer names admitted object")
	}
	return nil
}

func remapResidentDirectoryPath(move *models.DirectoryMove, path string) string {
	if path == "" {
		return path
	}
	for _, root := range move.Locations {
		rel, err := filepath.Rel(root, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.Join(move.Original, rel)
		}
	}
	return path
}

// Recovery proofs travel with their directory, including on a resumed Undo
// after the directory inverse completed but artifact cleanup did not.
func remapResidentDirectoryLedger(ledger *models.GeneratedFilesJSON) {
	if ledger.DirectoryMove == nil {
		return
	}
	remap := func(path string) string { return remapResidentDirectoryPath(ledger.DirectoryMove, path) }
	for i := range ledger.Delete {
		ledger.Delete[i] = remap(ledger.Delete[i])
	}
	for i := range ledger.PlannedDeletes {
		ledger.PlannedDeletes[i].Path = remap(ledger.PlannedDeletes[i].Path)
	}
	for i := range ledger.MoveBack {
		ledger.MoveBack[i].OriginalPath = remap(ledger.MoveBack[i].OriginalPath)
		ledger.MoveBack[i].NewPath = remap(ledger.MoveBack[i].NewPath)
		if ledger.MoveBack[i].Proof != nil {
			ledger.MoveBack[i].Proof.Path = remap(ledger.MoveBack[i].Proof.Path)
		}
	}
}

// residentCleanupAliasBoundary accepts an alternate spelling only when each
// traversed component is a real directory and one is the exact stop object.
// It grants no file ownership and never expands cleanup above that object.
func residentCleanupAliasBoundary(fs afero.Fs, dir, stop string) string {
	if filepath.Clean(stop) == "." {
		return ""
	}
	lst, ok := fs.(afero.Lstater)
	if !ok {
		return ""
	}
	info, noFollow, err := lst.LstatIfPossible(stop)
	if err != nil || !noFollow || info == nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ""
	}
	dev, ino, strong := fsutil.BoundObjectIdentity(fs, stop, info)
	if !strong {
		return ""
	}
	identity := &models.DirectoryMove{Device: dev, Inode: ino}
	for current := filepath.Clean(dir); ; current = filepath.Dir(current) {
		info, noFollow, err := lst.LstatIfPossible(current)
		if err != nil || !noFollow || info == nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ""
		}
		if directoryIdentityMatches(fs, current, info, identity) {
			return current
		}
		if filepath.Dir(current) == current {
			return ""
		}
	}
}
