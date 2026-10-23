package template

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// Cancel the underlying context at an observed checkpoint, rather than returning
// an invented Err value. Done and all subsequent Err calls retain the standard
// context cancellation contract, without a race or sleep-based scheduling guess.
type cancelAtRenderCheckpoint struct {
	context.Context
	cancel context.CancelFunc
	checks int
	at     int
}

func (c *cancelAtRenderCheckpoint) Err() error {
	c.checks++
	if c.checks == c.at {
		c.cancel()
	}
	return c.Context.Err()
}

func TestRenderCancellationAtProcessingCheckpoints(t *testing.T) {
	for _, tc := range []struct {
		name     string
		template string
		at       int
	}{
		{"before tag resolution", "<ID>", 2},
		{"after tag replacement", "<ID>", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			underlying, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			execCtx := &cancelAtRenderCheckpoint{Context: underlying, cancel: cancel, at: tc.at}
			ctx := &Context{ID: "ABC-123"}
			before, err := json.Marshal(ctx)
			require.NoError(t, err)

			got, err := NewEngine().ExecuteWithContext(execCtx, tc.template, ctx)

			require.Empty(t, got, "canceled rendering must not return partial output")
			require.EqualError(t, err, "template execution canceled: context canceled")
			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, tc.at, execCtx.checks)
			select {
			case <-execCtx.Done():
			default:
				t.Fatal("the underlying context was not canceled")
			}
			after, marshalErr := json.Marshal(ctx)
			require.NoError(t, marshalErr)
			require.Equal(t, before, after, "canceled rendering must preserve public template inputs")
			require.Nil(t, ctx.execCtx, "execution context must not be installed on the caller's input")
		})
	}
}

func TestReleaseDateOutsideFourDigitYearPreservesSource(t *testing.T) {
	for _, tc := range []struct {
		name string
		year int
		want string
	}{
		{"five digit year", 10000, "10000-01-02"},
		{"negative year", -1, "-0001-01-02"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// These are valid time.Time values, but their formatted dates cannot
			// round-trip through Go's fixed-width 2006-01-02 parsing layout.
			date := time.Date(tc.year, time.January, 2, 0, 0, 0, 0, time.UTC)
			ctx := &Context{ReleaseDate: &date}
			before := date
			got, err := NewEngine().Execute("<RELEASEDATE:YYYY>", ctx)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			require.Equal(t, before, *ctx.ReleaseDate)
		})
	}
}

func TestActressCommaLanguageModifiersPreserveNames(t *testing.T) {
	for _, tc := range []struct {
		name     string
		modifier string
		japanese string
		want     string
	}{
		{"blank component", "JA, ,FIRST", "日本名", "日本名"},
		{"blank component with name order fallback", "JA, ,FIRST", "", "Given Family"},
		{"multiple languages include Japanese", "EN,JA", "日本名", "日本名"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := &Context{ActressDetails: []ActressDetail{{FirstName: "Given", LastName: "Family", JapaneseName: tc.japanese}}}
			before := ctx.Clone()
			got, err := NewEngine().Execute("<ACTRESS:"+tc.modifier+">", ctx)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			require.Equal(t, before.ActressDetails, ctx.ActressDetails)
			require.False(t, ctx.FirstNameOrder, "tag-local ordering must not change context preferences")
		})
	}
}

func TestGroupedSingleBlankActressUsesUnknownPolicy(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode models.UnknownActressMode
		want string
	}{
		{"fallback", models.UnknownActressModeFallback, "uncredited"},
		{"skip", models.UnknownActressModeSkip, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := &Context{
				Actresses: []string{""}, GroupActress: true,
				UnknownActressMode: tc.mode, GroupUnknownActressName: "uncredited",
			}
			before := ctx.Clone()
			got, err := NewEngine().Execute("<ACTORS>", ctx)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			require.Equal(t, before.Actresses, ctx.Actresses)
			require.Equal(t, tc.mode, ctx.UnknownActressMode)
		})
	}
}
