package nfo

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Lexical only: never Stat/Open a Windows device name to verify rejection.
func TestNFOSidecarFoldRejectsWindowsDevices(t *testing.T) {
	for _, stem := range []string{"ＣＯＮ", "ＮＵＬ", "ＡＵＸ", "ＰＲＮ", "ＣＯＭ１", "ＬＰＴ９"} {
		assert.Equal(t, []string{stem + ".nfo", stem + "．ｎｆｏ"}, nfoSidecarFilenames(stem+"．ｍｋｖ"))
	}
	assert.Equal(t, []string{"ＲＣＴ.nfo", "ＲＣＴ．ｎｆｏ", "RCT.nfo", "RCT．ｎｆｏ"}, nfoSidecarFilenames("ＲＣＴ．ｍｋｖ"))

	wide := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r == ' ' {
				return 0x3000
			}
			if r >= '!' && r <= '~' {
				return r + 0xFEE0
			}
			return r
		}, s)
	}
	reserved := []string{"CoN", "nul", "Aux", "pRn", "CON ", "CON.backup", "con .backup.more", "CONIN$", "ConOut$"}
	for _, prefix := range []string{"COM", "LPT"} {
		for digit := 1; digit <= 9; digit++ {
			reserved = append(reserved, fmt.Sprintf("%s%d", prefix, digit))
		}
		for _, digit := range []string{"¹", "²", "³"} {
			reserved = append(reserved, prefix+digit)
		}
	}
	for _, ascii := range reserved {
		t.Run(ascii, func(t *testing.T) {
			stem := wide(ascii)
			assert.Equal(t, []string{stem + ".nfo", stem + "．ｎｆｏ"}, nfoSidecarFilenames(stem+"．ｍｋｖ"))
		})
	}
	for _, ascii := range []string{"CONSOLE", "XCON", "NULX", "COM0", "COM10", "LPT0", "LPT10", "COM¹X", ".hidden", "RCT-156-HD"} {
		t.Run("ordinary/"+ascii, func(t *testing.T) {
			stem := wide(ascii)
			assert.Equal(t, []string{stem + ".nfo", stem + "．ｎｆｏ", ascii + ".nfo", ascii + "．ｎｆｏ"}, nfoSidecarFilenames(stem+"．ｍｋｖ"))
		})
	}
}
