package dmm

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestExtractActressProfileMetadata(t *testing.T) {
	doc := docFromHTMLDMM(t, "<h1 class=\"list-title\"><span class=\"bold\">安倍亜沙美(あべあさみ)</span>の商品一覧</h1>")
	require.Equal(t, models.ActressInfo{DMMID: 19244, JapaneseName: "安倍亜沙美"}, extractActressProfileMetadata(doc, 19244))
}

func TestExtractActressProfileMetadataLatinName(t *testing.T) {
	doc := docFromHTMLDMM(t, "<h1 class=\"list-title\"><span class=\"bold\">Yui Hatano(はたのゆい)</span>の商品一覧</h1>")
	require.Equal(t, models.ActressInfo{DMMID: 5786, FirstName: "Yui", LastName: "Hatano"}, extractActressProfileMetadata(doc, 5786))
}
