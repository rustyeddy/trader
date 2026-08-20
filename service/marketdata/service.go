package marketdata

import (
	"errors"

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
// Service holds no transport, formatting, or presentation state. It is
// safe for concurrent use for the same reason *marketdata.Manager is:
// Service adds no mutable state of its own beyond the Manager it wraps.
type Service struct {
	manager *marketdata.Manager
}

// New constructs a Service over manager. manager must not be nil.
func New(manager *marketdata.Manager) (*Service, error) {
	if manager == nil {
		return nil, ErrNilManager
	}
	return &Service{manager: manager}, nil
}
