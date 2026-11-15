package security

import (
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
)

var sensitiveKeys = map[string]struct{}{
	"api_key":               {},
	"apikey":                {},
	"authorization":         {},
	"password":              {},
	"password_hash":         {},
	"secret":                {},
	"token":                 {},
	"session_token":         {},
	"signaling_token":       {},
	"csrf_token":            {},
	"cookie":                {},
	"set-cookie":            {},
	"master_encryption_key": {},
	"auth_header":           {},
}

var (
	bearerRegex = regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9\-._~+/]+=*`)
	skRegex     = regexp.MustCompile(`sk-[A-Za-z0-9\-_]{10,}`)
)

func NewRedactingLogger(out io.Writer) *slog.Logger {
	if out == nil {
		out = os.Stdout
	}
	handler := slog.NewJSONHandler(out, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			keyLower := strings.ToLower(a.Key)
			if _, sensitive := sensitiveKeys[keyLower]; sensitive {
				return slog.String(a.Key, "[REDACTED]")
			}
			if a.Value.Kind() == slog.KindString {
				return slog.String(a.Key, RedactString(a.Value.String()))
			}
			return a
		},
	})
	return slog.New(handler)
}

func RedactString(s string) string {
	if s == "" {
		return ""
	}
	out := bearerRegex.ReplaceAllString(s, "Bearer [REDACTED]")
	out = skRegex.ReplaceAllString(out, "sk-[REDACTED]")
	return out
}
