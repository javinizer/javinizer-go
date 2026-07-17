package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRootCommand_SilenceErrors(t *testing.T) {
	assert.True(t, rootCmd.SilenceErrors, "rootCmd should have SilenceErrors set to prevent Cobra from printing JSON sentinel errors")
}
