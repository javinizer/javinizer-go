# Unwired bounded metadata acquisition

`New(Options)` constructs an opt-in fetch boundary. Nothing registers it with a
scraper/provider, activates config, schedules work, writes a cache, or changes
existing HTTP client/proxy behavior. In particular it does not call
`httpclient.InitScraperClient`, whose existing direct-fallback behavior remains
unchanged.

## API and admission

- Explicit `Direct` or `SOCKS5` route; an unspecified route is an error. A direct
  route cannot carry proxy settings. SOCKS requires an explicit `socks5://`
  endpoint and numeric port. HTTP/HTTPS proxies, `socks5h`, browser/FlareSolverr,
  unknown routes and malformed proxy configuration fail construction, without
  DNS, authentication or requests. No route ever falls back to direct.
- All duration, encoded-body, response-header and concurrency limits must be
  positive. Concurrency is at most 1024; redirect hops are 0–20. Body limits must
  permit a `limit + 1` overflow probe. Caller-chosen limits should be appropriate
  to the future provider; these are not deployment defaults.
- `Fetch` performs one GET, with no application retries. `FetchImage` also fully
  validates JPEG, PNG or WebP using `imagevalidate` and explicit dimension/pixel
  limits. It uses the smaller of fetch and image byte limits. Invalid image
  limits fail before I/O. Neither Content-Type nor an image URL proves an image.
- One slot covers networking **and image decoding**. Saturation returns
  `ErrBusy` immediately; there are no queues or background workers. This limits
  work per client, not across independently constructed clients or retained
  result bytes. Future consumers must reuse a bounded number of clients.
- No public custom RoundTripper, DNS callback, dialer, TLS callback, trust-root
  option, cookie jar or arbitrary request headers are available. Tests in this
  package have narrow per-instance resolver/dial/trust-root seams; `New` uses
  the system resolver, context-aware TCP dialer and system certificate roots.

## Destination safety

Each hop resolves once locally, rejects the entire answer if any address is
invalid/internal, and connects only to those validated literals. Validation
reuses `ssrf.CheckURL` with **literal-only** URLs, for which the standard Go
resolver does not perform DNS. The actual dial boundary also reuses
`ssrf.WrapTransportWithSSRFCheck` on those literals; the original hostname is
never sent to the selected direct/SOCKS dialer for a second resolution. The new
boundary additionally rejects non-global-unicast (including multicast and
broadcast) and carrier-shared addresses. This is the shared SSRF address policy
plus those exclusions, not a source-specific hostname allowlist or a guarantee
about how every network routes otherwise admitted addresses.

SOCKS sends IPv4/IPv6 address-type CONNECT destinations, never domain-type
CONNECT destinations. Proxy DNS is independently validated and pinned under the
same address policy. **Private/local proxy endpoints are unsupported in this
initial slice**, including explicitly configured loopback proxies. A proxy
configuration never permits private destination URLs. Proxy credentials are
only used for SOCKS negotiation; they are never sent as target HTTP headers.
No environment proxy variables are consulted.

The original target URL/Host remains on the HTTP request. Standard TLS retains
hostname verification and SNI, with TLS 1.2 minimum; there is no insecure TLS
fallback. Non-ASCII URL hostnames are rejected in this initial slice rather than
introducing inconsistent IDNA normalization between DNS and transport.

Each redirect is parsed and validated anew, with explicit hop limits and
cross-origin opt-in. HTTPS→HTTP, credentials, fragments, non-HTTP(S) schemes and
invalid host/port are rejected. Origin comparison canonicalizes hostname case
and numeric/default ports. There is no referer/auth/cookie forwarding. A fresh
transport per hop with keepalives disabled prevents reuse from bypassing a later
hop's DNS check. Each request also sets `Request.Close` explicitly: this prevents
Go 1.27's automatic early-close body drain even when a peer ignores the outbound
Connection header. Redirect/error bodies are closed without draining untrusted
content; ordinary transport read-ahead remains bounded by its fixed buffers.

## Bounded lifetimes and receipts

The total request deadline covers admission, DNS, connection/TLS, all redirect
hops, response headers, body consumption and image checks. Earlier caller
context deadlines win. Each hop also shares one DNS/connect budget across
target DNS, proxy DNS and all direct/SOCKS address attempts. Failed candidates
split the remaining budget fairly; SOCKS negotiation is context-aware. TLS and
response headers have independent phase budgets, bounded by the total deadline.
After connection establishment the connect budget does not own the response
body. A per-hop owner closes the raw TCP/SOCKS socket before response-body or
transport cleanup, before the next hop, and before releasing the fetch slot. This
also prevents TLS close-notify writes from retaining sockets beyond the budget.
Cancellation closes owned sockets; cleanup waits for context-aware dial work and
closes successful sockets that race with cancellation. Native OS DNS resolution
may outlive a canceled caller inside Go's resolver; the lookup returns under the
caller deadline, but this utility does not reconfigure system DNS. Go codec CPU
work cannot be preempted; image dimension/pixel checks and concurrency limits
remain necessary.

Automatic compression is disabled; requests specify identity encoding, and
unexpected encodings are rejected. Headers are byte-bounded. Final responses
must be 2xx, excluding unsolicited 206 partial content. Oversized declared
lengths are rejected before reading; unknown/chunked lengths use `limit + 1`.
Truncation, framing/read errors and cancellation return no partial result.
Every response body is closed on success or failure.

A successful `Result` owns bytes, final URL, and SHA-256 of exactly those response
bytes. It is **not** a `SnapshotDigest`, trusted actor identity, canonical cache
record, whole-source completeness claim, or authority to mutate anything.
Errors intentionally omit raw URLs, response text, resolver/dial errors and
proxy credentials. The successful final URL may contain a sensitive query:
callers must not blindly log it.

## Decisions still required before activation

- Whether initial live metadata supports only direct/local-DNS SOCKS users, or
  waits for separately reviewed ultimate-target-pinned HTTP/HTTPS proxy support.
- An explicit local/private proxy endpoint trust policy, separate from target
  restrictions. No current configuration mapping grants that exception.
- Remote-DNS/privacy semantics: this `socks5` mode deliberately resolves targets
  locally and is **not** a replacement for `socks5h`.
- Native-platform DNS cancellation/lifetime evidence and a reviewed resolver
  policy before activation. Injected DNS fixtures and Go-resolver-only local
  runs do not prove termination of native/cgo resolver work on every platform.
  Existing global/system resolver behavior is not changed by this utility.
- Provider-specific source/CDN origins, redirect policy and resource limits;
  saved canonical response fixtures and exact source-ID/evidence contracts.
- Live responses must eventually be parsed into one immutable evidence snapshot;
  per-response hashes cannot replace registry-wide snapshot consistency or
  resolve same-name/conflicting-ID ambiguity.

All tests are deterministic fake DNS, registered literal-to-loopback bridges,
local TLS or `net.Pipe` SOCKS/HTTP transcripts. They do not contact providers.
