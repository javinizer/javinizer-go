package jav321

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestLabeledAnchorCaptureSkipsEmptyAndPreservesUniqueNames(t *testing.T) {
	got := extractLabeledAnchorValues(`<b>出演者</b>: <a></a><a>  </a><a><span>Alice &amp; Bob</span></a><a>Alice &amp; Bob</a><a>Carol</a><br>`, []string{"出演者"})
	require.Equal(t, []string{"Alice & Bob", "Carol"}, got)
}
