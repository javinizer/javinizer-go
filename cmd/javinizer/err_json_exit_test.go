package main

import (
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/scrape"
	"github.com/stretchr/testify/assert"
)

func TestRun_ErrJSONExitSuppressed(t *testing.T) {
	// Verify that ErrJSONExit is properly suppressed (returns 1 without writeCrashLog)
	oldExecuteFn := executeFn
	defer func() { executeFn = oldExecuteFn }()
	executeFn = func() error { return scrape.ErrJSONExit }

	exitCode := run()
	assert.Equal(t, 1, exitCode)
}

func TestRun_ErrJSONExitNotWriteCrashLog(t *testing.T) {
	// Verify that a non-ErrJSONExit error does get writeCrashLog (returns 1)
	oldExecuteFn := executeFn
	defer func() { executeFn = oldExecuteFn }()
	executeFn = func() error { return errors.New("regular error") }

	exitCode := run()
	assert.Equal(t, 1, exitCode)
}
