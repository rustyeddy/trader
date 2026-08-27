package execution

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	simbroker "github.com/rustyeddy/trader/adapters/broker/sim"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/cmd/trader/internal/clictx"
	"github.com/rustyeddy/trader/config"
	executionpkg "github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/pipeline"
	"github.com/rustyeddy/trader/risk"
	svcexecution "github.com/rustyeddy/trader/service/execution"
)

// accountConfig is the typed configuration a fresh simulated
// Broker/account is built from, resolved via config.Load the same way
// every other Trader composition-root config is — the same shape
// cmd/trader/broker's own accountConfig establishes, duplicated here
// rather than shared across command-family packages (issue #201).
type accountConfig struct {
	StartingCash string `config:"starting_cash" flag:"starting-cash" default:"10000"`
	Currency     string `config:"currency" flag:"currency" default:"USD"`
	AccountID    string `config:"account_id" flag:"account-id"`
}

// readAccountFlags reads accountFlags' values back off cmd. cmd is
// normally a leaf subcommand (evaluate.go, submit.go); Cobra merges
// an ancestor's PersistentFlags into a leaf command's own Flags() by
// the time RunE executes, so cmd.Flags().GetString("starting-cash")
// here correctly reads the value New registered on the parent
// "execution" command.
func readAccountFlags(cmd *cobra.Command) (accountFlags, error) {
	startingCash, err := cmd.Flags().GetString("starting-cash")
	if err != nil {
		return accountFlags{}, err
	}
	currency, err := cmd.Flags().GetString("currency")
	if err != nil {
		return accountFlags{}, err
	}
	accountID, err := cmd.Flags().GetString("account-id")
	if err != nil {
		return accountFlags{}, err
	}
	return accountFlags{startingCash: startingCash, currency: currency, accountID: accountID}, nil
}

// buildAccountConfig resolves an accountConfig from flags actually set
// on cmd, layered under the TRADER_STARTING_CASH/TRADER_CURRENCY/
// TRADER_ACCOUNT_ID environment variables.
func buildAccountConfig(cmd *cobra.Command, flags accountFlags) (accountConfig, error) {
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

	return config.Load[accountConfig](config.Options{
		EnvPrefix: clictx.EnvPrefix,
		Environ:   os.Environ(),
		Overrides: overrides,
	})
}

// noPriceSource is the simbroker.FillPriceSource "trader execution
// evaluate" uses: Deps requires a non-nil Prices unconditionally
// (ADR-015), even though evaluate never calls Submit and so never
// actually consults it — service/execution.Service.Evaluate never
// reaches the broker at all.
type noPriceSource struct{}

func (noPriceSource) Info() simbroker.ModelInfo {
	return simbroker.ModelInfo{Name: "none", Version: "v0"}
}

func (noPriceSource) Price(listing instrument.Listing, side order.Side) (num.Price, error) {
	return num.Price{}, fmt.Errorf("no fill price source configured")
}

// cliPriceSource is a single-symbol simbroker.FillPriceSource backing
// "trader execution submit"'s --price flag: this CLI has no live
// market data of its own, so the operator states the price a market
// order should fill at directly — the same convention cmd/trader/
// broker's own submit command establishes.
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

// buildSimBroker constructs a fresh, ephemeral simulated Broker with
// exactly one account, from cfg and prices, and returns the account ID
// actually used — either cfg.AccountID parsed, or freshly generated
// when it was left empty. gen supplies every identifier the returned
// Broker itself generates (fills, events); it is a separate concern
// from the id.Generator a leaf command uses to build its own
// order.Intent (see evaluate.go/submit.go), the same separation
// cmd/trader/broker's own submit command already establishes between
// buildSimBroker's internal generator and its own leaf-level one.
func buildSimBroker(cfg accountConfig, prices simbroker.FillPriceSource) (*simbroker.Broker, id.AccountID, error) {
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

// resolveSubmitPriceSource builds the simbroker.FillPriceSource
// "trader execution submit" should use: execution.planner's own v0
// scope always plans a Market/GTC order (Intent carries no
// limit-price hint yet), so --price is always required here, unlike
// cmd/trader/broker's own resolveSubmitPriceSource, which only
// requires --price for a Market order.Type specifically. This lives
// here rather than in submit.go so that leaf command file never needs
// to import adapters/broker/sim itself just to name the
// FillPriceSource interface type (see boundary_test.go's own "command
// handlers never import simulator internals" guard).
func resolveSubmitPriceSource(symbol, priceFlag string) (simbroker.FillPriceSource, error) {
	if priceFlag == "" {
		return nil, fmt.Errorf("--price is required")
	}
	priceVal, err := num.ParsePrice(priceFlag)
	if err != nil {
		return nil, fmt.Errorf("invalid --price: %w", err)
	}
	return cliPriceSource{symbol: symbol, price: priceVal}, nil
}

// registerIntentFlags registers the flags evaluate/submit share:
// instrument spec, --side, and the sizing/risk assumptions
// pipeline.Input needs.
func registerIntentFlags(cmd *cobra.Command, listing *simListingFlags, intent *intentFlags) {
	cmd.Flags().StringVar(&listing.symbol, "symbol", "", "instrument symbol, e.g. EURUSD (required)")
	cmd.Flags().StringVar(&listing.tickSize, "tick-size", "0.00001", "simulator tick size")
	cmd.Flags().StringVar(&listing.quantityIncrement, "quantity-increment", "1", "simulator quantity increment")
	cmd.Flags().StringVar(&listing.multiplier, "multiplier", "1", "simulator contract multiplier")
	cmd.Flags().StringVar(&intent.side, "side", "", "intent side: buy or sell (required)")
	cmd.Flags().StringVar(&intent.riskFraction, "risk-fraction", "0.01", "fraction of account equity to risk, e.g. 0.01 for 1%")
	cmd.Flags().StringVar(&intent.adverseDistance, "adverse-distance", "", "adverse price distance used for sizing (required)")
	cmd.Flags().StringVar(&intent.referencePrice, "reference-price", "", "valuation price for value-based risk rules (optional)")
	_ = cmd.MarkFlagRequired("symbol")
	_ = cmd.MarkFlagRequired("side")
	_ = cmd.MarkFlagRequired("adverse-distance")
}

// prepareRequest is evaluate/submit's shared setup: resolve the
// service/execution.Service and the SubmitRequest their intent flags
// describe. Building the request is identical for both use cases —
// only which Service method a leaf command then calls, and which
// simbroker.FillPriceSource it supplies, differs.
func prepareRequest(cmd *cobra.Command, accFlags accountFlags, listingFlags simListingFlags, intentFl intentFlags, prices simbroker.FillPriceSource) (*svcexecution.Service, svcexecution.SubmitRequest, error) {
	accCfg, err := buildAccountConfig(cmd, accFlags)
	if err != nil {
		return nil, svcexecution.SubmitRequest{}, err
	}

	listing, err := buildSimListing(listingFlags, "sim")
	if err != nil {
		return nil, svcexecution.SubmitRequest{}, err
	}

	side, err := parseIntentSide(intentFl.side)
	if err != nil {
		return nil, svcexecution.SubmitRequest{}, err
	}
	riskFraction, adverseDistance, referencePrice, err := buildSizingParams(intentFl)
	if err != nil {
		return nil, svcexecution.SubmitRequest{}, err
	}

	svc, accountID, err := buildService(cmd, accCfg, prices)
	if err != nil {
		return nil, svcexecution.SubmitRequest{}, err
	}

	gen := id.NewGenerator(clock.Real{}, id.Random{})
	intent, err := buildEnterIntent(gen, listing.InstrumentID(), side)
	if err != nil {
		return nil, svcexecution.SubmitRequest{}, err
	}

	return svc, svcexecution.SubmitRequest{
		AccountID:       accountID,
		Intent:          intent,
		Listing:         listing,
		RiskFraction:    riskFraction,
		AdverseDistance: adverseDistance,
		ReferencePrice:  referencePrice,
	}, nil
}

// buildService is buildSimBroker plus wrapping the result in the full
// M4 pipeline stack (execution.Planner, risk.Engine, risk.Sizer,
// pipeline.Pipeline) and a service/execution.Service, using the same
// logger root.go's own PersistentPreRunE already built and placed on
// cmd.Context() (clictx.LoggerFromContext) — matching cmd/trader/
// broker's own buildService.
//
// risk.NewEngine is constructed with no configured Rules: this CLI is
// a thin diagnostic/demonstration tool over the M4 pipeline, not a
// configurable risk-policy console — every risk.Rule constructor
// (#182-#184) takes its own numeric thresholds as Go values, and
// exposing each as a CLI flag would turn this into exactly the
// "trading terminal" #187's own scope explicitly says to avoid. A
// zero-rule Engine always allows structurally valid input, which is
// sufficient to exercise and demonstrate the full sizing -> planning
// -> risk -> request/submission path end to end.
func buildService(cmd *cobra.Command, cfg accountConfig, prices simbroker.FillPriceSource) (*svcexecution.Service, id.AccountID, error) {
	b, accountID, err := buildSimBroker(cfg, prices)
	if err != nil {
		return nil, id.AccountID{}, err
	}

	c := clock.Real{}
	gen := id.NewGenerator(c, id.Random{})
	planner, err := executionpkg.NewPlanner(executionpkg.Deps{Clock: c, IDs: gen})
	if err != nil {
		return nil, id.AccountID{}, err
	}
	engine, err := risk.NewEngine()
	if err != nil {
		return nil, id.AccountID{}, err
	}
	p, err := pipeline.NewPipeline(pipeline.Deps{
		Sizer:   risk.NewFixedFractionSizer(),
		Planner: planner,
		Engine:  engine,
		Broker:  b,
		IDs:     gen,
	})
	if err != nil {
		return nil, id.AccountID{}, err
	}

	svc, err := svcexecution.New(b, p, clictx.LoggerFromContext(cmd.Context()))
	if err != nil {
		return nil, id.AccountID{}, err
	}
	return svc, accountID, nil
}
