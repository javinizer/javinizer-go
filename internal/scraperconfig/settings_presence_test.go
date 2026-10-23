package scraperconfig

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestNumericPresenceDecodeAndRoundTrip(t *testing.T) {
	defaults := ScraperSettings{RateLimit: 500, Timeout: 30, RetryCount: 3}
	cases := []struct {
		name, json, yaml string
		want             [3]int
	}{
		{"omitted", `{}`, `{}`, [3]int{500, 30, 3}},
		{"zero", `{"rate_limit":0,"timeout":0,"retry_count":0}`, "rate_limit: 0\ntimeout: 0\nretry_count: 0", [3]int{}},
		{"nonzero", `{"rate_limit":100,"timeout":20,"retry_count":2}`, "rate_limit: 100\ntimeout: 20\nretry_count: 2", [3]int{100, 20, 2}},
		{"null inherits", `{"rate_limit":null,"timeout":null,"retry_count":null}`, "rate_limit: null\ntimeout: null\nretry_count: null", [3]int{500, 30, 3}},
		{"legacy zero", `{"request_delay":0,"max_retries":0}`, "request_delay: 0\nmax_retries: 0", [3]int{0, 30, 0}},
		{"legacy nonzero", `{"request_delay":100,"max_retries":2}`, "request_delay: 100\nmax_retries: 2", [3]int{100, 30, 2}},
		{"canonical zero wins", `{"rate_limit":0,"request_delay":100,"retry_count":0,"max_retries":2}`, "request_delay: 100\nrate_limit: 0\nmax_retries: 2\nretry_count: 0", [3]int{0, 30, 0}},
		{"canonical null wins", `{"rate_limit":null,"request_delay":100,"retry_count":null,"max_retries":2}`, "request_delay: 100\nrate_limit: null\nmax_retries: 2\nretry_count: null", [3]int{500, 30, 3}},
		{"legacy null inherits", `{"request_delay":null,"max_retries":null}`, "request_delay: null\nmax_retries: null", [3]int{500, 30, 3}},
	}
	codecs := []struct {
		name   string
		decode func([]byte, any) error
		encode func(any) ([]byte, error)
	}{
		{"json", json.Unmarshal, json.Marshal}, {"yaml", yaml.Unmarshal, yaml.Marshal},
	}
	for _, tc := range cases {
		for _, codec := range codecs {
			t.Run(tc.name+"/"+codec.name, func(t *testing.T) {
				input := tc.json
				if codec.name == "yaml" {
					input = tc.yaml
				}
				var original ScraperSettings
				require.NoError(t, codec.decode([]byte(input), &original))
				for _, roundtrip := range codecs {
					data, err := roundtrip.encode(&original)
					require.NoError(t, err)
					var decoded ScraperSettings
					require.NoError(t, roundtrip.decode(data, &decoded))
					clone := decoded.Clone()
					clone.MergeDefaultsFrom(defaults)
					clone.MergeDefaultsFrom(defaults)
					assert.Equal(t, tc.want, [3]int{clone.RateLimit, clone.Timeout, clone.RetryCount}, roundtrip.name)
				}
				original.MergeDefaultsFrom(defaults)
				assert.Equal(t, tc.want, [3]int{original.RateLimit, original.Timeout, original.RetryCount})
			})
		}
	}
}

func TestNumericPresenceProgrammaticLiterals(t *testing.T) {
	defaults := ScraperSettings{RateLimit: 500, Timeout: 30, RetryCount: 3}
	original := ScraperSettings{}
	original.MergeDefaultsFrom(defaults)
	assert.Equal(t, defaults, original)
	// Marshaling a literal must not manufacture explicit zero overrides.
	original = ScraperSettings{}
	data, err := json.Marshal(&original)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &original))
	original.MergeDefaultsFrom(defaults)
	assert.Equal(t, defaults, original)
}

func TestNumericPresenceInvalidInputDoesNotMutate(t *testing.T) {
	for _, input := range []string{`{"timeout":"bad"}`, `{"retry_count":0,"request_delay":"bad"}`, `{"max_retries":[]}`, `[]`} {
		original := ScraperSettings{Timeout: 20, RetryCount: 2}
		before := original.Clone()
		require.Error(t, json.Unmarshal([]byte(input), &original))
		assert.Equal(t, before, original)
	}
	for _, input := range []string{"timeout: bad", "request_delay: bad", "max_retries: []", "rate_limit: 0\nrate_limit: 2", "&loop {<<: *loop}", "<<: [2]"} {
		original := ScraperSettings{Timeout: 20, RetryCount: 2}
		before := original.Clone()
		require.Error(t, yaml.Unmarshal([]byte(input), &original))
		assert.Equal(t, before, original)
	}
}

func TestNumericPresenceYAMLMerge(t *testing.T) {
	var settings ScraperSettings
	require.NoError(t, yaml.Unmarshal([]byte("<<: &defaults {timeout: 0, request_delay: 0, max_retries: 0}\nrate_limit: 12"), &settings))
	settings.MergeDefaultsFrom(ScraperSettings{Timeout: 30, RateLimit: 500, RetryCount: 3})
	assert.Equal(t, [3]int{12, 0, 0}, [3]int{settings.RateLimit, settings.Timeout, settings.RetryCount})
}

func TestNumericPresencePartialDecodeAndNullReset(t *testing.T) {
	for _, codec := range []struct {
		name                string
		decode              func([]byte, any) error
		zero, partial, null string
	}{
		{"json", json.Unmarshal, `{"timeout":0,"rate_limit":0,"retry_count":0}`, `{"language":"en"}`, `{"timeout":null,"rate_limit":null,"retry_count":null}`},
		{"yaml", yaml.Unmarshal, "timeout: 0\nrate_limit: 0\nretry_count: 0", "language: en", "timeout: null\nrate_limit: null\nretry_count: null"},
	} {
		t.Run(codec.name, func(t *testing.T) {
			var settings ScraperSettings
			require.NoError(t, codec.decode([]byte(codec.zero), &settings))
			require.NoError(t, codec.decode([]byte(codec.partial), &settings))
			settings.MergeDefaultsFrom(ScraperSettings{Timeout: 30, RateLimit: 500, RetryCount: 3})
			assert.Equal(t, [3]int{}, [3]int{settings.RateLimit, settings.Timeout, settings.RetryCount})
			require.NoError(t, codec.decode([]byte(codec.null), &settings))
			settings.MergeDefaultsFrom(ScraperSettings{Timeout: 30, RateLimit: 500, RetryCount: 3})
			assert.Equal(t, [3]int{500, 30, 3}, [3]int{settings.RateLimit, settings.Timeout, settings.RetryCount})
		})
	}
}

func TestNumericPresenceJSONCaseAndDuplicateKeys(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  int
	}{
		{`{"RETRY_COUNT":0,"max_retries":2}`, 0},
		{`{"RETRY_COUNT":null,"max_retries":2}`, 3},
		{`{"retry_count":2,"retry_count":0}`, 0},
		{`{"retry_count":0,"retry_count":2}`, 2},
		{`{"retry_count":0,"retry_count":null}`, 3},
		{`{"retry_count":null,"retry_count":0}`, 0},
	} {
		t.Run(tc.input, func(t *testing.T) {
			var settings ScraperSettings
			require.NoError(t, json.Unmarshal([]byte(tc.input), &settings))
			settings.MergeDefaultsFrom(ScraperSettings{RetryCount: 3})
			assert.Equal(t, tc.want, settings.RetryCount)
		})
	}
	var nilSettings *ScraperSettings
	assert.False(t, nilSettings.HasExplicitRetryCount())
}

func TestNumericPresenceValueMarshal(t *testing.T) {
	for _, codec := range []struct {
		name   string
		encode func(any) ([]byte, error)
		decode func([]byte, any) error
	}{
		{"json", json.Marshal, json.Unmarshal}, {"yaml", yaml.Marshal, yaml.Unmarshal},
	} {
		t.Run(codec.name, func(t *testing.T) {
			for _, explicit := range []bool{false, true} {
				settings := ScraperSettings{}
				if explicit {
					require.NoError(t, json.Unmarshal([]byte(`{"timeout":0,"rate_limit":0,"retry_count":0}`), &settings))
				}
				data, err := codec.encode(map[string]ScraperSettings{"test": settings})
				require.NoError(t, err)
				var decoded map[string]ScraperSettings
				require.NoError(t, codec.decode(data, &decoded))
				got := decoded["test"]
				got.MergeDefaultsFrom(ScraperSettings{Timeout: 30, RateLimit: 500, RetryCount: 3})
				want := [3]int{500, 30, 3}
				if explicit {
					want = [3]int{}
				}
				assert.Equal(t, want, [3]int{got.RateLimit, got.Timeout, got.RetryCount})
			}
		})
	}
}
