package marketdata

import (
	"errors"
	"log/slog"

	"github.com/rustyeddy/trader/logging"
	"github.com/rustyeddy/trader/marketdata"
)

// ErrNilManager is returned by New when constructed with a nil Manager.
var ErrNilManager = errors.New("service/marketdata: manager is nil")

// Service is the application/service-layer boundary over a
// *marketdata.Manager (ADR-022). Transport adapters call Service
// operations instead of using a Manager directly, so orchestration that
// spans several Manager calls (see Update, issue #107) is implemented
// once and reused by every transport.
//
// Service holds no transport, formatting, or presentation state, and no
// mutable state of its own beyond the *marketdata.Manager it wraps and
// the *slog.Logger New scoped: its own concurrency properties are
// therefore exactly whatever the wrapped Manager's are, whatever those
// turn out to be documented as — logging a record adds no additional
// mutable state or synchronization of Service's own.
type Service struct {
	manager *marketdata.Manager
	logger  *slog.Logger
}

// New constructs a Service over manager. manager must not be nil.
//
// logger receives Service's own structured operation-boundary records
// (issue #128, ADR-023): completion and failure events for the use
// cases below, each scoped with the canonical
// logging.ComponentMarketData attribute so they stay identifiable
// after aggregation regardless of which *marketdata.Manager
// implementation detail eventually produced them. A nil logger is
// accepted and treated as logging.Discard() — matching this
// repository's own "inject a logger, or nothing at all, if silence is
// an acceptable default" convention (logging/doc.go) — so existing
// callers that have no logger to hand New yet are not forced to
// construct one merely to satisfy this signature.
func New(manager *marketdata.Manager, logger *slog.Logger) (*Service, error) {
	if manager == nil {
		return nil, ErrNilManager
	}
	if logger == nil {
		logger = logging.Discard()
	}
	return &Service{manager: manager, logger: logging.WithComponent(logger, logging.ComponentMarketData)}, nil
}
