package data

import (
	"context"
	"os"
	"path/filepath"

	"github.com/rustyeddy/trader/cmd/internal/marketdatacfg"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/cmd/trader/internal/clictx"
	"github.com/rustyeddy/trader/instrument"
	svc "github.com/rustyeddy/trader/internal/service/marketdata"
)

// datasetFlags holds the "data" command group's persistent flag
// values, backing the *marketdata.Manager every data subcommand
// (#109-#110) shares. Cobra flag names are chosen for CLI readability;
// buildDatasetConfig is the one place that maps them onto
// datasetConfig's own field names, the same split root.go's rootFlags/
// buildLoggingConfig already established for logging.
type datasetFlags struct {
	storeRoot     string
	rawRoot       string
	archiveRoot   string
	provider      string
	oandaBaseURL  string
	alpacaBaseURL string
}

// datasetConfig is the shared market-data configuration
// (marketdatacfg.Config, issue #433): the CLI and cmd/trader-mcp resolve
// it identically. See its doc comment for every field's semantics,
// including why credentials have no flag.
type datasetConfig = marketdatacfg.Config

// buildDatasetConfig resolves a datasetConfig from flags actually set
// on cmd, layered under the TRADER_STORE_ROOT/TRADER_RAW_ROOT/
// TRADER_PROVIDER/TRADER_OANDA_TOKEN/TRADER_OANDA_BASE_URL/
// TRADER_ALPACA_KEY_ID/TRADER_ALPACA_SECRET_KEY/TRADER_ALPACA_BASE_URL
// environment variables, via the same config.Load every Trader
// composition root uses (see root.go's buildLoggingConfig for the
// identical pattern this mirrors, including why only Changed flags
// are ever placed in Overrides). AlpacaKeyID/AlpacaSecretKey have no
// flag to check Changed against, for the identical reason OANDAToken
// does not — see datasetConfig's own doc comment — so both are
// resolved from the environment/config source only, never from
// Overrides.
func buildDatasetConfig(cmd *cobra.Command, flags datasetFlags) (datasetConfig, error) {
	overrides := map[string]string{}
	if cmd.Flags().Changed("store-root") {
		overrides["store-root"] = flags.storeRoot
	}
	if cmd.Flags().Changed("raw-root") {
		overrides["raw-root"] = flags.rawRoot
	}
	if cmd.Flags().Changed("archive-root") {
		overrides["archive-root"] = flags.archiveRoot
	}
	if cmd.Flags().Changed("provider") {
		overrides["provider"] = flags.provider
	}
	if cmd.Flags().Changed("oanda-base-url") {
		overrides["oanda-base-url"] = flags.oandaBaseURL
	}
	if cmd.Flags().Changed("alpaca-base-url") {
		overrides["alpaca-base-url"] = flags.alpacaBaseURL
	}

	return marketdatacfg.Load(os.Environ(), overrides)
}

// dataContext is what command.go's PersistentPreRunE attaches to the
// command context for every data subcommand to use: the service
// boundary itself, the resolver leaf commands register the requested
// instrument's Listing into before calling it (instruments are
// resolved per-request, not from a persistent catalog — see
// service/marketdata's RegisterFXInstrument), and the provider name
// every registered Listing must share with the Manager for
// ResolveInstrument's lookup to ever match.
type dataContext struct {
	Service     *svc.Service
	Resolver    *instrument.MemoryResolver
	Provider    string
	ArchiveRoot string
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
// own INSTRUMENT argument, since instruments are resolved per-request,
// not from a persistent catalog. OANDA credentials are configured only
// when both OANDAToken and OANDABaseURL are actually supplied — see
// datasetConfig's own doc comment for why an unconfigured pair is left
// for Manager itself to reject, only when a command that needs it is
// actually run.
//
// The Service is given the same logger root.go's own PersistentPreRunE
// already built and placed on cmd.Context() (clictx.LoggerFromContext) — not
// a second, independently constructed one — so every structured record
// a data subcommand's use case emits (issue #128) shares this
// invocation's own level/format/output configuration.
func buildDataContext(cmd *cobra.Command, flags datasetFlags) (dataContext, error) {
	cfg, err := buildDatasetConfig(cmd, flags)
	if err != nil {
		return dataContext{}, err
	}
	if err := marketdatacfg.ApplyDefaultRoots(&cfg); err != nil {
		return dataContext{}, err
	}
	if cmd.Name() == "stq2bars" && cfg.Provider == "oanda" && os.Getenv("TRADER_PROVIDER") == "" && !cmd.Flags().Changed("provider") {
		cfg.Provider = "stooq"
	}
	if cmd.Name() == "stq2bars" && cfg.ArchiveRoot == "" {
		dataDir, err := marketdatacfg.DefaultDataDir()
		if err != nil {
			return dataContext{}, err
		}
		cfg.ArchiveRoot = filepath.Join(dataDir, "archive", cfg.Provider)
	}

	bundle, err := marketdatacfg.New(cfg, clictx.LoggerFromContext(cmd.Context()))
	if err != nil {
		return dataContext{}, err
	}
	return dataContext{Service: bundle.Service, Resolver: bundle.Resolver, Provider: bundle.Provider, ArchiveRoot: bundle.ArchiveRoot}, nil
}
