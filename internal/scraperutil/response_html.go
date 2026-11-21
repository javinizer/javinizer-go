package scraperutil

import (
	"net/http"

	"golang.org/x/net/html/charset"
)

// bufferedHTMLResponse is the portion of a buffered response needed to decode
// HTML and preserve the caller's original string fallback.
type bufferedHTMLResponse interface {
	Body() []byte
	Header() http.Header
	String() string
}

// DecodeHTMLResponse decodes a buffered HTML response according to its declared
// or detected charset, preserving Resty's raw-response fallback on decode errors.
func DecodeHTMLResponse(resp bufferedHTMLResponse) string {
	body := resp.Body()
	if len(body) == 0 {
		return ""
	}
	contentType := resp.Header().Get("Content-Type")
	enc, _, _ := charset.DetermineEncoding(body, contentType)
	decoded, err := enc.NewDecoder().Bytes(body)
	if err != nil {
		return resp.String()
	}
	return string(decoded)
}
