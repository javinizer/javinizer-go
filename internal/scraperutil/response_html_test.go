package scraperutil

import (
	"net/http"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/require"
)

func TestDecodeHTMLResponse(t *testing.T) {
	for _, tc := range []struct{ name, contentType, body, want string }{
		{"empty", "text/html; charset=UTF-16LE", "", ""},
		{"successful decoding preserves whitespace", "text/html; charset=UTF-8", "  影\n", "  影\n"},
		{"declared legacy charset", "text/html; charset=Shift_JIS", "\x83\x65\x83\x58\x83\x67", "テスト"},
		{"truncated character is replacement data", "text/html; charset=Shift_JIS", "\x83\x65\x82", "テ�"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			header := http.Header{"Content-Type": {tc.contentType}}
			resp := (&resty.Response{RawResponse: &http.Response{Header: header}}).SetBody([]byte(tc.body))
			require.Equal(t, tc.want, DecodeHTMLResponse(resp))
		})
	}
}
