// Package marketdatacfg is the shared composition of Trader's market-data
// service for its binaries (issue #433): the typed configuration and its
// TRADER_* environment resolution, default data roots, provider
// credentials, and construction of an internal/marketdata.Manager and its
// application service.
//
// Both cmd/trader (the "data" commands) and cmd/trader-mcp build their
// market-data service through this package, so the two transports share
// one definition of configuration, defaults, and wiring instead of
// duplicating it. It is a composition-root package: it may read the
// environment and the wall clock, which domain packages may not
// (internal/config/arch_test.go).
package marketdatacfg

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/clock"
	"github.com/rustyeddy/trader/internal/config"
	marketruntime "github.com/rustyeddy/trader/internal/marketdata"
	svc "github.com/rustyeddy/trader/internal/service/marketdata"
	"github.com/rustyeddy/trader/marketdata"
)

// EnvPrefix is the environment-variable prefix for every Trader binary's
// configuration (config/doc.go): TRADER_STORE_ROOT, TRADER_PROVIDER, ...
const EnvPrefix = "TRADER"

// Config is the typed configuration a *marketdata.Manager is
// built from. StoreRoot and RawRoot are both optional here: neither
// carries a required:"true" tag, so config.Load never rejects an
// invocation that supplies neither. ApplyDefaultRoots (issue #141)
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
// the CLI's datasetFlags and buildDatasetConfig): a --oanda-token flag
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
type Config struct {
	StoreRoot       string `config:"store_root" flag:"store-root"`
	RawRoot         string `config:"raw_root" flag:"raw-root"`
	ArchiveRoot     string `config:"archive_root" flag:"archive-root"`
	Provider        string `config:"provider" flag:"provider" default:"oanda"`
	OANDAToken      string `config:"oanda_token" secret:"true"`
	OANDABaseURL    string `config:"oanda_base_url" flag:"oanda-base-url"`
	AlpacaKeyID     string `config:"alpaca_key_id" secret:"true"`
	AlpacaSecretKey string `config:"alpaca_secret_key" secret:"true"`
	AlpacaBaseURL   string `config:"alpaca_base_url" flag:"alpaca-base-url" default:"https://data.alpaca.markets"`
}

// Load resolves a Config from environ (TRADER_* variables) layered under
// overrides, keyed by flag name, via the same config.Load every Trader
// composition root uses. Credentials have no flag and come from the
// environment only (see Config).
func Load(environ []string, overrides map[string]string) (Config, error) {
	return config.Load[Config](config.Options{
		EnvPrefix: EnvPrefix,
		Environ:   environ,
		Overrides: overrides,
	})
}

// fxProviders names the providers whose instruments are FX pairs.
// "oanda" is Trader's only FX provider today; every other provider is
// treated as an equity/ETF provider.
var fxProviders = map[string]bool{"oanda": true}

// IsFXProvider reports whether provider's instruments are FX pairs.
func IsFXProvider(provider string) bool { return fxProviders[provider] }

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
// future non-FX provider — see IsFXProvider's two-way split) needs a *USEquityCalendar specifically, or Sync fails with
// Manager's own "requires Config.Calendar to be a *USEquityCalendar"
// ErrInvalidConfig every time (a real gap found and fixed while
// developing this issue: buildDataContext never set Calendar at all
// before).
func calendarForProvider(provider string) marketdata.Calendar {
	if IsFXProvider(provider) {
		return nil
	}
	return marketdata.NewUSEquityCalendar(marketdata.StandardUSEquityHolidays(alpacaCalendarYears()...))
}

// Bundle is one constructed market-data service: the application
// service, the (initially empty) resolver its Manager resolves
// instruments through, and the provider and archive root it was built
// for. Instruments are registered into Resolver per request.
type Bundle struct {
	Service     *svc.Service
	Resolver    *instrument.MemoryResolver
	Provider    string
	ArchiveRoot string
}

// New constructs the *marketdata.Manager and Service for cfg. cfg's roots
// should already be resolved (ApplyDefaultRoots). The Manager's Resolver
// starts empty. OANDA credentials are configured only when a token is
// supplied, and Alpaca credentials only when both halves are; a one-sided
// Alpaca pair is rejected here, with a message naming the missing
// variables but never their values.
func New(cfg Config, logger *slog.Logger) (Bundle, error) {
	resolver := instrument.NewMemoryResolver()
	managerCfg := marketruntime.Config{
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
		return Bundle{}, fmt.Errorf(
			"%w: Alpaca key ID and secret key must both be supplied together (set both TRADER_ALPACA_KEY_ID and TRADER_ALPACA_SECRET_KEY)", marketruntime.
				ErrInvalidConfig)
	}

	manager, err := marketruntime.New(managerCfg)
	if err != nil {
		return Bundle{}, err
	}

	service, err := svc.New(manager, logger)
	if err != nil {
		return Bundle{}, err
	}

	return Bundle{Service: service, Resolver: resolver, Provider: cfg.Provider, ArchiveRoot: cfg.ArchiveRoot}, nil
}
