package organizer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
)

// In-place duplicate-failure rig for codex P1 (PR #241): two in-place owners
// primed onto ONE canonical target — a literal "shared" folder format puts
// both target directories at /pool/shared and a literal-prefixed file format
// ("<ID> vidfile") gives both plans the identical target file while keeping
// every source-dedicated. Source files and the foreign inner-target plant all
// carry the match ID in their names so isDedicatedFolder stays true.
const (
	ipfOwnerSrc   = "/pool/oldA/ABC-100 ownA.mkv"
	ipfStandbySrc = "/pool/oldB/ABC-100 ownB.mkv"
	ipfPlant      = "/pool/oldA/ABC-100 vidfile.mkv"
	ipfTargetDir  = "/pool/shared"
	ipfTarget     = "/pool/shared/ABC-100 vidfile.mkv"
)

// vanishedOldDirFs reports the owner's old directory as permanently absent —
// the "source dir vanished post-priming" wedge: planning never stats OldDir
// (ReadDir lands first), while execute's first act is statting it.
type vanishedOldDirFs struct {
	afero.Fs
	oldDir string
}

func (p *vanishedOldDirFs) Stat(name string) (os.FileInfo, error) {
	if filepath.Clean(name) == filepath.Clean(p.oldDir) {
		return nil, &os.PathError{Op: "stat", Path: name, Err: os.ErrNotExist}
	}
	return p.Fs.Stat(name)
}

func ipfMatcher(t *testing.T) matcher.MatcherInterface {
	t.Helper()
	m, err := matcher.NewMatcher(&matcher.Config{RegexEnabled: false})
	require.NoError(t, err)
	return m
}

func ipfCmd(src, name string, tracker *DuplicateTracker, force bool) OrganizeCmd {
	return OrganizeCmd{
		Match:            models.FileMatchInfo{MovieID: "ABC-100", Path: src, Name: name, Extension: ".mkv"},
		Movie:            &models.Movie{ID: "ABC-100"},
		DestDir:          "/dest",
		MoveFiles:        true,
		ForceUpdate:      force,
		DuplicateTracker: tracker,
	}
}

// newIPFOrganizer builds the in-place organizer over fs (poison-wrapped or
// plain) and seeds the two source trees; withPlant arms the foreign
// inner-target file inside the owner's old directory.
func newIPFOrganizer(t *testing.T, orgFs afero.Fs, base afero.Fs, withPlant bool) *Organizer {
	t.Helper()
	cfg := &Config{
		FolderFormat:  "shared",
		FileFormat:    "<ID> vidfile",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeInPlace,
	}
	require.NoError(t, base.MkdirAll(filepath.FromSlash("/pool/oldA"), 0o755))
	require.NoError(t, base.MkdirAll(filepath.FromSlash("/pool/oldB"), 0o755))
	require.NoError(t, afero.WriteFile(base, filepath.FromSlash(ipfOwnerSrc), []byte("a-bytes"), 0o644))
	require.NoError(t, afero.WriteFile(base, filepath.FromSlash(ipfStandbySrc), []byte("b-bytes"), 0o644))
	if withPlant {
		require.NoError(t, afero.WriteFile(base, filepath.FromSlash(ipfPlant), []byte("plant-bytes"), 0o644))
	}
	return NewOrganizer(orgFs, cfg, nil, ipfMatcher(t))
}

func ipfPrime(tracker *DuplicateTracker) {
	tracker.PrimeBatch([]DuplicatePriming{
		{SourcePath: ipfOwnerSrc, TargetPath: ipfTarget, WillMove: true},
		{SourcePath: ipfStandbySrc, TargetPath: ipfTarget, WillMove: true},
	})
}

func ipfExists(t *testing.T, fs afero.Fs, path string) bool {
	t.Helper()
	exists, err := afero.Exists(fs, filepath.FromSlash(path))
	require.NoError(t, err)
	return exists
}

func ipfRead(t *testing.T, fs afero.Fs, path string) []byte {
	t.Helper()
	data, err := afero.ReadFile(fs, filepath.FromSlash(path))
	require.NoError(t, err)
	return data
}

// TestOrganize_InPlaceVanishedOldDir_PrePublicationReleases pins codex P1
// finding leg (a): an in-place owner whose SOURCE DIRECTORY vanished between
// priming and execution fails before ANY rename — a mutation-free failure.
// The blanket !plan.InPlace exemption used to keep such a row journaled with
// the shared target; now the result is marked pre-publication (exactly like
// the non-in-place legs) and the claim releases, so the promoted standby
// publishes and the failed owner's row can never journal the standby's
// renamed directory.
func TestOrganize_InPlaceVanishedOldDir_PrePublicationReleases(t *testing.T) {
	forceCasePosture(t, true)

	for _, force := range []bool{false, true} {
		mode := "normal mode"
		if force {
			mode = "force mode"
		}
		t.Run(mode, func(t *testing.T) {
			base := afero.NewMemMapFs()
			poison := &vanishedOldDirFs{Fs: base, oldDir: "/pool/oldA"}
			org := newIPFOrganizer(t, poison, base, false)
			tracker := NewDuplicateTracker(false)
			ipfPrime(tracker)

			resA, errA := org.Organize(context.Background(), ipfCmd(ipfOwnerSrc, "ABC-100 ownA.mkv", tracker, force))
			require.Error(t, errA, "the vanished source dir fails execute")
			assert.False(t, fsutil.PublishCompleted(errA), "nothing published — no partial-publish marker")
			require.NotNil(t, resA)
			assert.True(t, resA.PrePublication,
				"an in-place failure with NOTHING surviving is the pre-publication class")
			assert.False(t, resA.InPlaceRenamed, "no directory rename happened")

			// The released claim promotes the primed standby: its in-place
			// rename publishes the shared target.
			resB, errB := org.Organize(context.Background(), ipfCmd(ipfStandbySrc, "ABC-100 ownB.mkv", tracker, force))
			require.NoError(t, errB, "the promoted standby executes")
			require.NotNil(t, resB)
			assert.True(t, resB.Moved)
			assert.True(t, resB.InPlaceRenamed)

			// The failed owner's tree is untouched (mutation-free), the
			// standby's directory is now the shared target with its bytes.
			assert.True(t, ipfExists(t, base, ipfOwnerSrc), "owner source intact — nothing was renamed away")
			assert.Equal(t, []byte("b-bytes"), ipfRead(t, base, ipfTarget))
			assert.False(t, ipfExists(t, base, "/pool/oldB"), "the standby really renamed its directory")
		})
	}
}

// TestOrganize_InPlaceInnerRefusalRollbackLanded_PrePublicationReleases pins
// codex P1 finding leg (b): the owner's directory rename LANDS but the inner
// file rename is refused (a foreign file already carries the target name
// inside the renamed directory) and the rollback succeeds — nothing survived
// on disk. The failure is again the pre-publication class: markers cleared,
// claim released, promoted standby unaffected by the failed owner's journal.
func TestOrganize_InPlaceInnerRefusalRollbackLanded_PrePublicationReleases(t *testing.T) {
	forceCasePosture(t, true)

	base := afero.NewMemMapFs()
	org := newIPFOrganizer(t, base, base, true)
	tracker := NewDuplicateTracker(false)
	ipfPrime(tracker)

	// Non-force only: authorization suppresses the regular-file plant.
	resA, errA := org.Organize(context.Background(), ipfCmd(ipfOwnerSrc, "ABC-100 ownA.mkv", tracker, false))
	require.Error(t, errA)
	assert.Contains(t, filepath.ToSlash(errA.Error()), "ABC-100 vidfile.mkv",
		"the foreign inner-target refusal surfaces")
	assert.False(t, fsutil.PublishCompleted(errA))
	require.NotNil(t, resA)
	assert.True(t, resA.PrePublication,
		"rollback landed — nothing survived, so the leg is pre-publication")
	assert.False(t, resA.InPlaceRenamed, "the rolled-back rename no longer claims a surviving mutation")
	assert.Empty(t, resA.OldDirectoryPath)
	assert.Empty(t, resA.NewDirectoryPath)
	assert.Equal(t, ipfTarget, filepath.ToSlash(resA.NewPath),
		"NewPath keeps naming the intended shared target for display only")

	// Rollback restored the owner's tree byte-intact; the shared name is free.
	assert.Equal(t, []byte("a-bytes"), ipfRead(t, base, ipfOwnerSrc))
	assert.Equal(t, []byte("plant-bytes"), ipfRead(t, base, ipfPlant))
	assert.False(t, ipfExists(t, base, ipfTargetDir), "the failed owner's rename left no directory behind")

	// The released claim promotes the standby onto the freed destination.
	resB, errB := org.Organize(context.Background(), ipfCmd(ipfStandbySrc, "ABC-100 ownB.mkv", tracker, false))
	require.NoError(t, errB)
	assert.True(t, resB.Moved)
	assert.Equal(t, []byte("b-bytes"), ipfRead(t, base, ipfTarget))
	assert.False(t, ipfExists(t, base, "/pool/oldB"))
}

// TestOrganize_InPlaceRenameSurvivesRollbackRefusal_Settles pins codex P1
// finding leg (c) — the pinned decision: when the inner-rename rollback is
// REFUSED, the directory rename SURVIVES on disk (the destination name
// physically changed to this owner's target). That mutation is
// publication-equivalent, exactly like the fsutil.PublishCompleted partial
// class: the claim SETTLES (never releases, so no standby publishes or even
// double-attaches onto the owner's surviving directory), the failure result
// keeps its journal semantics (no pre-publication mark), and NewPath/FileName
// name where the bytes actually went — the OLD file name inside the renamed
// directory — so a revert unwinds exactly this surviving mutation.
func TestOrganize_InPlaceRenameSurvivesRollbackRefusal_Settles(t *testing.T) {
	forceCasePosture(t, true)

	for _, force := range []bool{false, true} {
		mode := "normal mode: standby keeps its duplicate conflict"
		if force {
			mode = "force mode: standby keeps the authorized-skip verdict"
		}
		t.Run(mode, func(t *testing.T) {
			base := afero.NewMemMapFs()
			// Refuse exactly the rollback rename (shared → oldA), once.
			poison := &rollbackRefusedFs{Fs: base, old: "/pool/shared", new: "/pool/oldA"}
			org := newIPFOrganizer(t, poison, base, true)
			tracker := NewDuplicateTracker(false)
			ipfPrime(tracker)

			resA, errA := org.Organize(context.Background(), ipfCmd(ipfOwnerSrc, "ABC-100 ownA.mkv", tracker, false))
			require.Error(t, errA)
			assert.False(t, fsutil.PublishCompleted(errA), "the inner publish never landed — refusal class only")
			require.NotNil(t, resA)
			assert.True(t, poison.fired.Load(), "the rollback rename was attempted and refused")
			assert.False(t, resA.PrePublication,
				"a surviving rename is NOT the pre-publication class — journal semantics stand")
			assert.True(t, resA.InPlaceRenamed, "the directory rename survived on disk")
			assert.Equal(t, "/pool/oldA", filepath.ToSlash(resA.OldDirectoryPath))
			assert.Equal(t, ipfTargetDir, filepath.ToSlash(resA.NewDirectoryPath))
			assert.Equal(t, filepath.Join(filepath.FromSlash(ipfTargetDir), "ABC-100 ownA.mkv"), resA.NewPath,
				"the journal names where the owner's bytes actually went")
			assert.Equal(t, "ABC-100 ownA.mkv", resA.FileName)

			// On disk: the shared directory stands with the owner's file at
			// its old name beside the foreign plant.
			assert.False(t, ipfExists(t, base, "/pool/oldA"))
			assert.Equal(t, []byte("a-bytes"), ipfRead(t, base, filepath.Join(ipfTargetDir, "ABC-100 ownA.mkv")))
			assert.Equal(t, []byte("plant-bytes"), ipfRead(t, base, filepath.Join(ipfTargetDir, "ABC-100 vidfile.mkv")))

			// The claim SETTLED on the failing owner: a release would have
			// promoted the primed standby into ownership immediately.
			owner, standby, waiters, present := claimQueueState(t, tracker, ipfTarget)
			require.True(t, present)
			assert.Equal(t, ipfOwnerSrc, owner, "settle keeps the failed owner owning its surviving mutation")
			assert.Equal(t, []string{ipfStandbySrc}, standby)
			assert.Empty(t, waiters)

			// The standby never publishes into (or double-attaches to) the
			// owner's surviving directory. Non-force: the settled claim's
			// duplicate verdict conflicts it. Force: its own plan refuses to
			// rename onto the physically OCCUPIED directory name — in-place
			// never swaps a foreign folder (#224) — which is exactly the
			// no-double-attach guard the settle preserves.
			resB, errB := org.Organize(context.Background(), ipfCmd(ipfStandbySrc, "ABC-100 ownB.mkv", tracker, force))
			require.Error(t, errB, "the standby never publishes onto the owner's surviving directory")
			assert.Nil(t, resB)
			if !force {
				assert.Contains(t, filepath.ToSlash(errB.Error()), "ABC-100 vidfile.mkv",
					"the settled claim verdicts the standby a duplicate conflict")
			} else {
				assert.Contains(t, filepath.ToSlash(errB.Error()), ipfTargetDir,
					"the surviving directory itself refuses the authorized rename (no double-attach)")
			}
			assert.Equal(t, []byte("b-bytes"), ipfRead(t, base, ipfStandbySrc),
				"the standby's bytes never left its source")
			assert.Equal(t, []byte("a-bytes"), ipfRead(t, base, filepath.Join(ipfTargetDir, "ABC-100 ownA.mkv")),
				"the owner's surviving bytes stand untouched")
			assert.Equal(t, []byte("plant-bytes"), ipfRead(t, base, filepath.Join(ipfTargetDir, "ABC-100 vidfile.mkv")),
				"no overwrite displaced the foreign plant either")
		})
	}
}
