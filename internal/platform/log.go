package platform

import (
	"io"
	"log/slog"
	"strings"
)

// redacted replaces the value of any attribute whose key looks secret.
const redacted = "[REDACTED]"

// NewLogger returns a JSON slog.Logger that writes to w, tags every record with
// the service name and build version, and redacts attributes whose keys look
// secret.
//
// Redaction is done at the handler, so a careless log call can't leak a
// credential. It is key-based: values that embed secrets under a harmless key
// are the caller's responsibility and are caught in review.
func NewLogger(w io.Writer, service string) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       slog.LevelInfo,
		ReplaceAttr: redact,
	})
	return slog.New(h).With(slog.String("service", service), slog.String("version", Version()))
}

// redact is the slog ReplaceAttr hook; it runs for every attribute, including
// those inside groups, and never changes the key.
func redact(_ []string, a slog.Attr) slog.Attr {
	if isSensitiveKey(a.Key) {
		return slog.String(a.Key, redacted)
	}
	return a
}

// isSensitiveKey reports whether key contains a lower-case substring that marks
// it as secret (15-security-and-reliability.md §3, §8), ignoring case.
// Substring matching also catches variants such as "db_password" or
// "refresh_token".
func isSensitiveKey(key string) bool {
	parts := [...]string{
		"password", "passwd", "secret", "token", "authorization", "cookie",
		"apikey", "api_key", "private_key", "dsn", "connection_string", "conn_string",
	}
	k := strings.ToLower(key)
	for _, part := range parts {
		if strings.Contains(k, part) {
			return true
		}
	}
	return false
}
