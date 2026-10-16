// Package accountactivitycfg is the composition root for the account
// activity CLI (issue #514, ADR-084): the typed configuration and its
// TRADER_* environment resolution, the default ledger root, provider
// credentials, and construction of the ledger store and a provider Source.
//
// It is the only place provider names appear in the account CLI. A provider
// is one entry in the registry below, so adding Alpaca (#516) adds an entry
// and no branching in any command handler. Like marketdatacfg it may read the
// environment and the wall clock, which domain packages may not.
package accountactivitycfg

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rustyeddy/trader/cmd/internal/marketdatacfg"
	"github.com/rustyeddy/trader/internal/accountactivity"
	"github.com/rustyeddy/trader/internal/adapters/accountactivity/filestore"
	"github.com/rustyeddy/trader/internal/adapters/accountactivity/oanda"
	"github.com/rustyeddy/trader/internal/clock"
	"github.com/rustyeddy/trader/internal/config"
	"github.com/rustyeddy/trader/num"
)

// EnvPrefix is the environment-variable prefix for every Trader binary.
const EnvPrefix = marketdatacfg.EnvPrefix

// Config is the account CLI's configuration.
//
// Environment and Account identify the stream and have no default: a mistyped
// command must not quietly read or sync a live account. The OANDA token has no
// flag and is never read from the config file (ADR-078): TRADER_OANDA_TOKEN,
// or oanda_token_file naming a file that holds it.
type Config struct {
	Broker string `config:"accountactivity_broker" flag:"broker" default:"oanda"`
	// Environment is "practice" or "live". Required for every command.
	Environment string `config:"accountactivity_environment" flag:"environment"`
	// Account is the provider's account ID. Required for every command.
	Account string `config:"accountactivity_account" flag:"account"`
	// LedgerRoot defaults to <data dir>/accountactivity.
	LedgerRoot string `config:"accountactivity_root" flag:"ledger-root"`
	// AccountCurrency overrides the currency otherwise found in the ledger.
	AccountCurrency string `config:"accountactivity_currency" flag:"account-currency"`

	OANDAToken     string `config:"oanda_token" secret:"true"`
	OANDATokenFile string `config:"oanda_token_file"`
}

// Load resolves a Config from the default config file, environ (TRADER_*
// variables) and overrides keyed by flag name, in increasing precedence. A nil
// environ reads the real process environment.
func Load(environ []string, overrides map[string]string) (Config, error) {
	if environ == nil {
		environ = os.Environ()
	}
	path, err := marketdatacfg.ConfigFile(environ)
	if err != nil {
		return Config{}, err
	}
	cfg, err := config.Load[Config](config.Options{
		EnvPrefix: EnvPrefix, Environ: environ, FilePath: path, Overrides: overrides,
	})
	if errors.Is(err, config.ErrSecretInFile) {
		return cfg, fmt.Errorf("config %s: %w (use the environment, or oanda_token_file for the OANDA token)", path, err)
	}
	if err != nil {
		return cfg, err
	}
	if cfg.OANDAToken == "" && cfg.OANDATokenFile != "" {
		token, err := marketdatacfg.ReadTokenFile(cfg.OANDATokenFile)
		if err != nil {
			return cfg, fmt.Errorf("oanda_token_file: %w", err)
		}
		cfg.OANDAToken = token
	}
	return cfg, nil
}

// Stream is the account stream the configuration names. It refuses an unknown
// broker, a missing or unknown environment, and a missing account, each with
// guidance, since there is deliberately no default to fall back on.
func (c Config) Stream() (accountactivity.SyncKey, error) {
	if _, ok := providers[c.Broker]; !ok {
		return accountactivity.SyncKey{}, fmt.Errorf("unknown broker %q (supported: %s)", c.Broker, strings.Join(Brokers(), ", "))
	}
	var env accountactivity.Environment
	switch strings.ToLower(strings.TrimSpace(c.Environment)) {
	case "practice":
		env = accountactivity.EnvironmentPractice
	case "live":
		env = accountactivity.EnvironmentLive
	case "":
		return accountactivity.SyncKey{}, errors.New("--environment is required: practice or live (TRADER_ACCOUNTACTIVITY_ENVIRONMENT); there is no default")
	default:
		return accountactivity.SyncKey{}, fmt.Errorf("--environment %q: want practice or live", c.Environment)
	}
	if strings.TrimSpace(c.Account) == "" {
		return accountactivity.SyncKey{}, errors.New("--account is required: the provider's account ID (TRADER_ACCOUNTACTIVITY_ACCOUNT)")
	}
	k := accountactivity.SyncKey{Provider: c.Broker, Environment: env, ProviderAccountID: strings.TrimSpace(c.Account)}
	return k, k.Validate()
}

// Currency parses AccountCurrency; the zero Currency means none was given.
func (c Config) Currency() (num.Currency, error) {
	if strings.TrimSpace(c.AccountCurrency) == "" {
		return num.Currency{}, nil
	}
	cur, err := num.ParseCurrency(strings.ToUpper(strings.TrimSpace(c.AccountCurrency)))
	if err != nil {
		return num.Currency{}, fmt.Errorf("--account-currency %q: %w", c.AccountCurrency, err)
	}
	return cur, nil
}

// Root is the ledger root: the configured one, else the per-user default
// beside the market data (XDG_DATA_HOME/trader/accountactivity).
func (c Config) Root() (string, error) {
	if c.LedgerRoot != "" {
		return c.LedgerRoot, nil
	}
	dir, err := marketdatacfg.DefaultDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "accountactivity"), nil
}

// OpenStore opens the file-backed ledger.
func OpenStore(c Config) (accountactivity.Store, error) {
	root, err := c.Root()
	if err != nil {
		return nil, err
	}
	return filestore.New(root)
}

// Bounds optionally limits an import to a time window, [From, To).
type Bounds struct{ From, To time.Time }

// SourceFactory builds a provider's Source.
type SourceFactory func(c Config, stream accountactivity.SyncKey, b Bounds, clk clock.Clock) (accountactivity.Source, error)

// providers is the registry of supported brokers. It is the one place a
// provider name appears.
var providers = map[string]SourceFactory{
	"oanda": newOANDASource,
}

// Brokers lists the supported broker names, sorted.
func Brokers() []string {
	names := make([]string, 0, len(providers))
	for n := range providers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// NewSource builds the Source for the configured broker.
func NewSource(c Config, stream accountactivity.SyncKey, b Bounds, clk clock.Clock) (accountactivity.Source, error) {
	f, ok := providers[c.Broker]
	if !ok {
		return nil, fmt.Errorf("unknown broker %q (supported: %s)", c.Broker, strings.Join(Brokers(), ", "))
	}
	return f(c, stream, b, clk)
}

type tokenCredential string

func (t tokenCredential) Token(context.Context) (string, error) { return string(t), nil }

func newOANDASource(c Config, stream accountactivity.SyncKey, b Bounds, clk clock.Clock) (accountactivity.Source, error) {
	if c.OANDAToken == "" {
		return nil, errors.New("no OANDA token: set TRADER_OANDA_TOKEN (or oanda_token_file); the token is never a flag")
	}
	base, err := oanda.BaseURL(stream.Environment)
	if err != nil {
		return nil, err
	}
	client, err := oanda.NewClient(base, &http.Client{Timeout: 60 * time.Second}, tokenCredential(c.OANDAToken))
	if err != nil {
		return nil, err
	}
	return oanda.NewSource(oanda.Config{
		Client: client, Environment: stream.Environment, AccountID: stream.ProviderAccountID,
		Bounds: oanda.Bounds{From: b.From, To: b.To}, Clock: clk,
	})
}
