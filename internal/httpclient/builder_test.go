package httpclient

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScraperClientBuilder_Defaults(t *testing.T) {
	client, err := NewScraperClientBuilder().Build()

	require.NoError(t, err)
	require.NotNil(t, client)
	require.NotNil(t, client.Client)

	assert.Equal(t, DefaultRetryCount, client.Client.RetryCount)
	assert.Nil(t, client.FlareSolverr)
	assert.Nil(t, client.ProxyProfile)
}

func TestScraperClientBuilder_Options(t *testing.T) {
	tests := []struct {
		name          string
		opts          []ScraperOption
		wantRetry     int
		wantHeader    map[string]string
		wantFlare     bool
		wantProxyProf bool
	}{
		{
			name:      "retry count option",
			opts:      []ScraperOption{WithRetryCount(5)},
			wantRetry: 5,
		},
		{
			name: "headers option",
			opts: []ScraperOption{
				WithHeaders(map[string]string{
					"X-Custom": "value",
				}),
			},
			wantHeader: map[string]string{"X-Custom": "value"},
		},
		{
			name: "multiple headers",
			opts: []ScraperOption{
				WithHeader("X-One", "1"),
				WithHeader("X-Two", "2"),
				WithHeaders(map[string]string{"X-Three": "3"}),
			},
			wantHeader: map[string]string{
				"X-One":   "1",
				"X-Two":   "2",
				"X-Three": "3",
			},
		},
		{
			name:          "return proxy profile",
			opts:          []ScraperOption{WithProxyProfileReturn(true)},
			wantProxyProf: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewScraperClientBuilder().Apply(tt.opts...).Build()

			require.NoError(t, err)
			require.NotNil(t, client)

			if tt.wantRetry != 0 {
				assert.Equal(t, tt.wantRetry, client.Client.RetryCount)
			}
			if tt.wantHeader != nil {
				for k, v := range tt.wantHeader {
					assert.Equal(t, v, client.Client.Header.Get(k))
				}
			}
			if tt.wantProxyProf {
				assert.NotNil(t, client.ProxyProfile)
			}
		})
	}
}

func TestScraperClientBuilder_BuildClient(t *testing.T) {
	client, err := NewScraperClientBuilder().
		Apply(WithRetryCount(7)).
		BuildClient()

	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, 7, client.RetryCount)
}

func TestScraperClientBuilder_BuildWithFlareSolverr(t *testing.T) {
	t.Run("disabled FlareSolverr", func(t *testing.T) {
		client, fs, err := NewScraperClientBuilder().
			Apply(WithFlareSolverr(true)).
			BuildWithFlareSolverr()

		require.NoError(t, err)
		require.NotNil(t, client)
		assert.Nil(t, fs)
	})

	t.Run("enabled FlareSolverr", func(t *testing.T) {
		fsCfg := config.FlareSolverrConfig{
			Enabled:    true,
			URL:        "http://localhost:8191/v1",
			Timeout:    30,
			MaxRetries: 3,
		}

		client, fs, err := NewScraperClientBuilder().
			Apply(
				WithFlareSolverr(true),
				WithGlobalFlareSolverr(fsCfg),
			).
			BuildWithFlareSolverr()

		require.NoError(t, err)
		require.NotNil(t, client)
		_ = fs
	})

	t.Run("direct proxy mode does not leak global proxy to FlareSolverr", func(t *testing.T) {
		globalProxy := config.ProxyConfig{
			Enabled: true,
			Profiles: map[string]config.ProxyProfile{
				"default": {URL: "http://proxy.example.com:8080"},
			},
			DefaultProfile: "default",
		}
		scraperDirectProxy := &config.ProxyConfig{
			Enabled: false,
		}
		fsCfg := config.FlareSolverrConfig{
			Enabled:    true,
			URL:        "http://localhost:8191/v1",
			Timeout:    30,
			MaxRetries: 3,
		}

		sc, err := FromScraperSettings(&config.ScraperSettings{
			UseFlareSolverr: true,
			Proxy:           scraperDirectProxy,
		}, &globalProxy, fsCfg).Build()

		require.NoError(t, err)
		require.NotNil(t, sc)
		require.NotNil(t, sc.FlareSolverr)
		assert.Nil(t, sc.FlareSolverr.requestProxy,
			"FlareSolverr requestProxy should be nil when scraper uses direct proxy mode")
	})

	t.Run("inherit proxy mode passes global proxy to FlareSolverr", func(t *testing.T) {
		globalProxy := config.ProxyConfig{
			Enabled: true,
			Profiles: map[string]config.ProxyProfile{
				"default": {URL: "http://proxy.example.com:8080"},
			},
			DefaultProfile: "default",
		}
		fsCfg := config.FlareSolverrConfig{
			Enabled:    true,
			URL:        "http://localhost:8191/v1",
			Timeout:    30,
			MaxRetries: 3,
		}

		sc, err := FromScraperSettings(&config.ScraperSettings{
			UseFlareSolverr: true,
		}, &globalProxy, fsCfg).Build()

		require.NoError(t, err)
		require.NotNil(t, sc)
		require.NotNil(t, sc.FlareSolverr)
		require.NotNil(t, sc.FlareSolverr.requestProxy,
			"FlareSolverr requestProxy should be set when scraper inherits global proxy")
		assert.Equal(t, "http://proxy.example.com:8080", sc.FlareSolverr.requestProxy.URL)
	})
}

func TestScraperClientBuilder_BuildWithProxy(t *testing.T) {
	t.Run("no proxy configured", func(t *testing.T) {
		client, proxy, err := NewScraperClientBuilder().
			BuildWithProxy()

		require.NoError(t, err)
		require.NotNil(t, client)
		require.NotNil(t, proxy)
		assert.Empty(t, proxy.URL)
	})

	t.Run("proxy configured", func(t *testing.T) {
		globalProxy := config.ProxyConfig{
			Enabled: true,
			Profiles: map[string]config.ProxyProfile{
				"default": {URL: "http://proxy.example.com:8080"},
			},
			DefaultProfile: "default",
		}

		client, proxy, err := NewScraperClientBuilder().
			Apply(WithGlobalProxy(globalProxy)).
			BuildWithProxy()

		require.NoError(t, err)
		require.NotNil(t, client)
		require.NotNil(t, proxy)
		assert.Equal(t, "http://proxy.example.com:8080", proxy.URL)
	})
}

func TestScraperClientBuilder_Cookies(t *testing.T) {
	cookies := map[string]string{
		"session": "abc123",
		"token":   "xyz789",
	}

	client, err := NewScraperClientBuilder().
		Apply(WithCookies(cookies)).
		Build()

	require.NoError(t, err)
	require.NotNil(t, client)

	cookieHeader := client.Client.Header.Get("Cookie")
	assert.Contains(t, cookieHeader, "session=abc123")
	assert.Contains(t, cookieHeader, "token=xyz789")
}

func TestScraperClientBuilder_FromScraperSettings(t *testing.T) {
	settings := &config.ScraperSettings{
		Timeout:    45,
		RetryCount: 5,
		Cookies: map[string]string{
			"auth": "test",
		},
	}

	globalProxy := &config.ProxyConfig{
		Enabled: true,
		Profiles: map[string]config.ProxyProfile{
			"default": {URL: "http://proxy.test:8080"},
		},
		DefaultProfile: "default",
	}

	fsCfg := config.FlareSolverrConfig{
		Enabled: false,
	}

	client, err := FromScraperSettings(settings, globalProxy, fsCfg).
		Apply(WithProxyProfileReturn(true)).
		Build()

	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, 5, client.Client.RetryCount)
	assert.NotNil(t, client.ProxyProfile)

	cookieHeader := client.Client.Header.Get("Cookie")
	assert.Contains(t, cookieHeader, "auth=test")
}

func TestScraperClientBuilder_DefaultConfig(t *testing.T) {
	cfg := defaultConfig()

	assert.Equal(t, DefaultTimeout, cfg.timeout)
	assert.Equal(t, DefaultRetryCount, cfg.retryCount)
	assert.False(t, cfg.flareSolverr)
	assert.False(t, cfg.returnProxyProfile)
	assert.NotNil(t, cfg.headers)
	assert.NotNil(t, cfg.cookies)
}

func TestScraperClientBuilder_NilScraperSettings(t *testing.T) {
	client, err := FromScraperSettings(nil, nil, config.FlareSolverrConfig{}).
		Build()

	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, DefaultRetryCount, client.Client.RetryCount)
}

func TestScraperClientBuilder_ZeroValuesUseDefaults(t *testing.T) {
	client, err := NewScraperClientBuilder().
		Apply(
			WithTimeout(0),
			WithRetryCount(0),
		).
		Build()

	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, DefaultRetryCount, client.Client.RetryCount)
}

func TestHeaderPresets(t *testing.T) {
	t.Run("StandardHTMLHeaders", func(t *testing.T) {
		headers := StandardHTMLHeaders()
		assert.Contains(t, headers, "Accept")
		assert.Contains(t, headers, "Accept-Language")
		assert.Contains(t, headers, "Connection")
	})

	t.Run("JSONAPIHeaders", func(t *testing.T) {
		headers := JSONAPIHeaders()
		assert.Contains(t, headers, "Accept")
		assert.Equal(t, "application/json, text/plain, */*", headers["Accept"])
	})

	t.Run("RefererHeader", func(t *testing.T) {
		headers := RefererHeader("https://example.com/page")
		assert.Equal(t, "https://example.com/page", headers["Referer"])
	})

	t.Run("JapaneseLanguageHeaders", func(t *testing.T) {
		headers := JapaneseLanguageHeaders()
		assert.Contains(t, headers["Accept-Language"], "ja")
	})

	t.Run("DMMHeaders", func(t *testing.T) {
		headers := DMMHeaders()
		assert.Contains(t, headers["Cookie"], "age_check_done=1")
		assert.Contains(t, headers["Cookie"], "cklg=ja")
	})

	t.Run("R18DevHeaders", func(t *testing.T) {
		headers := R18DevHeaders()
		assert.Equal(t, "application/json, text/plain, */*", headers["Accept"])
	})

	t.Run("UserAgentHeader", func(t *testing.T) {
		headers := UserAgentHeader("")
		assert.NotEmpty(t, headers["User-Agent"])

		headers = UserAgentHeader("CustomAgent/1.0")
		assert.Equal(t, "CustomAgent/1.0", headers["User-Agent"])
	})

	t.Run("CombineHeaders", func(t *testing.T) {
		combined := CombineHeaders(
			StandardHTMLHeaders(),
			RefererHeader("https://example.com"),
			map[string]string{"X-Custom": "value"},
		)

		assert.Contains(t, combined, "Accept")
		assert.Contains(t, combined, "Referer")
		assert.Contains(t, combined, "X-Custom")
	})

	t.Run("MergeCookieHeader", func(t *testing.T) {
		existing := map[string]string{"a": "1", "b": "2"}
		new := map[string]string{"b": "3", "c": "4"}

		merged := MergeCookieHeader(existing, new)

		assert.Contains(t, merged, "a=1")
		assert.Contains(t, merged, "b=3")
		assert.Contains(t, merged, "c=4")
	})
}

func TestScraperClientBuilder_ApplyChain(t *testing.T) {
	builder := NewScraperClientBuilder()

	client, err := builder.
		Apply(WithRetryCount(2)).
		Apply(WithHeader("X-First", "1")).
		Apply(WithHeader("X-Second", "2")).
		Build()

	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, 2, client.Client.RetryCount)
	assert.Equal(t, "1", client.Client.Header.Get("X-First"))
	assert.Equal(t, "2", client.Client.Header.Get("X-Second"))
}

func TestIsValidCookieName(t *testing.T) {
	t.Run("empty name is invalid", func(t *testing.T) {
		assert.False(t, IsValidCookieName(""))
	})

	t.Run("alphanumeric name is valid", func(t *testing.T) {
		assert.True(t, IsValidCookieName("session_id"))
	})

	t.Run("special token chars are valid", func(t *testing.T) {
		assert.True(t, IsValidCookieName("my-cookie!"))
	})

	t.Run("space is invalid", func(t *testing.T) {
		assert.False(t, IsValidCookieName("my cookie"))
	})

	t.Run("semicolon is invalid", func(t *testing.T) {
		assert.False(t, IsValidCookieName("my;cookie"))
	})

	t.Run("unicode is invalid", func(t *testing.T) {
		assert.False(t, IsValidCookieName("クッキー"))
	})
}

func TestSanitizeCookieValue(t *testing.T) {
	t.Run("normal value unchanged", func(t *testing.T) {
		assert.Equal(t, "abc123", SanitizeCookieValue("abc123"))
	})

	t.Run("removes semicolons", func(t *testing.T) {
		assert.Equal(t, "abc", SanitizeCookieValue("a;b;c"))
	})

	t.Run("removes quotes", func(t *testing.T) {
		assert.Equal(t, "abc", SanitizeCookieValue(`a"b"c`))
	})

	t.Run("removes backslashes", func(t *testing.T) {
		assert.Equal(t, "abc", SanitizeCookieValue(`a\b\c`))
	})

	t.Run("removes newlines", func(t *testing.T) {
		assert.Equal(t, "abc", SanitizeCookieValue("a\nb\nc"))
	})

	t.Run("removes carriage returns", func(t *testing.T) {
		assert.Equal(t, "abc", SanitizeCookieValue("a\rb\rc"))
	})

	t.Run("removes control characters", func(t *testing.T) {
		assert.Equal(t, "abc", SanitizeCookieValue("a\x00b\x01c"))
	})
}

func TestBuildCookieHeader(t *testing.T) {
	builder := NewScraperClientBuilder()

	t.Run("builds valid cookie header", func(t *testing.T) {
		result := builder.buildCookieHeader(map[string]string{
			"session": "abc123",
			"lang":    "en",
		})
		assert.Contains(t, result, "session=abc123")
		assert.Contains(t, result, "lang=en")
	})

	t.Run("skips invalid cookie names", func(t *testing.T) {
		result := builder.buildCookieHeader(map[string]string{
			"valid":    "yes",
			"inva lid": "no",
		})
		assert.Contains(t, result, "valid=yes")
		assert.NotContains(t, result, "inva lid")
	})

	t.Run("empty cookies returns empty string", func(t *testing.T) {
		result := builder.buildCookieHeader(map[string]string{})
		assert.Equal(t, "", result)
	})
}
