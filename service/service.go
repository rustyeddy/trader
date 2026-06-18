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
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/brokers/oanda"
)

// Service is the protocol-agnostic business-logic surface.
type Service struct {
	OANDA *oanda.Client
	Log   *slog.Logger

	// AccountID resolved at construction or via ResolveAccount.
	AccountID string

	// Backtests optionally overrides how compiled backtests are executed.
	Backtests trader.BacktestExecutor

	botsMu sync.RWMutex
	bots   map[string]*botEntry

	// tradeBotMu guards tradeBotMap, which maps OANDA tradeID → bot ID.
	// Populated by the live runner when a trade opens; read by LiveJournal
	// when the trade closes to tag the journal record.
	tradeBotMu  sync.RWMutex
	tradeBotMap map[string]string
}

// Config bundles the inputs needed to construct a Service.
type Config struct {
	// Env: "practice" or "live". Determines OANDA base URL.
	Env string

	// Token: OANDA API token. If empty, falls back to reading
	// ~/.config/oanda/pat.txt.
	Token string

	// AccountID: optional OANDA account. If empty, ResolveAccount picks one
	// (errors if the token has multiple accounts; caller must pick).
	AccountID string

	// Log: optional structured logger; defaults to slog.Default().
	Log *slog.Logger
}

// New builds a Service from the given Config. It resolves the OANDA base
// URL, falls back to ~/.config/oanda/pat.txt for the token when needed,
// and creates the OANDA client. AccountID is NOT auto-discovered here;
// call ResolveAccount(ctx) after New() if you want that.
func New(cfg Config) (*Service, error) {
	token := cfg.Token
	if token == "" {
		token = readTokenFile()
	}
	if token == "" {
		return nil, fmt.Errorf("service: no OANDA token (set OANDA_TOKEN, pass Token, or save to ~/.config/oanda/pat.txt)")
	}

	baseURL, err := oanda.BaseURL(cfg.Env)
	if err != nil {
		return nil, err
	}

	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}

	return &Service{
		OANDA:       &oanda.Client{BaseURL: baseURL, Token: token},
		Log:         log,
		AccountID:   cfg.AccountID,
		bots:        make(map[string]*botEntry),
		tradeBotMap: make(map[string]string),
	}, nil
}

// RegisterTradeBotID records that the given OANDA trade was opened by botID.
// Called by the live runner immediately after a successful PlaceMarketOrder.
func (s *Service) RegisterTradeBotID(tradeID, botID string) {
	if tradeID == "" || botID == "" {
		return
	}
	s.tradeBotMu.Lock()
	s.tradeBotMap[tradeID] = botID
	s.tradeBotMu.Unlock()
}

// LookupTradeBotID returns the bot ID that opened the given OANDA trade, or
// empty string if none was registered (e.g. trade opened outside a bot).
func (s *Service) LookupTradeBotID(tradeID string) string {
	s.tradeBotMu.RLock()
	id := s.tradeBotMap[tradeID]
	s.tradeBotMu.RUnlock()
	return id
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

// readTokenFile is the fallback used when no token is passed in Config.
// Lives in service so all consumers (CLI, REST, MCP) get the same
// resolution behavior.
func readTokenFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".config", "oanda", "pat.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
