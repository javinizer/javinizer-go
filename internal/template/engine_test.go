package template

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/mediainfo"
)

func TestTemplateEngine_Execute(t *testing.T) {
	engine := NewEngine()
	releaseDate := time.Date(2020, 9, 13, 0, 0, 0, 0, time.UTC)

	ctx := &Context{
		ID:          "IPX-535",
		ContentID:   "ipx00535",
		Title:       "Test Movie Title",
		ReleaseDate: &releaseDate,
		Runtime:     120,
		Director:    "Test Director",
		Maker:       "Test Studio",
		Label:       "Test Label",
		Series:      "Test Series",
		Actresses:   []string{"Sakura Momo", "Test Actress"},
		Genres:      []string{"Genre1", "Genre2", "Genre3"},
	}

	tests := []struct {
		name     string
		template string
		want     string
	}{
		{
			name:     "Simple ID",
			template: "<ID>",
			want:     "IPX-535",
		},
		{
			name:     "ID with title",
			template: "<ID> - <TITLE>",
			want:     "IPX-535 - Test Movie Title",
		},
		{
			name:     "Complex format",
			template: "<ID> [<STUDIO>] - <TITLE> (<YEAR>)",
			want:     "IPX-535 [Test Studio] - Test Movie Title (2020)",
		},
		{
			name:     "With runtime",
			template: "<ID> - <TITLE> (<RUNTIME>min)",
			want:     "IPX-535 - Test Movie Title (120min)",
		},
		{
			name:     "Truncated title",
			template: "<ID> - <TITLE:20>",
			want:     "IPX-535 - Test Movie Title",
		},
		{
			name:     "Truncated long title",
			template: "<TITLE:10>",
			want:     "Test~",
		},
		{
			name:     "Date format default",
			template: "<ID> (<RELEASEDATE>)",
			want:     "IPX-535 (2020-09-13)",
		},
		{
			name:     "Date format custom",
			template: "<ID> (<RELEASEDATE:YYYY-MM>)",
			want:     "IPX-535 (2020-09)",
		},
		{
			name:     "Actresses with default delimiter",
			template: "<ID> - <ACTORS>",
			want:     "IPX-535 - Sakura Momo, Test Actress",
		},
		{
			name:     "Actresses with custom delimiter",
			template: "<ID> - <ACTORS: and >",
			want:     "IPX-535 - Sakura Momo and Test Actress",
		},
		{
			name:     "Genres",
			template: "<ID> (<GENRES>)",
			want:     "IPX-535 (Genre1, Genre2, Genre3)",
		},
		{
			name:     "Director and label",
			template: "<DIRECTOR> - <LABEL>",
			want:     "Test Director - Test Label",
		},
		{
			name:     "Multiple tags",
			template: "<YEAR>/<STUDIO>/<ID> - <TITLE>",
			want:     "2020/Test Studio/IPX-535 - Test Movie Title",
		},
		{
			name:     "Actor name tag",
			template: "actress-<ACTORNAME>.jpg",
			want:     "actress-Test Movie Title.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := engine.Execute(tt.template, ctx)
			if err != nil {
				t.Errorf("Execute() error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("Execute() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSanitizeFilename(t *testing.T) {

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "Forward slash",
			input: "Test/Movie",
			want:  "Test-Movie",
		},
		{
			name:  "Backslash",
			input: "Test\\Movie",
			want:  "Test-Movie",
		},
		{
			name:  "Colon",
			input: "Test: Movie",
			want:  "Test - Movie",
		},
		{
			name:  "Question mark",
			input: "Test? Movie",
			want:  "Test Movie",
		},
		{
			name:  "Asterisk",
			input: "Test* Movie",
			want:  "Test Movie",
		},
		{
			name:  "Quotes",
			input: `Test "Movie"`,
			want:  "Test 'Movie'",
		},
		{
			name:  "Angle brackets",
			input: "Test<Movie>",
			want:  "Test(Movie)",
		},
		{
			name:  "Pipe",
			input: "Test|Movie",
			want:  "Test-Movie",
		},
		{
			name:  "Multiple spaces",
			input: "Test    Movie",
			want:  "Test Movie",
		},
		{
			name:  "Trailing spaces",
			input: "Test Movie  ",
			want:  "Test Movie",
		},
		{
			name:  "Trailing dots",
			input: "Test Movie...",
			want:  "Test Movie",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeFilename(tt.input)
			if got != tt.want {
				t.Errorf("SanitizeFilename() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTemplateEngine_IndexFormatting(t *testing.T) {
	engine := NewEngine()

	tests := []struct {
		name     string
		template string
		index    int
		want     string
	}{
		{
			name:     "No padding",
			template: "fanart<INDEX>.jpg",
			index:    5,
			want:     "fanart5.jpg",
		},
		{
			name:     "Padding 2",
			template: "fanart<INDEX:2>.jpg",
			index:    5,
			want:     "fanart05.jpg",
		},
		{
			name:     "Padding 3",
			template: "fanart<INDEX:3>.jpg",
			index:    5,
			want:     "fanart005.jpg",
		},
		{
			name:     "Padding 2 with 10",
			template: "fanart<INDEX:2>.jpg",
			index:    10,
			want:     "fanart10.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &Context{Index: tt.index}
			got, err := engine.Execute(tt.template, ctx)
			if err != nil {
				t.Errorf("Execute() error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("Execute() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTemplateEngine_TruncateTitle(t *testing.T) {
	engine := NewEngine()

	tests := []struct {
		name   string
		title  string
		maxLen int
		want   string
	}{
		{
			name:   "Short title - no truncation",
			title:  "Test Movie",
			maxLen: 50,
			want:   "Test Movie",
		},
		{
			name:   "Title exactly at limit - no truncation",
			title:  "Test Movie Title",
			maxLen: 16,
			want:   "Test Movie Title",
		},
		{
			name:   "English title - smart word boundary truncation",
			title:  "The Quick Brown Fox Jumps Over The Lazy Dog",
			maxLen: 40,
			want:   "The Quick Brown Fox Jumps Over The~",
		},
		{
			name:   "English title - no word boundary found",
			title:  "Supercalifragilisticexpialidocious",
			maxLen: 20,
			want:   "Supercalifragilis~",
		},
		{
			name:   "English title - very short limit",
			title:  "Test Movie Title",
			maxLen: 5,
			want:   "Te~",
		},
		{
			name:   "English title - limit less than 3",
			title:  "Test Movie Title",
			maxLen: 2,
			want:   "Te",
		},
		{
			name:   "Japanese title - CJK character truncation",
			title:  "これは非常に長い日本語のタイトルですが適切に切り詰められるべきです",
			maxLen: 17,
			want:   "これは非常に長い日本語のタイ~",
		},
		{
			name:   "Japanese title - exact character count",
			title:  "これは日本語です",
			maxLen: 8,
			want:   "これは日本~",
		},
		{
			name:   "Mixed title - CJK detection with English",
			title:  "Japanese Title 日本語タイトルとEnglish",
			maxLen: 22,
			want:   "Japanese Title 日本語タ~",
		},
		{
			name:   "Empty title",
			title:  "",
			maxLen: 50,
			want:   "",
		},
		{
			name:   "Zero max length - no truncation",
			title:  "Test Movie Title",
			maxLen: 0,
			want:   "Test Movie Title",
		},
		{
			name:   "Negative max length - no truncation",
			title:  "Test Movie Title",
			maxLen: -10,
			want:   "Test Movie Title",
		},
		{
			name:   "CJK title with maxLen <= 3 and title fits",
			title:  "日本",
			maxLen: 3,
			want:   "日本",
		},
		{
			name:   "CJK title with maxLen <= 3 needs truncation",
			title:  "日本語タイトル",
			maxLen: 2,
			want:   "日本語タイトル",
		},
		{
			name:   "English title with maxLen > 3 but no truncation needed",
			title:  "Short",
			maxLen: 10,
			want:   "Short",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.TruncateTitle(tt.title, tt.maxLen)
			if got != tt.want {
				t.Errorf("TruncateTitle() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTemplateEngine_ValidatePathLength(t *testing.T) {
	engine := NewEngine()

	tests := []struct {
		name    string
		path    string
		maxLen  int
		wantErr bool
	}{
		{
			name:    "Short path - valid",
			path:    "/Videos/IPX-535 [Studio] - Title (2020)/IPX-535.mp4",
			maxLen:  260,
			wantErr: false,
		},
		{
			name:    "Path exactly at limit - valid",
			path:    "/Videos/" + string(make([]rune, 230)),
			maxLen:  240,
			wantErr: false,
		},
		{
			name:    "Long path - invalid",
			path:    "/Videos/" + string(make([]rune, 250)),
			maxLen:  240,
			wantErr: true,
		},
		{
			name:    "Zero max length - no validation",
			path:    "/Videos/very/long/path/that/exceeds/limit/MP4-535 [Studio] - Title (2020)/MP4-535.mp4",
			maxLen:  0,
			wantErr: false,
		},
		{
			name:    "Negative max length - no validation",
			path:    "/Videos/very/long/path/that/exceeds/limit/MP4-535 [Studio] - Title (2020)/MP4-535.mp4",
			maxLen:  -10,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := engine.ValidatePathLength(tt.path, tt.maxLen)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePathLength() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestTemplateEngine_ContainsCJK(t *testing.T) {
	engine := NewEngine()

	tests := []struct {
		name string
		s    string
		want bool
	}{
		{
			name: "English only - no CJK",
			s:    "The Quick Brown Fox",
			want: false,
		},
		{
			name: "Japanese Hiragana",
			s:    "これはひらがなです",
			want: true,
		},
		{
			name: "Japanese Katakana",
			s:    "これはカタカナです",
			want: true,
		},
		{
			name: "Chinese characters",
			s:    "这是中文字符",
			want: true,
		},
		{
			name: "Korean characters",
			s:    "한국어 문자",
			want: true,
		},
		{
			name: "Mixed English and Japanese",
			s:    "Japanese Title 日本語タイトル",
			want: true,
		},
		{
			name: "Empty string",
			s:    "",
			want: false,
		},
		{
			name: "Numbers and symbols only",
			s:    "IPX-535 [Studio] - Title (2020)",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.containsCJK(tt.s)
			if got != tt.want {
				t.Errorf("containsCJK() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTemplateEngine_Conditionals(t *testing.T) {
	engine := NewEngine()
	releaseDate := time.Date(2020, 9, 13, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		template string
		ctx      *Context
		want     string
	}{
		{
			name:     "Simple conditional with value",
			template: "<ID><IF:SERIES> - <SERIES></IF>",
			ctx: &Context{
				ID:     "IPX-535",
				Series: "Test Series",
			},
			want: "IPX-535 - Test Series",
		},
		{
			name:     "Simple conditional without value",
			template: "<ID><IF:SERIES> - <SERIES></IF>",
			ctx: &Context{
				ID:     "IPX-535",
				Series: "",
			},
			want: "IPX-535",
		},
		{
			name:     "Conditional with ELSE - true branch",
			template: "<ID><IF:DIRECTOR> by <DIRECTOR><ELSE> (No Director)</IF>",
			ctx: &Context{
				ID:       "IPX-535",
				Director: "Test Director",
			},
			want: "IPX-535 by Test Director",
		},
		{
			name:     "Conditional with ELSE - false branch",
			template: "<ID><IF:DIRECTOR> by <DIRECTOR><ELSE> (No Director)</IF>",
			ctx: &Context{
				ID:       "IPX-535",
				Director: "",
			},
			want: "IPX-535 (No Director)",
		},
		{
			name:     "Multiple conditionals",
			template: "<ID><IF:SERIES> - <SERIES></IF><IF:LABEL> [<LABEL>]</IF>",
			ctx: &Context{
				ID:     "IPX-535",
				Series: "Test Series",
				Label:  "Test Label",
			},
			want: "IPX-535 - Test Series [Test Label]",
		},
		{
			name:     "Multiple conditionals - partial values",
			template: "<ID><IF:SERIES> - <SERIES></IF><IF:LABEL> [<LABEL>]</IF>",
			ctx: &Context{
				ID:     "IPX-535",
				Series: "Test Series",
				Label:  "",
			},
			want: "IPX-535 - Test Series",
		},
		{
			name:     "Conditional with year",
			template: "<ID><IF:YEAR> (<YEAR>)</IF>",
			ctx: &Context{
				ID:          "IPX-535",
				ReleaseDate: &releaseDate,
			},
			want: "IPX-535 (2020)",
		},
		{
			name:     "Conditional with year - no date",
			template: "<ID><IF:YEAR> (<YEAR>)</IF>",
			ctx: &Context{
				ID:          "IPX-535",
				ReleaseDate: nil,
			},
			want: "IPX-535",
		},
		{
			name:     "Complex conditional with multiple tags",
			template: "<IF:DIRECTOR>Director: <DIRECTOR> | Studio: <STUDIO><ELSE>Studio: <STUDIO></IF>",
			ctx: &Context{
				Director: "John Doe",
				Maker:    "Test Studio",
			},
			want: "Director: John Doe | Studio: Test Studio",
		},
		{
			name:     "Complex conditional - false branch",
			template: "<IF:DIRECTOR>Director: <DIRECTOR> | Studio: <STUDIO><ELSE>Studio: <STUDIO></IF>",
			ctx: &Context{
				Director: "",
				Maker:    "Test Studio",
			},
			want: "Studio: Test Studio",
		},
		{
			name:     "Array conditional - actresses",
			template: "<ID><IF:ACTRESSES> starring <ACTRESSES></IF>",
			ctx: &Context{
				ID:        "IPX-535",
				Actresses: []string{"Actress 1", "Actress 2"},
			},
			want: "IPX-535 starring Actress 1, Actress 2",
		},
		{
			name:     "Array conditional - empty",
			template: "<ID><IF:ACTRESSES> starring <ACTRESSES></IF>",
			ctx: &Context{
				ID:        "IPX-535",
				Actresses: []string{},
			},
			want: "IPX-535",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := engine.Execute(tt.template, tt.ctx)
			if err != nil {
				t.Errorf("Execute() error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("Execute() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTemplateEngine_GroupActress(t *testing.T) {
	engine := NewEngine()

	tests := []struct {
		name         string
		actresses    []string
		groupActress bool
		template     string
		want         string
	}{
		{
			name:         "Multiple actresses with GroupActress enabled",
			actresses:    []string{"Actress One", "Actress Two", "Actress Three"},
			groupActress: true,
			template:     "<ID> - <ACTORS>",
			want:         "IPX-535 - @Group",
		},
		{
			name:         "Multiple actresses with GroupActress disabled",
			actresses:    []string{"Actress One", "Actress Two", "Actress Three"},
			groupActress: false,
			template:     "<ID> - <ACTORS>",
			want:         "IPX-535 - Actress One, Actress Two, Actress Three",
		},
		{
			name:         "Single actress with GroupActress enabled (should not group)",
			actresses:    []string{"Single Actress"},
			groupActress: true,
			template:     "<ID> - <ACTORS>",
			want:         "IPX-535 - Single Actress",
		},
		{
			name:         "Single actress with GroupActress disabled",
			actresses:    []string{"Single Actress"},
			groupActress: false,
			template:     "<ID> - <ACTORS>",
			want:         "IPX-535 - Single Actress",
		},
		{
			name:         "No actresses with GroupActress enabled",
			actresses:    []string{},
			groupActress: true,
			template:     "<ID> - <ACTORS>",
			want:         "IPX-535 - ",
		},
		{
			name:         "Two actresses with GroupActress enabled",
			actresses:    []string{"Actress One", "Actress Two"},
			groupActress: true,
			template:     "<ID> - <ACTORS>",
			want:         "IPX-535 - @Group",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &Context{
				ID:           "IPX-535",
				Actresses:    tt.actresses,
				GroupActress: tt.groupActress,
			}

			got, err := engine.Execute(tt.template, ctx)
			if err != nil {
				t.Errorf("Execute() error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("Execute() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTemplateEngine_ResolutionTag(t *testing.T) {
	engine := NewEngine()

	t.Run("With cached mediainfo", func(t *testing.T) {
		ctx := &Context{
			ID: "TEST-001",
		}

		// Import mediainfo package for the test
		ctx.cachedMediaInfo = &mediainfo.VideoInfo{
			Height: 1080,
		}

		result, err := engine.Execute("<ID> - <RESOLUTION>", ctx)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}

		want := "TEST-001 - 1080p"
		if result != want {
			t.Errorf("Execute() = %q, want %q", result, want)
		}
	})

	t.Run("Without video file path", func(t *testing.T) {
		ctx := &Context{
			ID: "TEST-001",
		}

		result, err := engine.Execute("<ID> - <RESOLUTION>", ctx)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}

		want := "TEST-001 - "
		if result != want {
			t.Errorf("Execute() = %q, want %q", result, want)
		}
	})
}

func TestTemplateEngine_NilContext(t *testing.T) {
	engine := NewEngine()

	result, err := engine.Execute("<ID> - <TITLE>", nil)
	if err == nil {
		t.Error("Expected error with nil context, got nil")
	}
	if result != "" {
		t.Errorf("Expected empty result with nil context, got %q", result)
	}
	if err.Error() != "context cannot be nil" {
		t.Errorf("Expected 'context cannot be nil' error, got %q", err.Error())
	}
}

func TestTemplateEngine_UnknownTag(t *testing.T) {
	engine := NewEngine()
	ctx := &Context{ID: "TEST-001"}

	tests := []struct {
		name     string
		template string
		want     string
	}{
		{
			name:     "Unknown tag - replaced with empty",
			template: "<ID> - <UNKNOWNTAG>",
			want:     "TEST-001 - ",
		},
		{
			name:     "Unknown tag with modifier",
			template: "<ID> - <UNKNOWNTAG:modifier>",
			want:     "TEST-001 - ",
		},
		{
			name:     "Multiple unknown tags - not replaced",
			template: "<UNKNOWN1> <UNKNOWN2> <ID>",
			want:     "<UNKNOWN1> <UNKNOWN2> TEST-001",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := engine.Execute(tt.template, ctx)
			if err != nil {
				t.Errorf("Execute() error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("Execute() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTemplateEngine_EmptyModifiers(t *testing.T) {
	engine := NewEngine()
	ctx := &Context{
		ID:    "TEST-001",
		Title: "Test Movie Title",
		Index: 5,
	}

	tests := []struct {
		name     string
		template string
		want     string
	}{
		{
			name:     "Title with empty modifier - not processed",
			template: "<TITLE:>",
			want:     "<TITLE:>",
		},
		{
			name:     "Index with empty modifier - not processed",
			template: "fanart<INDEX:>.jpg",
			want:     "fanart<INDEX:>.jpg",
		},
		{
			name:     "Title with invalid modifier",
			template: "<TITLE:abc>",
			want:     "Test Movie Title",
		},
		{
			name:     "Index with invalid modifier - outputs format string",
			template: "fanart<INDEX:xyz>.jpg",
			want:     "fanart5yzd.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := engine.Execute(tt.template, ctx)
			if err != nil {
				t.Errorf("Execute() error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("Execute() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTemplateEngine_AllTags(t *testing.T) {
	engine := NewEngine()
	releaseDate := time.Date(2020, 9, 13, 0, 0, 0, 0, time.UTC)

	ctx := &Context{
		ID:               "IPX-535",
		ContentID:        "ipx00535",
		Title:            "Test Movie Title",
		OriginalTitle:    "テストムービータイトル",
		ReleaseDate:      &releaseDate,
		Runtime:          120,
		Director:         "Test Director",
		Maker:            "Test Studio",
		Label:            "Test Label",
		Series:           "Test Series",
		Actresses:        []string{"Actress One", "Actress Two"},
		Genres:           []string{"Genre1", "Genre2"},
		OriginalFilename: "original_file.mp4",
		FirstName:        "First",
		LastName:         "Last",
		Index:            3,
	}

	tests := []struct {
		name     string
		template string
		want     string
	}{
		{
			name:     "CONTENTID tag",
			template: "<CONTENTID>",
			want:     "ipx00535",
		},
		{
			name:     "ORIGINALTITLE tag",
			template: "<ORIGINALTITLE>",
			want:     "テストムービータイトル",
		},
		{
			name:     "MAKER synonym for STUDIO",
			template: "<MAKER>",
			want:     "Test Studio",
		},
		{
			name:     "LABEL tag",
			template: "<LABEL>",
			want:     "Test Label",
		},
		{
			name:     "SERIES tag",
			template: "<SERIES>",
			want:     "Test Series",
		},
		{
			name:     "FILENAME tag",
			template: "<FILENAME>",
			want:     "original_file.mp4",
		},
		{
			name:     "FIRSTNAME tag",
			template: "<FIRSTNAME>",
			want:     "First",
		},
		{
			name:     "LASTNAME tag",
			template: "<LASTNAME>",
			want:     "Last",
		},
		{
			name:     "ACTRESSES synonym for ACTORS",
			template: "<ACTRESSES>",
			want:     "Actress One, Actress Two",
		},
		{
			name:     "GENRES with custom delimiter",
			template: "<GENRES: / >",
			want:     "Genre1 / Genre2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := engine.Execute(tt.template, ctx)
			if err != nil {
				t.Errorf("Execute() error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("Execute() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTemplateEngine_ZeroAndEmptyValues(t *testing.T) {
	engine := NewEngine()

	ctx := &Context{
		ID:      "TEST-001",
		Runtime: 0,
		Index:   0,
	}

	tests := []struct {
		name     string
		template string
		want     string
	}{
		{
			name:     "Zero runtime",
			template: "<ID> (<RUNTIME>min)",
			want:     "TEST-001 (min)",
		},
		{
			name:     "Zero index",
			template: "fanart<INDEX>.jpg",
			want:     "fanart.jpg",
		},
		{
			name:     "Empty actresses array",
			template: "<ID> - <ACTORS>",
			want:     "TEST-001 - ",
		},
		{
			name:     "Empty genres array",
			template: "<ID> (<GENRES>)",
			want:     "TEST-001 ()",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := engine.Execute(tt.template, ctx)
			if err != nil {
				t.Errorf("Execute() error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("Execute() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTemplateEngine_DateFormatEdgeCases(t *testing.T) {
	engine := NewEngine()

	tests := []struct {
		name     string
		template string
		date     *time.Time
		want     string
	}{
		{
			name:     "YYYY only",
			template: "<RELEASEDATE:YYYY>",
			date:     timePtr(2020, 9, 13),
			want:     "2020",
		},
		{
			name:     "YY only",
			template: "<RELEASEDATE:YY>",
			date:     timePtr(2020, 9, 13),
			want:     "20",
		},
		{
			name:     "MM only",
			template: "<RELEASEDATE:MM>",
			date:     timePtr(2020, 9, 13),
			want:     "09",
		},
		{
			name:     "DD only",
			template: "<RELEASEDATE:DD>",
			date:     timePtr(2020, 9, 13),
			want:     "13",
		},
		{
			name:     "Complex date format",
			template: "<RELEASEDATE:YYYY/MM/DD>",
			date:     timePtr(2020, 9, 13),
			want:     "2020/09/13",
		},
		{
			name:     "Date format with text",
			template: "<RELEASEDATE:Released YYYY-MM-DD>",
			date:     timePtr(2020, 9, 13),
			want:     "Released 2020-09-13",
		},
		{
			name:     "Nil date with format",
			template: "<RELEASEDATE:YYYY-MM-DD>",
			date:     nil,
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &Context{
				ReleaseDate: tt.date,
			}
			got, err := engine.Execute(tt.template, ctx)
			if err != nil {
				t.Errorf("Execute() error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("Execute() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTemplateEngine_TruncateEdgeCases(t *testing.T) {
	engine := NewEngine()

	tests := []struct {
		name     string
		title    string
		maxLen   int
		expected string
	}{
		{
			name:     "Truncate at exactly word boundary",
			title:    "The Quick Brown Fox",
			maxLen:   13,
			expected: "The Quick~",
		},
		{
			name:     "Single character limit",
			title:    "Test",
			maxLen:   1,
			expected: "T",
		},
		{
			name:     "Limit equals 3 (edge case)",
			title:    "Test Movie",
			maxLen:   3,
			expected: "Tes",
		},
		{
			name:     "Limit equals 4 (first truncation marker case)",
			title:    "Test Movie",
			maxLen:   4,
			expected: "T~",
		},
		{
			name:     "Japanese mixed with spaces",
			title:    "これは テスト です 映画",
			maxLen:   10,
			expected: "これは テスト~",
		},
		{
			name:     "Korean characters",
			title:    "한국어 제목입니다",
			maxLen:   8,
			expected: "한국어 제~",
		},
		{
			name:     "Chinese characters",
			title:    "这是一个很长的中文标题",
			maxLen:   10,
			expected: "这是一个很长的~",
		},
		{
			name:     "Title shorter than limit",
			title:    "Short",
			maxLen:   100,
			expected: "Short",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.TruncateTitle(tt.title, tt.maxLen)
			if got != tt.expected {
				t.Errorf("TruncateTitle() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestTemplateEngine_NestedConditionals(t *testing.T) {
	engine := NewEngine()

	tests := []struct {
		name     string
		template string
		ctx      *Context
		want     string
	}{
		{
			name:     "Conditional inside another template",
			template: "<ID><IF:SERIES> - <SERIES></IF> - <TITLE>",
			ctx: &Context{
				ID:     "IPX-001",
				Series: "",
				Title:  "Test Movie",
			},
			want: "IPX-001 - Test Movie",
		},
		{
			name:     "Multiple conditionals in sequence",
			template: "<IF:DIRECTOR><DIRECTOR> presents </IF><IF:SERIES><SERIES>: </IF><TITLE>",
			ctx: &Context{
				Director: "John Doe",
				Series:   "Test Series",
				Title:    "Test Movie",
			},
			want: "John Doe presents Test Series: Test Movie",
		},
		{
			name:     "Empty IF block",
			template: "<ID><IF:SERIES></IF>",
			ctx: &Context{
				ID:     "IPX-001",
				Series: "Test",
			},
			want: "IPX-001",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := engine.Execute(tt.template, tt.ctx)
			if err != nil {
				t.Errorf("Execute() error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("Execute() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTemplateEngine_CaseModifiers(t *testing.T) {
	engine := NewEngine()

	ctx := &Context{
		ID:        "IPX-535",
		ContentID: "ipx00535",
	}

	tests := []struct {
		name     string
		template string
		want     string
	}{
		{
			name:     "ID with UPPERCASE modifier",
			template: "<ID:UPPERCASE>",
			want:     "IPX-535",
		},
		{
			name:     "ID with LOWERCASE modifier",
			template: "<ID:LOWERCASE>",
			want:     "ipx-535",
		},
		{
			name:     "ID with UPPER shorthand",
			template: "<ID:UPPER>",
			want:     "IPX-535",
		},
		{
			name:     "ID with LOWER shorthand",
			template: "<ID:LOWER>",
			want:     "ipx-535",
		},
		{
			name:     "CONTENTID with UPPERCASE modifier",
			template: "<CONTENTID:UPPERCASE>",
			want:     "IPX00535",
		},
		{
			name:     "CONTENTID with LOWERCASE modifier",
			template: "<CONTENTID:LOWERCASE>",
			want:     "ipx00535",
		},
		{
			name:     "ID without modifier (default case)",
			template: "<ID>",
			want:     "IPX-535",
		},
		{
			name:     "CONTENTID without modifier (default case)",
			template: "<CONTENTID>",
			want:     "ipx00535",
		},
		{
			name:     "ID with unknown modifier (returns value as-is)",
			template: "<ID:UNKNOWN>",
			want:     "IPX-535",
		},
		{
			name:     "Case insensitive modifier - lowercase input",
			template: "<ID:lowercase>",
			want:     "ipx-535",
		},
		{
			name:     "Case insensitive modifier - mixed case input",
			template: "<ID:LoWeRcAsE>",
			want:     "ipx-535",
		},
		{
			name:     "Complex template with multiple case modifiers",
			template: "<ID:UPPERCASE> - <CONTENTID:LOWERCASE>",
			want:     "IPX-535 - ipx00535",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := engine.Execute(tt.template, ctx)
			if err != nil {
				t.Errorf("Execute() error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("Execute() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Helper function to create time pointers
func timePtr(year, month, day int) *time.Time {
	t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	return &t
}

func TestTemplateEngine_TruncateTitleBytes(t *testing.T) {
	engine := NewEngine()

	tests := []struct {
		name     string
		title    string
		maxBytes int
		expected string
		comment  string
	}{
		{
			name:     "Short ASCII title - no truncation",
			title:    "Test Movie",
			maxBytes: 50,
			expected: "Test Movie",
			comment:  "Title fits within byte limit",
		},
		{
			name:     "ASCII title - truncate at word boundary",
			title:    "The Quick Brown Fox Jumps",
			maxBytes: 20,
			expected: "The Quick Brown~",
			comment:  "Reserves 1 byte for truncation marker, breaks at word boundary",
		},
		{
			name:     "ASCII title - no word boundary",
			title:    "Supercalifragilisticexpialidocious",
			maxBytes: 15,
			expected: "Supercalifra~",
			comment:  "No spaces, truncates mid-word with marker within budget",
		},
		{
			name:     "ASCII title - very small limit with marker",
			title:    "Test Movie Title",
			maxBytes: 5,
			expected: "Te~",
			comment:  "Budget of 4 bytes for content + 1 for marker = 5 total",
		},
		{
			name:     "ASCII title - small limit still allows marker",
			title:    "Test Movie Title",
			maxBytes: 2,
			expected: "Te",
			comment:  "Only 2 bytes, no room for marker with legacy reserve",
		},
		{
			name:     "Japanese title - CJK bytes",
			title:    "これは日本語のタイトルです",
			maxBytes: 20,
			expected: "これは日本~",
			comment:  "Budget 19 bytes = 6 CJK chars (18 bytes) + marker (1 byte) = 19 bytes total",
		},
		{
			name:     "Japanese title - exact fit",
			title:    "これは",
			maxBytes: 9,
			expected: "これは",
			comment:  "Exactly 9 bytes (3 chars × 3 bytes)",
		},
		{
			name:     "Japanese title - one byte short",
			title:    "これは日",
			maxBytes: 11,
			expected: "これ~",
			comment:  "Budget 10 bytes = 3 CJK chars (9 bytes) + marker (1 byte) = 10 bytes total",
		},
		{
			name:     "Mixed CJK and ASCII",
			title:    "Movie Title 映画タイトル",
			maxBytes: 20,
			expected: "Movie Title 映~",
			comment:  "Budget 19 bytes: 'Movie Title ' (12) + 2 CJK (6) + marker (1) = 19 bytes",
		},
		{
			name:     "Zero byte limit - no truncation",
			title:    "Test Movie Title",
			maxBytes: 0,
			expected: "",
			comment:  "Zero maxBytes returns empty string",
		},
		{
			name:     "Negative byte limit - no truncation",
			title:    "Test Movie Title",
			maxBytes: -10,
			expected: "",
			comment:  "Negative maxBytes returns empty string",
		},
		{
			name:     "Empty title",
			title:    "",
			maxBytes: 10,
			expected: "",
			comment:  "Empty input returns empty",
		},
		{
			name:     "Title exactly at byte limit",
			title:    "Test",
			maxBytes: 4,
			expected: "Test",
			comment:  "Exactly 4 bytes, fits perfectly",
		},
		{
			name:     "Cannot fit even one rune",
			title:    "これは",
			maxBytes: 2,
			expected: "",
			comment:  "First CJK char needs 3 bytes, can't fit in 2",
		},
		{
			name:     "ASCII with marker edge case",
			title:    "Test Movie Title",
			maxBytes: 6,
			expected: "Tes~",
			comment:  "Budget 5 bytes for content + 1 for marker = 6 total",
		},
		{
			name:     "Korean characters",
			title:    "한국어 제목입니다",
			maxBytes: 15,
			expected: "한국어~",
			comment:  "Budget 14 bytes, fits '한국어 제' then appends marker",
		},
		{
			name:     "Chinese characters",
			title:    "这是一个很长的中文标题",
			maxBytes: 24,
			expected: "这是一个很长的~",
			comment:  "Budget 23 bytes = 7 CJK chars (21 bytes) + marker (1) = 22 total",
		},
		{
			name:     "ASCII title at exact word boundary",
			title:    "The Quick Brown",
			maxBytes: 12,
			expected: "The~",
			comment:  "Budget 11 bytes breaks at last space and appends marker",
		},
		{
			name:     "Title shorter than limit",
			title:    "Short",
			maxBytes: 100,
			expected: "Short",
			comment:  "Well under limit",
		},
		{
			name:     "Single ASCII character limit",
			title:    "Test",
			maxBytes: 1,
			expected: "T",
			comment:  "Only 1 byte available",
		},
		{
			name:     "Limit equals 3",
			title:    "Test Movie",
			maxBytes: 3,
			expected: "Tes",
			comment:  "No room for marker, just 3 chars",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.TruncateTitleBytes(tt.title, tt.maxBytes)
			if got != tt.expected {
				t.Errorf("TruncateTitleBytes() = %q (len=%d bytes), want %q (len=%d bytes)\n  Comment: %s",
					got, len(got), tt.expected, len(tt.expected), tt.comment)
			}

			// Verify result doesn't exceed maxBytes
			if tt.maxBytes > 0 && len(got) > tt.maxBytes {
				t.Errorf("Result exceeds maxBytes: got %d bytes, max allowed %d bytes", len(got), tt.maxBytes)
			}

			// Verify we don't split UTF-8 sequences (all results should be valid UTF-8)
			if !isValidUTF8(got) {
				t.Errorf("Result contains invalid UTF-8 sequences: %q", got)
			}
		})
	}
}

// Helper to check if a string is valid UTF-8
func isValidUTF8(s string) bool {
	// Try to convert to runes and back - invalid UTF-8 will be replaced with replacement char
	runes := []rune(s)
	return string(runes) == s || len(s) == 0
}

func TestValidatePathLength(t *testing.T) {
	engine := NewEngine()

	tests := []struct {
		name        string
		path        string
		maxLen      int
		expectError bool
	}{
		{
			name:        "path within limit",
			path:        "/short/path",
			maxLen:      100,
			expectError: false,
		},
		{
			name:        "path at limit",
			path:        "/exact/length",
			maxLen:      13,
			expectError: false,
		},
		{
			name:        "path exceeds limit",
			path:        "/this/is/a/very/long/path",
			maxLen:      10,
			expectError: true,
		},
		{
			name:        "maxLen is zero - no validation",
			path:        "/any/length/path/should/pass",
			maxLen:      0,
			expectError: false,
		},
		{
			name:        "maxLen is negative - no validation",
			path:        "/any/path",
			maxLen:      -1,
			expectError: false,
		},
		{
			name:        "empty path",
			path:        "",
			maxLen:      100,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := engine.ValidatePathLength(tt.path, tt.maxLen)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				}
			}
		})
	}
}

// BenchmarkTemplateRender_Simple measures the performance of rendering simple templates
// This benchmark is for observation only - not a pass/fail gate
// Expected baseline: ~1ms per operation for simple templates
func BenchmarkTemplateRender_Simple(b *testing.B) {
	// Setup: Create template engine
	engine := NewEngine()

	// Setup: Create test data
	releaseDate := time.Date(2020, 9, 13, 0, 0, 0, 0, time.UTC)
	ctx := &Context{
		ID:          "IPX-535",
		ContentID:   "ipx00535",
		Title:       "Test Movie Title",
		ReleaseDate: &releaseDate,
	}

	// Simple template
	template := "<ID> - <TITLE>"

	// Reset timer to exclude setup time
	b.ResetTimer()

	// Benchmark loop
	for i := 0; i < b.N; i++ {
		_, err := engine.Execute(template, ctx)
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
	}
}

// BenchmarkTemplateRender_Complex measures the performance of rendering complex templates with conditionals
// This benchmark is for observation only - not a pass/fail gate
// Expected baseline: <5ms per operation for complex templates
func BenchmarkTemplateRender_Complex(b *testing.B) {
	// Setup: Create template engine
	engine := NewEngine()

	// Setup: Create test data with all fields
	releaseDate := time.Date(2020, 9, 13, 0, 0, 0, 0, time.UTC)
	ctx := &Context{
		ID:          "IPX-535",
		ContentID:   "ipx00535",
		Title:       "Test Movie Title",
		ReleaseDate: &releaseDate,
		Runtime:     120,
		Director:    "Test Director",
		Maker:       "Test Studio",
		Label:       "Test Label",
		Series:      "Test Series",
		Actresses:   []string{"Sakura Momo", "Test Actress"},
		Genres:      []string{"Genre1", "Genre2", "Genre3"},
	}

	// Complex template with custom functions
	template := "<ID> [<STUDIO>] - <TITLE> (<YEAR>)"

	// Reset timer to exclude setup time
	b.ResetTimer()

	// Benchmark loop
	for i := 0; i < b.N; i++ {
		_, err := engine.Execute(template, ctx)
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
	}
}

// BenchmarkTemplateRender_Parallel measures concurrent template rendering performance
// This benchmark tests template cache thread-safety and contention under load
// This is an optional enhancement - run with -race flag to validate thread-safety
func BenchmarkTemplateRender_Parallel(b *testing.B) {
	// Setup: Create template engine
	engine := NewEngine()

	// Setup: Create test data
	releaseDate := time.Date(2020, 9, 13, 0, 0, 0, 0, time.UTC)
	ctx := &Context{
		ID:          "IPX-535",
		ContentID:   "ipx00535",
		Title:       "Test Movie Title",
		ReleaseDate: &releaseDate,
		Runtime:     120,
		Director:    "Test Director",
		Maker:       "Test Studio",
		Label:       "Test Label",
		Series:      "Test Series",
		Actresses:   []string{"Sakura Momo", "Test Actress"},
		Genres:      []string{"Genre1", "Genre2", "Genre3"},
	}

	// Template to test
	template := "<ID> [<STUDIO>] - <TITLE> (<YEAR>)"

	// Reset timer to exclude setup time
	b.ResetTimer()

	// Run parallel benchmark
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := engine.Execute(template, ctx)
			if err != nil {
				b.Fatalf("Execute failed: %v", err)
			}
		}
	})
}
