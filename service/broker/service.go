package broker

import (
	"errors"
	"log/slog"

	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/logging"
)

// ErrNilBroker is returned by New when constructed with a nil
// brokerpkg.Broker.
var ErrNilBroker = errors.New("service/broker: broker is nil")

// Service is the application/service-layer boundary over a
// brokerpkg.Broker (ADR-022). Transport adapters call Service
// operations instead of using a brokerpkg.Broker/brokerpkg.Account
// directly, so every transport shares identical account-opening and
// logging behavior.
//
// Service holds no mutable state of its own beyond the brokerpkg
// .Broker it wraps and the *slog.Logger New scoped: it does not cache
// an opened brokerpkg.Account, a snapshot, or any other per-call
// result across operations (see the package doc comment).
type Service struct {
	broker brokerpkg.Broker
	logger *slog.Logger
}

// New constructs a Service over b. b must not be nil.
//
// logger receives Service's own structured operation-boundary records
// (ADR-023): completion and failure events for the use cases below,
// each scoped with the canonical logging.ComponentBroker attribute. A
// nil logger is accepted and treated as logging.Discard() — this
// repository's own "inject a logger, or nothing at all, if silence is
// an acceptable default" convention (logging/doc.go) — so a caller
// with no logger yet is not forced to construct one merely to satisfy
// this signature.
func New(b brokerpkg.Broker, logger *slog.Logger) (*Service, error) {
	if b == nil {
		return nil, ErrNilBroker
	}
	if logger == nil {
		logger = logging.Discard()
	}
	return &Service{broker: b, logger: logging.WithComponent(logger, logging.ComponentBroker)}, nil
}
