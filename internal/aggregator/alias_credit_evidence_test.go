package aggregator

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAliasMergeRetainsSelectedRawEvidence(t *testing.T) {
	for _, convert := range []bool{false, true} {
		resolver := newAliasResolverWithCache(&MetadataConfig{ActressDatabase: actressDatabaseConfigView{Enabled: true, ConvertAlias: convert}}, nil, map[string]string{"報告": "正規", "別名": "正規"})
		first := models.ActressInfo{JapaneseName: "報告", ThumbURL: "first.jpg"}
		second := models.ActressInfo{DMMID: 88, JapaneseName: "別名", ThumbURL: "second.jpg"}
		for _, sources := range [][]actressSource{{{Source: "first", Actresses: []models.ActressInfo{first}}, {Source: "second", Actresses: []models.ActressInfo{second}}}, {{Source: "second", Actresses: []models.ActressInfo{second}}, {Source: "first", Actresses: []models.ActressInfo{first}}}} {
			actors := newActressMerger().Merge(sources, actressMergeOptions{AliasResolver: resolver})
			require.Len(t, actors, 1)
			source, info, priority := actors[0].ScrapeEvidence()
			require.Equal(t, sources[0].Source, source)
			require.Equal(t, sources[0].Actresses[0], info)
			require.Zero(t, priority)
			require.Equal(t, 88, actors[0].DMMID)
			want := sources[0].Actresses[0].JapaneseName
			if convert {
				want = "正規"
			}
			require.Equal(t, want, actors[0].JapaneseName)
		}
	}
}
