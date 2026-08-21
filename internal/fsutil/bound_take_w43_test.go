package fsutil

// POSTER-WRITE-HARDENING wave-43 (codex P2, PR#215) — the conditional
// take-aside handoff inside TakeAside (bound_take.go): the reservation
// re-proof is no longer followed by a replace-aware rename of src; the
// reservation ITSELF vacates onto a fresh claimed vacated name NO-REPLACE,
// the vacated object re-proves against the claim identity, and src publishes
// onto the provably-free scratch name NO-REPLACE. These legs replay a plant
// in every window the wave-38 construction left open (verify→vacate,
// vacate→publish, publish→cleanup), the claim/claim-release wedge legs, the
// vacate wedge legs (vanished, collision, indeterminate), and the
// compensation residue discipline (ride-back NO-REPLACE + re-proven drop).
//
// Lookup vocabulary (Stat calls on the wrapper filesystems; claim handle
// Stats bypass the wrappers): scratch #1 = reservation re-proof, #2 =
// publish classification, #3 = post-move re-proof, #4 = the bound unlink;
// vac #1 = claim-release verify, #2 = the release's unlink-adjacent
// re-proof (wave-58 dual-reproof), #3 = vacate classification, #4 =
// post-vacate identity re-proof, #5 = success-path cleanup binding.
// Windows that must survive further ordinal shifts are scripted
// STRUCTURALLY (the release's own unlink, rename arming, claim close), not
// by counting lookups.

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/logging"
)

// w43VacNames lists the vacated-name residue (".vac." siblings) in dir.
func w43VacNames(t *testing.T, fs afero.Fs, dir string) []string {
	t.Helper()
	var names []string
	for _, e := range w38Entries(t, fs, dir) {
		if strings.Contains(e.Name(), ".vac.") {
			names = append(names, e.Name())
		}
	}
	return names
}

func TestTakeAsideW43_PlantInEveryWindow(t *testing.T) {
	t.Run("verify→vacate plant rides back byte-intact (reverify-mismatch refusal)", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w43-win1")
		srcInfo, _ := base.Stat(src)
		plant := []byte("plant between the re-proof and the vacate")
		fs := &w43PlantOnVacateRenameFs{Fs: base, scratch: scratch, plant: plant}

		hold, err := TakeAside(TakeAsideSpec{FS: fs, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.Nil(t, hold)
		require.ErrorIs(t, err, ErrTakeAsideForeign, "the vacate moved the plant — the post-vacate re-proof refuses it")
		require.NotErrorIs(t, err, ErrTakeAsideRestoreFailed, "the plant rode back onto the free scratch no-replace")
		require.Equal(t, "journal bytes", string(w38Read(t, base, src)), "src never relocated")
		require.Equal(t, plant, w38Read(t, base, scratch), "the plant is preserved byte-intact, never overwritten")
		require.Empty(t, w43VacNames(t, base, "/out/w43-win1"), "the plant rode back; no vacated-name litter")
	})

	t.Run("vacate→publish plant is the typed publish-collision refusal", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w43-win2")
		srcInfo, _ := base.Stat(src)
		plant := []byte("plant winning the vacate-to-publish gap")
		fs := &w43PlantOnPublishClassifyFs{Fs: base, scratch: scratch, plant: plant}

		hold, err := TakeAside(TakeAsideSpec{FS: fs, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.Nil(t, hold)
		require.ErrorIs(t, err, ErrPublishCollision, "the no-replace publish reported the plant")
		require.ErrorIs(t, err, ErrTakeAsideRestoreFailed,
			"the ride-back collided with the same plant — our placeholder strands recoverable at the vacated name")
		require.Equal(t, "journal bytes", string(w38Read(t, base, src)), "src never relocated")
		require.Equal(t, plant, w38Read(t, base, scratch), "the plant is preserved byte-intact")
		vacNames := w43VacNames(t, base, "/out/w43-win2")
		require.Len(t, vacNames, 1, "only our own inert placeholder strands")
		info, serr := base.Stat("/out/w43-win2/" + vacNames[0])
		require.NoError(t, serr)
		require.Zero(t, info.Size(), "the stranded vacated name holds our 0-byte reservation placeholder")
	})

	t.Run("publish→cleanup swap is preserved and the take stands", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w43-win3")
		srcInfo, _ := base.Stat(src)
		occupant := []byte("foreign swap at the vacated name")
		fs := &w43SwapVacAfterPublishFs{Fs: base, src: src, scratch: scratch, plant: occupant}

		hold, err := TakeAside(TakeAsideSpec{FS: fs, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.NoError(t, err, "the publish landed — the refused vacated-name cleanup is warn-only")
		require.Equal(t, "journal bytes", string(w38Read(t, base, scratch)))
		vacNames := w43VacNames(t, base, "/out/w43-win3")
		require.Len(t, vacNames, 1)
		require.Equal(t, occupant, w38Read(t, base, "/out/w43-win3/"+vacNames[0]),
			"the foreign occupant at the vacated name is never unlinked")
		require.NoError(t, hold.Unlink())
	})
}

func TestTakeAsideW43_VacateLegs(t *testing.T) {
	t.Run("reservation vanished before the vacate is an indeterminate refusal", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w43-vacvanish")
		srcInfo, _ := base.Stat(src)
		fs := &w43VacateVanishFs{Fs: base, scratch: scratch}

		hold, err := TakeAside(TakeAsideSpec{FS: fs, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.Nil(t, hold)
		require.ErrorContains(t, err, "vanished before its no-replace vacate")
		require.NotErrorIs(t, err, ErrTakeAsideForeign, "nothing foreign was proven — a plain indeterminate refusal")
		require.Equal(t, "journal bytes", string(w38Read(t, base, src)), "src untouched")
		_, lerr := base.Stat(scratch)
		require.ErrorIs(t, lerr, os.ErrNotExist, "the vanished reservation freed the scratch name")
		require.Empty(t, w43VacNames(t, base, "/out/w43-vacvanish"))
	})

	t.Run("plant at the vacated name between release and vacate is the typed collision", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w43-vaccoll")
		srcInfo, _ := base.Stat(src)
		plant := []byte("plant riding the vacated name")
		fs := &w43PlantAfterVacReleaseFs{Fs: base, plant: plant}

		hold, err := TakeAside(TakeAsideSpec{FS: fs, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.Nil(t, hold)
		require.ErrorContains(t, err, "take-aside vacate of the reservation")
		require.ErrorIs(t, err, ErrPublishCollision, "no-replace refused the occupied vacated name")
		require.Equal(t, "journal bytes", string(w38Read(t, base, src)))
		require.Equal(t, plant, w38Read(t, base, vacNameOf(t, base, "/out/w43-vaccoll")),
			"the plant at the vacated name is preserved byte-intact")
		_, serr := base.Stat(scratch)
		require.ErrorIs(t, serr, os.ErrNotExist,
			"nothing relocated — the still-claimed reservation was dropped re-proven, mirroring the wave-38 failed-move posture")
	})

	t.Run("post-vacate vanished is the vanished sentinel", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w43-vacgone")
		srcInfo, _ := base.Stat(src)
		fs := &w43VanishVacAfterVacateFs{Fs: base, scratch: scratch}

		hold, err := TakeAside(TakeAsideSpec{FS: fs, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.Nil(t, hold)
		require.ErrorIs(t, err, ErrTakeAsideVanished)
		require.ErrorContains(t, err, "post-vacate re-proof")
		require.Equal(t, "journal bytes", string(w38Read(t, base, src)), "src never relocated")
		_, lerr := base.Stat(scratch)
		require.ErrorIs(t, lerr, os.ErrNotExist, "the vacate freed the scratch name")
		require.Empty(t, w43VacNames(t, base, "/out/w43-vacgone"))
	})

	t.Run("post-vacate indeterminate lookup compensates cleanly", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w43-vacindet")
		srcInfo, _ := base.Stat(src)
		sentinel := errors.New("w43 post-vacate lookup wedged")
		fs := &w43FailPostVacateLookupFs{Fs: base, err: sentinel}

		hold, err := TakeAside(TakeAsideSpec{FS: fs, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.Nil(t, hold)
		require.ErrorIs(t, err, sentinel)
		require.NotErrorIs(t, err, ErrTakeAsideRestoreFailed, "the ride-back onto the free scratch landed")
		require.Equal(t, "journal bytes", string(w38Read(t, base, src)))
		_, lerr := base.Stat(scratch)
		require.ErrorIs(t, lerr, os.ErrNotExist, "the restored reservation was dropped re-proven")
		require.Empty(t, w43VacNames(t, base, "/out/w43-vacindet"))
	})
}

func TestTakeAsideW43_VacatedClaimLegs(t *testing.T) {
	t.Run("foreign swap at the claim release refuses and preserves", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w43-claimswap")
		srcInfo, _ := base.Stat(src)
		occupant := []byte("foreign swap inside the claim-release window")
		fs := &w43VacClaimCloseFs{Fs: base, plant: occupant}

		hold, err := TakeAside(TakeAsideSpec{FS: fs, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.Nil(t, hold)
		require.ErrorIs(t, err, ErrTakeAsideForeign)
		require.ErrorContains(t, err, "no longer names our claimed placeholder")
		vacNames := w43VacNames(t, base, "/out/w43-claimswap")
		require.Len(t, vacNames, 1)
		require.Equal(t, occupant, w38Read(t, base, "/out/w43-claimswap/"+vacNames[0]),
			"the foreign occupant is never unlinked")
		_, serr := base.Stat(scratch)
		require.ErrorIs(t, serr, os.ErrNotExist,
			"nothing relocated — the still-claimed reservation was dropped re-proven")
		require.Equal(t, "journal bytes", string(w38Read(t, base, src)))
	})

	t.Run("claim raced away on its own frees the release — take proceeds", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w43-claimvanish")
		srcInfo, _ := base.Stat(src)
		fs := &w43VacClaimCloseFs{Fs: base, removeOnly: true}

		hold, err := TakeAside(TakeAsideSpec{FS: fs, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.NoError(t, err, "an already-free vacated name releases itself")
		require.Equal(t, "journal bytes", string(w38Read(t, base, scratch)))
		require.NoError(t, hold.Unlink())
	})

	t.Run("entropy wedge refuses before anything relocates", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w43-entropy")
		srcInfo, _ := base.Stat(src)
		sentinel := errors.New("w43 entropy wedged")
		prev := takeAsideVacRandReader
		takeAsideVacRandReader = &w43FailReader{err: sentinel}
		t.Cleanup(func() { takeAsideVacRandReader = prev })

		hold, err := TakeAside(TakeAsideSpec{FS: base, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.Nil(t, hold)
		require.ErrorIs(t, err, sentinel)
		require.ErrorContains(t, err, "reserve the take-aside vacated name")
		_, serr := base.Stat(scratch)
		require.ErrorIs(t, serr, os.ErrNotExist, "the still-claimed reservation was dropped re-proven")
		require.Equal(t, "journal bytes", string(w38Read(t, base, src)))
	})

	t.Run("claim collision re-draws and the take proceeds", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w43-claimrace")
		srcInfo, _ := base.Stat(src)
		fs := &w43VacClaimOpenFailFs{Fs: base, err: os.ErrExist, maxClaims: 1}

		hold, err := TakeAside(TakeAsideSpec{FS: fs, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.NoError(t, err, "the first draw collided with a racer; the re-draw claimed")
		require.NoError(t, hold.Unlink())
		require.EqualValues(t, 1, fs.claims, "exactly one lost draw")
	})

	t.Run("repeated claim collisions exhaust the draw loop", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w43-claimexh")
		srcInfo, _ := base.Stat(src)
		fs := &w43VacClaimOpenFailFs{Fs: base, err: os.ErrExist, maxClaims: -1}

		hold, err := TakeAside(TakeAsideSpec{FS: fs, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.Nil(t, hold)
		require.ErrorContains(t, err, "take-aside vacated names exhausted")
		require.EqualValues(t, takeAsideVacClaimTries, fs.claims)
		_, serr := base.Stat(scratch)
		require.ErrorIs(t, serr, os.ErrNotExist, "the still-claimed reservation was dropped re-proven")
	})

	t.Run("claim open hard error refuses", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w43-claimerr")
		srcInfo, _ := base.Stat(src)
		sentinel := errors.New("w43 claim open wedged")
		fs := &w43VacClaimOpenFailFs{Fs: base, err: sentinel, maxClaims: 1}

		hold, err := TakeAside(TakeAsideSpec{FS: fs, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.Nil(t, hold)
		require.ErrorIs(t, err, sentinel)
		require.ErrorContains(t, err, "reserve take-aside vacated name")
		require.Equal(t, "journal bytes", string(w38Read(t, base, src)))
	})

	t.Run("claim whose identity cannot be read is retained (wave-r19, F1)", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w43-claimstat")
		srcInfo, _ := base.Stat(src)
		sentinel := errors.New("w43 claim stat wedged")
		fs := &w43VacClaimHandleFailFs{Fs: base, statErr: sentinel}

		var logs bytes.Buffer
		restoreLog := logging.SetOutput(&logs)
		t.Cleanup(restoreLog)

		hold, err := TakeAside(TakeAsideSpec{FS: fs, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.Nil(t, hold)
		require.ErrorIs(t, err, sentinel)
		require.ErrorContains(t, err, "stat take-aside vacated reservation")
		require.Len(t, w43VacNames(t, base, "/out/w43-claimstat"), 1,
			"wave-r19 (F1): the unproven claim is retained for manual cleanup — never unlinked when identity is unprovable")
		require.Contains(t, logs.String(), "left in place",
			"the retain-on-doubt leg warned that the placeholder's identity could not be proven")
	})

	t.Run("claim whose close fails releases its reservation identity-bound (wave-r19, F1)", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w43-claimclose")
		srcInfo, _ := base.Stat(src)
		sentinel := errors.New("w43 claim close wedged")
		fs := &w43VacClaimHandleFailFs{Fs: base, closeErr: sentinel}

		hold, err := TakeAside(TakeAsideSpec{FS: fs, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.Nil(t, hold)
		require.ErrorIs(t, err, sentinel)
		require.ErrorContains(t, err, "close take-aside vacated reservation")
		require.Empty(t, w43VacNames(t, base, "/out/w43-claimclose"))
	})

	t.Run("wedged release unlink refuses with our placeholder retained", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w43-relfail")
		srcInfo, _ := base.Stat(src)
		sentinel := errors.New("w43 release unlink wedged")
		fs := &w43VacRemoveFailFs{Fs: base, err: sentinel, failAt: 1}

		hold, err := TakeAside(TakeAsideSpec{FS: fs, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.Nil(t, hold)
		require.ErrorIs(t, err, sentinel)
		require.ErrorContains(t, err, "remove the take-aside vacated claim")
		vacNames := w43VacNames(t, base, "/out/w43-relfail")
		require.Len(t, vacNames, 1, "our own placeholder is retained when its verified unlink wedges")
		info, serr := base.Stat("/out/w43-relfail/" + vacNames[0])
		require.NoError(t, serr)
		require.Zero(t, info.Size())
		_, serr = base.Stat(scratch)
		require.ErrorIs(t, serr, os.ErrNotExist, "the still-claimed reservation was dropped re-proven")
	})

	t.Run("indeterminate release lookup refuses before anything relocates", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w43-relindet")
		srcInfo, _ := base.Stat(src)
		sentinel := errors.New("w43 release lookup wedged")
		fs := &w43FailNthVacStatFs{Fs: base, n: 1, err: sentinel}

		hold, err := TakeAside(TakeAsideSpec{FS: fs, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.Nil(t, hold)
		require.ErrorIs(t, err, sentinel)
		require.ErrorContains(t, err, "inspect the take-aside vacated claim")
		_, serr := base.Stat(scratch)
		require.ErrorIs(t, serr, os.ErrNotExist, "nothing relocated — the reservation was dropped re-proven")
		require.Len(t, w43VacNames(t, base, "/out/w43-relindet"), 1, "our placeholder retained")
	})

	// Wave-58 (codex P2): the claim release dual-reproves — the head verify,
	// then a second identity pin at syscall adjacency to the unlink — so the
	// legs below replay a racer inside THAT added verify→re-proof window.

	t.Run("claim vanished between the release proofs completes the release", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w43-relvanish2")
		srcInfo, _ := base.Stat(src)
		fs := &w43VacReleaseReproofRaceFs{Fs: base, removeOnly: true}

		hold, err := TakeAside(TakeAsideSpec{FS: fs, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.NoError(t, err, "gone between the proofs through nobody's unlink of ours — the release completed itself and the take proceeds")
		require.True(t, fs.done, "the racer actually fired inside the verify→re-proof window")
		require.Equal(t, "journal bytes", string(w38Read(t, base, scratch)))
		require.Empty(t, w43VacNames(t, base, "/out/w43-relvanish2"))
		require.NoError(t, hold.Unlink())
	})

	t.Run("indeterminate release re-proof refuses before anything relocates", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w43-relreproof")
		srcInfo, _ := base.Stat(src)
		sentinel := errors.New("w43 release re-proof wedged")
		fs := &w43FailNthVacStatFs{Fs: base, n: 2, err: sentinel}

		hold, err := TakeAside(TakeAsideSpec{FS: fs, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.Nil(t, hold)
		require.ErrorIs(t, err, sentinel)
		require.ErrorContains(t, err, "re-prove the vacated claim")
		require.NotErrorIs(t, err, ErrTakeAsideForeign, "an indeterminate answer proves nothing foreign")
		require.Equal(t, "journal bytes", string(w38Read(t, base, src)))
		require.Len(t, w43VacNames(t, base, "/out/w43-relreproof"), 1, "our placeholder retained byte-intact")
		_, serr := base.Stat(scratch)
		require.ErrorIs(t, serr, os.ErrNotExist, "nothing relocated — the reservation was dropped re-proven")
	})

	t.Run("foreign swap between the release proofs refuses and preserves", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w43-relswap2")
		srcInfo, _ := base.Stat(src)
		occupant := []byte("foreign swap inside the release's verify→re-proof window")
		fs := &w43VacReleaseReproofRaceFs{Fs: base, plant: occupant}

		hold, err := TakeAside(TakeAsideSpec{FS: fs, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.Nil(t, hold)
		require.ErrorIs(t, err, ErrTakeAsideForeign)
		require.ErrorContains(t, err, "changed identity between the proof and the unlink")
		require.True(t, fs.done, "the swap actually fired inside the verify→re-proof window")
		vacNames := w43VacNames(t, base, "/out/w43-relswap2")
		require.Len(t, vacNames, 1)
		require.Equal(t, occupant, w38Read(t, base, "/out/w43-relswap2/"+vacNames[0]),
			"the occupant swapped in under the re-proof is never unlinked")
		_, serr := base.Stat(scratch)
		require.ErrorIs(t, serr, os.ErrNotExist, "nothing relocated — the reservation was dropped re-proven")
		require.Equal(t, "journal bytes", string(w38Read(t, base, src)))
	})
}

// Wave-r19 (codex P2, PR#215 finding F1): the close-failure leg binds the
// cleanup to the captured identity (releaseTakeAsideVacClaim — SameFile at
// unlink adjacency, retain on doubt, never a pathname Remove). A foreign plant
// swapped onto the candidate at Close is refused by the identity-bound release
// and retained byte-intact; the close error still surfaces.
func TestClaimTakeAsideVacNameW19_CloseFailureForeignSwapRetains(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/out", 0o755))
	scratch := "/out/scratch"
	plant := []byte("foreign plant swapped onto the vacated claim")
	closeErr := errors.New("w19 claim close wedged")
	fs := &w43VacClaimHandleFailFs{Fs: base, closeErr: closeErr, plant: plant}

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	t.Cleanup(restoreLog)

	name, info, err := claimTakeAsideVacName(fs, scratch)
	require.ErrorIs(t, err, closeErr, "the original close failure still surfaces")
	require.Empty(t, name)
	require.Nil(t, info)
	require.Contains(t, logs.String(), "close-failure cleanup refused",
		"the close-failure cleanup warned when the identity-bound release refused the swapped occupant")
	// The foreign plant is retained at the candidate name byte-intact — the
	// identity-bound release never unlinked an unproven occupant.
	names := w43VacNames(t, base, "/out")
	require.Len(t, names, 1, "the foreign plant is retained — never a pathname Remove")
	require.Equal(t, plant, w38Read(t, base, "/out/"+names[0]))
}

func TestTakeAsideW43_CleanupLegs(t *testing.T) {
	t.Run("vacated name vanished on its own completes the cleanup", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w43-cleanvanish")
		srcInfo, _ := base.Stat(src)
		fs := &w43VanishVacAtPostMoveFs{Fs: base, scratch: scratch}

		hold, err := TakeAside(TakeAsideSpec{FS: fs, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.NoError(t, err)
		require.Equal(t, "journal bytes", string(w38Read(t, base, scratch)))
		require.Empty(t, w43VacNames(t, base, "/out/w43-cleanvanish"))
		require.NoError(t, hold.Unlink())
	})

	t.Run("wedged cleanup unlink keeps our placeholder and the take stands", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w43-cleanfail")
		srcInfo, _ := base.Stat(src)
		sentinel := errors.New("w43 cleanup unlink wedged")
		fs := &w43VacRemoveFailFs{Fs: base, err: sentinel, failAt: 3}

		hold, err := TakeAside(TakeAsideSpec{FS: fs, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.NoError(t, err, "the cleanup wedge is warn-only — the take stands")
		require.Equal(t, "journal bytes", string(w38Read(t, base, scratch)))
		vacNames := w43VacNames(t, base, "/out/w43-cleanfail")
		require.Len(t, vacNames, 1, "our inert placeholder lingers for manual cleanup")
		require.NoError(t, hold.Unlink())
	})

	t.Run("ride-back landing swapped before the drop is retained", func(t *testing.T) {
		base := afero.NewMemMapFs()
		src, scratch, claim := w38TakeFixture(t, base, "/out/w43-dropswap")
		srcInfo, _ := base.Stat(src)
		moveErr := errors.New("w43 publish wedged")
		plant := []byte("foreign swap of the restored reservation")
		fs := &w43SwapAfterRidebackFs{Fs: base, src: src, scratch: scratch, moveErr: moveErr, plant: plant}

		hold, err := TakeAside(TakeAsideSpec{FS: fs, Src: src, Scratch: scratch, Claim: claim, Prove: w38SameProve(srcInfo)})
		require.Nil(t, hold)
		require.ErrorIs(t, err, moveErr)
		require.NotErrorIs(t, err, ErrTakeAsideRestoreFailed, "the ride-back itself landed")
		require.Equal(t, "journal bytes", string(w38Read(t, base, src)))
		require.Equal(t, plant, w38Read(t, base, scratch),
			"the swap under the drop is never unlinked — retained byte-intact")
		require.Empty(t, w43VacNames(t, base, "/out/w43-dropswap"))
	})
}

// vacNameOf returns the single vacated sibling name in dir.
func vacNameOf(t *testing.T, fs afero.Fs, dir string) string {
	t.Helper()
	names := w43VacNames(t, fs, dir)
	require.Len(t, names, 1)
	return dir + "/" + names[0]
}

// --- wedge doubles ---------------------------------------------------------

// w43PlantOnVacateRenameFs plants at the scratch name inside the
// verify→vacate window (immediately before the vacate rename), so the
// no-replace vacate moves the PLANT onto the vacated name — the post-vacate
// identity re-proof must refuse.
type w43PlantOnVacateRenameFs struct {
	afero.Fs
	scratch string
	plant   []byte
	done    bool
}

func (f *w43PlantOnVacateRenameFs) Rename(oldname, newname string) error {
	if !f.done && oldname == f.scratch && strings.Contains(newname, ".vac.") {
		f.done = true
		if err := f.Fs.Remove(oldname); err != nil {
			return err
		}
		if err := afero.WriteFile(f.Fs, oldname, f.plant, 0o600); err != nil {
			return err
		}
	}
	return f.Fs.Rename(oldname, newname)
}

// w43PlantOnPublishClassifyFs plants at the scratch name at its second
// lookup — the vacate freed it (lookup 1 was the reservation re-proof) and
// the racer claims it inside the vacate→publish gap, so the no-replace
// publish's own classification finds the plant.
type w43PlantOnPublishClassifyFs struct {
	afero.Fs
	scratch string
	plant   []byte
	calls   int
}

func (f *w43PlantOnPublishClassifyFs) Stat(name string) (os.FileInfo, error) {
	if name == f.scratch {
		f.calls++
		if f.calls == 2 {
			if err := afero.WriteFile(f.Fs, name, f.plant, 0o600); err != nil {
				return nil, err
			}
		}
	}
	return f.Fs.Stat(name)
}

// w43SwapVacAfterPublishFs replays a foreign swap at the vacated name in the
// publish→cleanup window: the vacated name is learned from its O_EXCL claim
// and swapped (remove + recreate — never a bare MemMap overwrite, the
// w35-documented live-view hazard) the instant src→scratch lands.
type w43SwapVacAfterPublishFs struct {
	afero.Fs
	src     string
	scratch string
	vac     string
	plant   []byte
	done    bool
}

func (f *w43SwapVacAfterPublishFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if flag&os.O_EXCL != 0 && strings.Contains(name, ".vac.") {
		f.vac = name
	}
	return f.Fs.OpenFile(name, flag, perm)
}

func (f *w43SwapVacAfterPublishFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	if err == nil && !f.done && oldname == f.src && newname == f.scratch {
		f.done = true
		if rerr := f.Fs.Remove(f.vac); rerr != nil {
			return rerr
		}
		if werr := afero.WriteFile(f.Fs, f.vac, f.plant, 0o600); werr != nil {
			return werr
		}
	}
	return err
}

// w43VacateVanishFs removes the scratch reservation inside the verify→vacate
// window so the no-replace vacate answers ENOENT — the reservation-vanished
// leg.
type w43VacateVanishFs struct {
	afero.Fs
	scratch string
	done    bool
}

func (f *w43VacateVanishFs) Rename(oldname, newname string) error {
	if !f.done && oldname == f.scratch && strings.Contains(newname, ".vac.") {
		f.done = true
		if err := f.Fs.Remove(oldname); err != nil {
			return err
		}
	}
	return f.Fs.Rename(oldname, newname)
}

// w43PlantAfterVacReleaseFs plants at the just-released vacated name inside
// the release→vacate window: the plant's arrival is scripted atomically
// with the claim release's own unlink completing — the only instant the
// vacated name is provably free ahead of the no-replace vacate publish, so
// the publish's destination classification finds the draw occupied.
// Hooking the release Remove (not a Stat ordinal) keeps the plant in the
// right window no matter how many proofs the release itself runs ahead of
// the classification (wave-58 added the unlink-adjacent re-proof, shifting
// the ordinals the wave-43 Stat-count trigger relied on).
type w43PlantAfterVacReleaseFs struct {
	afero.Fs
	plant []byte
	done  bool
}

func (f *w43PlantAfterVacReleaseFs) Remove(name string) error {
	if err := f.Fs.Remove(name); err != nil {
		return err
	}
	if !f.done && strings.Contains(name, ".vac.") {
		f.done = true
		return afero.WriteFile(f.Fs, name, f.plant, 0o600)
	}
	return nil
}

// w43VacReleaseReproofRaceFs replays a racer inside the wave-58 claim
// release's verify→re-proof window: the FIRST vacated-name lookup (the
// release verify) answers with the still-intact claim, then the claim
// either vanishes on its own (removeOnly — the unlink-adjacent re-proof
// answers ENOENT and the release completes itself) or is swapped for a
// foreign occupant (remove + recreate — never a bare MemMap overwrite, the
// w35-documented live-view hazard — so the re-proof refuses its identity)
// before the release's second lookup runs.
type w43VacReleaseReproofRaceFs struct {
	afero.Fs
	plant      []byte
	removeOnly bool
	done       bool
}

func (f *w43VacReleaseReproofRaceFs) Stat(name string) (os.FileInfo, error) {
	info, err := f.Fs.Stat(name)
	if err == nil && !f.done && strings.Contains(name, ".vac.") {
		f.done = true
		if rerr := f.Fs.Remove(name); rerr != nil {
			return nil, rerr
		}
		if !f.removeOnly {
			if werr := afero.WriteFile(f.Fs, name, f.plant, 0o600); werr != nil {
				return nil, werr
			}
		}
	}
	return info, err
}

// w43FailPostVacateLookupFs wedges the FIRST vacated-name lookup AFTER a
// no-replace vacate rename landed — the post-vacate identity-binding
// instant (the take's post-vacate re-proof, the bound unlink's terminal
// re-bind). Arming off the rename (not a Stat ordinal) keeps the wedge at
// the binding instant no matter how many lookups the claim release runs
// ahead of it (wave-58 dual-reproof).
type w43FailPostVacateLookupFs struct {
	afero.Fs
	err   error
	armed bool
	done  bool
}

func (f *w43FailPostVacateLookupFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	if err == nil && strings.Contains(newname, ".vac.") {
		f.armed = true
	}
	return err
}

func (f *w43FailPostVacateLookupFs) Stat(name string) (os.FileInfo, error) {
	if f.armed && !f.done && strings.Contains(name, ".vac.") {
		f.done = true
		return nil, f.err
	}
	return f.Fs.Stat(name)
}

// w43VanishVacAfterVacateFs removes the vacated name immediately after the
// vacate rename landed — the post-vacate re-proof answers ENOENT.
type w43VanishVacAfterVacateFs struct {
	afero.Fs
	scratch string
	done    bool
}

func (f *w43VanishVacAfterVacateFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	if err == nil && !f.done && oldname == f.scratch && strings.Contains(newname, ".vac.") {
		f.done = true
		if rerr := f.Fs.Remove(newname); rerr != nil {
			return rerr
		}
	}
	return err
}

// w43FailNthVacStatFs wedges the nth lookup of a ".vac." name with a
// sentinel (1 = claim-release verify, 2 = the release's unlink-adjacent
// re-proof).
type w43FailNthVacStatFs struct {
	afero.Fs
	n     int
	err   error
	calls int
}

func (f *w43FailNthVacStatFs) Stat(name string) (os.FileInfo, error) {
	if strings.Contains(name, ".vac.") {
		f.calls++
		if f.calls == f.n {
			return nil, f.err
		}
	}
	return f.Fs.Stat(name)
}

// w43VacClaimCloseFs wraps the O_EXCL vacated-name claim handle and replays
// the claim→release race at its close: either a foreign swap (plant) or the
// claim vanishing on its own (removeOnly).
type w43VacClaimCloseFs struct {
	afero.Fs
	plant      []byte
	removeOnly bool
	done       bool
}

func (f *w43VacClaimCloseFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil || f.done || flag&os.O_EXCL == 0 || !strings.Contains(name, ".vac.") {
		return file, err
	}
	f.done = true
	return &w43VacClaimCloseFile{File: file, name: name, fs: f.Fs, plant: f.plant, removeOnly: f.removeOnly}, nil
}

type w43VacClaimCloseFile struct {
	afero.File
	name       string
	fs         afero.Fs
	plant      []byte
	removeOnly bool
}

func (f *w43VacClaimCloseFile) Close() error {
	err := f.File.Close()
	if err == nil {
		if rerr := f.fs.Remove(f.name); rerr != nil {
			return rerr
		}
		if !f.removeOnly {
			if werr := afero.WriteFile(f.fs, f.name, f.plant, 0o600); werr != nil {
				return werr
			}
		}
	}
	return err
}

// w43FailReader wedges the vacated-name entropy draw.
type w43FailReader struct{ err error }

func (r *w43FailReader) Read([]byte) (int, error) { return 0, r.err }

// w43VacClaimOpenFailFs fails ".vac." O_EXCL claims: maxClaims > 0 fails
// only that many initial claims (os.ErrExist models a racer's re-draw),
// maxClaims < 0 fails every claim (the exhausted-loop leg).
type w43VacClaimOpenFailFs struct {
	afero.Fs
	err       error
	maxClaims int
	claims    int
}

func (f *w43VacClaimOpenFailFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if flag&os.O_EXCL != 0 && strings.Contains(name, ".vac.") {
		if f.maxClaims < 0 || f.claims < f.maxClaims {
			f.claims++
			return nil, f.err
		}
	}
	return f.Fs.OpenFile(name, flag, perm)
}

// w43VacClaimHandleFailFs wraps the first ".vac." claim handle so its Stat
// (or Close) fails — the claim drops its own unreadable/unclosable
// reservation and refuses.
type w43VacClaimHandleFailFs struct {
	afero.Fs
	statErr  error
	closeErr error
	plant    []byte // non-nil: swap the candidate for these bytes at Close (the F1 close-failure foreign-swap leg)
	done     bool
}

func (f *w43VacClaimHandleFailFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil || f.done || flag&os.O_EXCL == 0 || !strings.Contains(name, ".vac.") {
		return file, err
	}
	f.done = true
	return &w43VacClaimHandleFailFile{File: file, fs: f.Fs, name: name, statErr: f.statErr, closeErr: f.closeErr, plant: f.plant}, nil
}

type w43VacClaimHandleFailFile struct {
	afero.File
	fs       afero.Fs
	name     string
	statErr  error
	closeErr error
	plant    []byte
}

func (f *w43VacClaimHandleFailFile) Stat() (os.FileInfo, error) {
	if f.statErr != nil {
		return nil, f.statErr
	}
	return f.File.Stat()
}

func (f *w43VacClaimHandleFailFile) Close() error {
	_ = f.File.Close()
	if f.closeErr != nil {
		if f.plant != nil {
			// Wave-r19 (F1): swap the candidate for a foreign plant before the
			// identity-bound release — the close-failure cleanup must refuse it
			// byte-intact (never a pathname Remove of an unproven object).
			_ = f.fs.Remove(f.name)
			_ = afero.WriteFile(f.fs, f.name, f.plant, 0o600)
		}
		return f.closeErr
	}
	return nil
}

// w43VacRemoveFailFs wedges the failAt-th Remove of a ".vac." name (1 = the
// claim release, 2 = the bound cleanup's terminal release (wave-r19, F2),
// 3 = the bound cleanup's verified terminal unlink).
type w43VacRemoveFailFs struct {
	afero.Fs
	err    error
	failAt int
	calls  int
}

func (f *w43VacRemoveFailFs) Remove(name string) error {
	if strings.Contains(name, ".vac.") {
		f.calls++
		if f.calls == f.failAt {
			return f.err
		}
	}
	return f.Fs.Remove(name)
}

// w43VanishVacAtPostMoveFs removes the vacated name at the post-move
// re-proof (scratch lookup 3) so the success-path cleanup finds it already
// gone.
type w43VanishVacAtPostMoveFs struct {
	afero.Fs
	scratch string
	vac     string
	calls   int
}

func (f *w43VanishVacAtPostMoveFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if flag&os.O_EXCL != 0 && strings.Contains(name, ".vac.") {
		f.vac = name
	}
	return f.Fs.OpenFile(name, flag, perm)
}

func (f *w43VanishVacAtPostMoveFs) Stat(name string) (os.FileInfo, error) {
	if name == f.scratch {
		f.calls++
		if f.calls == 3 {
			if err := f.Fs.Remove(f.vac); err != nil {
				return nil, err
			}
		}
	}
	return f.Fs.Stat(name)
}

// w43SwapAfterRidebackFs wedges the src publish outright, then swaps the
// just-restored reservation placeholder inside the ride-back→drop window —
// the drop's re-proof must refuse and retain (never unlink) the occupant.
type w43SwapAfterRidebackFs struct {
	afero.Fs
	src     string
	scratch string
	moveErr error
	plant   []byte
}

func (f *w43SwapAfterRidebackFs) Rename(oldname, newname string) error {
	if oldname == f.src && newname == f.scratch {
		return f.moveErr
	}
	if strings.Contains(oldname, ".vac.") && newname == f.scratch {
		err := f.Fs.Rename(oldname, newname)
		if err == nil {
			if rerr := f.Fs.Remove(newname); rerr != nil {
				return rerr
			}
			if werr := afero.WriteFile(f.Fs, newname, f.plant, 0o600); werr != nil {
				return werr
			}
		}
		return err
	}
	return f.Fs.Rename(oldname, newname)
}
