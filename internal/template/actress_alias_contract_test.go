package template

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestActressAliasContractValidationRenderingAndConditionals(t *testing.T) {
	engine := NewEngine()
	for _, tc := range []struct {
		alias, populated string
	}{
		{"aCtOrS", "First, Second"},
		{"AcTrEsSeS", "First, Second"},
		{"aCtReSs", "First"},
		{"AcToRnAmE", "First"},
		{"aCtReSsNaMe", "First"},
	} {
		t.Run(tc.alias, func(t *testing.T) {
			for _, populated := range []bool{true, false} {
				ctx := &Context{}
				want, branch := "", "absent"
				if populated {
					ctx.Actresses = []string{"First", "Second"}
					want, branch = tc.populated, "present"
				}
				before := ctx.Clone()
				tag := "<" + tc.alias + ">"
				tmpl := tag + "|<iF:" + tc.alias + ">present<eLsE>absent</iF>"
				require.NoError(t, engine.ValidateTags(tmpl), "ordinary and conditional aliases must both be known")
				got, err := engine.Execute(tmpl, ctx)
				require.NoError(t, err)
				require.Equal(t, want+"|"+branch, got)
				bounded, err := engine.ExecuteWithMaxBytes(tmpl, ctx, len(want+"|"+branch))
				require.NoError(t, err)
				require.Equal(t, got, bounded)
				require.Equal(t, before, ctx, "rendering must not alter the supplied context")
			}
		})
	}
}

func TestActressAliasContractSingleNamePrecedenceAndModifiers(t *testing.T) {
	detail := ActressDetail{FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣"}
	for _, alias := range []string{"ACTRESS", "ACTORNAME", "ACTRESSNAME"} {
		for _, tc := range []struct {
			name, modifier, want string
			ctx                  *Context
		}{
			{"explicit override", "JA,LAST", "Credited Alias", &Context{ActressName: "Credited Alias", ActressDetails: []ActressDetail{detail}, Actresses: []string{"Raw Name"}}},
			{"details before raw", "", "Hatano Yui", &Context{ActressDetails: []ActressDetail{detail}, Actresses: []string{"Raw Name"}}},
			{"context Japanese preference", "", "波多野結衣", &Context{ActressDetails: []ActressDetail{detail}, ActressLanguageJa: true}},
			{"English first overrides context", "EN,FIRST", "Yui Hatano", &Context{ActressDetails: []ActressDetail{detail}, ActressLanguageJa: true}},
			{"last overrides first default", "EN,LAST", "Hatano Yui", &Context{ActressDetails: []ActressDetail{detail}, FirstNameOrder: true}},
			{"Japanese modifier", "JA,FIRST", "波多野結衣", &Context{ActressDetails: []ActressDetail{detail}}},
			{"raw fallback stays verbatim", "JA,LAST", "Raw Name", &Context{Actresses: []string{"Raw Name", "Later Name"}}},
			{"empty", "EN,FIRST", "", &Context{}},
		} {
			t.Run(alias+"/"+tc.name, func(t *testing.T) {
				engine := NewEngine()
				before := tc.ctx.Clone()
				tmpl := "<" + alias
				if tc.modifier != "" {
					tmpl += ":" + tc.modifier
				}
				tmpl += ">"
				require.NoError(t, engine.ValidateTags(tmpl))
				got, err := engine.ExecuteWithContext(context.Background(), tmpl, tc.ctx)
				require.NoError(t, err)
				require.Equal(t, tc.want, got)
				require.Equal(t, before, tc.ctx)
			})
		}
	}
}

func TestActressAliasContractListLanguageOrderAndDelimiter(t *testing.T) {
	ctx := &Context{
		ActressDetails: []ActressDetail{
			{FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣"},
			{FirstName: "Aoi", LastName: "Tsukasa", JapaneseName: "葵つかさ"},
		},
		Actresses:         []string{"Ignored raw name"},
		ActressLanguageJa: true,
		FirstNameOrder:    true,
		ActressDelimiter:  " / ",
	}
	before := ctx.Clone()
	for _, alias := range []string{"aCtOrS", "aCtReSsEs"} {
		for _, tc := range []struct{ modifier, want string }{
			{"", "波多野結衣 / 葵つかさ"},
			{"EN,FIRST,DELIM= & ", "Yui Hatano & Aoi Tsukasa"},
			{"EN,LAST,DELIM=;", "Hatano Yui;Tsukasa Aoi"},
			{"JA,LAST,DELIM=", "波多野結衣葵つかさ"},
			{"|", "波多野結衣 / 葵つかさ"},
		} {
			t.Run(alias+"/"+tc.modifier, func(t *testing.T) {
				tmpl := "<" + alias
				if tc.modifier != "" {
					tmpl += ":" + tc.modifier
				}
				tmpl += ">"
				engine := NewEngine()
				require.NoError(t, engine.ValidateTags(tmpl))
				got, err := engine.Execute(tmpl, ctx)
				require.NoError(t, err)
				require.Equal(t, tc.want, got)
				require.Equal(t, before, ctx)
			})
		}
	}
}

func TestActressAliasContractUnknownTagValidationRemainsStrict(t *testing.T) {
	engine := NewEngine()
	ctx := &Context{Actresses: []string{"First"}}
	for _, tmpl := range []string{"<ACTRESS_TYPO>", "<IF:ACTRESS_TYPO>present<ELSE>absent</IF>"} {
		require.ErrorContains(t, engine.ValidateTags(tmpl), "unknown tag: ACTRESS_TYPO")
	}
	got, err := engine.Execute("before<ACTRESS_TYPO>after|<IF:ACTRESS_TYPO>present<ELSE>absent</IF>", ctx)
	require.NoError(t, err)
	require.Equal(t, "beforeafter|absent", got, "execution's existing unknown-tag behavior is separate from strict validation")
}
