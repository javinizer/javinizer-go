package dmm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildActressThumbCandidatesCombinesNameAndProfile(t *testing.T) {
	doc := docFromHTMLDMM(t, `<html><body>
		<img src="https://awsimgsrc.dmm.co.jp/pics_dig/mono/actjpgs/iseya_takami.jpg?w=125">
		<img src="https://pics.dmm.co.jp/mono/actjpgs/iseya_takami.jpg">
	</body></html>`)
	got := buildActressThumbCandidates("Takami", "Iseya", 9101, doc)
	require.Equal(t, []string{
		"https://pics.dmm.co.jp/mono/actjpgs/iseya_takami.jpg",
		"https://pics.dmm.co.jp/mono/actjpgs/takami_iseya.jpg",
		"https://awsimgsrc.dmm.co.jp/pics_dig/mono/actjpgs/iseya_takami.jpg",
	}, got)
}

func TestBuildActressThumbCandidatesNameOnlyWhenNoDMMID(t *testing.T) {
	got := buildActressThumbCandidates("Takami", "Iseya", 0, nil)
	require.Equal(t, []string{
		"https://pics.dmm.co.jp/mono/actjpgs/iseya_takami.jpg",
		"https://pics.dmm.co.jp/mono/actjpgs/takami_iseya.jpg",
	}, got)
}

func TestBuildActressThumbCandidatesSkipsNamesWhenMissing(t *testing.T) {
	doc := docFromHTMLDMM(t, `<html><body>
		<img src="https://awsimgsrc.dmm.co.jp/pics_dig/mono/actjpgs/solo.jpg">
	</body></html>`)
	got := buildActressThumbCandidates("", "", 42, doc)
	require.Equal(t, []string{
		"https://awsimgsrc.dmm.co.jp/pics_dig/mono/actjpgs/solo.jpg",
	}, got)
}

func TestBuildActressThumbCandidatesEmptyWhenNoSource(t *testing.T) {
	require.Empty(t, buildActressThumbCandidates("", "", 0, nil))
	require.Empty(t, buildActressThumbCandidates("", "", 42, docFromHTMLDMM(t, `<html></html>`)))
}
