package worker

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWithMovieEditLock_CallbackSeesOnlyCores pins the callback method set:
// it can compose lock-free cores, but cannot recursively acquire the public
// editor wrapper while the family key is held.
func TestWithMovieEditLock_CallbackSeesOnlyCores(t *testing.T) {
	store := NewInMemoryJobStore()
	job := seedOneMovie(t, store, "/v/cores.mp4", "CORES-001")

	var exported []string
	err := job.posterEditor.WithMovieEditLock("CORES-001", func(ops *LockedMovieOps) error {
		typ := reflect.TypeOf(ops)
		for i := 0; i < typ.NumMethod(); i++ {
			method := typ.Method(i)
			if method.PkgPath == "" { // exported method
				exported = append(exported, method.Name)
			}
		}
		return nil
	})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{
		"ApplyFieldOverride",
		"ApplyFieldOverrideWithRevisions",
		"ExcludeFamily",
		"MovieID",
		"UpdateMovieFamily",
		"UpdatePosterCrop",
		"UpdatePosterFromURL",
	}, exported)
}
