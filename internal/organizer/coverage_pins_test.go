package organizer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
)

// CoverPin* tests exist only for pinned branch surfaces (#224 phase-C). They
// exercise named legs that production flows don't otherwise reach.

func TestCoverPin_KindNameEveryBranch(t *testing.T) {
	assert.Equal(t, "file", PlanConflict{Kind: ConflictFile}.kindName())
	assert.Equal(t, "directory", PlanConflict{Kind: ConflictDirectory}.kindName())
	assert.Equal(t, "symlink", PlanConflict{Kind: ConflictSymlink}.kindName())
	assert.Equal(t, "unknown", PlanConflict{Kind: ConflictKind(255)}.kindName())
}

func TestCoverPin_JoinKeepsBare(t *testing.T) {
	got := joinPlanConflictPaths([]PlanConflict{{Path: "/a"}, {Path: "/b"}})
	assert.Equal(t, "/a; /b", got)
}

// Target-Stat failure must surface as an error rather than a quiet swallow.
func TestCoverPin_RefusalErrPropagation(t *testing.T) {
	fs := afero.NewMemMapFs()
	_ = fs.MkdirAll("/dest", 0o755)
	wrapped := statFails{fs}
	_, _, err := refuseIfUnsuppressibleAuthorizedDestination(wrapped, "/src/a.mp4", "/dest/fail-me.mp4")
	assert.Error(t, err)
}

type statFails struct{ afero.Fs }

func (s statFails) Stat(name string) (os.FileInfo, error) {
	if filepath.Base(name) == "fail-me.mp4" {
		return nil, errors.New("simulated stat failure")
	}
	return s.Fs.Stat(name)
}

func (s statFails) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if filepath.Base(name) == "fail-me.mp4" {
		return nil, false, errors.New("simulated lstat failure")
	}
	if lst, ok := s.Fs.(afero.Lstater); ok {
		return lst.LstatIfPossible(name)
	}
	info, err := s.Fs.Stat(name)
	return info, false, err
}
