package translation

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/logging"
)

func normalizeTranslationPayload(payload string) string {
	cleaned := strings.TrimSpace(payload)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	return strings.TrimSpace(cleaned)
}

func parseLLMTranslationPayload(payload string, expectedCount int) ([]string, error) {
	cleaned := normalizeTranslationPayload(payload)
	if expectedCount > 0 && strings.Contains(cleaned, translationCompactOutputMarker(0)) {
		parsed, err := parseCompactTranslationPayload(cleaned, expectedCount)
		if err != nil {
			return nil, err
		}
		logging.Debugf("Translation: parseLLMTranslationPayload parsed %d compact tagged items", len(parsed))
		return parsed, nil
	}
	return parseStringArrayPayload(cleaned)
}

func parseStringArrayPayload(payload string) ([]string, error) {
	cleaned := normalizeTranslationPayload(payload)

	logging.Debugf("Translation: parseStringArrayPayload input length=%d, first 200 chars: %s", len(cleaned), cleaned[:min(200, len(cleaned))])

	if result, err := unmarshalStringArray(cleaned); err == nil {
		logging.Debugf("Translation: parseStringArrayPayload direct unmarshal successful (%d items)", len(result))
		return result, nil
	}

	start := strings.IndexByte(cleaned, '[')
	end := strings.LastIndexByte(cleaned, ']')
	if start >= 0 && end > start {
		candidate := strings.TrimSpace(cleaned[start : end+1])
		if candidate != cleaned {
			if result, err := unmarshalStringArray(candidate); err == nil {
				logging.Debugf("Translation: parseStringArrayPayload extracted JSON array from wrapped content (%d items)", len(result))
				return result, nil
			}
		}
	}

	return nil, fmt.Errorf("failed to parse translated output payload as JSON string array")
}

func parseCompactTranslationPayload(payload string, expectedCount int) ([]string, error) {
	pos := 0
	out := make([]string, 0, expectedCount)

	for i := 0; i < expectedCount; i++ {
		startToken := translationCompactOutputMarker(i)
		start := strings.Index(payload[pos:], startToken)
		if start < 0 {
			return nil, fmt.Errorf("failed to parse compact translation payload: missing output marker %d", i)
		}
		start += pos + len(startToken)

		end := len(payload)
		if i+1 < expectedCount {
			nextToken := translationCompactOutputMarker(i + 1)
			next := strings.Index(payload[start:], nextToken)
			if next < 0 {
				return nil, fmt.Errorf("failed to parse compact translation payload: missing output marker %d", i+1)
			}
			end = start + next
		}

		out = append(out, strings.TrimSpace(payload[start:end]))
		pos = end
	}

	return out, nil
}

func unmarshalStringArray(payload string) ([]string, error) {
	var result []string
	if err := json.Unmarshal([]byte(payload), &result); err != nil {
		return nil, err
	}
	return result, nil
}
