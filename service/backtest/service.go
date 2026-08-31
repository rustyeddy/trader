package backtest

import (
	"errors"
	"log/slog"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/logging"
	"github.com/rustyeddy/trader/marketdata"
)

// ErrNilManager is returned by New when constructed with a nil
// *marketdata.Manager.
var ErrNilManager = errors.New("service/backtest: manager is nil")

// ErrNilResolver is returned by New when constructed with a nil
// instrument.Resolver.
var ErrNilResolver = errors.New("service/backtest: resolver is nil")

// ErrNilEnvironments is returned by New when constructed with a nil
// EnvironmentFactory.
var ErrNilEnvironments = errors.New("service/backtest: environment factory is nil")

// Service is the application/service-layer boundary over a
// *marketdata.Manager, an instrument.Resolver, and an injected
// EnvironmentFactory (ADR-022). See the package doc comment for why
// Environment construction is injected rather than owned here.
type Service struct {
	manager      *marketdata.Manager
	resolver     instrument.Resolver
	environments EnvironmentFactory
	logger       *slog.Logger
}

// New constructs a Service over manager, resolver, and environments.
// None of the three may be nil.
//
// logger receives Service's own structured operation-boundary records
// (ADR-023), each scoped with the canonical logging.ComponentBacktest
// attribute. A nil logger is accepted and treated as
// logging.Discard(), matching every other service subpackage's
// convention.
func New(manager *marketdata.Manager, resolver instrument.Resolver, environments EnvironmentFactory, logger *slog.Logger) (*Service, error) {
	if manager == nil {
		return nil, ErrNilManager
	}
	if resolver == nil {
		return nil, ErrNilResolver
	}
	if environments == nil {
		return nil, ErrNilEnvironments
	}
	if logger == nil {
		logger = logging.Discard()
	}
	return &Service{
		manager:      manager,
		resolver:     resolver,
		environments: environments,
		logger:       logging.WithComponent(logger, logging.ComponentBacktest),
	}, nil
}
