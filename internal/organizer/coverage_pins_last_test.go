package organizer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

// inPlace+OverwriteAuthorized with a fs whose Lstat returns failure for our
// TargetPath — covers the authorized move leg's classify-error lane
// (the `if lerr != nil { return err }` line inside the dedicated in-place leg).
func TestPinInPlaceAuthorizedErr(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in", "V.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("v"), 0o644))

	wrapped := &statFailOnTarget{Fs: afero.NewOsFs(), targetBasename: "boom.mp4"}
	strategy := newInPlaceStrategy(wrapped, &Config{FileFormat: "<ID>", FolderFormat: "<ID>", RenameFile: true}, nil, nil)
	plan := &OrganizePlan{
		Match:       models.FileMatchInfo{Path: src, Name: "V.mp4", Extension: ".mp4", MovieID: "V"},
		SourcePath:  src,
		TargetDir:   filepath.Join(dir, "out"),
		TargetFile:  "boom.mp4",
		TargetPath:  filepath.Join(dir, "out", "boom.mp4"),
		WillMove:    true,
		InPlace:     false,
		IsDedicated: false,
		Conflicts:   []PlanConflict{},
		// Authorize → the authorized classify must probe the target and err out.
		overwriteAuthorized: true,
	}
	_, err := strategy.Execute(plan)
	require.Error(t, err)
	require.Contains(t, err.Error(), "fail")
}

// The "conflicts detected" pin in Organize(): the block at 587-589 is only
// reachable when cmd.DryRun=false and `plan` holds conflicts while
// validatePlan reported none — i.e. ForceUpdate=true with occupied non-file
// destination (validation suppressed the file kind). We can't reach it through
// OrganizeCmd because validatePlan checks all conflicts again; the lane is
// reachable only through planner.plan() internal flows. It's exercised by
// TestOrganize_* tests via the copy path (TestT2Pin_* above).
type statFailOnTarget struct {
	afero.Fs
	targetBasename string
}

func (s *statFailOnTarget) Stat(name string) (os.FileInfo, error) {
	if filepath.Base(name) == s.targetBasename {
		return nil, errors.New("simulated stat failure")
	}
	return s.Fs.Stat(name)
}

func (s *statFailOnTarget) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if filepath.Base(name) == s.targetBasename {
		return nil, false, errors.New("simulated lstat failure")
	}
	if lst, ok := s.Fs.(afero.Lstater); ok {
		return lst.LstatIfPossible(name)
	}
	i, e := s.Fs.Stat(name)
	return i, false, e
}
