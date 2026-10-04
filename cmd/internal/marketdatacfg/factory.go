package marketdatacfg

import (
	"fmt"
	"log/slog"
	"path/filepath"

	marketruntime "github.com/rustyeddy/trader/internal/marketdata"
	svc "github.com/rustyeddy/trader/internal/service/marketdata"
)

// KnownProviders returns the supported provider names, sorted: the
// registered providers (marketdata.ProviderNames).
func KnownProviders() []string { return marketruntime.ProviderNames() }

// Factory builds a market-data Service for any provider from one base
// configuration (issue #433). A Manager is bound to one provider, so a
// transport serving requests for several providers (cmd/trader-mcp)
// asks the Factory for each request's provider and gets a fresh
// Service, with its own empty resolver, per call. The Service owns its
// resolver and archive root (issue #448), so a transport never sees
// either.
//
// Roots are resolved per provider. StoreRoot (canonical data) is
// shared. An explicitly configured RawRoot or ArchiveRoot applies only
// to the base Provider, because raw data and downloaded archives are
// provider-specific; every other provider gets its own default under
// DefaultDataDir (raw/<provider>, archive/<provider>).
//
// Every Manager the Factory builds for a provider shares one
// marketdata.WriteLock: the per-request Services all write the same
// stores, and their writes must not overlap (a raw extend is a
// read-modify-write; see marketdata.WriteLock).
type Factory struct {
	base   Config
	logger *slog.Logger
	locks  map[string]*marketruntime.WriteLock // per provider; fixed at construction
}

// NewFactory returns a Factory over base, an unresolved Config as Load
// returns it. It builds the base provider's Bundle once to fail fast on
// invalid configuration (for example a one-sided Alpaca credential).
func NewFactory(base Config, logger *slog.Logger) (*Factory, error) {
	f := &Factory{base: base, logger: logger, locks: map[string]*marketruntime.WriteLock{}}
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

// ForProvider builds the Service for provider; empty means
// DefaultProvider. An unsupported provider reports ErrUnknownProvider.
func (f *Factory) ForProvider(provider string) (*svc.Service, error) {
	b, err := f.Bundle(provider)
	if err != nil {
		return nil, err
	}
	return b.Service, nil
}

// Bundle builds the Bundle for provider (empty means DefaultProvider),
// resolving its roots and sharing the provider's write lock. It is for
// composition that needs more than the Service — a backtest reads bars
// through the Manager — and is never handed to a transport (ADR-068).
func (f *Factory) Bundle(provider string) (Bundle, error) {
	if provider == "" {
		provider = f.base.Provider
	}
	if _, err := marketruntime.LookupProvider(provider); err != nil {
		return Bundle{}, err
	}
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
