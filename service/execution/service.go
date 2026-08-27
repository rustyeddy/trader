package execution

import (
	"errors"
	"log/slog"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/logging"
	"github.com/rustyeddy/trader/pipeline"
)

// ErrNilBroker is returned by New when constructed with a nil
// broker.Broker.
var ErrNilBroker = errors.New("service/execution: broker is nil")

// ErrNilPipeline is returned by New when constructed with a nil
// *pipeline.Pipeline.
var ErrNilPipeline = errors.New("service/execution: pipeline is nil")

// Service is the application/service-layer boundary over a
// broker.Broker and a *pipeline.Pipeline (ADR-022). Transport adapters
// call Service operations instead of coordinating account snapshot
// retrieval and pipeline submission themselves. See the package doc
// comment for why both dependencies are injected separately.
type Service struct {
	broker   broker.Broker
	pipeline *pipeline.Pipeline
	logger   *slog.Logger
}

// New constructs a Service over b and p. Neither may be nil.
//
// logger receives Service's own structured operation-boundary records
// (ADR-023), each scoped with the canonical logging.ComponentExecution
// attribute. A nil logger is accepted and treated as
// logging.Discard(), matching every other service subpackage's
// convention.
func New(b broker.Broker, p *pipeline.Pipeline, logger *slog.Logger) (*Service, error) {
	if b == nil {
		return nil, ErrNilBroker
	}
	if p == nil {
		return nil, ErrNilPipeline
	}
	if logger == nil {
		logger = logging.Discard()
	}
	return &Service{broker: b, pipeline: p, logger: logging.WithComponent(logger, logging.ComponentExecution)}, nil
}
