package botsvc

import (
	"context"
	"log/slog"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/brokers/oanda"
)

// shared is the process-wide registry of running/stopped bots. Every
// consumer (CLI, REST, MCP, ...) within one process sees the same bots —
// caching lives here, in the provider, never in a caller-owned struct
// (Service or otherwise). Mirrors account/resolve.go's identical pattern
// for account sessions.
var shared Registry

// StartBotOnAccount builds and launches a live strategy bot on the given
// account. See Registry.StartBotOnAccount.
func StartBotOnAccount(ctx context.Context, acc *account.Account, cfg BotConfig, oandaClient *oanda.Client, log *slog.Logger) (*BotStatus, error) {
	return shared.StartBotOnAccount(ctx, acc, cfg, oandaClient, log)
}

// ListBotsForAccount returns a snapshot of the given account's bots.
func ListBotsForAccount(accountID string) []BotStatus {
	return shared.ListBotsForAccount(accountID)
}

// StopBot cancels the bot with the given ID and waits for it to exit.
func StopBot(id string) error {
	return shared.StopBot(id)
}

// ListBots returns a snapshot of all known bot statuses (running and stopped).
func ListBots() []BotStatus {
	return shared.ListBots()
}

// StopAllBots cancels every bot goroutine and waits for them to exit.
func StopAllBots() {
	shared.StopAllBots()
}

// WaitBot blocks until the bot with the given ID exits, then returns any
// runtime error the bot recorded.
func WaitBot(id string) error {
	return shared.WaitBot(id)
}

// GetBot returns the status of one bot by ID.
func GetBot(id string) (*BotStatus, error) {
	return shared.GetBot(id)
}

// RegisterTradeBotID records that the given OANDA trade was opened by botID.
func RegisterTradeBotID(tradeID, botID string) {
	shared.RegisterTradeBotID(tradeID, botID)
}

// LookupTradeBotID returns the bot ID that opened the given OANDA trade, or
// empty string if none was registered.
func LookupTradeBotID(tradeID string) string {
	return shared.LookupTradeBotID(tradeID)
}
