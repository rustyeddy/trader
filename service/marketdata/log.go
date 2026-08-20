package marketdata

import (
	"context"
	"log/slog"

	"github.com/rustyeddy/trader/logging"
)

// datasetAttrs returns the canonical, request-identifying attributes
// every MarketData service log record includes: which instrument,
// interval, and range the operation acted on (issue #128). Every call
// site appends its own operation-specific attributes (result counts,
// an error, and so on) after these, so a record's fixed prefix is
// always identifiable the same way regardless of which operation
// produced it.
func datasetAttrs(req DatasetRequest) []any {
	return []any{
		logging.InstrumentID, req.Instrument.String(),
		"interval", req.Interval.String(),
		"range_start", req.Range.Start(),
		"range_end", req.Range.End(),
	}
}

// logOutcome emits exactly one record for a completed operation:
// levelOK at levelOK when err is nil, ERROR when it is not. This is the
// one place every use case's own "log the operation boundary once"
// call funnels through, so success and failure share identical
// request-identifying attributes and differ only in level, message,
// and whichever operation-specific attrs the caller supplies.
//
// err is appended as the canonical "error" attribute only on failure;
// a successful call never carries a stray nil error attribute.
func (s *Service) logOutcome(ctx context.Context, levelOK slog.Level, okMsg, failMsg string, req DatasetRequest, err error, attrs ...any) {
	base := append(datasetAttrs(req), attrs...)
	if err != nil {
		s.logger.ErrorContext(ctx, failMsg, append(base, "error", err)...)
		return
	}
	s.logger.Log(ctx, levelOK, okMsg, base...)
}
