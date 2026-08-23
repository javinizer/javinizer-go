package worker

import (
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

type familySnapshotErrorLookup struct {
	resultstore.ResultReadFacade
}

func (l *familySnapshotErrorLookup) GetMovieResult(string) (*resultstore.MovieResult, error) {
	return nil, errors.New("result unavailable")
}

func TestLockedMovieOpsFamilyRevisionSnapshotSkipsUnreadableResult(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "FAM-1", "")
	lookup := &familySnapshotErrorLookup{ResultReadFacade: store}
	pe := NewPosterEditor(lookup, store, nil)
	ops := &LockedMovieOps{pe: pe, movieID: "FAM-1"}

	require.Empty(t, ops.familyRevisionSnapshot())
}
