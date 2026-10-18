package fsutil

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/spf13/afero"
)

// CopyPartialDigestSpan is the fixed head/tail window the copy-install
// interim delete intent's content marker binds (models.DeleteEntry
// CopySize/CopyPartialSHA256): the first and last this-many bytes of the
// object feed the interim digest, so the pin never streams a multi-gigabyte
// payload. Recovery no longer probes with this digest (codex P1,
// PRRT_kwDORn9KaM6novbT — the crash-surviving interim proof is not deletion
// authorization, so an unsealed entry retains regardless of a match); the
// constant stays shared by every producer so a journaled marker keeps one
// exact meaning.
const CopyPartialDigestSpan int64 = 64 << 10

// PartialCopyDigest derives the copy-install interim delete intent's
// pin-time content marker WITHOUT streaming the payload: it opens path,
// proves regularity from the OPEN HANDLE's own Stat (a through-link open
// never substitutes for a regularity proof), and digests
// framing ‖ head ‖ tail over at most 2*CopyPartialDigestSpan bounded reads.
// The returned pinned FileInfo is the handle's own Stat.
//
// Role (codex P1, PRRT_kwDORn9KaM6novbT): the digest is ONLY the interim
// marker, journaled before the publish stream exists — it is NOT deletion
// authorization. The unsealed entry survives the very crash it covers, so
// by recovery time the bounded size+head+tail proof can no longer
// distinguish the landed copy from a payload edited only between the digest
// windows; the reverter retains every entry still carrying this shape, and
// only the seal (FinalizeDeleteIntentCopyDigest swapping in the full
// streamed digest) ever lets recovery unlink the destination. (An earlier
// design let recovery fire on this proof, reasoning the execute→seal window
// was code-internal microseconds — the crash itself stretches that window
// to the whole crash→recovery interval, which is exactly the exposed state.)
func PartialCopyDigest(fs afero.Fs, path string) (pinned os.FileInfo, digest string, err error) {
	file, err := fs.Open(path)
	if err != nil {
		return nil, "", fmt.Errorf("open artifact for partial digest %s: %w", path, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			pinned, digest, err = nil, "", fmt.Errorf("close partial digest %s: %w", path, closeErr)
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return nil, "", fmt.Errorf("inspect artifact for partial digest %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, "", fmt.Errorf("partial digest %s: not a regular file (mode %v)", path, info.Mode())
	}
	size := info.Size()
	head := make([]byte, 0, min(size, CopyPartialDigestSpan))
	if size > 0 {
		head = head[:min(size, CopyPartialDigestSpan)]
		if _, err := file.ReadAt(head, 0); err != nil {
			return nil, "", fmt.Errorf("partial digest %s head: %w", path, err)
		}
	}
	var tail []byte
	if size > CopyPartialDigestSpan {
		tail = make([]byte, CopyPartialDigestSpan)
		if _, err := file.ReadAt(tail, size-CopyPartialDigestSpan); err != nil {
			return nil, "", fmt.Errorf("partial digest %s tail: %w", path, err)
		}
	}
	h := sha256.New()
	var framing [8]byte
	binary.BigEndian.PutUint64(framing[:], uint64(size))
	_, _ = h.Write(framing[:])
	_, _ = h.Write(head)
	_, _ = h.Write(tail)
	return info, hex.EncodeToString(h.Sum(nil)), nil
}
