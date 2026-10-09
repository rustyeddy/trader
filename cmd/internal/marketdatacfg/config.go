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
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

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
// config/env naming convention derives) supplies it directly; or
// oanda_token_file (issue #480) names a file holding it, so a default
// config file can say where the secret lives without containing it.
//
// AlpacaKeyID/AlpacaSecretKey follow OANDAToken's identical reasoning
// and pattern (issue #331): no CLI flag, secret:"true", supplied only
// via TRADER_ALPACA_KEY_ID/TRADER_ALPACA_SECRET_KEY. Load reads no
// config file, and neither consuming binary has a --config flag for
// this configuration. This is deliberately the one and only
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
	StoreRoot   string `config:"store_root" flag:"store-root"`
	RawRoot     string `config:"raw_root" flag:"raw-root"`
	ArchiveRoot string `config:"archive_root" flag:"archive-root"`
	Provider    string `config:"provider" flag:"provider" default:"oanda"`
	OANDAToken  string `config:"oanda_token" secret:"true"`
	// OANDATokenFile names a file holding the OANDA token (a leading ~/ is
	// expanded), for a default config file that must not contain the
	// secret itself. TRADER_OANDA_TOKEN, when set, wins over it. Load
	// reads the file into OANDAToken (issue #480).
	OANDATokenFile  string `config:"oanda_token_file"`
	OANDABaseURL    string `config:"oanda_base_url" flag:"oanda-base-url"`
	AlpacaKeyID     string `config:"alpaca_key_id" secret:"true"`
	AlpacaSecretKey string `config:"alpaca_secret_key" secret:"true"`
	AlpacaBaseURL   string `config:"alpaca_base_url" flag:"alpaca-base-url" default:"https://data.alpaca.markets"`
}

// DefaultConfigPath is the default config file (issue #480): the
// lowest-precedence source for every Config key, so per-machine values
// (data roots, the OANDA base URL, the OANDA token file) are set once.
// TRADER_CONFIG names a different file, which then must exist; a missing
// default file is simply not used. It is a variable so tests can point it
// somewhere hermetic.
var DefaultConfigPath = "/etc/trader/config.yml"

// ConfigPathEnv is the environment variable naming the config file.
const ConfigPathEnv = EnvPrefix + "_CONFIG"

// Load resolves a Config from the default config file, environ
// (TRADER_* variables) and overrides, keyed by flag name, in increasing
// precedence, via the same config.Load every Trader composition root
// uses. A nil environ reads the real process environment; tests pass an
// explicit, possibly empty, slice. Credentials have no flag and come from
// the environment or, for the OANDA token, a token file (see Config).
func Load(environ []string, overrides map[string]string) (Config, error) {
	if environ == nil {
		environ = os.Environ()
	}
	path, err := configFile(environ)
	if err != nil {
		return Config{}, err
	}
	if path != "" {
		if err := rejectCredentialsInFile(path); err != nil {
			return Config{}, err
		}
	}
	cfg, err := config.Load[Config](config.Options{
		EnvPrefix: EnvPrefix,
		Environ:   environ,
		FilePath:  path,
		Overrides: overrides,
	})
	if err != nil {
		return cfg, err
	}
	if cfg.OANDAToken == "" && cfg.OANDATokenFile != "" {
		token, err := readTokenFile(cfg.OANDATokenFile)
		if err != nil {
			return cfg, fmt.Errorf("oanda_token_file: %w", err)
		}
		cfg.OANDAToken = token
	}
	return cfg, nil
}

// configFile returns the config file to read, or "" for none: the file
// TRADER_CONFIG names (an error if it does not exist), else
// DefaultConfigPath when it exists.
func configFile(environ []string) (string, error) {
	prefix := ConfigPathEnv + "="
	for i := len(environ) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(environ[i], prefix); ok && v != "" {
			if _, err := os.Stat(v); err != nil {
				return "", fmt.Errorf("%s: %w", ConfigPathEnv, err)
			}
			return v, nil
		}
	}
	if DefaultConfigPath == "" {
		return "", nil
	}
	if _, err := os.Stat(DefaultConfigPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("default config %s: %w", DefaultConfigPath, err)
	}
	return DefaultConfigPath, nil
}

// fileForbiddenKeys are credentials the config file must never carry: the
// generic loader would happily read them from YAML, so they are rejected
// by name. The OANDA token reaches a run through oanda_token_file or
// TRADER_OANDA_TOKEN, Alpaca's credentials through the environment only.
var fileForbiddenKeys = []string{"oanda_token", "alpaca_key_id", "alpaca_secret_key"}

// rejectCredentialsInFile errors if the top level of the YAML file at path
// sets a forbidden credential key. The error names the key and the file,
// never a value.
func rejectCredentialsInFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config %s: %w", path, err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil // the generic loader reports malformed YAML itself
	}
	for _, key := range fileForbiddenKeys {
		if _, ok := doc[key]; ok {
			return fmt.Errorf("config %s: %s must not be set in a config file (use oanda_token_file or the environment)", path, key)
		}
	}
	return nil
}

// readTokenFile reads a secret from path, expanding a leading ~/ . Errors
// name the path, never the contents.
func readTokenFile(path string) (string, error) {
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand %s: %w", path, err)
		}
		path = filepath.Join(home, rest)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	return token, nil
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

// calendarForProvider builds the marketdata.Calendar the provider's
// registered CalendarKind names (marketdata.LookupProvider, issue #441).
// An FX calendar is left nil so Manager applies its own default
// (NewFXCalendar(FXCalendarParams{})); a US-equity provider needs a
// *USEquityCalendar, or Sync and normalization reject its bars (issue
// #331).
func calendarForProvider(info marketruntime.ProviderInfo) (marketdata.Calendar, error) {
	switch info.Calendar {
	case marketruntime.CalendarFX:
		return nil, nil
	case marketruntime.CalendarUSEquity:
		return marketdata.NewUSEquityCalendar(marketdata.StandardUSEquityHolidays(alpacaCalendarYears()...)), nil
	default:
		return nil, fmt.Errorf("provider %q: no calendar for %s", info.Name, info.Calendar)
	}
}

// Bundle is one constructed market-data service: the application
// service, the (initially empty) resolver its Manager resolves
// instruments through, and the provider and archive root it was built
// for. Instruments are registered into Resolver per request.
type Bundle struct {
	Service *svc.Service
	// Manager is the Service's Manager, for composition that needs it
	// directly (a backtest reads bars through it; service/backtest takes
	// it). Transports never receive it (ADR-068).
	Manager     *marketruntime.Manager
	Resolver    *instrument.MemoryResolver
	Provider    string
	ArchiveRoot string
	// writeLock is the lock shared with other Bundles over the same
	// stores, or nil (see newBundle).
	writeLock *marketruntime.WriteLock
}

// New constructs the *marketdata.Manager and Service for cfg. cfg's roots
// should already be resolved (ApplyDefaultRoots). The Manager's Resolver
// starts empty. OANDA credentials are configured only when a token is
// supplied, and Alpaca credentials only when both halves are; a one-sided
// Alpaca pair is rejected here, with a message naming the missing
// variables but never their values.
func New(cfg Config, logger *slog.Logger) (Bundle, error) {
	return newBundle(cfg, logger, nil)
}

// ErrUnknownProvider reports a provider with no registered metadata
// (marketdata.LookupProvider); it is marketdata.ErrUnknownProvider.
var ErrUnknownProvider = marketruntime.ErrUnknownProvider

// newBundle is New with the write lock the Manager shares with others
// over the same stores; nil gives the Manager its own (one Manager per
// process, as in the CLI).
func newBundle(cfg Config, logger *slog.Logger, writeLock *marketruntime.WriteLock) (Bundle, error) {
	info, err := marketruntime.LookupProvider(cfg.Provider)
	if err != nil {
		return Bundle{}, err
	}
	calendar, err := calendarForProvider(info)
	if err != nil {
		return Bundle{}, err
	}
	resolver := instrument.NewMemoryResolver()
	managerCfg := marketruntime.Config{
		Clock:        clock.Real{},
		StoreRoot:    cfg.StoreRoot,
		RawRoot:      cfg.RawRoot,
		Resolver:     resolver,
		ProviderName: cfg.Provider,
		Calendar:     calendar,
		WriteLock:    writeLock,
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

	service, err := svc.New(manager, logger, svc.WithResolver(resolver), svc.WithArchiveRoot(cfg.ArchiveRoot))
	if err != nil {
		return Bundle{}, err
	}

	return Bundle{Service: service, Manager: manager, Resolver: resolver, Provider: cfg.Provider, ArchiveRoot: cfg.ArchiveRoot, writeLock: writeLock}, nil
}
