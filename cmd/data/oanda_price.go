package data

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/config"
	"github.com/rustyeddy/trader/market"
	accountsvc "github.com/rustyeddy/trader/service/account"
)

// oandaAuth holds the optional OANDA credentials shared by data commands that
// can fall back to a live price query when --price / --rates is not supplied.
type oandaAuth struct {
	token     string
	env       string
	accountID string
}

// defaultOandaAuth returns credentials from environment variables.
func defaultOandaAuth() oandaAuth {
	return oandaAuth{
		token:     os.Getenv("OANDA_TOKEN"),
		env:       "practice",
		accountID: os.Getenv("OANDA_ACCOUNT_ID"),
	}
}

// applyGlobalOANDA overrides auth fields from the global config (rc) for any
// flag that was not explicitly set by the user. The priority chain is:
// explicit flag > global config > env var / default.
func applyGlobalOANDA(cmd *cobra.Command, auth *oandaAuth, rc *config.RootConfig) {
	if rc == nil {
		return
	}
	if !cmd.Flags().Changed("account-id") && rc.OANDAAccountID != "" {
		auth.accountID = rc.OANDAAccountID
	}
	if !cmd.Flags().Changed("token") && rc.OANDAToken != "" {
		auth.token = rc.OANDAToken
	}
	if !cmd.Flags().Changed("env") && rc.OANDAEnv != "" {
		auth.env = rc.OANDAEnv
	}
}

// fetchMidPrices queries OANDA for the current mid price of each instrument
// (trader format, e.g. "EURUSD").  Returns a map of instrument → mid price.
func fetchMidPrices(ctx context.Context, auth oandaAuth, instruments []string) (map[string]float64, error) {
	client, err := oanda.NewClient(auth.env, auth.token)
	if err != nil {
		return nil, fmt.Errorf("oanda: %w", err)
	}
	resolvedID, err := accountsvc.ResolveAccountID(ctx, client, auth.accountID)
	if err != nil {
		var amb accountsvc.AmbiguousAccountError
		if errors.As(err, &amb) {
			return nil, fmt.Errorf("multiple OANDA accounts — specify one with --account-id")
		}
		return nil, fmt.Errorf("oanda: %w", err)
	}

	oandaNames := make([]string, 0, len(instruments))
	for _, name := range instruments {
		inst := market.GetInstrument(name)
		if inst == nil {
			continue
		}
		oandaNames = append(oandaNames, inst.BaseCurrency+"_"+inst.QuoteCurrency)
	}

	prices, err := client.GetPricing(ctx, resolvedID, oandaNames...)
	if err != nil {
		return nil, fmt.Errorf("oanda: get pricing: %w", err)
	}

	result := make(map[string]float64, len(prices))
	for _, p := range prices {
		traderName := strings.ReplaceAll(p.Instrument, "_", "")
		result[traderName] = p.Mid
	}
	return result, nil
}
