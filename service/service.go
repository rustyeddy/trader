// Package service is the protocol-agnostic business-logic layer.
//
// CLI handlers (cmd/...), the future REST API (api/rest/), and the future
// MCP server (api/mcp/) all call into Service methods rather than reaching
// directly into the trader package. Each method takes typed inputs and
// returns typed outputs so error mapping and presentation stay at the
// edges of the binary.
//
// Service is intentionally protocol-free: no cobra, no HTTP, no MCP types
// leak in. Errors are returned as Go errors and presentation-layer code
// maps them to whatever framing it needs.
package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/brokers/oanda"
	botsvc "github.com/rustyeddy/trader/service/bots"
)

// Service is the protocol-agnostic business-logic surface.
type Service struct {
	OANDA *oanda.Client
	Log   *slog.Logger

	// AccountID is the default account: the one used by the back-compat
	// Service-level broker methods and by ResolveAccount. Account-scoped
	// callers (REST /accounts/{id}/…) go through Account(ctx, id) instead.
	AccountID string

	// Backtests optionally overrides how compiled backtests are executed.
	Backtests backtest.BacktestExecutor

	// bots tracks running/stopped live-strategy bots and the trade→bot
	// tagging map. Zero value is ready to use — see botsvc.Registry's doc
	// comment.
	bots botsvc.Registry
}

// Config bundles the inputs needed to construct a Service.
type Config struct {
	// Env: "practice" or "live". Determines OANDA base URL.
	Env string

	// Token: OANDA API token. If empty, falls back to reading
	// ~/.config/oanda/pat.txt (see brokers/oanda.ResolveToken).
	Token string

	// AccountID: optional OANDA account. If empty, ResolveAccount picks one
	// (errors if the token has multiple accounts; caller must pick).
	AccountID string

	// Log is required — construct it once at program startup (e.g. via
	// the top-level log package's Setup + L/Module) and pass it in
	// explicitly. New does not default to slog.Default(): a business-logic
	// constructor silently depending on a process global is exactly the
	// kind of implicit wiring this package is trying to avoid.
	Log *slog.Logger
}

// New builds a Service from the given Config. OANDA client construction
// (token/base-URL resolution) lives in brokers/oanda, not here — see
// oanda.NewClient. AccountID is NOT auto-discovered here; call
// ResolveAccount(ctx) after New() if you want that.
func New(cfg Config) (*Service, error) {
	if cfg.Log == nil {
		return nil, fmt.Errorf("service: Log is required")
	}

	client, err := oanda.NewClient(cfg.Env, cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("service: %w", err)
	}

	return &Service{
		OANDA:     client,
		Log:       cfg.Log,
		AccountID: cfg.AccountID,
	}, nil
}

// ResolveAccount fills in s.AccountID by querying OANDA when it wasn't
// already set. If the token has multiple accounts and none was specified,
// returns an AmbiguousAccountError listing the IDs.
func (s *Service) ResolveAccount(ctx context.Context) error {
	if s.AccountID != "" {
		return nil
	}
	accounts, err := s.OANDA.GetAccounts(ctx)
	if err != nil {
		return fmt.Errorf("discover accounts: %w", err)
	}
	if len(accounts) == 0 {
		return fmt.Errorf("no accounts found for this token")
	}
	if len(accounts) > 1 {
		ids := make([]string, len(accounts))
		for i, a := range accounts {
			ids[i] = a.ID
		}
		return AmbiguousAccountError{Accounts: ids}
	}
	s.AccountID = accounts[0].ID
	return nil
}

// AmbiguousAccountError is returned by ResolveAccount when the token has
// access to multiple accounts and none was specified. Callers should
// display the candidate IDs and ask the user to pick one.
type AmbiguousAccountError struct {
	Accounts []string
}

func (e AmbiguousAccountError) Error() string {
	return fmt.Sprintf("ambiguous account: multiple accounts available, specify one (%s)", strings.Join(e.Accounts, ", "))
}
