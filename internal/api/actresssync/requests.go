package actresssync

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

const maxBodyBytes = 512 << 10
const maxQueryBytes = 4096

// decodeJSON rejects extra documents, invalid UTF-8, duplicate keys, unknown
// fields (including nested choices), and bodies above a fixed memory budget.
func decodeJSON(c *gin.Context, destination any) bool {
	return decodeJSONBody(c, destination, false)
}

func decodeJSONBody(c *gin.Context, destination any, allowEmpty bool) bool {
	body := http.MaxBytesReader(c.Writer, c.Request.Body, maxBodyBytes)
	data, err := io.ReadAll(body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(c, http.StatusRequestEntityTooLarge, "SYNC_BODY_TOO_LARGE", "request body exceeds 512 KiB")
		} else {
			invalid(c)
		}
		return false
	}
	if allowEmpty && len(bytes.TrimSpace(data)) == 0 {
		return true
	}
	if !utf8.Valid(data) || len(bytes.TrimSpace(data)) == 0 || bytes.TrimSpace(data)[0] != '{' {
		invalid(c)
		return false
	}
	if err := uniqueJSONKeys(json.NewDecoder(bytes.NewReader(data)), 0); err != nil {
		invalid(c)
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		invalid(c)
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		invalid(c)
		return false
	}
	return true
}

var errJSONShape = errors.New("invalid JSON shape")

func uniqueJSONKeys(decoder *json.Decoder, depth int) error {
	if depth > 8 {
		return errJSONShape
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, container := token.(json.Delim)
	if !container {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]bool)
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || !requestField(name) || seen[name] {
				return errJSONShape
			}
			seen[name] = true
			if err := uniqueJSONKeys(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := uniqueJSONKeys(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return errJSONShape
	}
	_, err = decoder.Token()
	return err
}

// encoding/json accepts Unicode case-fold aliases for struct tags. An exact
// allowlist prevents those aliases from becoming hidden confirmation inputs.
func requestField(name string) bool {
	switch name {
	case "choices", "actress_id", "source_key", "review_id", "confirmed", "artifact_digest":
		return true
	default:
		return false
	}
}

func query(c *gin.Context, keys ...string) (url.Values, bool) {
	if len(c.Request.URL.RawQuery) > maxQueryBytes {
		invalid(c)
		return nil, false
	}
	values, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		invalid(c)
		return nil, false
	}
	for key, entries := range values {
		allowed := false
		for _, candidate := range keys {
			allowed = allowed || key == candidate
		}
		if !allowed || len(entries) != 1 || !utf8.ValidString(entries[0]) {
			invalid(c)
			return nil, false
		}
	}
	return values, true
}

func number(values url.Values, key string, fallback, maximum int) (int, bool) {
	text, exists := values[key]
	if !exists {
		return fallback, true
	}
	n, err := strconv.Atoi(text[0])
	return n, err == nil && n >= 0 && (maximum == 0 || n <= maximum) && strconv.Itoa(n) == text[0]
}

func page(c *gin.Context, values url.Values, defaultLimit, maxLimit int) (int, int, bool) {
	offset, offsetOK := number(values, "offset", 0, 0)
	limit, limitOK := number(values, "limit", defaultLimit, maxLimit)
	if !offsetOK || !limitOK || limit == 0 {
		invalid(c)
		return 0, 0, false
	}
	return offset, limit, true
}

func validHexID(id string) bool {
	if len(id) != 64 {
		return false
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func validJobID(id string) bool {
	return strings.HasPrefix(id, "name-") && validHexID(strings.TrimPrefix(id, "name-"))
}
