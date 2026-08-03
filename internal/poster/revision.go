package poster

import (
	"fmt"
	"os"
	"path/filepath"
)

// AssetRevision derives a cache generation/revision token for a temp poster
// asset from its filesystem metadata: mtime-nanoseconds + size. A rescrape or
// poster-from-URL refresh can replace the bytes of {posterID}-full.jpg from
// the SAME source URL, leaving every URL-level snapshot (the crop endpoint's
// pre/post-lock effective-source guard and the client's expected_source_url)
// equal while the displayed image changed; the revision distinguishes those
// generations. The token is OPAQUE to clients: the temp poster endpoint
// exposes it as the X-Poster-Revision response header and the crop endpoint
// validates PosterCropRequest.expected_poster_revision against the CURRENT
// cache file's revision under the poster-source lock.
func AssetRevision(fi os.FileInfo) string {
	if fi == nil {
		return ""
	}
	return fmt.Sprintf("%d-%d", fi.ModTime().UnixNano(), fi.Size())
}

// FullSourceRevision returns AssetRevision for the cached full-size poster
// source ({tempDir}/posters/{jobID}/{posterID}-full.jpg) — the same file
// CropWithBounds measures and the -full.jpg variant serveTempPoster backs with
// the X-Poster-Revision header, so a client token echoes exactly this value.
// jobID/posterID are re-validated for parity with every other manager method
// (callers resolve them from user input) even though the crop endpoint already
// validates both before reaching here.
func (pm *PosterManager) FullSourceRevision(jobID, posterID string) (string, error) {
	if err := ValidateJobID(jobID); err != nil {
		return "", err
	}
	if err := validatePosterID(posterID); err != nil {
		return "", err
	}
	tempPosterDir := filepath.Join(pm.tempDir, "posters", jobID)
	fi, err := pm.fs.Stat(filepath.Join(tempPosterDir, posterID+"-full.jpg"))
	if err != nil {
		return "", err
	}
	return AssetRevision(fi), nil
}
