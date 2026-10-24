package actresscache

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"
)

func validateJSON(ctx context.Context, data []byte, limits Limits, totalStrings *int64, required ...string) error {
	if !utf8.Valid(data) {
		return fmt.Errorf("invalid UTF-8")
	}
	if err := validateUnicodeEscapes(ctx, data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	tokens := jsonTokens{ctx: ctx, decoder: decoder, limits: limits, totalStrings: totalStrings}
	var rootKeys map[string]bool
	var walk func(int, string) error
	walk = func(depth int, field string) error {
		if depth > 8 {
			return fmt.Errorf("JSON nesting budget exceeded")
		}
		token, err := tokens.next(false)
		if err != nil {
			return err
		}
		if !validTokenType(field, token) {
			return fmt.Errorf("invalid JSON value for %q", field)
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := make(map[string]bool)
			if depth == 0 {
				rootKeys = seen
			}
			for decoder.More() {
				token, err := tokens.next(true)
				if err != nil {
					return err
				}
				key, ok := token.(string)
				if !ok || !knownField(key) {
					return fmt.Errorf("unknown JSON field %q", token)
				}
				if seen[key] {
					return fmt.Errorf("duplicate JSON field %q", key)
				}
				seen[key] = true
				if err := walk(depth+1, key); err != nil {
					return err
				}
			}
			_, err := tokens.next(false)
			if err != nil {
				return err
			}
		case '[':
			count := 0
			limit := limits.MaxExternalIDs
			if field == "aliases" {
				limit = limits.MaxAliases
			}
			for decoder.More() {
				count++
				if count > limit {
					return fmt.Errorf("%s collection budget exceeded", field)
				}
				if err := walk(depth+1, field+"[]"); err != nil {
					return err
				}
			}
			_, err := tokens.next(false)
			if err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(0, ""); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON data")
	}
	for _, field := range required {
		if !rootKeys[field] {
			return fmt.Errorf("missing required field %q", field)
		}
	}
	return ctx.Err()
}

func knownField(field string) bool {
	switch field {
	case "format", "schema_version", "record_count", "source", "source_id", "provenance", "metadata", "external_ids", "source_url", "dataset_sha256", "parser_revision", "observed_at", "first_name", "last_name", "japanese_name", "aliases", "thumbnail_url", "namespace", "value":
		return true
	default:
		return false
	}
}

func validTokenType(field string, token json.Token) bool {
	switch field {
	case "", "provenance", "metadata", "external_ids[]":
		return token == json.Delim('{')
	case "aliases", "external_ids":
		return token == json.Delim('[')
	case "schema_version", "record_count":
		_, ok := token.(json.Number)
		return ok
	default:
		_, ok := token.(string)
		return ok
	}
}

func validateUnicodeEscapes(ctx context.Context, data []byte) error {
	inString := false
	for i := 0; i < len(data); i++ {
		if i%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if data[i] == '"' {
			inString = !inString
			continue
		}
		if !inString || data[i] != '\\' {
			continue
		}
		if i+1 >= len(data) {
			return fmt.Errorf("truncated JSON escape")
		}
		i++
		if data[i] != 'u' {
			continue
		}
		if i+4 >= len(data) {
			return fmt.Errorf("truncated Unicode escape")
		}
		value, err := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		if err != nil {
			return fmt.Errorf("invalid Unicode escape: %w", err)
		}
		i += 4
		if value >= 0xdc00 && value <= 0xdfff {
			return fmt.Errorf("unpaired Unicode surrogate")
		}
		if value < 0xd800 || value > 0xdbff {
			continue
		}
		if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
			return fmt.Errorf("unpaired Unicode surrogate")
		}
		low, err := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
		if err != nil || low < 0xdc00 || low > 0xdfff {
			return fmt.Errorf("unpaired Unicode surrogate")
		}
		i += 6
	}
	return nil
}

type jsonTokens struct {
	ctx          context.Context
	decoder      *json.Decoder
	limits       Limits
	totalStrings *int64
	count        int
}

func (t *jsonTokens) next(key bool) (json.Token, error) {
	if err := t.ctx.Err(); err != nil {
		return nil, err
	}
	token, err := t.decoder.Token()
	if err != nil {
		return nil, err
	}
	t.count++
	if t.count > t.limits.MaxTokensPerRecord {
		return nil, fmt.Errorf("JSON token budget exceeded")
	}
	if text, ok := token.(string); ok && !key {
		if len(text) > t.limits.MaxStringBytes || int64(len(text)) > t.limits.MaxTotalStringBytes-*t.totalStrings {
			return nil, fmt.Errorf("decoded string budget exceeded")
		}
		*t.totalStrings += int64(len(text))
	}
	return token, nil
}
