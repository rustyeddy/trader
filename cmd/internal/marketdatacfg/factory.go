package marketdatacfg

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"

	marketruntime "github.com/rustyeddy/trader/internal/marketdata"
	svc "github.com/rustyeddy/trader/internal/service/marketdata"
)

// KnownProviders returns the supported provider names, sorted: the
// registered providers (marketdata.ProviderNames).
func KnownProviders() []string { return marketruntime.ProviderNames() }

// Factory provides the market-data Service for any provider from one
// base configuration (issue #433). A Manager is bound to one provider, so
// a transport serving requests for several providers (cmd/trader-mcp)
// asks the Factory for each request's provider.
//
// The Factory builds each provider's Bundle once and reuses it for its
// whole lifetime (issue #442): one Manager, resolver, and Service per
// provider, shared by every request. That is safe because:
//   - instrument registration is idempotent and deterministic (a symbol
//     always registers the same listing; MemoryResolver.RegisterOrGet,
//     #448), so a shared resolver holds exactly what fresh ones would;
//   - the Manager is safe for concurrent use: its partition cache is
//     mutex-guarded and never serves a partition whose file has changed
//     (another process's rebuild included), and its writes are serialized
//     by the provider's write lock;
//   - the Service holds no state of its own beyond those collaborators.
//
// The default provider is built eagerly by NewFactory, so configuration
// errors surface at startup; any other provider is built on first use,
// and a failed build is not cached, so its error is reported on each
// attempt. A Bundle holds no closable resources.
//
// The Service owns its resolver and archive root (issue #448), so a
// transport never sees either.
//
// Roots are resolved per provider. StoreRoot (canonical data) is
// shared. An explicitly configured RawRoot or ArchiveRoot applies only
// to the base Provider, because raw data and downloaded archives are
// provider-specific; every other provider gets its own default under
// DefaultDataDir (raw/<provider>, archive/<provider>).
//
// Each provider's Manager holds that provider's marketdata.WriteLock,
// created once here, so its writes never overlap (a raw extend is a
// read-modify-write; see marketdata.WriteLock).
type Factory struct {
	base   Config
	logger *slog.Logger
	locks  map[string]*marketruntime.WriteLock // per provider; fixed at construction

	mu      sync.Mutex
	bundles map[string]Bundle // built so far, by provider
}

// NewFactory returns a Factory over base, an unresolved Config as Load
// returns it. It builds the base provider's Bundle eagerly, to fail fast
// on invalid configuration (for example a one-sided Alpaca credential).
func NewFactory(base Config, logger *slog.Logger) (*Factory, error) {
	f := &Factory{base: base, logger: logger, locks: map[string]*marketruntime.WriteLock{}, bundles: map[string]Bundle{}}
	for _, p := range marketruntime.ProviderNames() {
		f.locks[p] = marketruntime.NewWriteLock()
	}
	if _, err := f.Bundle(base.Provider); err != nil {
		return nil, err
	}
	return f, nil
}

// DefaultProvider is the base configuration's provider (TRADER_PROVIDER).
func (f *Factory) DefaultProvider() string { return f.base.Provider }

// ForProvider returns the Service for provider, building it on first
// use; empty means DefaultProvider. An unsupported provider reports
// ErrUnknownProvider.
func (f *Factory) ForProvider(provider string) (*svc.Service, error) {
	b, err := f.Bundle(provider)
	if err != nil {
		return nil, err
	}
	return b.Service, nil
}

// Bundle returns provider's Bundle (empty means DefaultProvider),
// building it on first use and reusing it afterwards. It is for
// composition that needs more than the Service — a backtest reads bars
// through the Manager — and is never handed to a transport (ADR-068).
func (f *Factory) Bundle(provider string) (Bundle, error) {
	if provider == "" {
		provider = f.base.Provider
	}
	if _, err := marketruntime.LookupProvider(provider); err != nil {
		return Bundle{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if b, ok := f.bundles[provider]; ok {
		return b, nil
	}
	b, err := f.build(provider)
	if err != nil {
		return Bundle{}, err
	}
	f.bundles[provider] = b
	return b, nil
}

// build constructs provider's Bundle, resolving its roots.
func (f *Factory) build(provider string) (Bundle, error) {
	cfg := f.base
	if provider != f.base.Provider {
		cfg.Provider = provider
		cfg.RawRoot = ""
		cfg.ArchiveRoot = ""
	}
	if err := ApplyDefaultRoots(&cfg); err != nil {
		return Bundle{}, err
	}
	if cfg.ArchiveRoot == "" {
		dir, err := DefaultDataDir()
		if err != nil {
			return Bundle{}, err
		}
		cfg.ArchiveRoot = filepath.Join(dir, "archive", cfg.Provider)
	}
	b, err := newBundle(cfg, f.logger, f.locks[cfg.Provider])
	if err != nil {
		return Bundle{}, fmt.Errorf("market data for provider %q: %w", cfg.Provider, err)
	}
	return b, nil
}
