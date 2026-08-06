package logging

import (
	"log/slog"
	"strings"
)

// secretValue is deliberately stateless: it does not retain the value
// Secret was called with. LogValue never needs it, and holding onto it
// would keep the credential reachable through the wrapper itself — by
// reflection, by fmt's default struct formatting, or by any code that
// inspects the value before an slog handler resolves it as a LogValuer —
// defeating the point of wrapping it in the first place.
type secretValue struct{}

// Secret wraps v so logging it never reveals its actual content — logging a
// value wrapped with Secret always resolves to the string "REDACTED". This
// is the primary redaction mechanism, correct regardless of what key the
// value is logged under, and should be used at every call site that logs a
// credential, token, or other sensitive value:
//
//	logger.Info("authenticated", "password", logging.Secret(pw))
//
// v is accepted only so the call site documents what is being redacted; it
// is discarded immediately and never stored — see secretValue.
//
// New also installs a small denylist-based ReplaceAttr (see
// redactSensitiveKeys) as a second, best-effort layer that catches a
// sensitive value logged under a conventionally-named key without an
// explicit Secret wrap. Secret is the mechanism that is actually reliable,
// since it does not depend on guessing key names.
func Secret(v any) slog.LogValuer {
	_ = v
	return secretValue{}
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
