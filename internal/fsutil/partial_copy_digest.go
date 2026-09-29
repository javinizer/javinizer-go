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
// interim delete intent authenticates with (models.DeleteEntry
// CopySize/CopyPartialSHA256): the first and last this-many bytes of the
// object feed the interim digest, so the pin never streams a multi-gigabyte
// payload. All producers and all recovery probes share this constant — a
// span change retroactively revokes every pending interim pin (the recovery
// probe's digest can no longer match, so the entry retains: the safe
// direction), which is why the span is a constant rather than a journaled
// parameter.
const CopyPartialDigestSpan int64 = 64 << 10

// PartialCopyDigest authenticates a regular file's identity for the
// copy-install interim delete intent WITHOUT streaming the payload: it opens
// path, proves regularity from the OPEN HANDLE's own Stat (a through-link
// open never substitutes for a regularity proof — the pin compares against
// this exact identity later, mirroring the full-hash planned-delete leg's
// handle discipline), and digests framing ‖ head ‖ tail over at most
// 2*CopyPartialDigestSpan bounded reads.
//
// The returned pinned FileInfo is the handle's own Stat: callers removing
// the probed object bind the unlink to it (UnlinkVerified), never to a
// pathname lookup taken before or after.
//
// Threat model (why an interim partial proof suffices where the durable pin
// uses a full digest): the entry authenticates a destination the fenced
// publication claimed VACANT and installs through the verified no-replace
// stream, so the only object this pin can fire on between publish and seal
// is either (a) this apply's own just-landed copy — whose size+head+tail
// provably equal the pinned ones — or (b) a foreign file placed in the
// execute→seal window after somehow defeating the no-replace publish, whose
// contents would have to collide on size AND the first AND last 64KiB of the
// source. matching all three without reading the full payload is not a
// practical forgery, and any failure retains (retention-first). The seal
// (FinalizeDeleteIntentCopyDigest) replaces this shape with the streamed
// full digest at the same instant the install is proven, shrinking the
// partial-proof window to code-internal microseconds; the full digest then
// lives in the durable rows completion/reconcile write.
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
