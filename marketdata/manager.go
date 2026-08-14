package marketdata

import (
	"context"
	"errors"
	"fmt"

	"github.com/rustyeddy/trader/clock"
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
// this package and supplied at construction (see Config); provider- and
// storage-native types — OANDA rows, CSV records, filesystem paths,
// persistence layouts — never cross the Manager boundary.
//
// # Construction and lifecycle
//
// A Manager is created with New and is unusable in its zero value: its
// methods report ErrNotConfigured rather than misbehaving. Manager owns no
// background work — every operation is scoped to the context passed to it —
// so there is no Run, Start, or Stop, and construction starts no
// goroutines.
//
// This issue establishes construction, ownership, and dependency direction
// only. The read and mutation operations themselves depend on the M2-01 /
// ADR-020 query, coverage, and canonical-persistence contracts, which are
// not yet resolved; their method signatures are deliberately not frozen
// here. Until they land, the operations report ErrNotImplemented so a
// caller can never mistake an unbuilt operation for an empty-but-successful
// result.
type Manager struct {
	clock     clock.Clock
	storeRoot string

	// Collaborator seams. These are interfaces owned by this package and
	// injected through Config so that the real provider, storage,
	// normalization, and resampling implementations can live behind
	// internal boundaries without marketdata importing them (which would
	// create an import cycle, since those implementations depend on this
	// package's Bar type). They are permitted to be nil in this skeleton:
	// no operation depends on them yet.
	provider provider
	store    barStore
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
	// Manager operation.
	StoreRoot string

	// Provider and Store are optional internal collaborators. When nil,
	// Manager is still constructible; the operations that would use them
	// report ErrNotImplemented. They exist so tests and, later, the real
	// composition root can inject fakes or concrete internal
	// implementations without widening the public surface.
	Provider provider
	Store    barStore
}

// provider is the narrow internal contract for acquiring provider-native
// historical data. It is intentionally unexported and minimal in this
// skeleton: it names the seam without freezing an acquisition API that the
// unresolved M2-01 / ADR-020 contracts still govern.
type provider interface {
	// name identifies the provider for diagnostics and manifest lineage.
	name() string
}

// barStore is the narrow internal contract for reading and writing
// canonical bars. It is intentionally unexported and minimal in this
// skeleton, for the same reason as provider.
type barStore interface {
	// root reports the store's configured root, for diagnostics only.
	root() string
}

// Sentinel errors returned by Manager. They are classifiable with
// errors.Is so callers can distinguish a misconfigured Manager and a
// not-yet-built operation from a genuine data or I/O failure.
var (
	// ErrNotConfigured is returned by a method called on a zero-value or
	// otherwise unconfigured Manager (for example one built by mistake as a
	// struct literal rather than through New).
	ErrNotConfigured = errors.New("marketdata: manager is not configured")

	// ErrNotImplemented is returned by an operation whose contract depends
	// on the M2-01 / ADR-020 query, coverage, or canonical-persistence
	// decisions that have not yet landed. It is deliberately distinct from
	// a successful empty result and from an I/O error.
	ErrNotImplemented = errors.New("marketdata: manager operation not implemented")
)

// New constructs a Manager from cfg, validating that every required
// dependency is present. It returns a wrapped ErrInvalidConfig if the clock
// is nil or the store root is empty. New performs no I/O and starts no
// goroutines.
func New(cfg Config) (*Manager, error) {
	if cfg.Clock == nil {
		return nil, fmt.Errorf("marketdata: new manager: %w: clock is required", ErrInvalidConfig)
	}
	if cfg.StoreRoot == "" {
		return nil, fmt.Errorf("marketdata: new manager: %w: store root is required", ErrInvalidConfig)
	}
	return &Manager{
		clock:     cfg.Clock,
		storeRoot: cfg.StoreRoot,
		provider:  cfg.Provider,
		store:     cfg.Store,
	}, nil
}

// ErrInvalidConfig is returned (wrapped) by New when a required dependency
// is missing or invalid.
var ErrInvalidConfig = errors.New("marketdata: invalid manager config")

// configured reports whether m was constructed through New with its
// required dependencies. The zero-value Manager is not configured.
func (m *Manager) configured() bool {
	return m != nil && m.clock != nil && m.storeRoot != ""
}

// Sync is the explicit acquisition command: it obtains missing provider
// data for a requested range. It is a mutation, never triggered implicitly
// by a read.
//
// Its request and result types depend on the unresolved M2-01 / ADR-020
// contracts, so its full signature is not frozen in this issue; it reports
// ErrNotImplemented (or ErrNotConfigured on an unconfigured Manager) until
// those land.
func (m *Manager) Sync(ctx context.Context) error {
	if !m.configured() {
		return fmt.Errorf("marketdata: sync: %w", ErrNotConfigured)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("marketdata: sync: %w", err)
	}
	return fmt.Errorf("marketdata: sync: %w", ErrNotImplemented)
}

// Build is the explicit canonical-build command: it materializes canonical
// bars from previously acquired data and publishes them with their
// manifest. Like Sync it is a mutation, and like Sync its full signature
// waits on the M2-01 / ADR-020 contracts; it reports ErrNotImplemented (or
// ErrNotConfigured) until then.
func (m *Manager) Build(ctx context.Context) error {
	if !m.configured() {
		return fmt.Errorf("marketdata: build: %w", ErrNotConfigured)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("marketdata: build: %w", err)
	}
	return fmt.Errorf("marketdata: build: %w", ErrNotImplemented)
}
