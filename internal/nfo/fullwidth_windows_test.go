package nfo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Lexical only: never Stat/Open a Windows device name to verify rejection.
func TestNFOSidecarFoldRejectsWindowsDevices(t *testing.T) {
	for _, stem := range []string{"ＣＯＮ", "ＮＵＬ", "ＡＵＸ", "ＰＲＮ", "ＣＯＭ１", "ＬＰＴ９"} {
		assert.Equal(t, []string{stem + ".nfo", stem + "．ｎｆｏ"}, nfoSidecarFilenames(stem+"．ｍｋｖ"))
	}
	assert.Equal(t, []string{"ＲＣＴ.nfo", "ＲＣＴ．ｎｆｏ", "RCT.nfo", "RCT．ｎｆｏ"}, nfoSidecarFilenames("ＲＣＴ．ｍｋｖ"))
}
