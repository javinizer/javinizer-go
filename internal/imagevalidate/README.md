# Bounded image validation

Validate is an unwired validation boundary for future cache consumers. Callers
must supply positive encoded byte, dimension and pixel budgets. It reads at most
the encoded budget plus one byte, probes JPEG/PNG/WebP headers, checks dimensions
and pixels without multiplication overflow, fully decodes with the selected
codec, and checks decoded dimensions against the header. Other formats are
rejected. It does not install GIF support or change the existing image proxy's
AVIF/APNG response behavior.

Context cancellation is checked during reads and before and after decoding.
Go codecs cannot be interrupted during CPU work, and arbitrary blocking Reader
implementations must supply their own cancellation/deadline behavior. Future
consumers also need concurrency limits and network response/body deadlines.

The WebP fixture is golang.org/x/image v0.45.0's
`testdata/gopher-doc.1bpp.lossless.webp`, retained under testdata/LICENSE.
