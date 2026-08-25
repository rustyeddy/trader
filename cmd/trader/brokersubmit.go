package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	svcbroker "github.com/rustyeddy/trader/service/broker"
)

// brokerSubmitFlags holds "trader broker submit"'s own local flags,
// beyond the "broker" group's shared account flags and
// simListingFlags' instrument-spec flags.
type brokerSubmitFlags struct {
	format      string
	listing     simListingFlags
	side        string
	orderType   string
	timeInForce string
	quantity    string
	price       string
	limitPrice  string
	stopPrice   string
}

// newBrokerSubmitCmd implements "trader broker submit ..." (issue
// #155, M3-12): the mutating Submit use case against the one freshly
// built account. A Market order fills immediately, using --price as
// its fixed fill price (this CLI has no live market data of its own);
// Limit and Stop orders are accepted into StatusWorking and do not
// fill within this invocation (adapters/broker/sim only fills them via
// Broker.Advance, which no CLI command here drives — see newBrokerCmd's
// own doc comment on why cancel/replace are not exposed as normal
// commands for the identical reason).
func newBrokerSubmitCmd() *cobra.Command {
	var flags brokerSubmitFlags

	cmd := &cobra.Command{
		Use:   "submit",
		Short: "Submit an order to the simulated broker.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBrokerSubmit(cmd, flags)
		},
	}

	cmd.Flags().StringVar(&flags.format, "format", formatTable, "output format: table or json")
	cmd.Flags().StringVar(&flags.listing.symbol, "symbol", "", "instrument symbol, e.g. EURUSD (required)")
	cmd.Flags().StringVar(&flags.listing.tickSize, "tick-size", "0.00001", "simulator tick size")
	cmd.Flags().StringVar(&flags.listing.quantityIncrement, "quantity-increment", "1", "simulator quantity increment")
	cmd.Flags().StringVar(&flags.listing.multiplier, "multiplier", "1", "simulator contract multiplier")
	cmd.Flags().StringVar(&flags.side, "side", "", "order side: buy or sell (required)")
	cmd.Flags().StringVar(&flags.orderType, "type", "market", "order type: market, limit, or stop")
	cmd.Flags().StringVar(&flags.timeInForce, "tif", "GTC", "time in force: GTC, DAY, IOC, or FOK")
	cmd.Flags().StringVar(&flags.quantity, "quantity", "", "order quantity (required)")
	cmd.Flags().StringVar(&flags.price, "price", "", "fill price (required for --type market)")
	cmd.Flags().StringVar(&flags.limitPrice, "limit-price", "", "limit price (required for --type limit)")
	cmd.Flags().StringVar(&flags.stopPrice, "stop-price", "", "stop price (required for --type stop)")
	_ = cmd.MarkFlagRequired("symbol")
	_ = cmd.MarkFlagRequired("side")
	_ = cmd.MarkFlagRequired("quantity")

	return cmd
}

func runBrokerSubmit(cmd *cobra.Command, flags brokerSubmitFlags) error {
	formatter, err := resolveBrokerFormatter(flags.format)
	if err != nil {
		return err
	}

	accountFlags, err := readBrokerAccountFlags(cmd)
	if err != nil {
		return err
	}
	accountCfg, err := buildBrokerAccountConfig(cmd, accountFlags)
	if err != nil {
		return err
	}

	listing, err := buildSimListing(flags.listing, "sim")
	if err != nil {
		return err
	}

	sideVal, err := parseOrderSide(flags.side)
	if err != nil {
		return err
	}
	typeVal, err := parseOrderType(flags.orderType)
	if err != nil {
		return err
	}
	tifVal, err := parseTimeInForce(flags.timeInForce)
	if err != nil {
		return err
	}
	qty, err := num.ParseQuantity(flags.quantity)
	if err != nil {
		return fmt.Errorf("invalid --quantity: %w", err)
	}

	var limitP, stopP *num.Price
	if flags.limitPrice != "" {
		v, err := num.ParsePrice(flags.limitPrice)
		if err != nil {
			return fmt.Errorf("invalid --limit-price: %w", err)
		}
		limitP = &v
	}
	if flags.stopPrice != "" {
		v, err := num.ParsePrice(flags.stopPrice)
		if err != nil {
			return fmt.Errorf("invalid --stop-price: %w", err)
		}
		stopP = &v
	}

	prices, err := resolveSubmitPriceSource(typeVal, listing.Symbol(), flags.price)
	if err != nil {
		return err
	}

	svc, accountID, err := buildBrokerService(cmd, accountCfg, prices)
	if err != nil {
		return err
	}

	gen := id.NewGenerator(clock.Real{}, id.Random{})
	eventID, err := id.GenerateEventID(gen)
	if err != nil {
		return err
	}
	proposal, err := order.NewProposal(order.Proposal{
		Listing:     listing,
		AccountID:   accountID,
		Side:        sideVal,
		Type:        typeVal,
		TimeInForce: tifVal,
		Quantity:    qty,
		LimitPrice:  limitP,
		StopPrice:   stopP,
		Metadata:    id.Metadata{EventID: eventID},
	})
	if err != nil {
		return err
	}
	orderID, err := id.GenerateOrderID(gen)
	if err != nil {
		return err
	}
	req, err := order.NewRequest(proposal, orderID)
	if err != nil {
		return err
	}

	resp, err := svc.Submit(cmd.Context(), svcbroker.SubmitRequest{
		AccountRequest: svcbroker.AccountRequest{AccountID: accountID},
		Order:          req,
	})
	if err != nil {
		return err
	}
	return formatter.FormatSubmit(cmd.OutOrStdout(), resp)
}
