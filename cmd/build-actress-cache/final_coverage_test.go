package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMainSuccessAndFailurePaths(t *testing.T) {
	oldArgs, oldExit := os.Args, exit
	t.Cleanup(func() { os.Args, exit = oldArgs, oldExit })

	os.Args = []string{"build-actress-cache", "--list-sources"}
	main()

	var code int
	exit = func(got int) { code = got }
	os.Args = []string{"build-actress-cache"}
	main()
	assert.Equal(t, 1, code)
}

func TestRunReturnsParseErrors(t *testing.T) {
	err := run(t.Context(), []string{"--workers", "0"}, os.Stdout, os.Stderr)
	assert.ErrorContains(t, err, "workers")
}
func TestStringListStringSet(t *testing.T) {
	var sl stringList
	assert.Equal(t, "", sl.String())
	assert.NoError(t, sl.Set("a, b, c"))
	assert.Equal(t, "a,b,c", sl.String())
}

func TestParameterMapStringSet(t *testing.T) {
	var pm parameterMap
	assert.NoError(t, pm.Set("key=value"))
	assert.Equal(t, "key=value", pm.String())
	assert.NoError(t, pm.Set("x=y"))
	assert.Equal(t, "key=value,x=y", pm.String())
	assert.ErrorContains(t, pm.Set("bad"), "key=value")
}
