package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	simbroker "github.com/rustyeddy/trader/adapters/broker/sim"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/config"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	svcbroker "github.com/rustyeddy/trader/service/broker"
)

// brokerAccountConfig is the typed configuration a fresh simulated
// Broker/account is built from, resolved via config.Load the same way
// every other Trader composition-root config is (see
// dataservice.go's datasetConfig for the identical pattern).
type brokerAccountConfig struct {
	StartingCash string `config:"starting_cash" flag:"starting-cash" default:"10000"`
	Currency     string `config:"currency" flag:"currency" default:"USD"`
	AccountID    string `config:"account_id" flag:"account-id"`
}

// readBrokerAccountFlags reads brokerAccountFlags' values back off
// cmd. cmd is normally a leaf subcommand (accounts.go, snapshot.go,
// submit.go); Cobra merges an ancestor's PersistentFlags into a leaf
// command's own Flags() by the time RunE executes, so
// cmd.Flags().GetString("starting-cash") here correctly reads the
// value newBrokerCmd registered on the parent "broker" command,
// without this package needing to pass a *brokerAccountFlags pointer
// down to each leaf command constructor.
func readBrokerAccountFlags(cmd *cobra.Command) (brokerAccountFlags, error) {
	startingCash, err := cmd.Flags().GetString("starting-cash")
	if err != nil {
		return brokerAccountFlags{}, err
	}
	currency, err := cmd.Flags().GetString("currency")
	if err != nil {
		return brokerAccountFlags{}, err
	}
	accountID, err := cmd.Flags().GetString("account-id")
	if err != nil {
		return brokerAccountFlags{}, err
	}
	return brokerAccountFlags{startingCash: startingCash, currency: currency, accountID: accountID}, nil
}

// buildBrokerAccountConfig resolves a brokerAccountConfig from flags
// actually set on cmd, layered under the TRADER_STARTING_CASH/
// TRADER_CURRENCY/TRADER_ACCOUNT_ID environment variables.
func buildBrokerAccountConfig(cmd *cobra.Command, flags brokerAccountFlags) (brokerAccountConfig, error) {
	overrides := map[string]string{}
	if cmd.Flags().Changed("starting-cash") {
		overrides["starting-cash"] = flags.startingCash
	}
	if cmd.Flags().Changed("currency") {
		overrides["currency"] = flags.currency
	}
	if cmd.Flags().Changed("account-id") {
		overrides["account-id"] = flags.accountID
	}

	return config.Load[brokerAccountConfig](config.Options{
		EnvPrefix: envPrefix,
		Environ:   os.Environ(),
		Overrides: overrides,
	})
}

// noPriceSource is the simbroker.FillPriceSource used by commands that
// never submit an order (accounts, snapshot): Deps requires a non-nil
// Prices unconditionally (ADR-015), even though these commands never
// call Submit and so never actually consult it.
type noPriceSource struct{}

func (noPriceSource) Info() simbroker.ModelInfo {
	return simbroker.ModelInfo{Name: "none", Version: "v0"}
}

func (noPriceSource) Price(listing instrument.Listing, side order.Side) (num.Price, error) {
	return num.Price{}, fmt.Errorf("no fill price source configured")
}

// cliPriceSource is a single-symbol simbroker.FillPriceSource backing
// "trader broker submit"'s --price flag: this CLI has no live market
// data of its own, so the operator states the price a market order
// should fill at directly.
type cliPriceSource struct {
	symbol string
	price  num.Price
}

func (c cliPriceSource) Info() simbroker.ModelInfo {
	return simbroker.ModelInfo{Name: "cli-fixed-price", Version: "v0", Config: "symbol=" + c.symbol}
}

func (c cliPriceSource) Price(listing instrument.Listing, side order.Side) (num.Price, error) {
	if c.symbol == "" || listing.Symbol() != c.symbol {
		return num.Price{}, fmt.Errorf("no price configured for %s (use --price)", listing.Symbol())
	}
	return c.price, nil
}

// resolveSubmitPriceSource builds the simbroker.FillPriceSource "trader
// broker submit" should use: noPriceSource for a Limit/Stop order,
// which never consults Deps.Prices at Submit time (issue #149), or a
// cliPriceSource fixed to priceFlag for a Market order, which does.
// This lives here rather than in brokersubmit.go so that leaf command
// file never needs to import adapters/broker/sim itself just to name
// the FillPriceSource interface type (see boundary_test.go's own
// "command handlers never import simulator internals" guard).
func resolveSubmitPriceSource(orderType order.Type, symbol, priceFlag string) (simbroker.FillPriceSource, error) {
	if orderType != order.Market {
		return noPriceSource{}, nil
	}
	if priceFlag == "" {
		return nil, fmt.Errorf("--price is required for --type market")
	}
	priceVal, err := num.ParsePrice(priceFlag)
	if err != nil {
		return nil, fmt.Errorf("invalid --price: %w", err)
	}
	return cliPriceSource{symbol: symbol, price: priceVal}, nil
}

// buildSimBroker constructs a fresh, ephemeral simulated Broker with
// exactly one account, from cfg and prices, and returns the account ID
// actually used — either cfg.AccountID parsed, or freshly generated
// when it was left empty. See newBrokerCmd's own doc comment for why
// this is built fresh on every call rather than shared/cached: it
// deliberately never persists across separate trader invocations.
func buildSimBroker(cfg brokerAccountConfig, prices simbroker.FillPriceSource) (*simbroker.Broker, id.AccountID, error) {
	c := clock.Real{}
	gen := id.NewGenerator(c, id.Random{})

	var accountID id.AccountID
	var err error
	if cfg.AccountID != "" {
		accountID, err = id.ParseAccountID(cfg.AccountID)
		if err != nil {
			return nil, id.AccountID{}, fmt.Errorf("invalid --account-id: %w", err)
		}
	} else {
		accountID, err = id.GenerateAccountID(gen)
		if err != nil {
			return nil, id.AccountID{}, err
		}
	}

	currency, err := num.ParseCurrency(cfg.Currency)
	if err != nil {
		return nil, id.AccountID{}, fmt.Errorf("invalid --currency: %w", err)
	}
	startingCash, err := num.ParseMoney(cfg.StartingCash, currency)
	if err != nil {
		return nil, id.AccountID{}, fmt.Errorf("invalid --starting-cash: %w", err)
	}

	deps := simbroker.Deps{Clock: c, IDs: gen, Prices: prices}
	b, err := simbroker.NewBroker("sim", deps, simbroker.AccountConfig{AccountID: accountID, StartingCash: startingCash})
	if err != nil {
		return nil, id.AccountID{}, err
	}
	return b, accountID, nil
}

// buildBrokerService is buildSimBroker plus wrapping the result in a
// service/broker.Service, using the same logger root.go's own
// PersistentPreRunE already built and placed on cmd.Context()
// (loggerFromContext) — matching dataservice.go's buildDataContext.
func buildBrokerService(cmd *cobra.Command, cfg brokerAccountConfig, prices simbroker.FillPriceSource) (*svcbroker.Service, id.AccountID, error) {
	b, accountID, err := buildSimBroker(cfg, prices)
	if err != nil {
		return nil, id.AccountID{}, err
	}
	svc, err := svcbroker.New(b, loggerFromContext(cmd.Context()))
	if err != nil {
		return nil, id.AccountID{}, err
	}
	return svc, accountID, nil
}
