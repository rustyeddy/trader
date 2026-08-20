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
	storeRoot    string
	rawRoot      string
	provider     string
	oandaToken   string
	oandaBaseURL string
}

// datasetConfig is the typed configuration a *marketdata.Manager is
// built from. StoreRoot is required: every data command needs
// somewhere to read (and, for #110's mutating commands, publish)
// canonical data. RawRoot is optional here at the framework level —
// Bars never needs it — even though Coverage and Plan will report a
// clear error from Manager itself if it is empty when they need it;
// #109's own leaf commands don't second-guess that.
//
// OANDAToken/OANDABaseURL are both optional here for the identical
// reason: only Sync (and Update, when it needs to Sync) actually
// requires them, and *marketdata.Manager itself already reports a
// clear ErrInvalidConfig error — for a missing credential, or for one
// supplied without the other — when a command that actually needs
// them is run without them configured. #110's own commands don't
// second-guess that either.
type datasetConfig struct {
	StoreRoot    string `config:"store_root" flag:"store-root" required:"true"`
	RawRoot      string `config:"raw_root" flag:"raw-root"`
	Provider     string `config:"provider" flag:"provider" default:"oanda"`
	OANDAToken   string `config:"oanda_token" flag:"oanda-token" secret:"true"`
	OANDABaseURL string `config:"oanda_base_url" flag:"oanda-base-url"`
}

// buildDatasetConfig resolves a datasetConfig from flags actually set
// on cmd, layered under the TRADER_STORE_ROOT/TRADER_RAW_ROOT/
// TRADER_PROVIDER/TRADER_OANDA_TOKEN/TRADER_OANDA_BASE_URL environment
// variables, via the same config.Load every Trader composition root
// uses (see root.go's buildLoggingConfig for the identical pattern
// this mirrors, including why only Changed flags are ever placed in
// Overrides).
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
	if cmd.Flags().Changed("oanda-token") {
		overrides["oanda-token"] = flags.oandaToken
	}
	if cmd.Flags().Changed("oanda-base-url") {
		overrides["oanda-base-url"] = flags.oandaBaseURL
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
// resolved per-request, not from a persistent catalog — see
// service/marketdata's RegisterFXInstrument), and the provider name
// every registered Listing must share with the Manager for
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

// oandaTokenCredential satisfies marketdata.Config.OANDACredential's
// oanda.CredentialProvider interface structurally
// (Token(ctx) (string, error)) without importing marketdata/internal —
// the same technique service/marketdata's own tests use, now needed
// here in production code for #110's Sync/Update commands.
type oandaTokenCredential string

func (c oandaTokenCredential) Token(context.Context) (string, error) {
	return string(c), nil
}

// buildDataContext constructs the *marketdata.Manager and Service a
// data subcommand invocation needs. The Manager's Resolver starts
// empty: nothing is registered into it until a leaf command parses its
// own INSTRUMENT argument, since instruments are resolved per-request,
// not from a persistent catalog. OANDA credentials are configured only
// when both OANDAToken and OANDABaseURL are actually supplied — see
// datasetConfig's own doc comment for why an unconfigured pair is left
// for Manager itself to reject, only when a command that needs it is
// actually run.
func buildDataContext(cmd *cobra.Command, flags datasetFlags) (dataContext, error) {
	cfg, err := buildDatasetConfig(cmd, flags)
	if err != nil {
		return dataContext{}, err
	}

	resolver := instrument.NewMemoryResolver()
	managerCfg := marketdata.Config{
		Clock:        clock.Real{},
		StoreRoot:    cfg.StoreRoot,
		RawRoot:      cfg.RawRoot,
		Resolver:     resolver,
		ProviderName: cfg.Provider,
	}
	// OANDACredential must stay a genuinely nil interface when no token
	// was supplied: oandaTokenCredential("") is a *non-nil* interface
	// value wrapping an empty string, which would silently defeat
	// Manager's own "credential and base URL must be supplied together"
	// check (comparing cfg.OANDACredential == nil) if assigned
	// unconditionally here.
	if cfg.OANDAToken != "" {
		managerCfg.OANDACredential = oandaTokenCredential(cfg.OANDAToken)
	}
	managerCfg.OANDABaseURL = cfg.OANDABaseURL

	manager, err := marketdata.New(managerCfg)
	if err != nil {
		return dataContext{}, err
	}

	service, err := svc.New(manager)
	if err != nil {
		return dataContext{}, err
	}

	return dataContext{Service: service, Resolver: resolver, Provider: cfg.Provider}, nil
}
