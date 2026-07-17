package logging

import (
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
)

const Redacted = "[REDACTED]"

var (
	databaseURL    = regexp.MustCompile(`(?i)(postgres(?:ql)?://[^:/@\s]+:)[^@\s]+(@)`)
	bearerToken    = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/=-]+`)
	assignedSecret = regexp.MustCompile(`(?i)((?:password|token|secret|authorization|cookie)\s*[=:]\s*)(?:bearer\s+)?[^,;\s]+`)
)

func NewJSONHandler(output io.Writer, level slog.Leveler) slog.Handler {
	return slog.NewJSONHandler(output, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: replaceAttribute,
	})
}

func replaceAttribute(_ []string, attribute slog.Attr) slog.Attr {
	attribute.Value = attribute.Value.Resolve()
	if attribute.Key == slog.TimeKey {
		return slog.Time(attribute.Key, attribute.Value.Time().UTC())
	}
	if sensitiveKey(attribute.Key) {
		return slog.String(attribute.Key, Redacted)
	}
	if attribute.Key == "error" || attribute.Key == "summary" {
		return slog.String(attribute.Key, redactText(fmt.Sprint(attribute.Value.Any())))
	}
	return attribute
}

func sensitiveKey(value string) bool {
	value = strings.ToLower(strings.ReplaceAll(value, "-", "_"))
	for _, name := range []string{"password", "token", "secret", "authorization", "cookie", "private_key", "keystore"} {
		if value == name || strings.HasPrefix(value, name+"_") || strings.HasSuffix(value, "_"+name) {
			return true
		}
	}
	return false
}

func redactText(value string) string {
	value = databaseURL.ReplaceAllString(value, `$1`+Redacted+`$2`)
	value = bearerToken.ReplaceAllString(value, `$1`+Redacted)
	return assignedSecret.ReplaceAllString(value, `$1`+Redacted)
}
