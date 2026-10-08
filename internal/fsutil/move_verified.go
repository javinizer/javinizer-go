package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/config"
)

// VerifiedSourceProof is the admission-binding hook of the verified publish
// composites (codex P1, PRRT_kwDORn9KaM6m9ae4): the caller pins which object
// the source path named at admission time, and the composite invokes it with
// a FRESH lookup of the object it is about to consume — the taken-aside
// object at its claimed name on the move leg, the just-opened handle's own
// Stat on the copy leg. A nil answer admits the object; a non-nil answer
// refuses the leg BEFORE the rejected object is published, with whatever the
// take-aside already relocated restored no-replace.
type VerifiedSourceProof func(path string, info os.FileInfo) error

// MoveFileNoReplaceVerified is MoveFileNoReplace with the source consumption
// bound to a caller-admitted identity. Validation and publication are
// otherwise separate filesystem operations: a rename-swap landing between the
// caller's last validation and this call leaves the source path naming a
// replacement object, and an unverified composite would consume THAT. The
// take-aside vocabulary (bound_take.go) closes the window without any
// open-by-identity syscall:
//
//  1. a fresh unpredictable claim sibling name is reserved O_EXCL and freed
//     identity-bound (claim/releaseTakeAsideVacName), then src moves onto it
//     NO-REPLACE (PublishNoReplace — a rename where the kernel offers a
//     no-replace rename, a link+verified-unlink on other POSIX OsFs, the
//     classify-then-rename test leg on virtual filesystems). Whatever src
//     named at that instant rides over byte-intact; a vanished source is the
//     typed take-aside vanish class.
//  2. the object AT the claim name is re-proven to name the admitted
//     identity. A mismatch (a swap won the window before the take) refuses:
//     the taken-aside object — the foreign replacement — rides back onto src
//     NO-REPLACE, so foreign bytes are never consumed and never displaced
//     (restore collisions join ErrTakeAsideRestoreFailed and leave both
//     recoverable).
//  3. the verified claim publishes onto dst NO-REPLACE. The residual
//     proof→publish gap is the accepted fresh-claimed-name terminal boundary
//     of UnlinkVerified: the claim name is a crypto-token draw no foreign
//     writer can predict, so re-resolving it cannot be raced short of
//     reclaiming the draw itself.
//  4. EXDEV (library on another volume): the claim is opened, its handle
//     re-proven against the admission proof (and, since the descriptor pins
//     the object but not its bytes, the opened object's handle-derived
//     identity is re-proven once more between the stream's completion and
//     the publish), streamed into dest-adjacent
//     O_EXCL staging and bound-published no-replace (the verified copy's
//     construction), then the consumed claim is removed ONLY through
//     UnlinkVerified's claim-bound terminal unlink. A refused cleanup keeps
//     BOTH objects and wraps ErrPublishCompleted exactly like
//     MoveFileNoReplace's cross-device leg, so pending-kind classifiers hold.
//
// Occupancy, unsupported-volume, and publish-completed classes mirror
// MoveFileNoReplace's (ErrPublishCollision / ErrPublishNoReplaceUnsupported /
// ErrPublishCompleted) so callers classify on the unchanged vocabulary; the
// admission refusal additionally carries ErrTakeAsideForeign. A nil proof
// preserves the legacy by-name behavior unchanged.
func MoveFileNoReplaceVerified(fs afero.Fs, src, dst string, proof VerifiedSourceProof) error {
	_, err := MoveFileNoReplaceVerifiedMode(fs, src, dst, proof, stagingFileMode())
	return err
}

// MoveFileNoReplaceVerifiedMode is MoveFileNoReplaceVerified with an explicit
// staging mode for its cross-device leg (codex P2, PRRT_kwDORn9KaM6pw_EY): the
// same-volume rename carries the consumed object's own permission bits either
// way, while the EXDEV stream publishes destination-adjacent bytes and would
// otherwise stage them at the umask-masked default — widening a private or
// read-only source the caller admitted. The mode applies verbatim; a nil proof
// keeps the legacy by-name behavior AND the default staging mode: the mode
// describes an ADMITTED source, and nothing was admitted without a proof. The
// installed object's identity rides out (codex P1, PRRT_kwDORn9KaM6p3Dq1): a
// same-volume rename installs the claimed object itself and the EXDEV leg the
// object its stream published, so the caller binds THAT identity rather than
// a destination lookup taken after a later writer may have replanted the name.
func MoveFileNoReplaceVerifiedMode(fs afero.Fs, src, dst string, proof VerifiedSourceProof, mode os.FileMode) (*BoundInstallIdentity, error) {
	if proof == nil {
		return nil, MoveFileNoReplace(fs, src, dst)
	}
	done, err := classifyNoreplaceDestination(fs, src, dst)
	if done || err != nil {
		return nil, err
	}
	if err := fs.MkdirAll(filepath.Dir(dst), config.DirPerm); err != nil {
		return nil, fmt.Errorf("verified move: create destination directory: %w", err)
	}
	claimName, claimClaim, cerr := claimTakeAsideVacName(fs, src)
	if cerr != nil {
		return nil, fmt.Errorf("verified move: reserve the source claim name for %s: %w", src, cerr)
	}
	if relErr := releaseTakeAsideVacClaim(fs, claimName, claimClaim); relErr != nil {
		return nil, fmt.Errorf("verified move: free the source claim name for %s: %w", src, relErr)
	}
	restoreClaim := func(cause error) error {
		// Wedge compensation mirrors BoundAside.Restore: the taken-aside object
		// rides back onto the source name NO-REPLACE, so a racer re-claiming
		// the source name mid-compensation is never clobbered — the object
		// stays recoverable at the claim name and the error classifies.
		if back := PublishNoReplace(fs, claimName, src); back != nil {
			return errors.Join(cause, fmt.Errorf("%w: %s stays recoverable at claim %s: %v", ErrTakeAsideRestoreFailed, src, claimName, back))
		}
		return cause
	}
	if takeErr := PublishNoReplace(fs, src, claimName); takeErr != nil {
		if errors.Is(takeErr, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: verified move source %s vanished under the claim take", ErrTakeAsideVanished, src)
		}
		if PublishCompleted(takeErr) && !PublishRefusal(takeErr) {
			// The take is an INTERNAL hop: its completed-with-residue class
			// describes the CLAIM name (a link landed there but the source
			// unlink refused), never the destination — nothing reached dst.
			// Leaking ErrPublishCompleted upward would let compensating callers
			// register the video target as published, so the class is re-stated
			// stripped (refusal classes stay wrapped below). Both names hold
			// the source's bytes; any residue is recoverable at the
			// unguessable claim name.
			return nil, fmt.Errorf("verified move: claim take of %s refused — nothing reached the destination (any residue recoverable at the claim name): %v", src, takeErr)
		}
		return nil, fmt.Errorf("verified move: claim take of %s refused — nothing relocated: %w", src, takeErr)
	}
	claimInfo, statErr := asideLstat(fs, claimName)
	switch {
	case errors.Is(statErr, os.ErrNotExist):
		return nil, fmt.Errorf("%w: verified move claim %s empty at the post-take re-proof", ErrTakeAsideVanished, claimName)
	case statErr != nil:
		return nil, restoreClaim(fmt.Errorf("verified move: inspect the taken claim %s: %w", claimName, statErr))
	}
	if perr := proof(claimName, claimInfo); perr != nil {
		return nil, restoreClaim(fmt.Errorf("verified move: the claimed object failed its admission proof (%w): %w", ErrTakeAsideForeign, perr))
	}
	claimDevice, claimInode, claimOK := BoundObjectIdentity(fs, claimName, claimInfo)
	if pubErr := PublishNoReplace(fs, claimName, dst); pubErr != nil {
		if !isCrossDeviceError(pubErr) {
			return nil, restoreClaim(fmt.Errorf("verified move: no-replace publish of the claimed %s onto %s refused: %w", claimName, dst, pubErr))
		}
		installed, copyErr := copyClaimAcrossDevices(fs, claimName, dst, proof, mode)
		if copyErr != nil {
			return nil, restoreClaim(copyErr)
		}
		if rmErr := UnlinkVerified(fs, claimName, claimInfo); rmErr != nil {
			return nil, fmt.Errorf("%w: verified move published to %s but the claimed-source cleanup refused (%w) — the taken-aside source stays recoverable at %s", ErrPublishCompleted, dst, rmErr, claimName)
		}
		// The cross-device leg published a NEW object: the stream tail's
		// installed identity is the object this call produced.
		return installed, nil
	}
	// Same-volume rename: the destination now names the claimed object the take
	// hop captured, so that pre-publish stat IS the installed identity.
	return newBoundInstallIdentity(claimInfo, claimDevice, claimInode, claimOK), nil
}

// copyClaimAcrossDevices is the verified move's EXDEV leg: the claimed
// (admission-proven) object is opened through the verified-source open (the
// delete-shared platform leg), the handle re-proven against the admission
// proof before a byte flows, and streamed through the verified copy's
// stage/publish tail. Every failure leaves the unwinding to the caller's
// claim compensation; the handle itself is closed inside
// streamVerifiedSource on every branch, before the caller's bound unlink
// re-points the consumed claim (close-before-remove — load-bearing on
// Windows, where a stale pin parks the name's directory slot).
func copyClaimAcrossDevices(fs afero.Fs, claimName, dst string, proof VerifiedSourceProof, mode os.FileMode) (*BoundInstallIdentity, error) {
	srcFile, err := openVerifiedSource(fs, claimName)
	if err != nil {
		return nil, fmt.Errorf("verified move: open the claimed source %s for the cross-device publish: %w", claimName, err)
	}
	return streamVerifiedSource(fs, srcFile, claimName, dst, proof, "verified move", "claim handle", true, mode)
}

// streamVerifiedSource is the shared verified-stream inner leg of both
// verified composites: the caller pins the source object through
// openVerifiedSource and owns NO further handle discipline — the defer below
// is registered at entry, so the handle closes deterministically in EVERY
// exit branch (stat failure, admission refusal, staging/stream/publish
// failure, success) before the caller runs its next filesystem verb against
// the consumed entry. On Windows that close-before-remove ordering is
// load-bearing: the verb (bound unlink, swap cleanup, follow-up publish)
// fails against an entry whose any past opener omitted the delete share,
// and a leaked handle holds the slot until process exit. The handle's own
// Stat re-proves against the admission proof BEFORE a byte flows, and the
// pinned descriptor supplies the staged stream, so a name-swap anywhere
// inside the window can never retarget the published bytes. The descriptor
// pins the file object, not an immutable snapshot of its bytes, so the
// opened object's handle-derived identity re-proves between the stream's
// completion and the bound publish (reproofStreamedSource — handle-legs-only,
// never a path re-resolve): an in-place rewrite landing mid-stream refuses
// with the staged copy discarded instead of publishing content the proof
// never admitted.
func streamVerifiedSource(fs afero.Fs, srcFile afero.File, src, dst string, proof VerifiedSourceProof, op, noun string, crossDevice bool, mode os.FileMode) (*BoundInstallIdentity, error) {
	defer func() { _ = srcFile.Close() }()
	srcInfo, statErr := srcFile.Stat()
	if statErr != nil {
		return nil, fmt.Errorf("%s: inspect the open %s %s: %w", op, noun, src, statErr)
	}
	if perr := proof(src, srcInfo); perr != nil {
		return nil, fmt.Errorf("%s: the open %s failed its admission proof (%w): %w", op, noun, ErrTakeAsideForeign, perr)
	}
	installed, copyErr := copyStreamNoReplaceReproof(fs, srcFile, dst, reproofStreamedSource(srcFile, srcInfo, src, op, noun), mode)
	if copyErr != nil {
		if crossDevice {
			return nil, fmt.Errorf("%s: cross-device publish of the claim onto %s: %w", op, dst, copyErr)
		}
		return nil, copyErr
	}
	return installed, nil
}

// CopyFileNoReplaceVerified is CopyFileNoReplace with the consumed bytes bound
// to a caller-admitted identity through the OPEN HANDLE: the source is opened
// once, the handle's own Stat is proven against the admission proof, and the
// stream stages exclusively from that handle. The descriptor pins the inode
// from open to read completion, so a rename-swap of the source NAME anywhere
// inside the window cannot retarget the staged bytes — either the admission
// identity publishes, or a pre-verify swap refuses typed (ErrTakeAsideForeign)
// having landed nothing. Classification, occupancy, and publish classes mirror
// CopyFileNoReplace. The no-clobber capability preflight is deliberately left
// to the bound publish itself: the probe's verdict and the publish's refusal
// derive from the same volumes, and the take-aside / staged-publish legs
// already refuse unsupported volumes with the identical typed class. A nil
// proof preserves the legacy by-name behavior unchanged.
func CopyFileNoReplaceVerified(fs afero.Fs, src, dst string, proof VerifiedSourceProof) error {
	_, err := CopyFileNoReplaceVerifiedMode(fs, src, dst, proof, stagingFileMode())
	return err
}

// CopyFileNoReplaceVerifiedMode is CopyFileNoReplaceVerified with an explicit
// destination staging mode (codex P2, PRRT_kwDORn9KaM6pw_EY): the deferred
// publication's copy lane publishes the ADMITTED source's permission bits
// through it, so the library entry keeps a private (0640) or read-only
// source's bits instead of inheriting the umask-masked staging default — the
// widening the pre-deferral flow never had (prepareArtifact staged the copy at
// the source's own bits and the publication renamed that staged file into
// place). The mode applies verbatim: the caller admitted these exact bits, so
// no umask re-masking applies. A nil proof keeps the sibling's contract — the
// by-name leg and the default staging mode.
func CopyFileNoReplaceVerifiedMode(fs afero.Fs, src, dst string, proof VerifiedSourceProof, mode os.FileMode) (*BoundInstallIdentity, error) {
	if proof == nil {
		return nil, CopyFileNoReplace(fs, src, dst)
	}
	done, err := classifyNoreplaceDestination(fs, src, dst)
	if done || err != nil {
		return nil, err
	}
	if err := fs.MkdirAll(filepath.Dir(dst), config.DirPerm); err != nil {
		return nil, fmt.Errorf("verified copy: create destination directory: %w", err)
	}
	srcFile, err := openVerifiedSource(fs, src)
	if err != nil {
		return nil, fmt.Errorf("verified copy: open source %s: %w", src, err)
	}
	return streamVerifiedSource(fs, srcFile, src, dst, proof, "verified copy", "source handle", false, mode)
}

// CopyFileNoReplaceVerifiedInstall is CopyFileNoReplaceVerified with the
// INSTALLED destination object's identity handed back (codex P1, PR #276,
// finding ntCe6): the verified copy proves WHICH object it consumed, and the
// returned FileInfo proves WHICH object it installed — the bound publish's
// post-publish reverify stat (os.SameFile-bound to the staged inode on the
// native legs; see copyStreamNoReplaceInstallReproof for the virtual leg's
// lookup and the indeterminate-lookup doubt class). A caller recording the
// install for later compensation (the deferred publication's rollback
// observation) must bind THAT identity to its record instead of re-resolving
// the destination name afterwards: between this copy's lock release and the
// caller's observation an external writer can replace the destination, and a
// record re-derived from the NAME then authenticates the foreign successor —
// arming an identity-verified delete against bytes this operation never
// wrote. Error classes are exactly CopyFileNoReplaceVerified's (the legacy
// nil-proof lane returns a nil identity with the by-name behavior).
func CopyFileNoReplaceVerifiedInstall(fs afero.Fs, src, dst string, proof VerifiedSourceProof) (*BoundInstallIdentity, error) {
	if proof == nil {
		return nil, CopyFileNoReplace(fs, src, dst)
	}
	done, err := classifyNoreplaceDestination(fs, src, dst)
	if done || err != nil {
		return nil, err
	}
	if err := fs.MkdirAll(filepath.Dir(dst), config.DirPerm); err != nil {
		return nil, fmt.Errorf("verified copy: create destination directory: %w", err)
	}
	srcFile, err := openVerifiedSource(fs, src)
	if err != nil {
		return nil, fmt.Errorf("verified copy: open source %s: %w", src, err)
	}
	return streamVerifiedSourceInstall(fs, srcFile, src, dst, proof)
}

// streamVerifiedSourceInstall is the identity-returning twin of
// streamVerifiedSource for the same-volume copy lane, sharing its handle
// discipline byte for byte: the defer closes the pinned source descriptor in
// EVERY exit branch before the caller runs its next filesystem verb, and the
// admission proof re-runs against the handle's own Stat before a byte flows.
// The publish tail returns the installed object's proven identity; a refusal
// (admission, mid-stream drift, publish classes) discards the staged copy and
// attributes no identity.
func streamVerifiedSourceInstall(fs afero.Fs, srcFile afero.File, src, dst string, proof VerifiedSourceProof) (*BoundInstallIdentity, error) {
	defer func() { _ = srcFile.Close() }()
	srcInfo, statErr := srcFile.Stat()
	if statErr != nil {
		return nil, fmt.Errorf("verified copy: inspect the open source handle %s: %w", src, statErr)
	}
	if perr := proof(src, srcInfo); perr != nil {
		return nil, fmt.Errorf("verified copy: the open source handle failed its admission proof (%w): %w", ErrTakeAsideForeign, perr)
	}
	return copyStreamNoReplaceInstallReproof(fs, srcFile, dst, reproofStreamedSource(srcFile, srcInfo, src, "verified copy", "source handle"))
}

// CopyFileNoReplaceVerifiedDigest is CopyFileNoReplaceVerified with the
// published bytes' sha256 teed off the single verified stream (seal evidence
// for the deferred publication's interim copy pin): the digest certifies the
// EXACT admitted-object bytes that reached the destination — the proof is
// re-run against the open handle before a byte flows, and the pinned object's
// handle-derived identity is re-proven between the stream's completion and
// the publish (a mid-stream in-place rewrite refuses: the staged copy is
// discarded and no digest is attributed), and the tee
// counts only what that handle yields, so neither a swap nor a rewrite can
// make the returned digest describe bytes other than the published ones. A
// nil proof degrades to the by-name CopyFileNoReplaceDigest, mirroring the
// verified composite's nil contract.
func CopyFileNoReplaceVerifiedDigest(fs afero.Fs, src, dst string, proof VerifiedSourceProof) (string, error) {
	digest, _, err := CopyFileNoReplaceVerifiedDigestMode(fs, src, dst, proof, stagingFileMode())
	return digest, err
}

// CopyFileNoReplaceVerifiedDigestMode is CopyFileNoReplaceVerifiedDigest with an
// explicit destination staging mode (codex P2, PRRT_kwDORn9KaM6pw_EY) — the
// digest lane's twin of CopyFileNoReplaceVerifiedMode, same contract: the mode
// is the ADMITTED source's bits applied verbatim, and a nil proof keeps the
// by-name leg with the default staging mode.
func CopyFileNoReplaceVerifiedDigestMode(fs afero.Fs, src, dst string, proof VerifiedSourceProof, mode os.FileMode) (string, *BoundInstallIdentity, error) {
	if proof == nil {
		digest, err := CopyFileNoReplaceDigest(fs, src, dst)
		return digest, nil, err
	}
	done, err := classifyNoreplaceDestination(fs, src, dst)
	if done || err != nil {
		return "", nil, err
	}
	if err := fs.MkdirAll(filepath.Dir(dst), config.DirPerm); err != nil {
		return "", nil, fmt.Errorf("verified copy: create destination directory: %w", err)
	}
	srcFile, err := openVerifiedSource(fs, src)
	if err != nil {
		return "", nil, fmt.Errorf("verified copy: open source %s: %w", src, err)
	}
	return streamVerifiedSourceDigest(fs, srcFile, src, dst, proof, "verified copy", "source handle", mode)
}

// streamVerifiedSourceDigest is the digest-returning twin of
// streamVerifiedSource for the same-volume copy lane, sharing its handle
// discipline byte for byte: the defer closes the pinned source descriptor in
// EVERY exit branch before the caller runs its next filesystem verb, and the
// admission proof re-runs against the handle's own Stat before a byte flows.
// The digest tees the staged stream, and the same handle-derived post-stream
// re-proof runs before the bound publish — a mid-stream in-place rewrite
// discards the staged copy and attributes no digest — so a returned digest
// counts exactly the bytes the proof admitted.
func streamVerifiedSourceDigest(fs afero.Fs, srcFile afero.File, src, dst string, proof VerifiedSourceProof, op, noun string, mode os.FileMode) (string, *BoundInstallIdentity, error) {
	defer func() { _ = srcFile.Close() }()
	srcInfo, statErr := srcFile.Stat()
	if statErr != nil {
		return "", nil, fmt.Errorf("%s: inspect the open %s %s: %w", op, noun, src, statErr)
	}
	if perr := proof(src, srcInfo); perr != nil {
		return "", nil, fmt.Errorf("%s: the open %s failed its admission proof (%w): %w", op, noun, ErrTakeAsideForeign, perr)
	}
	return copyStreamNoReplaceDigestReproof(fs, srcFile, dst, reproofStreamedSource(srcFile, srcInfo, src, op, noun), mode)
}

// reproofStreamedSource builds the post-stream re-proof both verified stream
// lanes run between the staged copy's completion and its bound publish (codex
// P2, PRRT_kwDORn9KaM6nkVjY): the handle pinned at open keeps the file
// OBJECT, not an immutable snapshot of its bytes, so an in-place rewrite
// landing after the admission proof but mid-stream would otherwise publish —
// and on the digest lane digest-certify — content the proof never admitted.
// The re-proof compares the OPENED OBJECT's identity captured through the
// SAME handle mechanism on both sides of the stream — admitted is the
// pre-stream handle Stat the admission proof just accepted, and the closure
// repeats the handle Stat after the staged stream completes. It deliberately
// never re-runs the caller's proof or re-resolves the source path: the
// Windows admission identity strong leg (BoundObjectIdentity) derives the
// volume-serial+file-index pair by re-opening the PATH, so re-running it
// after a post-open rename-over would bind the REPLACEMENT and falsely refuse
// the still-admitted object the descriptor pins (the wave-44 Windows false
// trip). A rename moves the NAME, not the handle legs — the dev/inode or
// volume/index pair plus size and last write time all hold across one —
// while an in-place truncate/rewrite of the pinned object moves the size or
// write-time legs and refuses with the ErrTakeAsideForeign class; the shared
// tail discards the staged copy (nothing published, no digest attributed).
// Handles exposing no comparable kernel key (virtual filesystems, wrapping
// handles) keep the size+modtime legs only, the POSIX in-memory posture.
func reproofStreamedSource(srcFile afero.File, admitted os.FileInfo, src, op, noun string) func() error {
	admDevice, admInode, admStrong := streamedHandleIdentity(srcFile, admitted)
	admSize, admModTime := admitted.Size(), admitted.ModTime()
	return func() error {
		info, err := srcFile.Stat()
		if err != nil {
			return fmt.Errorf("%s: re-inspect the streamed %s %s: %w", op, noun, src, err)
		}
		curDevice, curInode, curStrong := streamedHandleIdentity(srcFile, info)
		if curStrong != admStrong || (admStrong && (curDevice != admDevice || curInode != admInode)) ||
			info.Size() != admSize || !info.ModTime().Equal(admModTime) {
			return fmt.Errorf("%s: the streamed %s %s drifted from its admitted object mid-stream (%w)", op, noun, src, ErrTakeAsideForeign)
		}
		return nil
	}
}
