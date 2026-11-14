package config

import (
	"crypto/sha256"
	"encoding"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestConfigLockDelimiterContract(t *testing.T) {
	separators := []string{",", "\n", "\r", "\t", " "}
	for _, a := range separators {
		for _, b := range separators {
			for _, c := range separators {
				sep := a + b + c
				input := sep + "ignored" + sep + "pid=17" + sep + "time=123456789" + sep
				pid, created, ok := parseConfigLockMetadata(input)
				require.True(t, ok, "%q", input)
				require.Equal(t, 17, pid, "%q", input)
				require.Equal(t, int64(123456789), created, "%q", input)
				pid, created, ok = parseConfigLockMetadata(strings.Repeat(sep, 3))
				require.False(t, ok)
				require.Zero(t, pid)
				require.Zero(t, created)
			}
		}
	}
	for _, tc := range []struct {
		name, input string
		pid         int
		created     int64
		ok          bool
	}{
		{"last values win", "pid=1,time=2,pid=3,time=4", 3, 4, true},
		{"invalid duplicate rejects", "pid=1,time=2,pid=bad", 0, 0, false},
		{"invalid first rejects", "pid=bad,pid=1,time=2", 0, 0, false},
		{"missing pid", ",time=4,,", 0, 4, false},
		{"missing time", "\npid=3\n", 3, 0, false},
		{"empty pid", "pid=,time=4", 0, 0, false},
		{"extra equals", "pid=3=4,time=5", 0, 0, false},
		{"signed values", "pid=-3,time=-4", -3, -4, true},
		{"time overflow", "pid=3,time=9223372036854775808", 0, 0, false},
		{"unicode key and value trim", "\u00a0pid\u00a0=\u00a03\u00a0,time=4", 3, 4, true},
		{"unicode is not delimiter", "pid=3\u00a0time=4", 0, 0, false},
		{"invalid utf8 unknown field", "\xff,pid=3,time=4", 3, 4, true},
		{"unknown values ignored", "other=bad=2,pid=3,time=4", 3, 4, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pid, created, ok := parseConfigLockMetadata(tc.input)
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.pid, pid)
			require.Equal(t, tc.created, created)
		})
	}
}

func TestSettingsHashPrimitiveEncodingContract(t *testing.T) {
	marshalers := []reflect.Type{reflect.TypeFor[json.Marshaler](), reflect.TypeFor[encoding.TextMarshaler]()}
	var check func(reflect.Type)
	check = func(typ reflect.Type) {
		for _, iface := range marshalers {
			require.False(t, typ.Implements(iface), "%v implements %v", typ, iface)
			require.False(t, reflect.PointerTo(typ).Implements(iface), "*%v implements %v", typ, iface)
		}
		switch typ.Kind() {
		case reflect.String, reflect.Bool:
		case reflect.Struct:
			for i := 0; i < typ.NumField(); i++ {
				check(typ.Field(i).Type)
			}
		default:
			t.Fatalf("hash input contains unsupported contract type %v", typ)
		}
	}
	check(reflect.TypeFor[settingsHashInput]())
	_, err := json.Marshal(settingsHashInput{})
	require.NoError(t, err)
}

func TestSettingsHashPersistedVectors(t *testing.T) {
	var zero TranslationConfig
	require.Equal(t, "14ebbacaedd97a45", zero.SettingsHash())
	thinking := true
	base := TranslationConfig{
		SourceLanguage: "  JA\t", TargetLanguage: " EN\n", ApplyToPrimary: true, OverwriteExistingTarget: true,
		Fields:           TranslationFieldsConfig{Title: true, Description: true, Maker: true, Series: true, Actresses: true},
		OpenAI:           OpenAITranslationConfig{Model: "model<&>\u2028"},
		OpenAICompatible: OpenAICompatibleTranslationConfig{Model: "model<&>\u2028", EnableThinking: &thinking},
		Anthropic:        AnthropicTranslationConfig{Model: "model<&>\u2028"},
		DeepL:            DeepLTranslationConfig{Mode: models.DeepLMode("pro")},
		Google:           GoogleTranslationConfig{Mode: models.GoogleMode("paid")},
	}
	for _, tc := range []struct{ provider, want string }{
		{"openai", "91d57144915f50f0"},
		{"openai-compatible", "c75bac9e2cc74ada"},
		{"openai_compatible", "c965269be85ecdb0"},
		{"anthropic", "3398faf30abfee92"},
		{"deepl", "c375744c7becfd2c"},
		{"google", "6c09594a174a4604"},
		{"custom", "554a42905cb48508"},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			cfg := base
			cfg.Provider = tc.provider
			require.Equal(t, tc.want, cfg.SettingsHash())
			cfg.Enabled = true
			cfg.TimeoutSeconds = 999
			cfg.OpenAI.APIKey = "not-a-real-key"
			cfg.OpenAI.BaseURL = "https://unused.invalid"
			cfg.OpenAICompatible.APIKey = "not-a-real-key"
			cfg.OpenAICompatible.BaseURL = "https://unused.invalid"
			cfg.OpenAICompatible.BackendType = "other"
			cfg.Anthropic.APIKey = "not-a-real-key"
			cfg.Anthropic.BaseURL = "https://unused.invalid"
			cfg.DeepL.APIKey = "not-a-real-key"
			cfg.DeepL.BaseURL = "https://unused.invalid"
			cfg.Google.APIKey = "not-a-real-key"
			cfg.Google.BaseURL = "https://unused.invalid"
			require.Equal(t, tc.want, cfg.SettingsHash())
		})
	}
	invalid := base
	invalid.Provider = "openai"
	invalid.OpenAI.Model = "model\xff"
	encoded, err := json.Marshal(settingsHashInput{
		Provider: "openai", SourceLanguage: "ja", TargetLanguage: "en",
		ApplyToPrimary: true, OverwriteExistingTarget: true, Fields: base.Fields,
		OpenAIModel: "model\xff",
	})
	require.NoError(t, err)
	digest := sha256.Sum256(encoded)
	require.Equal(t, hex.EncodeToString(digest[:8]), invalid.SettingsHash())
}
