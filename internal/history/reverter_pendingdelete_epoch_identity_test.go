package history

import (
	"os"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
)

// codex P2 (PRRT_kwDORn9KaM6nHyl5): the hard-link pin's presence rides the
// explicit identity_pinned marker, never the mtime — an epoch-dated source
// (a restored or normalized media file) serializes an all-zero tuple whose
// journal decode must still authenticate the linked name at recovery, on
// both the real (strong tuple) and in-memory (weak metadata legs) reading
// paths. Pre-marker rows keep exactly the branch their own evidence implies.

// epochFile pins path's atime/mtime to the Unix epoch — the shape a freshly
// restored or normalized media file presents.
func epochFile(t *testing.T, fs afero.Fs, path string) {
	t.Helper()
	require.NoError(t, fs.Chtimes(path, time.Unix(0, 0), time.Unix(0, 0)))
}

func TestCleanupGeneratedFilesFS_EpochStrongIdentityPinConsumesLinkedName(t *testing.T) {
	base, root, source, dest := setupLinkPair(t)
	epochFile(t, base, source)
	srcInfo, err := os.Stat(source)
	require.NoError(t, err)
	require.Zero(t, srcInfo.ModTime().Unix(), "the source now reads as epoch-dated — the shape the sentinel lost")
	dev, ino, identityOK := fsutil.BoundObjectIdentity(base, source, srcInfo)
	require.True(t, identityOK, "OsFs exposes the kernel identity")

	pin := models.DeleteEntry{
		Path:            dest,
		IdentityPinned:  true,
		IdentityStrong:  true,
		IdentityDev:     dev,
		IdentityIno:     ino,
		IdentitySize:    srcInfo.Size(),
		IdentityModUnix: 0,
	}
	require.NoError(t, os.Link(source, dest))

	// pinLedgerOp runs the pin through the production ledger marshal/parse —
	// the epoch tuple rides as all-zero and the marker carries the presence.
	cleanupGeneratedFilesFS(base, pinLedgerOp(models.OperationTypeHardlink, pin), root)

	_, statErr := os.Lstat(dest)
	assert.True(t, os.IsNotExist(statErr), "the epoch-dated pin consumed the linked name instead of silently retaining it")
	assert.FileExists(t, source, "the source's bytes survive — unlinking a name never frees the shared inode")
}

func TestCleanupGeneratedFilesFS_EpochWeakIdentityPinConsumes(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/lib", 0o755))
	require.NoError(t, afero.WriteFile(base, "/lib/movie.mkv", []byte("memfs video"), 0o644))
	epochFile(t, base, "/lib/movie.mkv")
	info, err := base.Stat("/lib/movie.mkv")
	require.NoError(t, err)
	require.Zero(t, info.ModTime().Unix())
	pin := models.DeleteEntry{Path: "/lib/movie.mkv", IdentityPinned: true, IdentitySize: info.Size()}

	cleanupGeneratedFilesFS(base, pinLedgerOp(models.OperationTypeHardlink, pin), "/lib")

	exists, existsErr := afero.Exists(base, "/lib/movie.mkv")
	require.NoError(t, existsErr)
	assert.False(t, exists, "an identity-free filesystem authenticates the epoch pin on the metadata legs — presence now comes from the marker, a shape pre-marker code could not express at all")
}

// Pre-marker rows must land on EXACTLY the branch their serialized evidence
// always implied: a legacy sentinel row still authenticates, a pre-marker
// STRONG epoch blob authenticates through its surviving strong tuple, and a
// pre-marker WEAK epoch blob — which carried no expressible evidence — keeps
// the retain leg (never a silent flip to removal).
func TestCleanupGeneratedFilesFS_LegacyIdentityPinsKeepTheirBranch(t *testing.T) {
	base, root, source, dest := setupLinkPair(t)
	srcInfo, err := os.Stat(source)
	require.NoError(t, err)
	dev, ino, identityOK := fsutil.BoundObjectIdentity(base, source, srcInfo)
	require.True(t, identityOK)

	t.Run("pre-marker sentinel pin still consumes the linked name", func(t *testing.T) {
		require.NoError(t, os.Link(source, dest))
		legacy := models.DeleteEntry{Path: dest, IdentityStrong: true, IdentityDev: dev, IdentityIno: ino, IdentitySize: srcInfo.Size(), IdentityModUnix: srcInfo.ModTime().Unix()}
		require.False(t, legacy.IdentityPinned, "the row is built in the pre-marker shape")
		require.True(t, legacy.HasIdentityPin(), "the mtime sentinel keeps classifying")
		cleanupGeneratedFilesFS(base, pinLedgerOp(models.OperationTypeHardlink, legacy), root)
		_, statErr := os.Lstat(dest)
		assert.True(t, os.IsNotExist(statErr))
		assert.FileExists(t, source)
	})

	t.Run("pre-marker strong epoch blob consumes through its strong evidence", func(t *testing.T) {
		epochFile(t, base, source)
		epochInfo, statErr := os.Stat(source)
		require.NoError(t, statErr)
		require.Zero(t, epochInfo.ModTime().Unix(), "the tuple's mtime leg serialized as absent in the pre-marker blob")
		resetDest(t, source, dest)
		require.NoError(t, os.Link(source, dest), "the prior leg consumed the earlier link — re-install the crash-window shape")
		legacy := models.DeleteEntry{Path: dest, IdentityStrong: true, IdentityDev: dev, IdentityIno: ino, IdentitySize: epochInfo.Size()}
		require.False(t, legacy.IdentityPinned)
		require.True(t, legacy.HasIdentityPin(), "the strong tuple overrides the zeroed sentinel")
		cleanupGeneratedFilesFS(base, pinLedgerOp(models.OperationTypeHardlink, legacy), root)
		_, lStatErr := os.Lstat(dest)
		assert.True(t, os.IsNotExist(lStatErr), "the legacy epoch row lands on the identity leg — no silent retention")
		assert.FileExists(t, source)
	})

	t.Run("pre-marker weak epoch row keeps the retain leg", func(t *testing.T) {
		resetDest(t, source, dest)
		require.NoError(t, os.Link(source, dest), "the prior leg consumed the earlier link — re-install the occupant")
		legacy := models.DeleteEntry{Path: dest, IdentitySize: srcInfo.Size()}
		require.False(t, legacy.IdentityPinned)
		require.False(t, legacy.HasIdentityPin(), "the pre-marker blob recorded no presence evidence for this shape")
		cleanupGeneratedFilesFS(base, pinLedgerOp(models.OperationTypeHardlink, legacy), root)
		// Same object, never removed or replaced: the empty-hash leg's
		// retain posture, exactly as the pre-marker dispatch behaved.
		destInfo, statErr := os.Stat(dest)
		require.NoError(t, statErr, "the evidence-free legacy row retains the occupant")
		assert.True(t, os.SameFile(srcInfo, destInfo), "the retained occupant is untouched — no removal, no recreation")
	})
}
