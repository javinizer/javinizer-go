package template

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTemplateWithLanguageTags(t *testing.T) {
	releaseDate := time.Date(2020, 9, 13, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		engineOpts EngineOptions
		template   string
		ctx        *Context
		want       string
		wantErr    bool
	}{
		{
			name:       "Explicit language tag en",
			engineOpts: EngineOptions{},
			template:   "<TITLE:en>",
			ctx: &Context{
				Title: "Base Title",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
					"ja": {Language: "ja", Title: "Japanese Title"},
				},
			},
			want:    "English Title",
			wantErr: false,
		},
		{
			name:       "Explicit language tag ja",
			engineOpts: EngineOptions{},
			template:   "<TITLE:ja>",
			ctx: &Context{
				Title: "Base Title",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
					"ja": {Language: "ja", Title: "Japanese Title"},
				},
			},
			want:    "Japanese Title",
			wantErr: false,
		},
		{
			name:       "Explicit language tag zh",
			engineOpts: EngineOptions{},
			template:   "<TITLE:zh>",
			ctx: &Context{
				Title: "Base Title",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
					"zh": {Language: "zh", Title: "Chinese Title"},
				},
			},
			want:    "Chinese Title",
			wantErr: false,
		},
		{
			name:       "Explicit language not found - fallback to base",
			engineOpts: EngineOptions{},
			template:   "<TITLE:ko>",
			ctx: &Context{
				Title: "Base Title",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
				},
			},
			want:    "Base Title",
			wantErr: false,
		},
		{
			name:       "Multiple language tags in same template",
			engineOpts: EngineOptions{},
			template:   "<TITLE:en> / <TITLE:ja>",
			ctx: &Context{
				Title: "Base Title",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
					"ja": {Language: "ja", Title: "Japanese Title"},
				},
			},
			want:    "English Title / Japanese Title",
			wantErr: false,
		},
		{
			name:       "Language tag with truncation - combined not supported",
			engineOpts: EngineOptions{DefaultLanguage: "en"},
			template:   "<TITLE:en>",
			ctx: &Context{
				Title: "Base Title",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "Very Long English Title That Needs Truncation"},
				},
			},
			want:    "Very Long English Title That Needs Truncation",
			wantErr: false,
		},
		{
			name:       "Language tag for DIRECTOR",
			engineOpts: EngineOptions{},
			template:   "<DIRECTOR:en>",
			ctx: &Context{
				Director: "Base Director",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Director: "English Director"},
					"ja": {Language: "ja", Director: "Japanese Director"},
				},
			},
			want:    "English Director",
			wantErr: false,
		},
		{
			name:       "Language tag for MAKER",
			engineOpts: EngineOptions{},
			template:   "<MAKER:ja>",
			ctx: &Context{
				Maker: "Base Maker",
				Translations: map[string]models.MovieTranslation{
					"ja": {Language: "ja", Maker: "Japanese Maker"},
				},
			},
			want:    "Japanese Maker",
			wantErr: false,
		},
		{
			name:       "Language tag for STUDIO synonym",
			engineOpts: EngineOptions{},
			template:   "<STUDIO:en>",
			ctx: &Context{
				Maker: "Base Maker",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Maker: "English Studio"},
				},
			},
			want:    "English Studio",
			wantErr: false,
		},
		{
			name:       "Language tag for LABEL",
			engineOpts: EngineOptions{},
			template:   "<LABEL:zh>",
			ctx: &Context{
				Label: "Base Label",
				Translations: map[string]models.MovieTranslation{
					"zh": {Language: "zh", Label: "Chinese Label"},
				},
			},
			want:    "Chinese Label",
			wantErr: false,
		},
		{
			name:       "Language tag for SERIES",
			engineOpts: EngineOptions{},
			template:   "<SERIES:en>",
			ctx: &Context{
				Series: "Base Series",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Series: "English Series"},
				},
			},
			want:    "English Series",
			wantErr: false,
		},
		{
			name:       "Language tag for DESCRIPTION",
			engineOpts: EngineOptions{},
			template:   "<DESCRIPTION:ja>",
			ctx: &Context{
				Description: "Base Description",
				Translations: map[string]models.MovieTranslation{
					"ja": {Language: "ja", Description: "Japanese Description"},
				},
			},
			want:    "Japanese Description",
			wantErr: false,
		},
		{
			name:       "Language tag for ORIGINALTITLE",
			engineOpts: EngineOptions{},
			template:   "<ORIGINALTITLE:ja>",
			ctx: &Context{
				OriginalTitle: "Base OriginalTitle",
				Translations: map[string]models.MovieTranslation{
					"ja": {Language: "ja", OriginalTitle: "Japanese OriginalTitle"},
				},
			},
			want:    "Japanese OriginalTitle",
			wantErr: false,
		},
		{
			name:       "Mixed language tags and regular tags",
			engineOpts: EngineOptions{DefaultLanguage: "en"},
			template:   "<ID> - <TITLE:ja> (<TITLE:en>) [<STUDIO>]",
			ctx: &Context{
				ID:    "IPX-123",
				Title: "Base Title",
				Maker: "Base Maker",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title", Maker: "English Studio"},
					"ja": {Language: "ja", Title: "Japanese Title"},
				},
			},
			want:    "IPX-123 - Japanese Title (English Title) [English Studio]",
			wantErr: false,
		},
		{
			name:       "Language tag in conditional content",
			engineOpts: EngineOptions{},
			template:   "<IF:TITLE><TITLE:en><ELSE>No Title</IF>",
			ctx: &Context{
				Title: "Base Title",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
				},
			},
			want:    "English Title",
			wantErr: false,
		},
		{
			name:       "Language tag conditional - no translation returns base",
			engineOpts: EngineOptions{},
			template:   "<IF:TITLE><TITLE:ja><ELSE>No Japanese Title</IF>",
			ctx: &Context{
				Title:        "Base Title",
				Translations: map[string]models.MovieTranslation{},
			},
			want:    "Base Title",
			wantErr: false,
		},
		{
			name:       "Invalid uppercase EN treated as legacy modifier",
			engineOpts: EngineOptions{DefaultLanguage: "en"},
			template:   "<TITLE:EN>",
			ctx: &Context{
				Title: "Base Title",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
				},
			},
			want:    "English Title",
			wantErr: false,
		},
		{
			name:       "Case insensitive tag parsing",
			engineOpts: EngineOptions{},
			template:   "<title:en>",
			ctx: &Context{
				Title: "Base Title",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
				},
			},
			want:    "English Title",
			wantErr: false,
		},
		{
			name:       "Mixed case tag and modifier - modifier is case sensitive",
			engineOpts: EngineOptions{DefaultLanguage: "en"},
			template:   "<Title:En>",
			ctx: &Context{
				Title: "Base Title",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
				},
			},
			want:    "English Title",
			wantErr: false,
		},
		{
			name:       "Full template with multiple translated fields",
			engineOpts: EngineOptions{DefaultLanguage: "en"},
			template:   "<ID> [<TITLE:ja>] - <TITLE:en> by <DIRECTOR:en> (<YEAR>)",
			ctx: &Context{
				ID:          "IPX-535",
				Title:       "Base Title",
				Director:    "Base Director",
				ReleaseDate: &releaseDate,
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title", Director: "English Director"},
					"ja": {Language: "ja", Title: "Japanese Title"},
				},
			},
			want:    "IPX-535 [Japanese Title] - English Title by English Director (2020)",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewEngineWithOptions(tt.engineOpts)
			got, err := engine.Execute(tt.template, tt.ctx)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestTemplateWithDefaultLanguage(t *testing.T) {
	releaseDate := time.Date(2020, 9, 13, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		engineOpts EngineOptions
		template   string
		ctx        *Context
		want       string
		wantErr    bool
	}{
		{
			name:       "No default language - TITLE uses base field",
			engineOpts: EngineOptions{},
			template:   "<ID> - <TITLE>",
			ctx: &Context{
				ID:    "IPX-123",
				Title: "Base Title",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
				},
			},
			want:    "IPX-123 - Base Title",
			wantErr: false,
		},
		{
			name:       "Default language en - TITLE uses translation",
			engineOpts: EngineOptions{DefaultLanguage: "en"},
			template:   "<ID> - <TITLE>",
			ctx: &Context{
				ID:    "IPX-123",
				Title: "Base Title",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
				},
			},
			want:    "IPX-123 - English Title",
			wantErr: false,
		},
		{
			name:       "Default language ja - TITLE uses Japanese",
			engineOpts: EngineOptions{DefaultLanguage: "ja"},
			template:   "<ID> - <TITLE>",
			ctx: &Context{
				ID:    "IPX-123",
				Title: "Base Title",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
					"ja": {Language: "ja", Title: "Japanese Title"},
				},
			},
			want:    "IPX-123 - Japanese Title",
			wantErr: false,
		},
		{
			name:       "Default language not found - fallback to base",
			engineOpts: EngineOptions{DefaultLanguage: "ko"},
			template:   "<ID> - <TITLE>",
			ctx: &Context{
				ID:    "IPX-123",
				Title: "Base Title",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
				},
			},
			want:    "IPX-123 - Base Title",
			wantErr: false,
		},
		{
			name:       "Fallback languages work when default not found",
			engineOpts: EngineOptions{DefaultLanguage: "ko", FallbackLanguages: []string{"ja", "en"}},
			template:   "<ID> - <TITLE>",
			ctx: &Context{
				ID:    "IPX-123",
				Title: "Base Title",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
					"ja": {Language: "ja", Title: "Japanese Title"},
				},
			},
			want:    "IPX-123 - Japanese Title",
			wantErr: false,
		},
		{
			name:       "Context default overrides engine default",
			engineOpts: EngineOptions{DefaultLanguage: "en"},
			template:   "<ID> - <TITLE>",
			ctx: &Context{
				ID:              "IPX-123",
				Title:           "Base Title",
				DefaultLanguage: "ja",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
					"ja": {Language: "ja", Title: "Japanese Title"},
				},
			},
			want:    "IPX-123 - Japanese Title",
			wantErr: false,
		},
		{
			name:       "Explicit tag overrides defaults",
			engineOpts: EngineOptions{DefaultLanguage: "en"},
			template:   "<ID> - <TITLE:zh>",
			ctx: &Context{
				ID:              "IPX-123",
				Title:           "Base Title",
				DefaultLanguage: "ja",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
					"ja": {Language: "ja", Title: "Japanese Title"},
					"zh": {Language: "zh", Title: "Chinese Title"},
				},
			},
			want:    "IPX-123 - Chinese Title",
			wantErr: false,
		},
		{
			name:       "Default language with truncation modifier",
			engineOpts: EngineOptions{DefaultLanguage: "en"},
			template:   "<TITLE:30>",
			ctx: &Context{
				Title: "Base Title",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "Very Long English Title That Should Be Truncated"},
				},
			},
			want:    "Very Long English Title~",
			wantErr: false,
		},
		{
			name:       "Default language affects multiple translatable tags",
			engineOpts: EngineOptions{DefaultLanguage: "en"},
			template:   "<TITLE> by <DIRECTOR> [<MAKER>]",
			ctx: &Context{
				Title:    "Base Title",
				Director: "Base Director",
				Maker:    "Base Maker",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title", Director: "English Director", Maker: "English Studio"},
				},
			},
			want:    "English Title by English Director [English Studio]",
			wantErr: false,
		},
		{
			name:       "Mixed default and explicit language tags",
			engineOpts: EngineOptions{DefaultLanguage: "en"},
			template:   "<TITLE> (<TITLE:ja>)",
			ctx: &Context{
				Title: "Base Title",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
					"ja": {Language: "ja", Title: "Japanese Title"},
				},
			},
			want:    "English Title (Japanese Title)",
			wantErr: false,
		},
		{
			name:       "Non-translatable tag unaffected by default language",
			engineOpts: EngineOptions{DefaultLanguage: "en"},
			template:   "<ID> - <RUNTIME>min",
			ctx: &Context{
				ID:      "IPX-123",
				Runtime: 120,
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
				},
			},
			want:    "IPX-123 - 120min",
			wantErr: false,
		},
		{
			name:       "Full template with default language",
			engineOpts: EngineOptions{DefaultLanguage: "en"},
			template:   "<ID> [<MAKER>] - <TITLE> (<YEAR>)",
			ctx: &Context{
				ID:          "IPX-535",
				Title:       "Base Title",
				Maker:       "Base Maker",
				ReleaseDate: &releaseDate,
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title", Maker: "English Studio"},
				},
			},
			want:    "IPX-535 [English Studio] - English Title (2020)",
			wantErr: false,
		},
		{
			name:       "Empty translation field - fallback to next candidate",
			engineOpts: EngineOptions{DefaultLanguage: "en", FallbackLanguages: []string{"ja"}},
			template:   "<TITLE>",
			ctx: &Context{
				Title: "Base Title",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: ""},
					"ja": {Language: "ja", Title: "Japanese Title"},
				},
			},
			want:    "Japanese Title",
			wantErr: false,
		},
		{
			name:       "All candidates empty - fallback to base",
			engineOpts: EngineOptions{DefaultLanguage: "en", FallbackLanguages: []string{"ja"}},
			template:   "<TITLE>",
			ctx: &Context{
				Title: "Base Title",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: ""},
					"ja": {Language: "ja", Title: ""},
				},
			},
			want:    "Base Title",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewEngineWithOptions(tt.engineOpts)
			got, err := engine.Execute(tt.template, tt.ctx)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestTemplateBackwardCompatibility(t *testing.T) {
	releaseDate := time.Date(2020, 9, 13, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		engineOpts EngineOptions
		template   string
		ctx        *Context
		want       string
		wantErr    bool
	}{
		{
			name:       "Numeric truncation modifier works without default language",
			engineOpts: EngineOptions{},
			template:   "<TITLE:30>",
			ctx: &Context{
				Title: "Very Long Title That Should Be Truncated To Fit",
			},
			want:    "Very Long Title That~",
			wantErr: false,
		},
		{
			name:       "Numeric truncation modifier works with default language",
			engineOpts: EngineOptions{DefaultLanguage: "en"},
			template:   "<TITLE:30>",
			ctx: &Context{
				Title: "Base Title",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "Very Long English Title That Should Be Truncated"},
				},
			},
			want:    "Very Long English Title~",
			wantErr: false,
		},
		{
			name:       "Date format modifier still works",
			engineOpts: EngineOptions{},
			template:   "<RELEASEDATE:YYYY-MM>",
			ctx: &Context{
				ReleaseDate: &releaseDate,
			},
			want:    "2020-09",
			wantErr: false,
		},
		{
			name:       "Actor delimiter modifier still works",
			engineOpts: EngineOptions{},
			template:   "<ACTORS: | >",
			ctx: &Context{
				Actresses: []string{"Actor1", "Actor2", "Actor3"},
			},
			want:    "Actor1 | Actor2 | Actor3",
			wantErr: false,
		},
		{
			name:       "Genres delimiter modifier still works",
			engineOpts: EngineOptions{},
			template:   "<GENRES:;>",
			ctx: &Context{
				Genres: []string{"Genre1", "Genre2"},
			},
			want:    "Genre1;Genre2",
			wantErr: false,
		},
		{
			name:       "Index padding modifier still works",
			engineOpts: EngineOptions{},
			template:   "screenshot<INDEX:3>.jpg",
			ctx: &Context{
				Index: 5,
			},
			want:    "screenshot005.jpg",
			wantErr: false,
		},
		{
			name:       "Part padding modifier still works",
			engineOpts: EngineOptions{},
			template:   "movie-pt<PART:2>.mp4",
			ctx: &Context{
				PartNumber: 3,
			},
			want:    "movie-pt03.mp4",
			wantErr: false,
		},
		{
			name:       "Case modifier UPPERCASE still works",
			engineOpts: EngineOptions{},
			template:   "<ID:UPPERCASE>",
			ctx: &Context{
				ID: "ipx-123",
			},
			want:    "IPX-123",
			wantErr: false,
		},
		{
			name:       "Case modifier LOWERCASE still works",
			engineOpts: EngineOptions{},
			template:   "<ID:LOWERCASE>",
			ctx: &Context{
				ID: "IPX-123",
			},
			want:    "ipx-123",
			wantErr: false,
		},
		{
			name:       "Legacy template without translations works",
			engineOpts: EngineOptions{},
			template:   "<ID> [<STUDIO>] - <TITLE> (<YEAR>)",
			ctx: &Context{
				ID:          "IPX-535",
				Title:       "Test Movie Title",
				Maker:       "Test Studio",
				ReleaseDate: &releaseDate,
			},
			want:    "IPX-535 [Test Studio] - Test Movie Title (2020)",
			wantErr: false,
		},
		{
			name:       "Legacy template with translations but no default language",
			engineOpts: EngineOptions{},
			template:   "<ID> - <TITLE>",
			ctx: &Context{
				ID:    "IPX-535",
				Title: "Base Title",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
				},
			},
			want:    "IPX-535 - Base Title",
			wantErr: false,
		},
		{
			name:       "Conditional blocks still work",
			engineOpts: EngineOptions{},
			template:   "<ID><IF:SERIES> - <SERIES></IF>",
			ctx: &Context{
				ID:     "IPX-535",
				Series: "Test Series",
			},
			want:    "IPX-535 - Test Series",
			wantErr: false,
		},
		{
			name:       "Conditional with ELSE still works",
			engineOpts: EngineOptions{},
			template:   "<ID><IF:DIRECTOR> by <DIRECTOR><ELSE> (Unknown Director)</IF>",
			ctx: &Context{
				ID:       "IPX-535",
				Director: "",
			},
			want:    "IPX-535 (Unknown Director)",
			wantErr: false,
		},
		{
			name:       "STUDIO synonym for MAKER still works",
			engineOpts: EngineOptions{},
			template:   "<ID> [<STUDIO>]",
			ctx: &Context{
				ID:    "IPX-535",
				Maker: "Test Studio",
			},
			want:    "IPX-535 [Test Studio]",
			wantErr: false,
		},
		{
			name:       "ACTRESSES synonym for ACTORS still works",
			engineOpts: EngineOptions{},
			template:   "<ACTRESSES>",
			ctx: &Context{
				Actresses: []string{"Actress1", "Actress2"},
			},
			want:    "Actress1, Actress2",
			wantErr: false,
		},
		{
			name:       "DISC synonym for PART still works",
			engineOpts: EngineOptions{},
			template:   "movie-cd<DISC:2>.mp4",
			ctx: &Context{
				PartNumber: 1,
			},
			want:    "movie-cd01.mp4",
			wantErr: false,
		},
		{
			name:       "GroupActress feature still works",
			engineOpts: EngineOptions{},
			template:   "<ID> - <ACTORS>",
			ctx: &Context{
				ID:           "IPX-535",
				Actresses:    []string{"A1", "A2", "A3"},
				GroupActress: true,
			},
			want:    "IPX-535 - @Group",
			wantErr: false,
		},
		{
			name:       "All original tags still work without translations",
			engineOpts: EngineOptions{},
			template:   "<ID> - <CONTENTID> - <TITLE> - <ORIGINALTITLE> - <YEAR> - <RELEASEDATE> - <RUNTIME> - <DIRECTOR> - <MAKER> - <LABEL> - <SERIES> - <ACTORS> - <GENRES>",
			ctx: &Context{
				ID:            "IPX-535",
				ContentID:     "ipx00535",
				Title:         "Title",
				OriginalTitle: "OriginalTitle",
				ReleaseDate:   &releaseDate,
				Runtime:       120,
				Director:      "Director",
				Maker:         "Maker",
				Label:         "Label",
				Series:        "Series",
				Actresses:     []string{"A1"},
				Genres:        []string{"G1"},
			},
			want:    "IPX-535 - ipx00535 - Title - OriginalTitle - 2020 - 2020-09-13 - 120 - Director - Maker - Label - Series - A1 - G1",
			wantErr: false,
		},
		{
			name:       "TITLE with numeric preserves truncation over language",
			engineOpts: EngineOptions{DefaultLanguage: "en"},
			template:   "<TITLE:30>",
			ctx: &Context{
				Title: "Base Title That Is Very Long",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title That Is Very Long And Needs Truncation"},
				},
			},
			want:    "English Title That Is Very~",
			wantErr: false,
		},
		{
			name:       "Explicit language tag with numeric truncation - combined not supported",
			engineOpts: EngineOptions{DefaultLanguage: "en"},
			template:   "<TITLE:ja>",
			ctx: &Context{
				Title: "Base Title",
				Translations: map[string]models.MovieTranslation{
					"en": {Language: "en", Title: "English Title"},
					"ja": {Language: "ja", Title: "Japanese Title That Is Very Long"},
				},
			},
			want:    "Japanese Title That Is Very Long",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewEngineWithOptions(tt.engineOpts)
			got, err := engine.Execute(tt.template, tt.ctx)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}
