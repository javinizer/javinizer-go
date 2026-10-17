package models

import "time"

// FileMatchInfo is the canonical type that crosses all seam boundaries.
// It carries file match metadata from scan+match through apply, organize,
// and preview. Adding a new multipart field requires updating only this
// struct — no bridge functions or duplicate types needed.
//
// Filename-spelling contract (fullwidth ASCII):
//   - Path is the file's actual on-disk path, raw spelling included; it is
//     the source of truth for file I/O (moves, renames, directory scans)
//     and can end in a fullwidth extension (RCT-156-HD．ｍｋｖ).
//   - Name is the file's basename with its extension spelling folded to
//     ASCII (RCT-156-HD．ｍｋｖ reports "RCT-156-HD.mkv"); the stem keeps its
//     raw spelling so fullwidth-written custom regexes still see it.
//   - Extension is the folded ASCII extension and is always a literal
//     suffix of Name. Base-name derivation must go through
//     strings.TrimSuffix(Name, Extension) — deriving from base(Path)
//     instead retains the raw fullwidth extension.
type FileMatchInfo struct {
	Path        string    `json:"path"`
	MovieID     string    `json:"movie_id"`
	IsMultiPart bool      `json:"is_multi_part"`
	PartNumber  int       `json:"part_number"`
	PartSuffix  string    `json:"part_suffix"`
	Name        string    `json:"name"`
	Extension   string    `json:"extension"`
	Size        int64     `json:"size"`
	ModTime     time.Time `json:"mod_time"`
}

// FirstValidFileResult returns the first FileMatchInfo with a non-empty Path,
// or nil if none found. This is a utility used by the preview orchestrator
// and downloader to extract multipart info from file results.
func FirstValidFileResult(fileResults []FileMatchInfo) *FileMatchInfo {
	for i := range fileResults {
		if fileResults[i].Path != "" {
			return &fileResults[i]
		}
	}
	return nil
}
