package organizer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
)

func probeAdmittedFixture(t *testing.T) (*Organizer, string, string, string, string) {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "ABC-123.mkv")
	sub := filepath.Join(dir, "ABC-123.srt")
	require.NoError(t, os.WriteFile(src, []byte("video"), 0o600))
	require.NoError(t, os.WriteFile(sub, []byte("subtitle"), 0o600))
	org := NewOrganizer(afero.NewOsFs(), &Config{
		FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true,
		OperationMode: operationmode.OperationModeOrganize,
		MoveSubtitles: true, SubtitleExtensions: []string{".srt"},
	}, nil, nil)
	dest := filepath.Join(dir, "out")
	return org, src, sub, dest, dir
}

func probeAdmittedPlan(t *testing.T, org *Organizer, src, dest string, move bool) *OrganizePlan {
	t.Helper()
	plan, err := org.PlanOrganize(context.Background(), OrganizeCmd{
		Match: models.FileMatchInfo{MovieID: "ABC-123", Path: src, Name: "ABC-123.mkv", Extension: ".mkv"},
		Movie: &models.Movie{ID: "ABC-123"}, DestDir: dest, MoveFiles: move, LinkMode: LinkModeNone,
	})
	require.NoError(t, err)
	return plan
}

func subtitleResultFor(result *OrganizeResult, original string) *SubtitleResult {
	for i := range result.Subtitles {
		if filepath.Clean(result.Subtitles[i].OriginalPath) == filepath.Clean(original) {
			return &result.Subtitles[i]
		}
	}
	return nil
}

// The pre-journaled subtitle set is frozen at the probe: a subtitle created in
// the probe→execute window is refused with the same skip classification the
// occupied gate uses — the admitted subset still installs and the application
// succeeds (codex P2, PRRT_kwDORn9KaM6m9afD).
func TestProbeAdmittedFreezesOutNewlyAppearedSubtitle(t *testing.T) {
	for _, move := range []bool{true, false} {
		t.Run(map[bool]string{false: "copy", true: "move"}[move], func(t *testing.T) {
			org, src, sub, dest, dir := probeAdmittedFixture(t)
			plan := probeAdmittedPlan(t, org, src, dest, move)
			moves := org.PlanSubtitleMoves(plan)
			require.Len(t, moves, 1, "the single enumerated subtitle is installed-able at probe")

			late := filepath.Join(dir, "ABC-123.jpn.srt")
			require.NoError(t, os.WriteFile(late, []byte("late subtitle"), 0o600), "a new subtitle appears inside the probe→execute window")

			result, err := org.ExecuteOrganizePlan(plan, move, LinkModeNone)
			require.NoError(t, err)
			require.Len(t, result.Subtitles, 2, "execute's rescan sees both sources; the gate classifies the late one")

			admitted := subtitleResultFor(result, sub)
			require.NotNil(t, admitted)
			assert.True(t, admitted.Moved || admitted.Copied, "the admitted subtitle installs")
			assert.False(t, admitted.Skipped)

			frozen := subtitleResultFor(result, late)
			require.NotNil(t, frozen)
			assert.True(t, frozen.Skipped, "the un-journaled late source is refused as a skip")
			assert.False(t, frozen.Moved || frozen.Copied, "no install the journal never covered")

			targetStem := filepath.Join(dest, "ABC-123", "ABC-123")
			got, readErr := os.ReadFile(targetStem + ".srt")
			require.NoError(t, readErr)
			assert.Equal(t, "subtitle", string(got), "the admitted endpoint installed")
			require.NoFileExists(t, targetStem+".jpn.srt", "the frozen-out endpoint never lands")
			require.FileExists(t, late, "the refused source is left untouched")
			require.FileExists(t, result.NewPath, "the video leg is unaffected")
			_, statErr := os.Stat(src)
			assert.Equal(t, move, os.IsNotExist(statErr), "move consumes, copy retains the video source")
		})
	}
}

// Un-probed plans (non-deferred direct flows) keep the historical
// rescan-at-execute behavior: the nil admission set never gates.
func TestUnprobedPlanInstallsNewlyAppearedSubtitle(t *testing.T) {
	for _, move := range []bool{true, false} {
		t.Run(map[bool]string{false: "copy", true: "move"}[move], func(t *testing.T) {
			org, src, _, dest, dir := probeAdmittedFixture(t)
			plan := probeAdmittedPlan(t, org, src, dest, move)
			late := filepath.Join(dir, "ABC-123.jpn.srt")
			require.NoError(t, os.WriteFile(late, []byte("late subtitle"), 0o600))

			result, err := org.ExecuteOrganizePlan(plan, move, LinkModeNone)
			require.NoError(t, err)
			require.Len(t, result.Subtitles, 2)
			frozen := subtitleResultFor(result, late)
			require.NotNil(t, frozen)
			assert.Equal(t, move, frozen.Moved)
			assert.Equal(t, !move, frozen.Copied)
			target := filepath.Join(dest, "ABC-123", "ABC-123.jpn.srt")
			got, readErr := os.ReadFile(target)
			require.NoError(t, readErr)
			assert.Equal(t, "late subtitle", string(got), "legacy flows install newly appeared subtitles as before")
		})
	}
}

// Later probes keep the first enumeration: a source discovered by a SECOND
// probe is neither returned (the "what execute would deliver" contract) nor
// installed.
func TestSecondProbeKeepsFrozenEnumeration(t *testing.T) {
	org, src, sub, dest, dir := probeAdmittedFixture(t)
	plan := probeAdmittedPlan(t, org, src, dest, true)
	require.Len(t, org.PlanSubtitleMoves(plan), 1)

	late := filepath.Join(dir, "ABC-123.jpn.srt")
	require.NoError(t, os.WriteFile(late, []byte("late subtitle"), 0o600))
	moves := org.PlanSubtitleMoves(plan)
	require.Len(t, moves, 1, "the freeze survives a re-probe")
	assert.Equal(t, sub, moves[0].OriginalPath)

	result, err := org.ExecuteOrganizePlan(plan, true, LinkModeNone)
	require.NoError(t, err)
	frozen := subtitleResultFor(result, late)
	require.NotNil(t, frozen)
	assert.True(t, frozen.Skipped)
	require.FileExists(t, late)
}

// The freeze composes with the round-31 occupied gate: an admitted source
// whose endpoint is occupied at execute keeps the occupied-skip outcome, and
// the late source beside it stays frozen out.
func TestProbeAdmittedComposesWithOccupiedGate(t *testing.T) {
	org, src, sub, dest, dir := probeAdmittedFixture(t)
	plan := probeAdmittedPlan(t, org, src, dest, true)
	require.Len(t, org.PlanSubtitleMoves(plan), 1)

	target := filepath.Join(dest, "ABC-123", "ABC-123.srt")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.WriteFile(target, []byte("foreign occupant"), 0o644))
	late := filepath.Join(dir, "ABC-123.jpn.srt")
	require.NoError(t, os.WriteFile(late, []byte("late subtitle"), 0o600))

	result, err := org.ExecuteOrganizePlan(plan, true, LinkModeNone)
	require.NoError(t, err)
	occupied := subtitleResultFor(result, sub)
	require.NotNil(t, occupied)
	assert.True(t, occupied.Skipped, "the admitted-but-occupied endpoint skips (round-31 gate)")
	assert.False(t, occupied.Moved)
	require.FileExists(t, sub, "the occupied-skip source is retained")
	frozen := subtitleResultFor(result, late)
	require.NotNil(t, frozen)
	assert.True(t, frozen.Skipped)
	require.FileExists(t, late)
	got, _ := os.ReadFile(target)
	assert.Equal(t, "foreign occupant", string(got), "the occupant's bytes are never touched")
}

// admitSnapshotProof pins the admission identity EAGERLY — dev/inode out of a
// POSIX Stat_t, the volume-serial+file-index handle identity through
// fsutil.BoundObjectIdentity on Windows — and re-proves the object the
// composite is about to consume at equal strength. os.SameFile is NOT this pin
// on Windows: a path-captured FileInfo there records the path, not the object,
// and SameFile's lazy file-id load re-opens that path at COMPARE time — with
// the verified move the source name is already vacated when the claim re-proof
// runs (the comparison fails and the happy path is refused), and with a
// pre-execute swap the same lazy load binds the REPLACEMENT, which the proof
// then silently admits (CI run 36525447204, windows leg). BoundObjectIdentity
// captures the identity at admission, while the name still resolves to the
// admitted object, mirroring the workflow's deferred-source binding (codex P1,
// PRRT_kwDORn9KaM6m9ae4).
func admitSnapshotProof(t *testing.T, fs afero.Fs, path string) fsutil.VerifiedSourceProof {
	t.Helper()
	admitted, err := os.Lstat(path)
	require.NoError(t, err)
	admDev, admIno, admStrong := fsutil.BoundObjectIdentity(fs, path, admitted)
	return func(probed string, info os.FileInfo) error {
		if info == nil || !info.Mode().IsRegular() {
			return fmt.Errorf("%s names a non-regular entry", path)
		}
		dev, ino, strong := fsutil.BoundObjectIdentity(fs, probed, info)
		if admStrong && (!strong || dev != admDev || ino != admIno) {
			return fmt.Errorf("%s names a different object", path)
		}
		if info.Size() != admitted.Size() || !info.ModTime().Equal(admitted.ModTime()) {
			return fmt.Errorf("%s names a different object", path)
		}
		return nil
	}
}

func verifiedDispatchPlan(t *testing.T, org *Organizer, src, dest string, move bool, proof fsutil.VerifiedSourceProof) *OrganizePlan {
	t.Helper()
	plan := probeAdmittedPlan(t, org, src, dest, move)
	plan.BindVerifiedSource(proof)
	return plan
}

// The organizer's no-replace lanes honor a bound admission proof: both the
// move and the copy leg consume the admitted object, while a refusing proof
// aborts before anything lands (codex P1, PRRT_kwDORn9KaM6m9ae4).
func TestVerifiedSourceProofDispatchHappyPath(t *testing.T) {
	for _, move := range []bool{true, false} {
		t.Run(map[bool]string{false: "copy", true: "move"}[move], func(t *testing.T) {
			org, src, _, dest, _ := probeAdmittedFixture(t)
			plan := verifiedDispatchPlan(t, org, src, dest, move, admitSnapshotProof(t, afero.NewOsFs(), src))
			result, err := org.ExecuteOrganizePlan(plan, move, LinkModeNone)
			require.NoError(t, err)
			require.FileExists(t, result.NewPath)
			got, _ := os.ReadFile(result.NewPath)
			assert.Equal(t, "video", string(got))
			_, statErr := os.Stat(src)
			assert.Equal(t, move, os.IsNotExist(statErr), "the verified move consumes, the verified copy retains")
		})
	}
}

func TestVerifiedSourceProofDispatchRefusal(t *testing.T) {
	for _, move := range []bool{true, false} {
		t.Run(map[bool]string{false: "copy", true: "move"}[move], func(t *testing.T) {
			org, src, _, dest, dir := probeAdmittedFixture(t)
			proof := admitSnapshotProof(t, afero.NewOsFs(), src)
			// Rename-swap the source for a different object after the admission
			// snapshot: the proof then refuses inside execute.
			aside := filepath.Join(dir, "swap.bin")
			require.NoError(t, os.WriteFile(aside, []byte("replacement payload"), 0o600))
			require.NoError(t, os.Remove(src))
			require.NoError(t, os.Rename(aside, src))
			plan := verifiedDispatchPlan(t, org, src, dest, move, proof)
			result, err := org.ExecuteOrganizePlan(plan, move, LinkModeNone)
			require.Error(t, err)
			require.ErrorIs(t, err, fsutil.ErrTakeAsideForeign)
			require.NotNil(t, result)
			assert.False(t, result.Moved, "a refused publish never reports a clean move")
			target := filepath.Join(dest, "ABC-123", "ABC-123.mkv")
			require.NoFileExists(t, target, "nothing lands at the destination on refusal")
			got, rerr := os.ReadFile(src)
			require.NoError(t, rerr)
			assert.Equal(t, "replacement payload", string(got), "the foreign replacement is restored/retained byte-intact")
		})
	}
}

// The verified copy's refusal in execute arrives through the same wrapper the
// legacy copy lane uses; the publish-completed and refusal classifiers stay
// disjoint on the refusal text.
func TestVerifiedSourceProofDispatchRefusalClassification(t *testing.T) {
	org, src, _, dest, dir := probeAdmittedFixture(t)
	proof := admitSnapshotProof(t, afero.NewOsFs(), src)
	aside := filepath.Join(dir, "swap.bin")
	require.NoError(t, os.WriteFile(aside, []byte("replacement payload"), 0o600))
	require.NoError(t, os.Remove(src))
	require.NoError(t, os.Rename(aside, src))
	plan := verifiedDispatchPlan(t, org, src, dest, false, proof)
	_, err := org.ExecuteOrganizePlan(plan, false, LinkModeNone)
	require.Error(t, err)
	mapped := mapNoReplaceRefusal(fmt.Errorf("failed to copy file: %w", err), filepath.Join(dest, "x"))
	assert.True(t, errors.Is(mapped, fsutil.ErrTakeAsideForeign))
	assert.False(t, fsutil.PublishRefusal(mapped), "an admission refusal is a pre-publication failure, never a refusal class")
	assert.False(t, fsutil.PublishCompleted(mapped))
}
