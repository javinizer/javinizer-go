package localization

import (
	"os"
	"reflect"
	"runtime"
	"testing"

	"golang.org/x/text/language"
)

func TestNormalizeOne(t *testing.T) {
	testCases := []struct {
		name string
		raw  string
		want string
	}{
		{"plain two-letter", "ja", "ja"},
		{"underscore region", "ja_JP", "ja-JP"},
		{"codeset stripped", "ja_JP.UTF-8", "ja-JP"},
		{"codeset stripped lowercase", "en_us.utf-8", "en-us"},
		{"modifier stripped", "de_DE@euro", "de-DE"},
		{"codeset and modifier stripped", "de_DE.UTF-8@euro", "de-DE"},
		{"already bcp47", "pt-BR", "pt-BR"},
		{"empty", "", ""},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeOne(tc.raw); got != tc.want {
				t.Errorf("normalizeOne(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestParseLanguageEnv(t *testing.T) {
	testCases := []struct {
		name  string
		value string
		want  []string
	}{
		{"empty", "", nil},
		{"whitespace only", "   ", nil},
		{"single", "ja_JP", []string{"ja_JP"}},
		{"colon list", "ja:en_US:en", []string{"ja", "en_US", "en"}},
		{"trailing separator", "ja:en:", []string{"ja", "en"}},
		{"internal blanks skipped", "ja::en", []string{"ja", "en"}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseLanguageEnv(tc.value)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseLanguageEnv(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestNormalizeAndValidate(t *testing.T) {
	testCases := []struct {
		name string
		raw  []string
		want []string
	}{
		{
			name: "codeset stripped and underscore converted",
			raw:  []string{"ja_JP.UTF-8"},
			want: []string{"ja-JP"},
		},
		{
			name: "neutral C locale dropped",
			raw:  []string{"C", "en_US.UTF-8"},
			want: []string{"en-US"},
		},
		{
			name: "POSIX neutral dropped",
			raw:  []string{"POSIX", "de_DE"},
			want: []string{"de-DE"},
		},
		{
			name: "all neutral yields empty",
			raw:  []string{"C", "POSIX", "C.UTF-8"},
			want: []string{},
		},
		{
			name: "malformed tag dropped",
			raw:  []string{"ja_JP.UTF-8", "!!bad!!"},
			want: []string{"ja-JP"},
		},
		{
			name: "dedup preserves order",
			raw:  []string{"ja_JP.UTF-8", "ja_JP", "en_US.UTF-8"},
			want: []string{"ja-JP", "en-US"},
		},
		{
			name: "script tag preserved",
			raw:  []string{"zh_Hans"},
			want: []string{"zh-Hans"},
		},
		{
			name: "modifier stripped before validation",
			raw:  []string{"de_DE.UTF-8@euro"},
			want: []string{"de-DE"},
		},
		{
			name: "empty input",
			raw:  []string{},
			want: []string{},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeAndValidate(tc.raw)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("normalizeAndValidate(%v) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestDetectFromEnv(t *testing.T) {
	testCases := []struct {
		name    string
		env     map[string]string
		wantRaw []string
	}{
		{
			name:    "LANG set",
			env:     map[string]string{"LANG": "ja_JP.UTF-8"},
			wantRaw: []string{"ja_JP.UTF-8"},
		},
		{
			name: "LC_ALL overrides LANG",
			env: map[string]string{
				"LC_ALL": "de_DE.UTF-8",
				"LANG":   "ja_JP.UTF-8",
			},
			wantRaw: []string{"de_DE.UTF-8", "ja_JP.UTF-8"},
		},
		{
			name: "LC_MESSAGES used when LC_ALL empty",
			env: map[string]string{
				"LC_MESSAGES": "fr_FR.UTF-8",
				"LANG":        "en_US.UTF-8",
			},
			wantRaw: []string{"fr_FR.UTF-8", "en_US.UTF-8"},
		},
		{
			name: "LANGUAGE colon list takes precedence",
			env: map[string]string{
				"LANGUAGE": "ja:en_US:en",
				"LC_ALL":   "de_DE.UTF-8",
				"LANG":     "en_US.UTF-8",
			},
			wantRaw: []string{"ja", "en_US", "en", "de_DE.UTF-8", "en_US.UTF-8"},
		},
		{
			name:    "empty and missing vars yield nothing",
			env:     map[string]string{"LANG": ""},
			wantRaw: nil,
		},
		{
			name:    "no env set",
			env:     map[string]string{},
			wantRaw: nil,
		},
		{
			name: "whitespace only values ignored",
			env: map[string]string{
				"LC_ALL": "   ",
				"LANG":   "  ",
			},
			wantRaw: nil,
		},
		{
			name: "C and POSIX env values pass through raw (filtered later)",
			env: map[string]string{
				"LC_ALL": "C",
				"LANG":   "POSIX",
			},
			wantRaw: []string{"C", "POSIX"},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			getenv := func(key string) string { return tc.env[key] }
			got := detectFromEnv(getenv)
			if !reflect.DeepEqual(got, tc.wantRaw) {
				t.Errorf("detectFromEnv = %v, want %v", got, tc.wantRaw)
			}
		})
	}
}

func TestDetectOSLocale(t *testing.T) {
	testCases := []struct {
		name    string
		env     map[string]string
		want    []string
		wantAny bool
	}{
		{
			name: "LANG resolves to BCP 47",
			env:  map[string]string{"LANG": "ja_JP.UTF-8"},
			want: []string{"ja-JP"},
		},
		{
			name: "LC_ALL overrides LANG",
			env: map[string]string{
				"LC_ALL": "de_DE.UTF-8",
				"LANG":   "ja_JP.UTF-8",
			},
			want: []string{"de-DE", "ja-JP"},
		},
		{
			name: "LANGUAGE preference list first",
			env: map[string]string{
				"LANGUAGE": "ja:en_US",
				"LANG":     "en_US.UTF-8",
			},
			want: []string{"ja", "en-US"},
		},
		{
			name:    "C and POSIX fall back to English",
			env:     map[string]string{"LC_ALL": "C", "LANG": "POSIX"},
			want:    []string{"en"},
			wantAny: true,
		},
		{
			name:    "empty env falls back to English",
			env:     map[string]string{},
			want:    []string{"en"},
			wantAny: true,
		},
		{
			name:    "malformed tags dropped to English",
			env:     map[string]string{"LANG": "!!invalid!!"},
			want:    []string{"en"},
			wantAny: true,
		},
		{
			name: "script tag detected",
			env:  map[string]string{"LANG": "zh_Hans"},
			want: []string{"zh-Hans"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for k := range tc.env {
				t.Setenv(k, tc.env[k])
			}
			for _, k := range []string{"LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
				if _, set := tc.env[k]; !set {
					t.Setenv(k, "")
				}
			}
			got := DetectOSLocale()
			if tc.wantAny && runtime.GOOS == "windows" {
				if len(got) == 0 || got[0] == "" {
					t.Fatalf("DetectOSLocale = %v, want non-empty OS fallback on Windows", got)
				}
				if _, err := language.Parse(got[0]); err != nil {
					t.Fatalf("DetectOSLocale = %v, first tag %q not valid BCP 47: %v", got, got[0], err)
				}
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("DetectOSLocale = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDetectOSLocaleNeverPanics(t *testing.T) {
	for _, k := range []string{"LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
		t.Setenv(k, "")
	}
	got := DetectOSLocale()
	if len(got) == 0 {
		t.Fatal("DetectOSLocale returned empty; expected at least English fallback")
	}
	if got[0] == "" {
		t.Error("DetectOSLocale returned an empty first tag")
	}
	if runtime.GOOS == "windows" {
		if _, err := language.Parse(got[0]); err != nil {
			t.Errorf("DetectOSLocale first tag %q not valid BCP 47: %v", got[0], err)
		}
		return
	}
	if got[0] != "en" {
		t.Errorf("empty input = %q, want en", got[0])
	}
}

var _ = os.Getenv
