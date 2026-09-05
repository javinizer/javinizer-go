package models

// SubtitleMove records the original and new path of a moved subtitle file.
// Mode distinction (#224 phase E): Moved means the source was relocated
// (revert moves it back); Copied means the source was retained and the
// destination holds our installed copy (revert deletes that copy).
type SubtitleMove struct {
	OriginalPath string
	NewPath      string
	Moved        bool
	Copied       bool
}
