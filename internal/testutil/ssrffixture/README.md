# Synthetic SSRF fixtures

NewServer bridges only a registered synthetic `8.8.8.8:port` to that server's
local listener. It retains production DNS validation and literal pinning;
unregistered synthetic endpoints fail locally. Nested fixtures restore the
previous transport in cleanup order.

Use serial tests and finish background HTTP work before cleanup. This helper
replaces http.DefaultTransport; its registry does not make global transport
mutation safe for parallel tests.
