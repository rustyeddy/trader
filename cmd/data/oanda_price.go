package data

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	trader "github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/service"
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
func applyGlobalOANDA(cmd *cobra.Command, auth *oandaAuth, rc *trader.RootConfig) {
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
	svc, err := service.New(service.Config{
		Env:       auth.env,
		Token:     auth.token,
		AccountID: auth.accountID,
	})
	if err != nil {
		return nil, fmt.Errorf("oanda: %w", err)
	}
	if err := svc.ResolveAccount(ctx); err != nil {
		var amb service.AmbiguousAccountError
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

	prices, err := svc.OANDA.GetPricing(ctx, svc.AccountID, oandaNames...)
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
