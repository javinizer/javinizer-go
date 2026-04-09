# HTTP Client Package

This package provides a unified HTTP client factory for all scrapers, eliminating code duplication and providing consistent proxy, retry, and header handling.

## Overview

The package implements three patterns:
- **Builder Pattern**: For custom HTTP client configuration
- **Factory Functions**: Quick client creation for common use cases
- **FlareSolverr Integration**: For sites requiring browser challenge bypass

## Usage Examples

### Simple Scrapers (No ProxyProfile needed)

For scrapers that don't need browser capabilities:

```go
import (
    "github.com/go-resty/resty/v2"
    "github.com/javinizer/javinizer-go/internal/httpclient"
)

func NewHTTPClient(cfg *config.ScraperSettings, globalProxy *config.ProxyConfig, globalFlareSolverr config.FlareSolverrConfig) (*resty.Client, error) {
    return httpclient.FromScraperSettings(cfg, globalProxy, globalFlareSolverr,
        httpclient.WithHeaders(httpclient.CombineHeaders(
            httpclient.StandardHTMLHeaders(),
            httpclient.UserAgentHeader(cfg.UserAgent),
        )),
    ).BuildClient()
}
```

Returns: `(*resty.Client, error)`

### FlareSolverr Scrapers (Browser bypass)

For scrapers that need FlareSolverr to bypass Cloudflare/browser challenges:

```go
import (
    "github.com/go-resty/resty/v2"
    "github.com/javinizer/javinizer-go/internal/httpclient"
)

func NewHTTPClient(cfg *config.ScraperSettings, globalProxy *config.ProxyConfig, globalFlareSolverr config.FlareSolverrConfig) (*resty.Client, *httpclient.FlareSolverr, error) {
    builder := httpclient.FromScraperSettings(cfg, globalProxy, globalFlareSolverr,
        httpclient.WithHeaders(httpclient.StandardHTMLHeaders()),
        httpclient.WithHeaders(httpclient.UserAgentHeader(cfg.UserAgent)),
    )
    return builder.BuildWithFlareSolverr()
}
```

Returns: `(*resty.Client, *httpclient.FlareSolverr, error)`

### Browser Scrapers (Need ProxyProfile)

For scrapers that need to pass proxy configuration to browser automation:

```go
import (
    "github.com/go-resty/resty/v2"
    "github.com/javinizer/javinizer-go/internal/httpclient"
    "github.com/javinizer/javinizer-go/internal/config"
)

func NewHTTPClient(cfg *config.ScraperSettings, globalProxy *config.ProxyConfig, globalFlareSolverr config.FlareSolverrConfig) (*resty.Client, *config.ProxyProfile, error) {
    return httpclient.FromScraperSettings(cfg, globalProxy, globalFlareSolverr,
        httpclient.WithHeaders(httpclient.CombineHeaders(
            httpclient.DMMHeaders(),
            httpclient.UserAgentHeader(cfg.UserAgent),
        )),
    ).BuildWithProxy()
}
```

Returns: `(*resty.Client, *config.ProxyProfile, error)`

## Header Presets

The package provides pre-built header presets:

| Preset | Description |
|--------|-------------|
| `StandardHTMLHeaders()` | Standard browser headers for HTML pages |
| `JSONAPIHeaders()` | Headers for JSON API requests |
| `JapaneseLanguageHeaders()` | HTML headers with Japanese language preference |
| `DMMHeaders()` | DMM-specific headers with cookies |
| `R18DevHeaders()` | R18Dev API headers |
| `RefererHeader(url)` | Creates a Referer header |
| `UserAgentHeader(ua)` | User-Agent header with default fallback |

### Combining Headers

```go
headers := httpclient.CombineHeaders(
    httpclient.StandardHTMLHeaders(),
    httpclient.UserAgentHeader("custom-agent"),
    httpclient.RefererHeader("https://example.com"),
)
```

### Merging Cookies

```go
existingCookies := map[string]string{"session": "abc123"}
newCookies := map[string]string{"token": "xyz789"}
cookieHeader := httpclient.MergeCookieHeader(existingCookies, newCookies)
// Result: "session=abc123; token=xyz789"
```

## Functional Options

The builder supports these options:

| Option | Description |
|--------|-------------|
| `WithTimeout(d)` | Set request timeout (default: 30s) |
| `WithRetryCount(n)` | Set retry count (default: 3) |
| `WithGlobalProxy(cfg)` | Set global proxy configuration |
| `WithGlobalFlareSolverr(cfg)` | Set global FlareSolverr configuration |
| `WithScraperProxy(cfg)` | Set scraper-specific proxy override |
| `WithFlareSolverr(enabled)` | Enable FlareSolverr for this scraper |
| `WithHeader(k, v)` | Add a single header |
| `WithHeaders(m)` | Add multiple headers |
| `WithCookies(m)` | Add cookies |
| `WithReturnProxyProfile()` | Request ProxyProfile in response |

## Builder Methods

| Method | Returns | Description |
|--------|---------|-------------|
| `BuildClient()` | `(*resty.Client, error)` | Simple client, no proxy profile |
| `BuildWithProxy()` | `(*resty.Client, *ProxyProfile, error)` | Client with proxy profile for browser use |
| `BuildWithFlareSolverr()` | `(*resty.Client, *FlareSolverr, error)` | Client with FlareSolverr instance |
| `Build()` | `(*ScraperClient, error)` | Full ScraperClient with all components |

## FlareSolverr Usage

```go
// Create FlareSolverr-enabled client
client, flare, err := httpclient.FromScraperSettings(cfg, globalProxy, globalFlareSolverr,
    httpclient.WithHeaders(httpclient.StandardHTMLHeaders()),
    httpclient.WithFlareSolverr(true),
).BuildWithFlareSolverr()

// Make request through FlareSolverr
resp, err := flare.RequestGet(ctx, "https://example.com", nil)

// The HTML content is in resp.Solution.Response
html := resp.Solution.Response
```

## Proxy Support

The package automatically handles:
- HTTP/HTTPS proxies
- SOCKS5 proxies
- Proxy authentication
- Per-scraper proxy overrides

Proxy priority: `ScraperSettings.Proxy` > `Global Proxy` > `None`

## Migration Guide

### Before (Duplicated Code)

```go
func NewHTTPClient(cfg *config.Config) (*resty.Client, error) {
    client := resty.New()
    client.SetTimeout(30 * time.Second)
    client.SetRetryCount(3)
    
    // Handle proxy
    if cfg.Proxy.Enabled {
        // 40+ lines of proxy setup...
    }
    
    // Set headers
    client.SetHeaders(map[string]string{
        "Accept": "text/html...",
        // More headers...
    })
    
    return client, nil
}
```

### After (Using Factory)

```go
func NewHTTPClient(cfg *config.ScraperSettings, globalProxy *config.ProxyConfig, globalFlareSolverr config.FlareSolverrConfig) (*resty.Client, error) {
    return httpclient.FromScraperSettings(cfg, globalProxy, globalFlareSolverr,
        httpclient.WithHeaders(httpclient.StandardHTMLHeaders()),
    ).BuildClient()
}
```

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      Scraper Packages                        │
│  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐           │
│  │   DMM   │ │ JavBus  │ │ JavDB   │ │  FC2    │ ...       │
│  └────┬────┘ └────┬────┘ └────┬────┘ └────┬────┘           │
│       │           │           │           │                 │
│       └───────────┴───────────┴───────────┘                 │
│                       │                                     │
│                       ▼                                     │
│  ┌─────────────────────────────────────────────────────┐   │
│  │           internal/httpclient                         │   │
│  │  ┌──────────────┐  ┌──────────────┐  ┌───────────┐  │   │
│  │  │   Builder    │  │   Factory    │  │ FlareSol. │  │   │
│  │  │   builder.go │  │  factory.go  │  │(embedded) │  │   │
│  │  └──────────────┘  └──────────────┘  └───────────┘  │   │
│  │  ┌──────────────┐  ┌──────────────┐                  │   │
│  │  │   Presets    │  │   Client     │                  │   │
│  │  │  presets.go  │  │  client.go   │                  │   │
│  │  └──────────────┘  └──────────────┘                  │   │
│  └─────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

## Testing

```bash
# Run package tests
go test ./internal/httpclient/...

# Run with race detector
go test -race ./internal/httpclient/...
```
