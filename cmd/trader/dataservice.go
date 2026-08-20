package main

import (
	"context"
	"os"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/config"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	svc "github.com/rustyeddy/trader/service/marketdata"
)

// datasetFlags holds the "data" command group's persistent flag
// values, backing the *marketdata.Manager every data subcommand
// (#109-#110) shares. Cobra flag names are chosen for CLI readability;
// buildDatasetConfig is the one place that maps them onto
// datasetConfig's own field names, the same split root.go's rootFlags/
// buildLoggingConfig already established for logging.
type datasetFlags struct {
	storeRoot string
	rawRoot   string
	provider  string
}

// datasetConfig is the typed configuration a *marketdata.Manager is
// built from. StoreRoot is required: every data command needs
// somewhere to read (and, for #110's mutating commands, publish)
// canonical data. RawRoot is optional here at the framework level —
// Bars never needs it — even though Coverage and Plan will report a
// clear error from Manager itself if it is empty when they need it;
// #109's own leaf commands don't second-guess that.
type datasetConfig struct {
	StoreRoot string `config:"store_root" flag:"store-root" required:"true"`
	RawRoot   string `config:"raw_root" flag:"raw-root"`
	Provider  string `config:"provider" flag:"provider" default:"oanda"`
}

// buildDatasetConfig resolves a datasetConfig from flags actually set
// on cmd, layered under the TRADER_STORE_ROOT/TRADER_RAW_ROOT/
// TRADER_PROVIDER environment variables, via the same config.Load every
// Trader composition root uses (see root.go's buildLoggingConfig for
// the identical pattern this mirrors, including why only Changed
// flags are ever placed in Overrides).
func buildDatasetConfig(cmd *cobra.Command, flags datasetFlags) (datasetConfig, error) {
	overrides := map[string]string{}
	if cmd.Flags().Changed("store-root") {
		overrides["store-root"] = flags.storeRoot
	}
	if cmd.Flags().Changed("raw-root") {
		overrides["raw-root"] = flags.rawRoot
	}
	if cmd.Flags().Changed("provider") {
		overrides["provider"] = flags.provider
	}

	return config.Load[datasetConfig](config.Options{
		EnvPrefix: envPrefix,
		Environ:   os.Environ(),
		Overrides: overrides,
	})
}

// dataContext is what data.go's PersistentPreRunE attaches to the
// command context for every data subcommand to use: the service
// boundary itself, the resolver leaf commands register the requested
// instrument's Listing into before calling it (instruments are
// resolved per-request, not from a persistent catalog -- see
// dataargs.go's parseFXListing), and the provider name every
// registered Listing must share with the Manager for
// ResolveInstrument's lookup to ever match.
type dataContext struct {
	Service  *svc.Service
	Resolver *instrument.MemoryResolver
	Provider string
}

type dataContextKey struct{}

func withDataContext(ctx context.Context, dc dataContext) context.Context {
	return context.WithValue(ctx, dataContextKey{}, dc)
}

func dataContextFrom(ctx context.Context) (dataContext, bool) {
	dc, ok := ctx.Value(dataContextKey{}).(dataContext)
	return dc, ok
}

// buildDataContext constructs the *marketdata.Manager and Service a
// data subcommand invocation needs. The Manager's Resolver starts
// empty: nothing is registered into it until a leaf command parses its
// own INSTRUMENT argument, since #109's scope is proving the read
// commands, not building a persistent instrument catalog.
func buildDataContext(cmd *cobra.Command, flags datasetFlags) (dataContext, error) {
	cfg, err := buildDatasetConfig(cmd, flags)
	if err != nil {
		return dataContext{}, err
	}

	resolver := instrument.NewMemoryResolver()
	manager, err := marketdata.New(marketdata.Config{
		Clock:        clock.Real{},
		StoreRoot:    cfg.StoreRoot,
		RawRoot:      cfg.RawRoot,
		Resolver:     resolver,
		ProviderName: cfg.Provider,
	})
	if err != nil {
		return dataContext{}, err
	}

	service, err := svc.New(manager)
	if err != nil {
		return dataContext{}, err
	}

	return dataContext{Service: service, Resolver: resolver, Provider: cfg.Provider}, nil
}
