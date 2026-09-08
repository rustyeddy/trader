package data

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/cmd/trader/internal/clictx"
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
	storeRoot     string
	rawRoot       string
	provider      string
	oandaBaseURL  string
	alpacaBaseURL string
}

// datasetConfig is the typed configuration a *marketdata.Manager is
// built from. StoreRoot and RawRoot are both optional here: neither
// carries a required:"true" tag, so config.Load never rejects an
// invocation that supplies neither. buildDataContext (issue #141)
// fills an empty StoreRoot/RawRoot with a computed per-user default
// after Load returns, rather than a static struct-tag default —
// config's own default:"value" tag is a literal string with no
// path/home-directory expansion, and the default here depends on
// $HOME and (for RawRoot) the resolved Provider.
//
// OANDAToken/OANDABaseURL are both optional here for the identical
// reason: only Sync (and Update, when it needs to Sync) actually
// requires them, and *marketdata.Manager itself already reports a
// clear ErrInvalidConfig error — for a missing credential, or for one
// supplied without the other — when a command that actually needs
// them is run without them configured. #110's own commands don't
// second-guess that either.
//
// OANDAToken deliberately has no corresponding CLI flag (see
// datasetFlags and buildDatasetConfig below): a --oanda-token flag
// would put the secret in shell history and in the process command
// line (visible via ps, /proc/<pid>/cmdline, process monitors),
// defeating the care config's own secret:"true" tag takes elsewhere.
// TRADER_OANDA_TOKEN (the environment variable this field's
// config/env naming convention derives) is the only way to supply it
// today; a credential-file or keyring mechanism is a reasonable
// future addition if that ever proves insufficient, but is not
// invented speculatively here.
//
// AlpacaKeyID/AlpacaSecretKey follow OANDAToken's identical reasoning
// and pattern (issue #331): no CLI flag, secret:"true", supplied only
// via TRADER_ALPACA_KEY_ID/TRADER_ALPACA_SECRET_KEY (or a Trader-owned
// --config YAML file using this same dotted config-key naming — see
// config.Options.FilePath). This is deliberately the one and only
// credential source, for consistency with OANDA's own CLI story: this
// package does not additionally read a third-party tool's own profile
// file format (for example a local Alpaca CLI's own
// ~/.config/alpaca/profiles/*.yaml, api_key/secret_key keys) — that
// would give Alpaca a second, bespoke credential-sourcing mechanism
// no other provider has, coupling this repository's CLI to another
// tool's file schema as an implicit contract. An operator who already
// has such a file can export its two values as
// TRADER_ALPACA_KEY_ID/TRADER_ALPACA_SECRET_KEY in their shell profile.
// (The marketdata package's own opt-in "alpacasmoke" smoke tests read
// that file directly — see marketdata/alpaca_smoke_test.go — but only
// because they cannot use environment variables at all,
// config/arch_test.go's TestDomainPackagesDoNotReadEnvOrFlags
// forbidding os.Getenv outside config/cmd/test; this package has no
// such restriction and uses the standard, already-established config
// mechanism instead.)
type datasetConfig struct {
	StoreRoot       string `config:"store_root" flag:"store-root"`
	RawRoot         string `config:"raw_root" flag:"raw-root"`
	Provider        string `config:"provider" flag:"provider" default:"oanda"`
	OANDAToken      string `config:"oanda_token" secret:"true"`
	OANDABaseURL    string `config:"oanda_base_url" flag:"oanda-base-url"`
	AlpacaKeyID     string `config:"alpaca_key_id" secret:"true"`
	AlpacaSecretKey string `config:"alpaca_secret_key" secret:"true"`
	AlpacaBaseURL   string `config:"alpaca_base_url" flag:"alpaca-base-url" default:"https://data.alpaca.markets"`
}

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
	if cmd.Flags().Changed("provider") {
		overrides["provider"] = flags.provider
	}
	if cmd.Flags().Changed("oanda-base-url") {
		overrides["oanda-base-url"] = flags.oandaBaseURL
	}
	if cmd.Flags().Changed("alpaca-base-url") {
		overrides["alpaca-base-url"] = flags.alpacaBaseURL
	}

	return config.Load[datasetConfig](config.Options{
		EnvPrefix: clictx.EnvPrefix,
		Environ:   os.Environ(),
		Overrides: overrides,
	})
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

// alpacaKeyIDSecretCredential satisfies
// marketdata.Config.AlpacaCredential's alpaca.CredentialProvider
// interface structurally (Credentials(ctx) (string, string, error))
// without importing marketdata/internal — the identical technique
// oandaTokenCredential uses above, and marketdata/alpaca_smoke_test.go's
// own alpacaSmokeCredential test seam uses independently (issue #331).
type alpacaKeyIDSecretCredential struct {
	keyID, secretKey string
}

func (c alpacaKeyIDSecretCredential) Credentials(context.Context) (string, string, error) {
	return c.keyID, c.secretKey, nil
}

// alpacaCalendarYears returns a generous, forward-and-backward year
// range for marketdata.StandardUSEquityHolidays, computed from the
// real wall clock. This file is a composition root — exactly where
// the architecture document says time.Now belongs, unlike marketdata's
// own deterministic core — so calling it once here, to size a holiday
// table, is not the "hidden time.Now deep in domain code" problem that
// rule guards against. A wide window (30 years back, 5 forward) means
// the CLI does not need to be redeployed merely because a calendar
// year rolled over.
func alpacaCalendarYears() []int {
	now := clock.Real{}.Now().UTC().Year()
	years := make([]int, 0, 36)
	for y := now - 30; y <= now+5; y++ {
		years = append(years, y)
	}
	return years
}

// calendarForProvider selects the marketdata.Calendar implementation
// syncOneAlpaca/syncOneOANDA each actually require (issue #331):
// leaving it nil for an FX provider lets Manager apply its own correct
// default (NewFXCalendar(FXCalendarParams{})), but "alpaca" (and any
// future non-FX provider — see fxProviders' identical two-way split in
// args.go) needs a *USEquityCalendar specifically, or Sync fails with
// Manager's own "requires Config.Calendar to be a *USEquityCalendar"
// ErrInvalidConfig every time (a real gap found and fixed while
// developing this issue: buildDataContext never set Calendar at all
// before).
func calendarForProvider(provider string) marketdata.Calendar {
	if fxProviders[provider] {
		return nil
	}
	return marketdata.NewUSEquityCalendar(marketdata.StandardUSEquityHolidays(alpacaCalendarYears()...))
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
	if err := applyDefaultDataRoots(&cfg); err != nil {
		return dataContext{}, err
	}

	resolver := instrument.NewMemoryResolver()
	managerCfg := marketdata.Config{
		Clock:        clock.Real{},
		StoreRoot:    cfg.StoreRoot,
		RawRoot:      cfg.RawRoot,
		Resolver:     resolver,
		ProviderName: cfg.Provider,
		Calendar:     calendarForProvider(cfg.Provider),
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

	// AlpacaCredential and AlpacaBaseURL are set together, only when
	// both AlpacaKeyID and AlpacaSecretKey are supplied — unlike
	// OANDABaseURL above, AlpacaBaseURL carries a non-empty default
	// (datasetConfig's own "https://data.alpaca.markets" tag), so
	// forwarding it unconditionally would leave managerCfg.AlpacaBaseURL
	// non-empty even when no Alpaca credential was ever configured,
	// which would trip Manager's own "credential and base URL must be
	// supplied together" check for every command run against a
	// different provider (a real regression caught by
	// vertical_slice_test.go's OANDA-only scenarios while developing
	// this).
	//
	// A one-sided pair (exactly one of KeyID/SecretKey set) is
	// rejected explicitly here, rather than silently falling through
	// to the both-empty case: PR #334 review correctly pointed out
	// that letting Manager's later, generic "Alpaca credential/base
	// URL is not configured" Sync-time error stand in for this case
	// hides the operator's actual mistake — they *did* configure
	// something, just not both halves of it. This mirrors #331's own
	// acceptance criterion (an incomplete pair must be classifiable
	// and clear, the same as OANDA's construction-time "must be
	// supplied together" check), surfaced at config-resolution time
	// rather than only at Sync time.
	switch {
	case cfg.AlpacaKeyID != "" && cfg.AlpacaSecretKey != "":
		managerCfg.AlpacaCredential = alpacaKeyIDSecretCredential{keyID: cfg.AlpacaKeyID, secretKey: cfg.AlpacaSecretKey}
		managerCfg.AlpacaBaseURL = cfg.AlpacaBaseURL
	case cfg.AlpacaKeyID != "" || cfg.AlpacaSecretKey != "":
		return dataContext{}, fmt.Errorf(
			"%w: Alpaca key ID and secret key must both be supplied together (set both TRADER_ALPACA_KEY_ID and TRADER_ALPACA_SECRET_KEY)",
			marketdata.ErrInvalidConfig)
	}

	manager, err := marketdata.New(managerCfg)
	if err != nil {
		return dataContext{}, err
	}

	service, err := svc.New(manager, clictx.LoggerFromContext(cmd.Context()))
	if err != nil {
		return dataContext{}, err
	}

	return dataContext{Service: service, Resolver: resolver, Provider: cfg.Provider}, nil
}
