package template

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestActorListOmitsBlankDisplayNames(t *testing.T) {
	for _, structured := range []bool{false, true} {
		for _, tc := range []struct {
			name  string
			names []string
			want  string
		}{
			{"empty", []string{""}, ""},
			{"all blank", []string{"", " \t", ""}, ""},
			{"leading", []string{"", "Alpha"}, "Alpha"},
			{"trailing", []string{"Alpha", ""}, "Alpha"},
			{"interspersed", []string{"", "Alpha", " ", "Beta", ""}, "Alpha / Beta"},
			{"preserve names", []string{" Alpha ", "Beta", "Alpha"}, " Alpha  / Beta / Alpha"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				ctx := &Context{Actresses: tc.names, ActressDelimiter: " / "}
				if structured {
					for _, name := range tc.names {
						ctx.ActressDetails = append(ctx.ActressDetails, ActressDetail{FirstName: name})
					}
				}
				before := ctx.Clone()
				for _, tag := range []string{"<ACTORS>", "<ACTRESSES>", "<ACTORS:DELIM= / >"} {
					got, err := NewEngine().Execute(tag, ctx)
					require.NoError(t, err)
					require.Equal(t, tc.want, got)
				}
				require.Equal(t, before.Actresses, ctx.Actresses)
				require.Equal(t, before.ActressDetails, ctx.ActressDetails)
			})
		}
	}
}

func TestActorListBlankNamesPreserveGrouping(t *testing.T) {
	for _, mode := range []models.UnknownActressMode{models.UnknownActressModeSkip, models.UnknownActressModeFallback} {
		ctx := &Context{Actresses: []string{"", "Alpha"}, GroupActress: true, UnknownActressMode: mode}
		for _, tc := range []struct {
			minimum int
			want    string
		}{
			{0, "@Group"}, {2, "@Group"}, {3, "Alpha"},
		} {
			ctx.GroupActressMin = tc.minimum
			got, err := NewEngine().Execute("<ACTORS>", ctx)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		}
		ctx.Actresses = []string{"", ""}
		ctx.GroupActressMin = 2
		got, err := NewEngine().Execute("<ACTORS>", ctx)
		require.NoError(t, err)
		require.Equal(t, "@Group", got)
		ctx.GroupActressMin = 3
		got, err = NewEngine().Execute("<ACTORS>", ctx)
		require.NoError(t, err)
		require.Empty(t, got)
	}
}

func TestActorListBlankNamesPreserveFormatting(t *testing.T) {
	ctx := &Context{
		ActressDetails: []ActressDetail{{}, {FirstName: "Given", LastName: "Family", JapaneseName: "日本名"}, {}},
		FirstNameOrder: true,
	}
	for _, tc := range []struct{ tag, want string }{
		{"<ACTORS>", "Given Family"},
		{"<ACTRESSES:LAST>", "Family Given"},
		{"<ACTORS:JA>", "日本名"},
		{"<ACTORS:FIRST,EN,DELIM=|>", "Given Family"},
		{"<ACTORS:DELIM=>", "Given Family"},
	} {
		got, err := NewEngine().Execute(tc.tag, ctx)
		require.NoError(t, err)
		require.Equal(t, tc.want, got)
	}
}
