package logging

import (
	"log/slog"
	"strings"
)

// secretValue wraps a value so its structured-log representation is always
// the literal string "REDACTED", regardless of what the value actually is
// or what key it is logged under.
type secretValue struct {
	v any
}

// Secret wraps v so logging it never reveals its actual content — logging a
// value wrapped with Secret always resolves to the string "REDACTED". This
// is the primary redaction mechanism, correct regardless of what key the
// value is logged under, and should be used at every call site that logs a
// credential, token, or other sensitive value:
//
//	logger.Info("authenticated", "password", logging.Secret(pw))
//
// New also installs a small denylist-based ReplaceAttr (see
// redactSensitiveKeys) as a second, best-effort layer that catches a
// sensitive value logged under a conventionally-named key without an
// explicit Secret wrap. Secret is the mechanism that is actually reliable,
// since it does not depend on guessing key names.
func Secret(v any) slog.LogValuer {
	return secretValue{v: v}
}

// LogValue implements slog.LogValuer.
func (s secretValue) LogValue() slog.Value {
	return slog.StringValue("REDACTED")
}

// sensitiveKeys is a best-effort denylist of attribute key names commonly
// used for credentials, matched case-insensitively. It exists as defense in
// depth, not as the primary redaction mechanism — see Secret.
var sensitiveKeys = map[string]bool{
	"password":    true,
	"passwd":      true,
	"secret":      true,
	"token":       true,
	"api_key":     true,
	"apikey":      true,
	"credential":  true,
	"credentials": true,
	"private_key": true,
	"privatekey":  true,
}

// redactSensitiveKeys is a slog.HandlerOptions.ReplaceAttr function that
// redacts any attribute whose key (case-insensitively) appears in
// sensitiveKeys. It leaves a slog.KindGroup value alone: replacing a group
// with a plain string would collapse whatever structure it carried, and a
// group attribute's own leaf values are still visited by ReplaceAttr
// individually.
func redactSensitiveKeys(_ []string, a slog.Attr) slog.Attr {
	if a.Value.Kind() != slog.KindGroup && sensitiveKeys[strings.ToLower(a.Key)] {
		return slog.String(a.Key, "REDACTED")
	}
	return a
}
