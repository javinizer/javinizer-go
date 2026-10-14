package workflow

import "github.com/spf13/afero"

// A distinct final name can already be a hardlink to its exact source. Only
// strong, eager, no-follow object identity establishes that this is a resident
// no-op; matching bytes or metadata cannot justify a no-op or ownership of a private input.
func artifactSameRegularFile(fs afero.Fs, source, target string) bool {
	lst, ok := fs.(afero.Lstater)
	if !ok {
		return false
	}
	info, noFollow, err := lst.LstatIfPossible(source)
	if err != nil || !noFollow {
		return false
	}
	identity := captureArtifactSourceIdentity(fs, source, info)
	if !identity.hasDevIno {
		return false
	}
	info, noFollow, err = lst.LstatIfPossible(target)
	if err != nil || !noFollow || !identity.matches(fs, target, info) {
		return false
	}
	// A target probe must not authorize an old inode whose source name was
	// concurrently replaced. Publication independently revalidates admission.
	info, noFollow, err = lst.LstatIfPossible(source)
	return err == nil && noFollow && identity.matches(fs, source, info)
}
