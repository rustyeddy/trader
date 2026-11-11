package account

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/cmd/internal/accountactivitycfg"
	"github.com/rustyeddy/trader/internal/accountactivity"
	"github.com/rustyeddy/trader/internal/clock"
)

// deps are the seams a test replaces: where configuration comes from, the
// ledger, the provider and the clock. The production values are the real
// environment, the file ledger, the registered providers and the wall clock.
type deps struct {
	environ   func() []string
	openStore func(accountactivitycfg.Config) (accountactivity.Store, error)
	newSource accountactivitycfg.SourceFactory
	clock     clock.Clock
}

func defaultDeps() deps {
	return deps{
		environ:   os.Environ,
		openStore: accountactivitycfg.OpenStore,
		newSource: func(c accountactivitycfg.Config, s accountactivity.SyncKey, b accountactivitycfg.Bounds, clk clock.Clock) (accountactivity.Source, error) {
			return accountactivitycfg.NewSource(c, s, b, clk)
		},
		clock: clock.Real{},
	}
}

// groupFlags are the persistent flags every account subcommand shares.
type groupFlags struct {
	broker, environment, account, ledgerRoot, accountCurrency, format string
}

// New returns the "account" command group.
func New() *cobra.Command { return newCommand(defaultDeps()) }

func newCommand(d deps) *cobra.Command {
	var flags groupFlags

	cmd := &cobra.Command{
		Use:   "account",
		Short: "Broker account activity and the trades reconstructed from it.",
		Long: "Sync, list and summarize broker account activity, and list or inspect the " +
			"completed trades reconstructed from it.\n\n" +
			"Every command names its stream explicitly with --environment and --account; " +
			"there is no default environment. The OANDA token is read from " +
			"TRADER_OANDA_TOKEN (or oanda_token_file), never from a flag.",
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}

	pf := cmd.PersistentFlags()
	pf.StringVar(&flags.broker, "broker", "", "broker (default oanda)")
	pf.StringVar(&flags.environment, "environment", "", "practice or live (required; no default)")
	pf.StringVar(&flags.account, "account", "", "the broker's account ID (required)")
	pf.StringVar(&flags.ledgerRoot, "ledger-root", "",
		"account-activity ledger root (default $XDG_DATA_HOME/trader/accountactivity, or ~/.local/share/trader/accountactivity)")
	pf.StringVar(&flags.accountCurrency, "account-currency", "",
		"account currency, overriding the one found in the ledger (used only for slippage cost)")
	pf.StringVar(&flags.format, "format", formatTable, "output format: "+formatTable+", "+formatJSON+" or "+formatOrg+" (a trading journal of completed trades)")

	cmd.AddCommand(newActivityCmd(d, &flags))
	cmd.AddCommand(newTradesCmd(d, &flags))
	return cmd
}

// session is what every leaf command needs once its flags are resolved.
type session struct {
	cfg       accountactivitycfg.Config
	stream    accountactivity.SyncKey
	store     accountactivity.Store
	formatter Formatter
}

// open resolves configuration for a leaf command: flags actually set win over
// the environment and config file, the stream must be named, and the format
// must be known. It opens the ledger last, so a usage error never touches disk.
func (d deps) open(cmd *cobra.Command, flags *groupFlags) (session, error) {
	overrides := map[string]string{}
	for flag, val := range map[string]string{
		"broker": flags.broker, "environment": flags.environment, "account": flags.account,
		"ledger-root": flags.ledgerRoot, "account-currency": flags.accountCurrency,
	} {
		if cmd.Flags().Changed(flag) {
			overrides[flag] = val
		}
	}
	cfg, err := accountactivitycfg.Load(d.environ(), overrides)
	if err != nil {
		return session{}, err
	}
	stream, err := cfg.Stream()
	if err != nil {
		return session{}, err
	}
	f, err := formatterFor(flags.format)
	if err != nil {
		return session{}, err
	}
	store, err := d.openStore(cfg)
	if err != nil {
		return session{}, fmt.Errorf("opening the ledger: %w", err)
	}
	return session{cfg: cfg, stream: stream, store: store, formatter: f}, nil
}
