package marketdata

import (
	"errors"
	"log/slog"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/logging"
	marketruntime "github.com/rustyeddy/trader/internal/marketdata"
)

// ErrNilManager is returned by New when constructed with a nil Manager.
var ErrNilManager = errors.New("service/marketdata: manager is nil")

// Service is the application/service-layer boundary over a
// *marketdata.Manager (ADR-022). Transport adapters call Service
// operations instead of using a Manager directly, so orchestration that
// spans several Manager calls (see Update, issue #107) is implemented
// once and reused by every transport.
//
// Service holds no transport, formatting, or presentation state. Its
// collaborators are the *marketdata.Manager it wraps, the *slog.Logger
// New scoped, and, when configured, the instrument resolver
// (WithResolver) and archive root (WithArchiveRoot); the Service's own
// fields never change after New. The resolver is the one mutable
// collaborator: ResolveInstrument registers listings into it, so it must
// be safe for concurrent use — instrument.MemoryResolver is, and
// registration through it is atomic (RegisterOrGet). Beyond that, the
// Service's concurrency properties are exactly the wrapped Manager's;
// logging adds no mutable state or synchronization of its own.
type Service struct {
	manager     *marketruntime.Manager
	logger      *slog.Logger
	provider    string
	resolver    *instrument.MemoryResolver
	archiveRoot string
}

// Option configures a Service at construction.
type Option func(*Service)

// WithResolver gives the Service the resolver its Manager resolves
// instruments through, so ResolveInstrument can register into it. It
// must be the same resolver the Manager was configured with.
func WithResolver(resolver *instrument.MemoryResolver) Option {
	return func(s *Service) { s.resolver = resolver }
}

// WithArchiveRoot sets where ConvertStooqArchive looks for provider
// archives when a request names neither an archive nor a root.
func WithArchiveRoot(root string) Option {
	return func(s *Service) { s.archiveRoot = root }
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
//
// opts add the resolver and archive root that instrument resolution and
// archive conversion need (WithResolver, WithArchiveRoot).
func New(manager *marketruntime.Manager, logger *slog.Logger, opts ...Option) (*Service, error) {
	if manager == nil {
		return nil, ErrNilManager
	}
	if logger == nil {
		logger = logging.Discard()
	}
	s := &Service{
		manager:  manager,
		logger:   logging.WithComponent(logger, logging.ComponentMarketData),
		provider: manager.ProviderName(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// Provider is the market-data provider this Service serves.
func (s *Service) Provider() string { return s.provider }
