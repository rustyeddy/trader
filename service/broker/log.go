package broker

import (
	"context"
	"log/slog"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/logging"
)

// logOutcome emits exactly one record for a completed operation:
// levelOK at levelOK when err is nil, ERROR when it is not. This is the
// one place every use case's own "log the operation boundary once"
// call funnels through, so success and failure share identical
// request-identifying attributes and differ only in level, message,
// and whichever operation-specific attrs the caller supplies.
//
// accountID is included only when non-zero — Accounts (issue #154) is
// broker-wide, not account-scoped, and has none to report. err is
// appended as the canonical "error" attribute only on failure; a
// successful call never carries a stray nil error attribute.
//
// Every attribute this package logs comes from a typed request/result
// field (account/order/instrument identity, order status, a result
// count) — never a whole request, response, or broker config value
// dumped wholesale. This is what keeps a future credential-bearing
// field (a real adapter's API token, for instance) out of these
// records by construction: nothing here ever has the opportunity to
// serialize a config struct it was never handed in the first place.
// See the package doc comment's "Logging and credential redaction"
// section for the current M3 scope and what is explicitly deferred to
// the real-adapter milestone (M5).
func (s *Service) logOutcome(ctx context.Context, levelOK slog.Level, okMsg, failMsg string, accountID id.AccountID, err error, attrs ...any) {
	base := attrs
	if !accountID.IsZero() {
		base = append([]any{logging.AccountID, accountID.String()}, base...)
	}
	if err != nil {
		s.logger.ErrorContext(ctx, failMsg, append(base, "error", err)...)
		return
	}
	s.logger.Log(ctx, levelOK, okMsg, base...)
}
