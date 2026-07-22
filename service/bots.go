package service

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/account"
	accountsvc "github.com/rustyeddy/trader/service/account"
	botsvc "github.com/rustyeddy/trader/service/bots"
)

// StartBot launches a bot on the default account. See botsvc.Registry.StartBotOnAccount.
func (s *Service) StartBot(ctx context.Context, cfg BotConfig) (*BotStatus, error) {
	if err := s.ResolveAccount(ctx); err != nil {
		return nil, fmt.Errorf("bots: %w", err)
	}
	acc, err := accountsvc.Resolve(ctx, s.AccountID, s.OANDA, s.Log)
	if err != nil {
		return nil, fmt.Errorf("bots: %w", err)
	}
	return s.bots.StartBotOnAccount(ctx, acc, cfg, s.OANDA, s.Log)
}

// StartBotOnAccount launches a bot on the given account (REST/MCP
// account-scoped routes resolve the account themselves via
// accountsvc.Resolve/ResolveFirst and call this instead of StartBot).
func (s *Service) StartBotOnAccount(ctx context.Context, acc *account.Account, cfg BotConfig) (*BotStatus, error) {
	return s.bots.StartBotOnAccount(ctx, acc, cfg, s.OANDA, s.Log)
}

// ListBotsForAccount returns a snapshot of the given account's bots.
func (s *Service) ListBotsForAccount(accountID string) []BotStatus {
	return s.bots.ListBotsForAccount(accountID)
}

// ListBots returns a snapshot of all known bot statuses (running and stopped).
func (s *Service) ListBots() []BotStatus {
	return s.bots.ListBots()
}

// StopBot cancels the bot with the given ID and waits for it to exit.
func (s *Service) StopBot(id string) error {
	return s.bots.StopBot(id)
}

// StopAllBots cancels every bot goroutine and waits for them to exit.
func (s *Service) StopAllBots() {
	s.bots.StopAllBots()
}

// WaitBot blocks until the bot with the given ID exits, then returns any
// runtime error the bot recorded.
func (s *Service) WaitBot(id string) error {
	return s.bots.WaitBot(id)
}

// GetBot returns the status of one bot by ID.
func (s *Service) GetBot(id string) (*BotStatus, error) {
	return s.bots.GetBot(id)
}

// RegisterTradeBotID records that the given OANDA trade was opened by botID.
// Called by the live runner immediately after a successful PlaceMarketOrder.
func (s *Service) RegisterTradeBotID(tradeID, botID string) {
	s.bots.RegisterTradeBotID(tradeID, botID)
}

// LookupTradeBotID returns the bot ID that opened the given OANDA trade, or
// empty string if none was registered (e.g. trade opened outside a bot).
func (s *Service) LookupTradeBotID(tradeID string) string {
	return s.bots.LookupTradeBotID(tradeID)
}

// Types re-exported as aliases so existing call sites (service.BotConfig{...}
// etc.) keep compiling unchanged while the implementation lives in
// service/bots — same technique as service/review.go.
type (
	BotConfig      = botsvc.BotConfig
	BotStatus      = botsvc.BotStatus
	StrategyConfig = botsvc.StrategyConfig
)
