package marketdata

import (
	"context"
	"errors"
	"fmt"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/instrument"
)

// Manager is Trader's sole application-service boundary for historical
// market data (issue #71, M2-03; ADR-020). Every consumer that needs
// canonical bars, coverage, acquisition, or canonical builds goes through
// a Manager; no caller accesses providers, files, stores, normalizers, or
// bar builders directly.
//
// # Reads never mutate
//
// Manager separates two visibly different capability groups. Historical
// queries are read-only: they never download, rebuild, repair, publish a
// new revision, or otherwise change the selected dataset, and missing data
// is reported explicitly rather than silently synchronized. Acquisition
// and canonical builds are explicit, separate commands. This split is what
// keeps backtests deterministic, so it is part of the contract rather than
// an implementation detail.
//
// # Boundary
//
//	Consumer
//	   |
//	   v
//	marketdata.Manager
//	   |
//	   +--> internal provider / acquisition
//	   +--> internal raw / canonical storage
//	   +--> internal normalization / validation
//	   +--> internal bar build / resampling
//	   +--> internal inventory / coverage
//	   +--> internal cache
//
// Manager coordinates these responsibilities; it does not implement all of
// them itself. The collaborator contracts are narrow interfaces owned by
// this package and wired internally during construction; provider- and
// storage-native types — OANDA rows, CSV records, filesystem paths,
// persistence layouts — never cross the Manager boundary.
//
// # Construction and lifecycle
//
// A Manager is created with New and is unusable in its zero value; New is
// the only way to obtain a valid Manager. Manager owns no background work —
// operations, when they are added, are scoped to the context passed to them —
// so there is no Run, Start, or Stop, and construction starts no goroutines.
//
// This issue establishes construction, ownership, and dependency direction
// only. The read and mutation operations themselves depend on the M2-01 /
// ADR-020 query, coverage, and canonical-persistence contracts, which are
// not yet resolved; their method signatures are deliberately not frozen
// here. Until they land, the operations report ErrNotImplemented so a
// caller can never mistake an unbuilt operation for an empty-but-successful
// result.
type Manager struct {
	clock        clock.Clock
	storeRoot    string
	resolver     instrument.Resolver
	providerName string

	// Collaborator seams. These are interfaces owned by this package so the
	// real provider, storage, normalization, and resampling
	// implementations can live behind internal boundaries without
	// marketdata importing them (which would create an import cycle, since
	// those implementations depend on this package's Bar type). They are
	// wired only within this package, permitted to be nil in this
	// skeleton, and are not public extension points.
	provider provider
	store    barStore

	// cache is Manager's own bounded, FIFO-evicted memory cache of
	// published canonical partitions (issue #78, ADR-020). It is never
	// exposed: no accessor returns it, and no Config field can replace it
	// with an external implementation. Caching is entirely Manager's own
	// affair, the same way the store itself is.
	cache *barCache
}

// Config holds the explicit dependencies a Manager is constructed from.
// Configuration is a composition-root concern: Manager never reads
// environment variables, configuration files, or a home directory. The
// composition root loads whatever configuration it uses and supplies the
// resulting typed values here.
type Config struct {
	// Clock is the deterministic time seam (ADR-015) Manager uses wherever
	// the current time matters — judging whether the current interval is
	// still open, and answering as-of coverage queries without look-ahead.
	// It is required.
	Clock clock.Clock

	// StoreRoot is the root location of the canonical store, supplied as
	// configuration. It is required. Its interpretation (a filesystem path,
	// today) is an internal storage concern and is never exposed through a
	// Manager operation. Manager builds its own store implementation from
	// this path internally (see New); no Config field lets a caller supply
	// a store implementation of its own, and the store type itself is
	// never exported.
	StoreRoot string

	// Resolver resolves an instrument.ID to the tradable Listing this
	// Manager's provider exposes it under (ADR-016), giving Manager the
	// provider-native display symbol its canonical store partitions are
	// keyed by. It is required.
	Resolver instrument.Resolver

	// ProviderName identifies, opaquely, the canonical dataset provider
	// this Manager serves — for example "oanda" — matching the value
	// recorded in each partition's Manifest.Provider and used as the
	// provider argument to Resolver.ResolveInstrument. It is required.
	ProviderName string

	// CacheCapacity bounds the number of canonical partitions Manager's
	// internal memory cache retains before evicting the oldest one (FIFO;
	// see barCache). Zero or negative selects a package-defined default.
	// This is the only cache-shaped knob Config exposes: the cache
	// implementation itself is never a Config field, so nothing outside
	// this package can supply, replace, or directly read it.
	CacheCapacity int

	// provider and store are optional internal collaborators. They remain
	// unexported so no external package can supply provider or storage
	// implementations through Config. They exist only for in-package
	// tests: real construction always builds its own canonicalCSVStore
	// from StoreRoot (see New).
	provider provider
	store    barStore
}

// provider is the narrow internal contract for acquiring provider-native
// historical data. It is intentionally unexported and minimal in this
// skeleton: it names the seam without freezing an acquisition API that the
// unresolved M2-01 / ADR-020 contracts still govern.
type provider interface {
	// name identifies the provider for diagnostics and manifest lineage.
	name() string
}

// barStore is the internal contract for reading and writing canonical
// Bar/Manifest pairs. canonicalCSVStore (issue #77, ADR-020) is its one
// implementation today; the interface exists so a later implementation
// (a Parquet store, say) can be substituted without changing Manager or
// its wiring, and so the store's own contract tests can run against any
// implementation satisfying it.
type barStore interface {
	// root reports the store's configured root, for diagnostics only.
	root() string
	// publish writes m and bs as the current revision for key,
	// atomically per the guarantee documented on
	// canonicalCSVStore.publish.
	publish(ctx context.Context, key partitionKey, m Manifest, bs BarSet) error
	// load reads the current published (Manifest, BarSet) pair for key,
	// verifying they still Match before returning either.
	load(ctx context.Context, key partitionKey) (Manifest, BarSet, error)
}

// ErrInvalidConfig is returned (wrapped) by New when a required dependency
// is missing or invalid.
var ErrInvalidConfig = errors.New("marketdata: invalid manager config")

// New constructs a Manager from cfg, validating that every required
// dependency is present. It returns a wrapped ErrInvalidConfig if the
// clock, store root, resolver, or provider name is missing. New performs
// no I/O and starts no goroutines.
//
// New builds its own canonical store from cfg.StoreRoot when cfg.store is
// unset (the normal case for every caller outside this package, since
// cfg.store is unexported). No composition root ever supplies, replaces,
// or otherwise sees the store implementation: it is constructed here and
// held only as m.store, an unexported field with no accessor.
func New(cfg Config) (*Manager, error) {
	if cfg.Clock == nil {
		return nil, fmt.Errorf("marketdata: new manager: %w: clock is required", ErrInvalidConfig)
	}
	if cfg.StoreRoot == "" {
		return nil, fmt.Errorf("marketdata: new manager: %w: store root is required", ErrInvalidConfig)
	}
	if cfg.Resolver == nil {
		return nil, fmt.Errorf("marketdata: new manager: %w: resolver is required", ErrInvalidConfig)
	}
	if cfg.ProviderName == "" {
		return nil, fmt.Errorf("marketdata: new manager: %w: provider name is required", ErrInvalidConfig)
	}

	store := cfg.store
	if store == nil {
		store = newCanonicalCSVStore(cfg.StoreRoot)
	}

	return &Manager{
		clock:        cfg.Clock,
		storeRoot:    cfg.StoreRoot,
		resolver:     cfg.Resolver,
		providerName: cfg.ProviderName,
		provider:     cfg.provider,
		store:        store,
		cache:        newBarCache(cfg.CacheCapacity),
	}, nil
}

// configured reports whether m was constructed through New with its
// required dependencies. It is the explicit, tested predicate for the
// zero-value-unusable contract: the zero-value Manager, and a nil *Manager,
// are both reported as not configured rather than being allowed to
// misbehave.
func (m *Manager) configured() bool {
	return m != nil && m.clock != nil && m.storeRoot != "" &&
		m.resolver != nil && m.providerName != "" && m.store != nil
}

// Manager's first operation, Bars, is defined in query.go (issue #78):
// a read-only historical query that never downloads, rebuilds, or
// otherwise mutates the canonical store. Acquisition and canonical-build
// commands remain future, separate operations — see the read-versus-
// mutation split documented on the Manager type above.
//
// Earlier drafts carried placeholder Sync(ctx) and Build(ctx) methods that
// returned an ErrNotImplemented sentinel. They were removed in response to
// the M2-03 architectural review: Manager's operations must not be frozen
// speculatively. Their real inputs and result contracts depend on the
// still-unresolved M2-01 / ADR-020 query, coverage, and
// canonical-persistence decisions, so the operations — and the
// ErrNotImplemented / ErrNotConfigured sentinels that existed only to serve
// those placeholders — are introduced with their real use cases, not here.
