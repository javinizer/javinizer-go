package scraperconfig

import (
	"strings"
	"testing"
)

func TestProxyProfile_Redact_RedactsURLCredentials(t *testing.T) {
	p := ProxyProfile{
		URL:      "http://user:pass@proxy.example.com:8080",
		Username: "admin",
		Password: "secret",
	}
	got := p.Redact()

	if got.Username != RedactedValue {
		t.Errorf("Username = %q, want %q", got.Username, RedactedValue)
	}
	if got.Password != RedactedValue {
		t.Errorf("Password = %q, want %q", got.Password, RedactedValue)
	}
	if strings.Contains(got.URL, "user") || strings.Contains(got.URL, "pass") {
		t.Errorf("URL still contains credentials: %q", got.URL)
	}
	if !strings.Contains(got.URL, "proxy.example.com:8080") {
		t.Errorf("URL lost host portion: %q", got.URL)
	}
	if !strings.Contains(got.URL, "@proxy.example.com:8080") {
		t.Errorf("URL missing userinfo separator and host: %q", got.URL)
	}
}

func TestProxyProfile_Redact_URLNoCredentials(t *testing.T) {
	p := ProxyProfile{
		URL:      "http://proxy.example.com:8080",
		Username: "admin",
		Password: "secret",
	}
	got := p.Redact()

	if got.URL != "http://proxy.example.com:8080" {
		t.Errorf("URL = %q, want unchanged", got.URL)
	}
}

func TestProxyProfile_Redact_MalformedURL(t *testing.T) {
	p := ProxyProfile{
		URL:      "://not-a-valid-url",
		Username: "admin",
		Password: "secret",
	}
	got := p.Redact()

	if got.URL != RedactedValue {
		t.Errorf("malformed URL = %q, want %q", got.URL, RedactedValue)
	}
}

func TestProxyProfile_Redact_EmptyURL(t *testing.T) {
	p := ProxyProfile{
		URL:      "",
		Username: "admin",
		Password: "secret",
	}
	got := p.Redact()

	if got.URL != "" {
		t.Errorf("empty URL = %q, want empty", got.URL)
	}
}
