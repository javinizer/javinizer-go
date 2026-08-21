package fsutil

import (
	"bytes"
	cryptorand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/spf13/afero"
)

const (
	// ReplacementBusySuffix is adjacent to a destination so every process
	// arbitrating that destination observes the same ownership marker.
	ReplacementBusySuffix         = ".dlbusy"
	replacementBusyStaleAge       = 2 * time.Minute
	replacementBusyQuarantineMark = ".quarantine-"
)

// ErrReplacementBusy means another live process owns a destination replacement.
var ErrReplacementBusy = errors.New("replacement destination is busy")

// replacementBusyBootAt prevents a PID reused after this process started from
// being mistaken for an owner from this boot. The timestamp in the marker is
// written before the writer renames the destination aside.
var replacementBusyBootAt = time.Now()
var replacementIsWindows = runtime.GOOS == "windows"

// replacementPIDLiveness is richer than a bool: failure to inspect a PID can
// mean that the owner is gone, still alive, or simply undecidable from this
// process's permissions and platform probes.
type replacementPIDLiveness uint8

const (
	replacementPIDDead replacementPIDLiveness = iota
	replacementPIDAlive
	replacementPIDUnprobeable
)

var replacementProbePIDAliveAware = replacementProbePIDAliveAwarePlatform
var replacementProcessStartTime = replacementProcessStartTimePlatform

// replacementStartTimeFromUnixNano validates a platform-provided owner start
// time. A non-positive stamp (Windows handed back a zero/1601 FILETIME, or a
// platform reported the Unix epoch itself) cannot describe a real marker
// owner, so it yields nil and classification keeps the liveness-only fallback
// rather than comparing garbage against the marker timestamp.
//
//nolint:unused // wired in the windows-tagged seam (replacement_busy_pid_windows.go); host-GOOS lint cannot see the cross-platform use.
func replacementStartTimeFromUnixNano(nsec int64) *time.Time {
	if nsec <= 0 {
		return nil
	}
	start := time.Unix(0, nsec)
	return &start
}

var replacementBusyRandom = replacementBusyRandomPlatform
var replacementCryptoRandomRead = cryptorand.Read

// ReplacementBusyPath returns the durable in-flight marker for dest.
func ReplacementBusyPath(dest string) string { return dest + ReplacementBusySuffix }

// ReadReplacementBusyToken reads the bytes of dest's durable busy marker. The
// sweep records the token its O_EXCL claim just wrote (wave-55) so each
// mutating stage gate can re-prove the on-disk marker still names this
// claimant's token. A marker that vanished, was taken aside, or was
// re-acquired by another claimant reads a different (or absent) token.
func ReadReplacementBusyToken(fs afero.Fs, dest string) (string, error) {
	content, err := afero.ReadFile(fs, ReplacementBusyPath(dest))
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// ReplacementBusyMarkerIsOurs reports whether dest's durable busy marker
// currently reads exactly token. A marker that is missing, unreadable, or
// byte-divergent is not provably ours: another claimant owns the name now (the
// reclaim took it aside, or a successor re-claimed it), so the caller must
// abandon its stage before mutating. The token carries the claimant's pid and
// a nanosecond timestamp, so byte equality is the ownership identity fact — no
// two live claims share it.
func ReplacementBusyMarkerIsOurs(fs afero.Fs, dest, token string) bool {
	current, err := ReadReplacementBusyToken(fs, dest)
	if err != nil {
		return false
	}
	return current == token
}

// AcquireReplacementBusy atomically claims the destination-adjacent marker.
// Writers create it before moving the destination aside; sweepers create it
// before touching a backup. A marker from a dead PID is reclaimed, as is a
// well-formed marker carrying released=1 (the in-band release pre-wave-38
// wedged releases recorded so a wedged removal could not busy-block the
// destination for that process's lifetime), while a malformed marker is
// never reclaimed based on its age alone so an unowned file cannot be
// mistaken for Javinizer's marker.
func AcquireReplacementBusy(fs afero.Fs, dest string) (func(), error) {
	release, _, err := AcquireReplacementBusyEx(fs, dest)
	return release, err
}

// AcquireReplacementBusyEx is AcquireReplacementBusy with a provenance-bound
// token return (wave-56, history finding F2): the token the claimant just
// wrote rides the acquisition API itself, so a ledger that records it never
// re-reads the marker by pathname — a racing swap or a transient read
// failure after the acquire could otherwise adopt an empty/foreign token
// and silently disarm the ownership-attestation gate. Callers that journal
// the claim for the in-process sweep ledger MUST record THIS token; the
// once-guarded release closes over the same token. The token is always
// non-empty on a nil error (a fresh pid+time marker); a caller that treats
// an empty token as a failed acquire refuses to record the claim (provenance
// unavailable).
func AcquireReplacementBusyEx(fs afero.Fs, dest string) (func(), string, error) {
	path := ReplacementBusyPath(dest)
	for {
		token := replacementBusyToken()
		file, err := fs.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			if _, err = file.WriteString(token); err != nil {
				discardBusyMarkerClaim(fs, path, file, nil)
				return nil, "", fmt.Errorf("write replacement busy marker: %w", err)
			}
			if err = file.Sync(); err != nil {
				discardBusyMarkerClaim(fs, path, file, nil)
				return nil, "", fmt.Errorf("sync replacement busy marker: %w", err)
			}
			// The identity anchor is captured BEFORE the close: the close-failure
			// leg below (and only it) runs with the handle already shut, where a
			// fstat can no longer be taken.
			claimIdentity, claimStatErr := file.Stat()
			if claimStatErr != nil {
				discardBusyMarkerClaim(fs, path, file, nil)
				return nil, "", fmt.Errorf("stat replacement busy marker: %w", claimStatErr)
			}
			if err = file.Close(); err != nil {
				discardBusyMarkerClaim(fs, path, file, claimIdentity)
				return nil, "", fmt.Errorf("close replacement busy marker: %w", err)
			}
			var once sync.Once
			return func() {
				once.Do(func() { releaseReplacementBusy(fs, path, token) })
			}, token, nil
		}
		if !os.IsExist(err) {
			return nil, "", fmt.Errorf("create replacement busy marker: %w", err)
		}

		inspection, inspectErr := replacementBusyInspect(fs, path)
		if inspectErr != nil {
			return nil, "", fmt.Errorf("inspect replacement busy marker: %w", inspectErr)
		}
		if !inspection.stale {
			return nil, "", ErrReplacementBusy
		}
		if !inspection.reclaimable {
			logging.Warnf("replacement busy marker %s is stale but not a recognized Javinizer marker; preserving it", path)
			return nil, "", ErrReplacementBusy
		}
		if !inspection.hasObservedToken {
			// The marker disappeared while it was being inspected. Refresh from
			// the create attempt instead of renaming without bytes to validate.
			continue
		}

		// Do not remove path based on the earlier inspection. Another claimant
		// may have won the same stale-marker decision in the meantime. Rename
		// is the portable afero ownership claim: only the claimant whose source
		// rename succeeds may inspect and dispose of its uniquely named
		// successor.
		takeoverPath, nameErr := replacementBusyTakeoverPath(path)
		if nameErr != nil {
			return nil, "", fmt.Errorf("name replacement busy takeover marker: %w", nameErr)
		}
		if renameErr := fs.Rename(path, takeoverPath); renameErr != nil {
			if !os.IsNotExist(renameErr) {
				return nil, "", fmt.Errorf("claim replacement busy marker: %w", renameErr)
			}

			// A failed source rename means another claimant won. Re-read the
			// marker from scratch; never apply the stale result above to the
			// winner's replacement marker.
			refreshed, refreshErr := replacementBusyInspect(fs, path)
			if refreshErr != nil {
				return nil, "", fmt.Errorf("reinspect replacement busy marker: %w", refreshErr)
			}
			if !refreshed.stale {
				return nil, "", ErrReplacementBusy
			}
			if !refreshed.reclaimable {
				logging.Warnf("replacement busy marker %s is stale but not a recognized Javinizer marker; preserving it", path)
				return nil, "", ErrReplacementBusy
			}
			continue
		}

		// The takeover content AND its identity are observed through ONE open
		// handle (the releaseObserve discipline): the token bytes and the
		// identity snapshot ride the same descriptor, so the byte-compare below
		// can never alias the recorded token to a successor object, and the
		// reclaim unlink re-proves the name against THAT identity at unlink
		// adjacency — a foreign swap inside the read→remove window is refused
		// byte-intact instead of being deleted as the stale marker.
		claimedToken, observedIdentity, readErr := replacementBusyObserveTakeover(fs, takeoverPath)
		if readErr != nil {
			// We own the successor, but cannot prove which marker it contains.
			// Leave it in place and fail closed rather than consuming it.
			return nil, "", fmt.Errorf("read replacement busy takeover marker: %w", readErr)
		}
		if !bytes.Equal(claimedToken, inspection.observedToken) {
			if returnErr := replacementBusyReturnTakeover(fs, path, takeoverPath, claimedToken, observedIdentity); returnErr != nil {
				return nil, "", returnErr
			}
			return nil, "", ErrReplacementBusy
		}
		if removeErr := releaseClaimedBusyObject(fs, takeoverPath, observedIdentity); removeErr != nil {
			// The successful rename proves ownership of takeoverPath AT THE
			// RENAME INSTANT; the bound release re-derives it at unlink
			// adjacency and refuses a swapped occupant. Any wedge stops the
			// acquire rather than guessing about on-disk state.
			return nil, "", fmt.Errorf("reclaim replacement busy marker: %w", removeErr)
		}
	}
}

func replacementBusyToken() string {
	return fmt.Sprintf("pid=%d,time=%d", os.Getpid(), time.Now().UnixNano())
}

func replacementBusyTakeoverPath(path string) (string, error) {
	random, err := replacementBusyRandom()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s.takeover-%d-%x", path, os.Getpid(), random), nil
}

func replacementBusyQuarantinePath(path string) (string, error) {
	random, err := replacementBusyRandom()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%s%d-%x", path, replacementBusyQuarantineMark, os.Getpid(), random), nil
}

// replacementBusyReturnTakeover puts back the bytes found after a successful
// claimant rename. The exclusive placeholder serializes the restore: other
// claimants can observe it, but cannot acquire from it while the owned
// successor rides back home. If the destination is already occupied,
// preserve the bytes in a unique quarantine sibling instead of overwriting
// that live marker.
//
// POSTER-WRITE-HARDENING wave-47 (codex P2, PR#215): the restore itself is
// rebound end to end —
//
//  1. the placeholder's identity is captured THROUGH ITS OPEN HANDLE before
//     the close, and the name is FREED by the identity-bound release
//     (releaseClaimedBusyObject): the placeholder was previously overwritten
//     by a replace-aware restore rename — a foreign occupant swapped onto
//     the predictable .dlbusy name inside the claim→restore window had its
//     bytes silently replaced. Only the provably-ours placeholder is ever
//     freed now; any divergence preserves the occupant byte-intact and
//     routes the takeover bytes to quarantine instead;
//  2. the freed name then receives the takeover bytes NO-REPLACE
//     (PublishNoReplace): a claimant winning the release→restore window owns
//     a LIVE marker that must prevail (typed refusal) — the takeover bytes
//     land in quarantine byte-intact, exactly like the occupied-path leg;
//  3. the takeover file's own removal is bound to the identity the caller
//     observed it with (wave-47's handle-bound observe), never a bare
//     pathname.
//
// The w17 close-failure contract is unchanged: the restore attempt still
// rides over the placeholder claim (the name is never left stranded with a
// tokenless marker where the identity release can run), the close error
// always surfaces, and an unrestorable close keeps the takeover bytes at
// their uniquely-named sibling.
func replacementBusyReturnTakeover(fs afero.Fs, path, takeoverPath string, content []byte, observed os.FileInfo) error {
	placeholder, err := fs.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		placeholderIdentity, statErr := placeholder.Stat()
		closeErr := placeholder.Close()
		if statErr != nil {
			// The placeholder's own identity is unreadable — never release by
			// pathname what cannot be re-proven. Both occupants stay for manual
			// cleanup (the w17 residual); the close error dominates surfacing
			// exactly like the pre-shape close-failure leg.
			logging.Warnf("replacement busy takeover return of %s: the restore placeholder's identity could not be captured (%v); the placeholder and the takeover bytes at %s are both left for manual cleanup", path, statErr, takeoverPath)
			if closeErr != nil {
				return fmt.Errorf("close replacement busy restore placeholder: %w", closeErr)
			}
			return fmt.Errorf("stat replacement busy restore placeholder %s: %w", path, statErr)
		}
		if relErr := releaseClaimedBusyObject(fs, path, placeholderIdentity); relErr != nil {
			if closeErr == nil {
				// The marker name is UNPROVEN (a foreign occupant survived there
				// or the release wedged): never restore over it — the takeover
				// bytes ride to quarantine beside the original occupied leg, and
				// the marker path is left exactly as found.
				return replacementBusyQuarantineTakeover(fs, path, takeoverPath, content, observed)
			}
			logging.Warnf("replacement busy takeover restore after placeholder close failure AND the identity-bound placeholder release failed for %s: removing the placeholder did not complete (%v) — the placeholder could not be removed provably-bound; the destination may busy-block until the marker is removed manually; displaced bytes stay recoverable at %s", path, relErr, takeoverPath)
			return fmt.Errorf("close replacement busy restore placeholder: %w", closeErr)
		}
		// The marker name is provably FREE: the takeover bytes ride home
		// NO-REPLACE. A claimant winning the release→restore window (typed
		// refusal) keeps its live marker and the takeover bytes are preserved
		// in quarantine; any harder publish failure keeps the takeover file
		// recoverable at its own unique name (the w17 residual), unchanged.
		if renameErr := PublishNoReplace(fs, takeoverPath, path); renameErr != nil {
			if PublishRefusal(renameErr) {
				logging.Warnf("replacement busy marker %s was re-claimed inside the takeover-restore window by another process; preserved the displaced bytes in quarantine", path)
				qErr := replacementBusyQuarantineTakeover(fs, path, takeoverPath, content, observed)
				if closeErr != nil {
					return fmt.Errorf("close replacement busy restore placeholder: %w", closeErr)
				}
				return qErr
			}
			if closeErr != nil {
				logging.Warnf("replacement busy takeover restore after placeholder close failure failed for %s: %v; removing the placeholder proceeded identity-bound before the restore attempt and the claimed bytes stay recoverable at %s (an absent marker self-heals)", path, renameErr, takeoverPath)
				return fmt.Errorf("close replacement busy restore placeholder: %w", closeErr)
			}
			return fmt.Errorf("restore replacement busy marker: %w", renameErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close replacement busy restore placeholder: %w", closeErr)
		}
		return nil
	}
	if !os.IsExist(err) {
		return fmt.Errorf("reserve replacement busy restore path: %w", err)
	}

	return replacementBusyQuarantineTakeover(fs, path, takeoverPath, content, observed)
}

// replacementBusyQuarantineTakeover preserves the displaced takeover bytes
// in a fresh unique quarantine sibling (the occupied/proven-unfree marker
// path is never touched) and then frees the takeover name — bound to the
// identity the caller observed the takeover with, so a foreign swap under
// the takeover name is never unlinked in its place.
func replacementBusyQuarantineTakeover(fs afero.Fs, path, takeoverPath string, content []byte, observed os.FileInfo) error {
	quarantinePath, nameErr := replacementBusyQuarantinePath(path)
	if nameErr != nil {
		return fmt.Errorf("name replacement busy quarantine marker: %w", nameErr)
	}
	quarantine, openErr := fs.OpenFile(quarantinePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if openErr != nil {
		return fmt.Errorf("create replacement busy quarantine marker: %w", openErr)
	}
	if written, writeErr := quarantine.Write(content); writeErr != nil {
		_ = quarantine.Close()
		return fmt.Errorf("write replacement busy quarantine marker: %w", writeErr)
	} else if written != len(content) {
		_ = quarantine.Close()
		return fmt.Errorf("write replacement busy quarantine marker: short write (%d/%d)", written, len(content))
	}
	if syncErr := quarantine.Sync(); syncErr != nil {
		_ = quarantine.Close()
		return fmt.Errorf("sync replacement busy quarantine marker: %w", syncErr)
	}
	if closeErr := quarantine.Close(); closeErr != nil {
		return fmt.Errorf("close replacement busy quarantine marker: %w", closeErr)
	}
	logging.Warnf("replacement busy marker %s was claimed by another process; preserved it in quarantine %s", path, quarantinePath)
	if removeErr := releaseClaimedBusyObject(fs, takeoverPath, observed); removeErr != nil {
		return fmt.Errorf("remove replacement busy takeover marker after quarantine: %w", removeErr)
	}
	return nil
}

// replacementBusyObserveTakeover reads the takeover file and captures ITS
// identity through one open handle (the releaseObserve discipline): Stat and
// Read ride the SAME descriptor, so the recorded bytes and the identity
// snapshot provably belong to ONE object — a pathname swap between a
// path-based read and a separate Lstat could never alias the pair, and the
// callers' bound releases re-prove the name against this snapshot at unlink
// adjacency. Close errors are ignored: a read-only close mutates nothing and
// the identity is already bound.
func replacementBusyObserveTakeover(fs afero.Fs, takeoverPath string) ([]byte, os.FileInfo, error) {
	handle, err := fs.Open(takeoverPath)
	if err != nil {
		return nil, nil, err
	}
	info, serr := handle.Stat()
	content, rerr := io.ReadAll(handle)
	_ = handle.Close()
	if serr != nil {
		return nil, nil, serr
	}
	if rerr != nil {
		return nil, nil, rerr
	}
	return content, info, nil
}

// releaseClaimedBusyObject unlinks the object at path ONLY while it still
// provably names expect, through the wave-44 bound-unlink construction
// (BoundAside.Unlink): path is re-proved no-follow against expect, the
// proven object vacates onto a fresh claimed terminal name NO-REPLACE, is
// re-bound to expect at the terminal name, and only the terminal name is
// unlinked. A name that vanished on its own completed the cleanup by itself;
// anything else — a foreign swap inside the verify→unlink window, an
// indeterminate lookup — is REFUSED typed (ErrTakeAsideForeign), the occupant
// rewound onto path NO-REPLACE byte-intact, so the caller routes around the
// name rather than deleting what it cannot prove. Wave-59 (codex P2, PR#215
// finding F1): the pre-shape verify→Remove pair unlinked path BY PATHNAME —
// a swap between the no-follow re-prove and fs.Remove deleted the foreign
// occupant (on the canonical .dlbusy path, another claimant's live marker);
// the bound construction closes that window end to end.
func releaseClaimedBusyObject(fs afero.Fs, path string, expect os.FileInfo) error {
	return (&BoundAside{fs: fs, scratch: path, held: expect, moved: true}).Unlink()
}

// discardBusyMarkerClaim is the AcquireReplacementBusy claim-failure cleanup
// with the wave-45 identity binding (DiscardFailedExclusiveStaging's shape):
// the marker name is PREDICTABLE (dest + .dlbusy), so the failure legs never
// unlink it by pathname alone. The name is re-derived no-follow while the
// handle is still open (the pinned inode makes the comparison race-free —
// the handle stat feeds expect when the leg can still fstat, otherwise the
// caller's pre-close capture rides in through expect already set), and only
// a current occupant that provably names OUR claimed marker is ever
// removed. Every other answer — a foreign swap, an indeterminate lookup, an
// unreadable handle identity — keeps the occupant byte-intact with a warn;
// a name that vanished on its own completed the cleanup by itself.
func discardBusyMarkerClaim(fs afero.Fs, path string, fh afero.File, expect os.FileInfo) {
	if expect == nil {
		info, serr := fh.Stat()
		if serr != nil {
			_ = fh.Close()
			logging.Warnf("replacement busy marker %s claim cleanup could not read the claim handle's identity (%v) — the marker is left in place (possibly foreign); manual cleanup advised", path, serr)
			return
		}
		expect = info
	}
	cur, lerr := asideLstat(fs, path)
	_ = fh.Close()
	switch {
	case os.IsNotExist(lerr):
		return // vanished on its own — nothing left to clean
	case lerr != nil:
		logging.Warnf("replacement busy marker %s claim cleanup could not inspect the name (%v) — the occupant is left byte-intact; manual cleanup advised", path, lerr)
		return
	case !asideSameObject(cur, expect):
		logging.Warnf("replacement busy marker %s no longer names this process's claim during cleanup (foreign substitution) — the substitute is preserved byte-intact; manual cleanup advised", path)
		return
	}
	// Codex P2 (wave-57B): the verify→unlink window must stay bound — a
	// racing claimant that replaced the canonical name after asideLstat
	// would otherwise have ITS marker deleted. Vacate the proven-us object
	// onto a fresh claimed terminal name, re-bind identity, and unlink only
	// that terminal; any doubt preserves bytes.
	vacName, vacClaim, cerr := claimTakeAsideVacName(fs, path)
	if cerr != nil {
		logging.Warnf("replacement busy marker %s claim cleanup could not reserve its terminal name (%v) — the occupant is left byte-intact; manual cleanup advised", path, cerr)
		return
	}
	hold, terr := TakeAside(TakeAsideSpec{FS: fs, Src: path, Scratch: vacName, Claim: vacClaim, Prove: func(cur os.FileInfo) error {
		if !asideSameObject(cur, expect) {
			return fmt.Errorf("take-aside occupant diverged from the claimed marker")
		}
		return nil
	}})
	if terr != nil {
		logging.Warnf("replacement busy marker %s claim cleanup take-aside failed (%v) — the occupant is left byte-intact; manual cleanup advised", path, terr)
		return
	}
	if ulErr := hold.Unlink(); ulErr != nil {
		logging.Warnf("replacement busy marker %s claim cleanup unlink refused (%v) — the occupant is preserved; manual cleanup advised", path, ulErr)
	}
}

func replacementBusyRandomPlatform() (uint64, error) {
	var raw [8]byte
	if _, err := replacementCryptoRandomRead(raw[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(raw[:]), nil
}

// replacementBusyReleaseBackoff is the brief delay between release unlink
// retries. A worthy unlink failure is typically a transient network-FS
// hiccup, so two short retries buy recovery time without stalling callers.
var replacementBusyReleaseBackoff = []time.Duration{10 * time.Millisecond, 25 * time.Millisecond}

// replacementBusyReleasedField is the in-band release field
// replacementBusyInspect decodes (keeping the pid/time fields so the token
// still parses as well-formed). Wave-38 (codex P2, PR#215 finding F4) moved
// the release path to the take-aside unlink (a wedged release frees the
// marker name BEFORE any unlink can wedge, so release no longer rewrites
// anything); the decode arm stays for markers left on disk by pre-wave-38
// wedged releases, which remain reclaimable through the takeover rules.

// releaseReplacementBusy removes the marker only when its bytes AND its
// identity still carry our token — and never by the marker PATHNAME
// (wave-38, codex P2, PR#215 finding F4): a directory writer swapping the
// marker between the token read and a pathname Remove would have release
// delete the REPLACEMENT marker, letting a third process acquire the name
// while the replacement marker's real owner stays active. The release runs
// the generalized no-replace take-aside instead:
//
//  1. OBSERVE the marker through one open handle (replacementBusyReleaseObserve):
//     the identity snapshot (dev/inode where exposed, size, mtime) rides the
//     same descriptor as THE bytes read, so the recorded token can never be
//     aliased to a successor object;
//  2. TAKE the observed marker aside onto a fresh O_EXCL-reserved sibling
//     scratch (a take can never displace foreign bytes: the scratch is our
//     own claimed placeholder) and re-prove the moved object at the scratch
//     name against the observed identity — a mid-take swap restores the
//     moved object back NO-REPLACE where the name is still free (carry-on,
//     never a deletion of what it cannot prove);
//  3. UNLINK only the scratch, re-bound to the observed identity at every
//     unlink attempt. A transiently failing take-aside unlink is retried
//     with replacementBusyReleaseBackoff; a persistent wedge leaves only the
//     inert scratch sibling (the marker name is already free, so the release
//     is achieved and no later claimant is ever busy-blocked) and is
//     surfaced through a warn for manual cleanup.
func releaseReplacementBusy(fs afero.Fs, path, token string) {
	content, observed, ok := replacementBusyReleaseObserve(fs, path)
	if !ok || content != token {
		return
	}
	scratch, claim, cerr := replacementBusyClaimReleaseScratch(fs, path)
	if cerr != nil {
		logging.Warnf("replacement busy marker %s could not reserve a release take-aside name (%v); the marker stays as-is — later claims arbitrate it through the normal stale rules", path, cerr)
		return
	}
	hold, terr := TakeAside(TakeAsideSpec{
		FS:      fs,
		Src:     path,
		Scratch: scratch,
		Claim:   claim,
		Prove: func(moved os.FileInfo) error {
			if !asideSameObject(moved, observed) {
				return fmt.Errorf("marker taken aside from %s is not the observed token object — foreign marker preserved: %w", path, ErrTakeAsideForeign)
			}
			return nil
		},
	})
	if terr != nil {
		logging.Warnf("replacement busy marker %s could not be taken aside for release (%v); nothing was removed by name — later claims arbitrate the marker normally", path, terr)
		return
	}
	var removeErr error
	for attempt := 0; ; attempt++ {
		removeErr = hold.Unlink()
		if removeErr == nil || errors.Is(removeErr, ErrTakeAsideForeign) {
			// nil: taken-aside object removed (or vanished by itself).
			// Foreign: a swap raced the unlink window — the refusal preserved
			// it and the marker name is already free; retrying cannot help.
			break
		}
		if attempt >= len(replacementBusyReleaseBackoff) {
			break
		}
		time.Sleep(replacementBusyReleaseBackoff[attempt])
	}
	if removeErr != nil {
		logging.Warnf("replacement busy marker %s release: the take-aside unlink of %s failed (%v); the marker name is free, the inert scratch awaits manual cleanup", path, scratch, removeErr)
	}
}

// replacementBusyReleaseObserve reads the marker and captures ITS identity
// through one open handle (wave-38, finding F4): Stat and Read ride the SAME
// descriptor, so the recorded token and the dev/inode snapshot provably
// belong to ONE observed object — a pathname swap between a path-based read
// and a separate Lstat could never alias the record. Close errors are
// ignored: a read-only close mutates nothing and the identity is already
// bound. Any observation failure answers not-ok (best-effort release posture).
func replacementBusyReleaseObserve(fs afero.Fs, path string) (string, os.FileInfo, bool) {
	handle, err := fs.Open(path)
	if err != nil {
		return "", nil, false
	}
	info, serr := handle.Stat()
	content := []byte(nil)
	var rerr error
	if serr == nil {
		content, rerr = io.ReadAll(handle)
	}
	_ = handle.Close()
	if serr != nil || rerr != nil {
		return "", nil, false
	}
	return string(content), info, true
}

// replacementBusyReleaseClaimTries bounds the release-take-aside scratch
// claim loop; every collision or racing claimant costs one draw.
const replacementBusyReleaseClaimTries = 16

// replacementBusyClaimReleaseScratch atomically reserves a uniquely named
// sibling scratch for the release take-aside (wave-38, finding F4): mint via
// the takeover-name grammar (PID + crypto random), claim O_CREATE|O_EXCL,
// capture the reservation's own identity through the open handle's pre-close
// Stat (mirroring the quarantine-claim discipline) so the take's pre-move
// re-proof can refuse a foreign reservation swap. An O_EXCL collision
// re-draws; a reservation whose identity cannot be read (or whose close
// fails) is dropped rather than renaming over unverified bytes.
func replacementBusyClaimReleaseScratch(fs afero.Fs, path string) (string, os.FileInfo, error) {
	for attempt := 0; attempt < replacementBusyReleaseClaimTries; attempt++ {
		candidate, err := replacementBusyTakeoverPath(path)
		if err != nil {
			return "", nil, fmt.Errorf("name release take-aside scratch for %s: %w", path, err)
		}
		reservation, rerr := fs.OpenFile(candidate, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		switch {
		case rerr == nil:
			info, serr := reservation.Stat()
			if serr != nil {
				// Codex P2 (r21): with no identity read there is nothing to
				// authenticate — the claim's own name may already be foreign, so
				// the occupant stays for manual cleanup (never a blind pathname unlink).
				_ = reservation.Close()
				logging.Warnf("replacement busy release reservation %s retained — identity unproven on claim (%v); manual cleanup advised", candidate, serr)
				return "", nil, fmt.Errorf("stat release take-aside reservation %s: %w", candidate, serr)
			}
			if cerr := reservation.Close(); cerr != nil {
				// Codex P2 (r21): bind the cleanup to the captured claim identity
				// — SameFile reproof at adjacency to the unlink; a swapped
				// occupant is preserved.
				if relErr := releaseClaimedBusyObject(fs, candidate, info); relErr != nil {
					logging.Warnf("replacement busy release reservation %s retained — failed close and bound cleanup refused (%v); manual cleanup advised", candidate, relErr)
				}
				return "", nil, fmt.Errorf("close release take-aside reservation %s: %w", candidate, cerr)
			}
			return candidate, info, nil
		case os.IsExist(rerr):
			continue // a racer claimed this draw first — draw again
		default:
			return "", nil, fmt.Errorf("reserve release take-aside scratch %s: %w", candidate, rerr)
		}
	}
	return "", nil, fmt.Errorf("release take-aside names exhausted for %s after %d attempts", path, replacementBusyReleaseClaimTries)
}

// replacementBusyIsReleased decodes the in-band release field with the same
// field discipline as parseReplacementBusyToken so a hand-crafted lookalike
// (missing pid/time) still classifies as malformed, never as released.
func replacementBusyIsReleased(content string) bool {
	parts := strings.FieldsFunc(content, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
	for _, part := range parts {
		keyValue := strings.SplitN(part, "=", 2)
		if len(keyValue) == 2 && strings.TrimSpace(keyValue[0]) == "released" && strings.TrimSpace(keyValue[1]) == "1" {
			return true
		}
	}
	return false
}

type replacementBusyInspection struct {
	stale            bool
	reclaimable      bool
	observedToken    []byte
	hasObservedToken bool
}

// replacementBusyInspect separates age/liveness/start-time classification from
// ownership. An old malformed marker may be stale for arbitration purposes,
// but it is not safe to remove because its name and mtime do not prove
// Javinizer created it.
//
// Classification precedence (the first applicable arm wins):
//  1. Malformed content is retained and is never reclaimable by age alone.
//  2. A well-formed marker carrying the released field (the owner's final
//     unlink failed transiently and it recorded the release in-band via
//     releaseReplacementBusy) is stale and reclaimable regardless of PID
//     liveness or age, ending the process-lifetime block a wedged removal
//     would otherwise create. The reclaim still goes through the wave
//     takeover/quarantine rules; the released bytes are preserved or
//     returned, never silently overwritten. This arm deliberately precedes
//     the live-PID arms because a marker may carry this very process's PID.
//  3. A well-formed marker whose PID probe proves alive is never expired by
//     age.
//  4. Within that live arm, W20's start-time proof that the PID started after
//     the marker marks it stale as PID reuse. Linux derives the start time
//     from /proc/<pid>/stat; K4 arms Windows with the same proof through
//     K32 GetProcessTimes. An unreadable start time keeps the liveness-only
//     behavior rather than inventing evidence.
//  5. A probe that proves the PID is dead marks the marker stale.
//  6. On POSIX, age is consulted only when the probe is
//     undecidable/unprobeable; Windows retains its conservative access-denied
//     behavior and does not expire an unprobeable owner by age.
func replacementBusyInspect(fs afero.Fs, path string) (replacementBusyInspection, error) {
	content, err := afero.ReadFile(fs, path)
	if err != nil {
		if os.IsNotExist(err) {
			return replacementBusyInspection{stale: true, reclaimable: true}, nil
		}
		return replacementBusyInspection{}, err
	}
	inspection := replacementBusyInspection{observedToken: content, hasObservedToken: true}
	info, err := fs.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return replacementBusyInspection{stale: true, reclaimable: true, observedToken: content, hasObservedToken: true}, nil
		}
		return replacementBusyInspection{}, err
	}
	pid, created, ok := parseReplacementBusyToken(string(content))
	if !ok {
		inspection.stale = time.Since(info.ModTime()) > replacementBusyStaleAge
		return inspection, nil
	}
	if replacementBusyIsReleased(string(content)) {
		// The owner declared the release in-band after its final unlink wedged.
		// This arm deliberately precedes the same-PID and live-probe arms: the
		// marker is well-formed and may carry this very process's PID, but the
		// recorded release supersedes both, exactly as a proven-dead PID would.
		inspection.stale = true
		inspection.reclaimable = true
		return inspection, nil
	}
	createdAt := time.Unix(0, created)
	if pid == os.Getpid() {
		// This timestamp is a PID-reuse boundary, not a two-minute lease:
		// markers from before this process started belong to a prior owner,
		// while current-run markers stay busy for as long as this process runs.
		inspection.stale = createdAt.Before(replacementBusyBootAt)
		inspection.reclaimable = true
		return inspection, nil
	}
	// First prove that the recorded owner is live. Linux then distinguishes a
	// reused PID by comparing /proc starttime with the marker's wall-clock
	// timestamp. A start time after the marker proves that this is a different
	// process and makes the marker stale. If start time cannot be established,
	// the positive liveness proof still wins over marker age.
	liveness := replacementProbePIDAliveAware(pid)
	switch liveness {
	case replacementPIDAlive:
		if replacementProcessStartTime != nil {
			if processStartTime := replacementProcessStartTime(pid); processStartTime != nil && processStartTime.After(createdAt) {
				inspection.stale = true
				inspection.reclaimable = true
				return inspection, nil
			}
		}
		inspection.reclaimable = true
		return inspection, nil
	case replacementPIDDead:
		inspection.stale = true
		inspection.reclaimable = true
		return inspection, nil
	case replacementPIDUnprobeable:
		if replacementIsWindows {
			// Access denial does not prove that a Windows owner is gone; retain
			// the marker rather than allowing an untrusted process to reclaim it.
			inspection.reclaimable = true
			return inspection, nil
		}
		inspection.stale = time.Since(createdAt) > replacementBusyStaleAge
		inspection.reclaimable = true
		return inspection, nil
	default:
		// An unknown result is not the explicit undecidable seam. Fail closed
		// rather than letting an unexpected value use the age fallback.
		inspection.reclaimable = true
		return inspection, nil
	}
}

func parseReplacementBusyToken(content string) (pid int, created int64, ok bool) {
	var pidSet, timeSet bool
	parts := strings.FieldsFunc(content, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
	for _, part := range parts {
		keyValue := strings.SplitN(part, "=", 2)
		if len(keyValue) != 2 {
			continue
		}
		switch strings.TrimSpace(keyValue[0]) {
		case "pid":
			value, err := strconv.Atoi(strings.TrimSpace(keyValue[1]))
			if err != nil {
				return 0, 0, false
			}
			pid, pidSet = value, true
		case "time":
			value, err := strconv.ParseInt(strings.TrimSpace(keyValue[1]), 10, 64)
			if err != nil {
				return 0, 0, false
			}
			created, timeSet = value, true
		}
	}
	return pid, created, pidSet && timeSet
}
