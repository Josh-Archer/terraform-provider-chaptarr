package client

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"
)

func sanitizeResponseMessage(body []byte, secret string) string {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return "request failed without a response message"
	}

	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err == nil {
		if sanitized, err := json.Marshal(redactValue(decoded, secret)); err == nil {
			return truncateMessage(redactKnownSecret(string(sanitized), secret), maxErrorMessageBytes)
		}
	}

	// Arbitrary malformed or plain-text server responses cannot be proven safe
	// with pattern matching. Omit them completely instead of risking a partial
	// credential leak from quoting, escaping, or an unknown authentication form.
	return "request failed with a non-JSON response; content omitted"
}

func redactValue(value any, secret string) any {
	switch typed := value.(type) {
	case map[string]any:
		redacted := make(map[string]any, len(typed))
		for key, nested := range typed {
			if isSensitiveKey(key) {
				redacted[key] = "[REDACTED]"
				continue
			}
			redacted[key] = redactValue(nested, secret)
		}
		return redacted
	case []any:
		redacted := make([]any, len(typed))
		for index, nested := range typed {
			redacted[index] = redactValue(nested, secret)
		}
		return redacted
	case string:
		return sanitizePlainText(typed, secret)
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	canonical := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, key)
	for _, marker := range []string{
		"apikey",
		"authorization",
		"cookie",
		"password",
		"passphrase",
		"secret",
		"token",
		"privatekey",
		"credential",
	} {
		if strings.Contains(canonical, marker) {
			return true
		}
	}
	return false
}

func sanitizePlainText(value, secret string) string {
	value = redactKnownSecret(value, secret)
	lower := strings.ToLower(value)
	if isSensitiveKey(value) || strings.Contains(lower, "bearer ") || strings.Contains(lower, "basic ") || strings.Contains(lower, "digest ") {
		return "[REDACTED]"
	}
	return value
}

func redactKnownSecret(value, secret string) string {
	if secret == "" {
		return value
	}
	return strings.ReplaceAll(value, secret, "[REDACTED]")
}

func truncateMessage(value string, maximumBytes int) string {
	if len(value) <= maximumBytes {
		return value
	}
	if maximumBytes <= 3 {
		return strings.Repeat(".", maximumBytes)
	}
	truncated := value[:maximumBytes-3]
	for !utf8.ValidString(truncated) {
		truncated = truncated[:len(truncated)-1]
	}
	return truncated + "..."
}
