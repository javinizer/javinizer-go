package history

// POSTER-WRITE-HARDENING wave-36 (codex local review round 6, PR#215 finding
// F2) — destStillNamesRestoredObject's content qualifier unit legs: the
// wave-31 identity recheck shares the published-bytes digest requirement
// wherever both a provable identity AND the restored bytes are known, so an
// inode-reused substitute with equal metadata but different bytes is
// rejected by the sibling gate too, and an unreadable occupant fails
// closed.

import (
	"crypto/sha256"
	"errors"
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w36ReadFailFs opens the destination fine, then fails the content READ —
// the hash stream's I/O wedge.
type w36ReadFailFs struct {
	afero.Fs
	dest string
	err  error
}

type w36ReadFailFile struct {
	afero.File
	err error
}

func (f w36ReadFailFile) Read([]byte) (int, error) { return 0, f.err }

func (f *w36ReadFailFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	if name == f.dest && flag&os.O_WRONLY == 0 && flag&os.O_RDWR == 0 && flag&os.O_CREATE == 0 {
		return w36ReadFailFile{File: file, err: f.err}, nil
	}
	return file, nil
}

func TestRestoredDestIdentityW36_ContentQualifierLegs(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/w36i", 0o755))
	dest := "/w36i/poster.jpg"
	require.NoError(t, afero.WriteFile(base, dest, []byte("actual bytes"), 0o644))
	info, err := lstatRestoreSource(base, dest)
	require.NoError(t, err)

	id := restoredDestIdentityFromContent(info, sha256.Sum256([]byte("actual bytes")))
	require.True(t, id.known, "a MemMapFs publish stat still carries the (hashless) identity facts")
	require.True(t, id.hashed, "the published-bytes digest rides the identity")
	require.True(t, destStillNamesRestoredObject(base, dest, id),
		"the untouched destination verifies — content equal")

	// Different bytes never verify: identity metadata (size, mtime) is
	// read off the occupant-untouched FileInfo the identity snapshot
	// captured, and the content digest disagrees.
	other := restoredDestIdentityFromContent(info, sha256.Sum256([]byte("other bytes")))
	require.False(t, destStillNamesRestoredObject(base, dest, other),
		"content mismatch answers false — the inode/metadata equivalence is no longer enough")

	// An unreadable occupant fails closed at the hash stage.
	require.False(t, destStillNamesRestoredObject(&w36ReadFailFs{Fs: base, dest: dest, err: errors.New("w36 read wedged")}, dest, id),
		"a hash read failure answers false (fail closed)")
	// An unopenable occupant fails closed the same way.
	require.False(t, destStillNamesRestoredObject(&w36DestOpenFailFs{Fs: base, dest: dest, err: errors.New("w36 open wedged")}, dest, id),
		"a hash open failure answers false (fail closed)")

	// The unknown-identity residual stands unaltered: neither facts nor
	// bytes are bound, so the recheck skips.
	require.True(t, destStillNamesRestoredObject(base, dest, restoredDestIdentity{}),
		"the documented virtual-leg skip (no provable identity) is unchanged")
}
