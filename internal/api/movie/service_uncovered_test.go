package movie

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMovieDeps_GetAllowedDirs_Uncovered(t *testing.T) {
	deps := MovieDeps{AllowedDirs: []string{"/dir1", "/dir2"}}
	dirs := deps.getAllowedDirs()
	assert.Equal(t, []string{"/dir1", "/dir2"}, dirs)
}

func TestMovieDeps_GetAllowedDirs_Empty_Uncovered(t *testing.T) {
	deps := MovieDeps{}
	dirs := deps.getAllowedDirs()
	assert.Nil(t, dirs)
}

func TestMovieDeps_GetWorkflow_NilFn_Uncovered(t *testing.T) {
	deps := MovieDeps{}
	wf := deps.getWorkflow()
	assert.Nil(t, wf)
}

func TestWithAllowedDirs_Uncovered(t *testing.T) {
	opt := WithAllowedDirs([]string{"/test"})
	deps := MovieDeps{}
	opt(&deps)
	assert.Equal(t, []string{"/test"}, deps.AllowedDirs)
}
