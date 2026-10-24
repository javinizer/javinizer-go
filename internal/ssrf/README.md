# Dial endpoint validation

CheckURL is a preflight check. It does not pin a later independent connection.
NewSSRFSafeClient and WrapTransportWithSSRFCheck validate the entire DNS answer
for each connection and pass only validated literal addresses to the existing
dialer. Empty, malformed and mixed private answers fail before any dial.
HTTP Host and ordinary TLS SNI/certificate verification remain hostname based.
Custom TLS dial callbacks fail closed; custom plain/SOCKS dialers must honor the
literal endpoint they receive.

The wrapper keeps the transport and proxy selector. For an HTTP proxy it guards
the proxy endpoint, not destination DNS performed by that proxy. It must not be
used as a target-safe fetch adapter for new cache consumers without a separate
proxy-aware destination policy.

The optional dial budget belongs to the caller and covers DNS plus all address
attempts. Each attempt receives a bounded share of the remaining deadline so a
stalled earlier address leaves time for later validated addresses. TestDirectProxy supplies its existing 30-second connection policy so
IPv6/IPv4 and SOCKS destination failover remain within one budget. An opaque
custom dialer without an explicit budget gets a single validated address attempt
and its original context; this avoids multiplying an unknown per-call timeout.
NewSSRFSafeClient and the wrapper's default fallback own a 30-second dial budget.
Cancellation and supplied shorter deadlines bound all attempts. Client total
and response-header timeouts remain separate and unchanged.
