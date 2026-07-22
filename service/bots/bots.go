// Package botsvc is the service-layer orchestration for live strategy
// bots: starting/stopping them, tracking their running status, and tagging
// trades with the bot that opened them.
package botsvc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/rustyeddy/trader/types"
)

// BotConfig is the payload needed to start a live strategy bot via the API.
type BotConfig struct {
	Instrument     string         `json:"instrument"`
	TickInterval   string         `json:"tick_interval"` // e.g. "60s", "5m"
	RiskPct        float64        `json:"risk_pct"`
	MaxUnits       int64          `json:"max_units"`
	MaxPositionUSD float64        `json:"max_position_usd"`
	Strategy       StrategyConfig `json:"strategy"`
}

// BotStatus is the public view of a running or stopped bot.
type BotStatus struct {
	ID           string     `json:"id"`
	AccountID    string     `json:"account_id"`
	Instrument   string     `json:"instrument"`
	StrategyName string     `json:"strategy_name"`
	StrategyKind string     `json:"strategy_kind"`
	RiskPct      float64    `json:"risk_pct"`
	TickInterval string     `json:"tick_interval"`
	StartedAt    time.Time  `json:"started_at"`
	StoppedAt    *time.Time `json:"stopped_at,omitempty"`
	Status       string     `json:"status"` // "running" | "stopped" | "error"
	Error        string     `json:"error,omitempty"`
	// Runtime stats — updated each tick.
	Ticks  int `json:"ticks"`
	Opens  int `json:"opens"`
	Closes int `json:"closes"`
}

// botEntry is the internal record tracking a bot goroutine.
type botEntry struct {
	BotStatus
	mu     sync.Mutex
	cancel context.CancelFunc
	done   <-chan struct{}
}

// Registry tracks running/stopped bots and the trade→bot tagging map. The
// zero value is ready to use — no constructor is required, so a Registry
// can sit as a plain value field on any struct (including one built via a
// bare struct literal in tests) without an explicit initialization step.
//
// Registry does not hold an OANDA client or logger of its own; methods that
// need them take them as parameters, supplied fresh by the caller on every
// call — same rationale as account.Registry.
type Registry struct {
	botsMu sync.RWMutex
	bots   map[string]*botEntry

	// tradeBotMu guards tradeBotMap, which maps OANDA tradeID → bot ID.
	// Populated by the live runner when a trade opens; read by LiveJournal
	// when the trade closes to tag the journal record.
	tradeBotMu  sync.RWMutex
	tradeBotMap map[string]string
}

// StartBotOnAccount builds and launches a live strategy bot on the given
// account inside the serve process. Returns the bot's initial status
// (Status="running"). The bot runs until StopBot is called or the parent
// context is cancelled. Bots are tracked in this shared registry, tagged
// with the account's ID — bot orchestration is a service concern, not
// per-account state, even though it acts on an Account via order placement
// (see docs/Manual/architecture-broker-account-order.org, Phase 3).
func (r *Registry) StartBotOnAccount(ctx context.Context, acc *account.Account, cfg BotConfig, oandaClient *oanda.Client, log *slog.Logger) (*BotStatus, error) {
	if cfg.Instrument == "" {
		return nil, fmt.Errorf("bots: instrument is required")
	}

	interval, err := parseBotDuration(cfg.TickInterval, 60*time.Second)
	if err != nil {
		return nil, fmt.Errorf("bots: invalid tick_interval: %w", err)
	}

	strategy, err := BuildLiveStrategy(cfg.Strategy, cfg.Instrument, oandaClient, acc.ID, acc.UpdateTradeStop, log)
	if err != nil {
		return nil, fmt.Errorf("bots: %w", err)
	}

	id := newBotID()
	// Use context.Background() as the bot's parent — the bot must outlive the
	// HTTP request that created it. StopBot/StopAllBots cancel it explicitly.
	botCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	tickIntervalStr := cfg.TickInterval
	if tickIntervalStr == "" {
		tickIntervalStr = "60s"
	}

	entry := &botEntry{
		BotStatus: BotStatus{
			ID:           id,
			AccountID:    acc.ID,
			Instrument:   cfg.Instrument,
			StrategyName: strategy.Name(),
			StrategyKind: cfg.Strategy.Kind,
			RiskPct:      cfg.RiskPct,
			TickInterval: tickIntervalStr,
			StartedAt:    time.Now().UTC(),
			Status:       "running",
		},
		cancel: cancel,
		done:   done,
	}

	// Wrap the strategy so each Tick call updates the bot's stats.
	tracked := &statsTrackingStrategy{inner: strategy, entry: entry}

	r.botsMu.Lock()
	if r.bots == nil {
		r.bots = make(map[string]*botEntry)
	}
	r.bots[id] = entry
	r.botsMu.Unlock()

	go func() {
		defer close(done)
		runErr := acc.RunLiveStrategy(botCtx, account.LiveRunConfig{
			Instrument:         cfg.Instrument,
			TickInterval:       interval,
			Strategy:           tracked,
			RiskPct:            types.RateFromFloat(cfg.RiskPct / 100.0),
			MaxUnits:           cfg.MaxUnits,
			MaxPositionUSD:     cfg.MaxPositionUSD,
			BotID:              id,
			RegisterTradeBotID: r.RegisterTradeBotID,
		})
		now := time.Now().UTC()
		r.botsMu.Lock()
		if e, ok := r.bots[id]; ok {
			e.mu.Lock()
			e.StoppedAt = &now
			if runErr != nil && botCtx.Err() == nil {
				e.Status = "error"
				e.Error = runErr.Error()
			} else {
				e.Status = "stopped"
			}
			e.mu.Unlock()
		}
		r.botsMu.Unlock()
	}()

	status := entry.BotStatus
	return &status, nil
}

// ListBotsForAccount returns a snapshot of the given account's bots
// (running and stopped). See ListBots for the all-accounts view.
func (r *Registry) ListBotsForAccount(accountID string) []BotStatus {
	r.botsMu.RLock()
	defer r.botsMu.RUnlock()
	out := make([]BotStatus, 0)
	for _, e := range r.bots {
		e.mu.Lock()
		status := e.BotStatus
		e.mu.Unlock()
		if status.AccountID == accountID {
			out = append(out, status)
		}
	}
	return out
}

// StopBot cancels the bot with the given ID and waits for it to exit.
func (r *Registry) StopBot(id string) error {
	r.botsMu.RLock()
	entry, ok := r.bots[id]
	r.botsMu.RUnlock()
	if !ok {
		return fmt.Errorf("bots: bot %q not found", id)
	}
	entry.cancel()
	<-entry.done
	return nil
}

// ListBots returns a snapshot of all known bot statuses (running and stopped).
func (r *Registry) ListBots() []BotStatus {
	r.botsMu.RLock()
	defer r.botsMu.RUnlock()
	out := make([]BotStatus, 0, len(r.bots))
	for _, e := range r.bots {
		e.mu.Lock()
		status := e.BotStatus
		e.mu.Unlock()
		out = append(out, status)
	}
	return out
}

// StopAllBots cancels every bot goroutine and waits for them to exit.
// It does NOT close open OANDA positions — those remain on the broker.
// Call this during graceful server shutdown.
//
// All entries are snapshotted (not just Status=="running") because a bot's
// status field is updated inside the goroutine before close(done) runs; in
// that brief window the status is already "stopped"/"error" but the goroutine
// hasn't returned yet. Cancelling an already-cancelled context is a no-op, so
// calling cancel() on every entry is safe.
func (r *Registry) StopAllBots() {
	r.botsMu.RLock()
	entries := make([]*botEntry, 0, len(r.bots))
	for _, e := range r.bots {
		entries = append(entries, e)
	}
	r.botsMu.RUnlock()

	for _, e := range entries {
		e.cancel()
	}
	for _, e := range entries {
		<-e.done
	}
}

// WaitBot blocks until the bot with the given ID exits, then returns any
// runtime error the bot recorded. Use this to run a bot in the foreground
// (e.g. from the CLI) without polling.
func (r *Registry) WaitBot(id string) error {
	r.botsMu.RLock()
	entry, ok := r.bots[id]
	r.botsMu.RUnlock()
	if !ok {
		return fmt.Errorf("bots: bot %q not found", id)
	}
	<-entry.done
	entry.mu.Lock()
	errStr := entry.Error
	entry.mu.Unlock()
	if errStr != "" {
		return fmt.Errorf("%s", errStr)
	}
	return nil
}

// GetBot returns the status of one bot by ID.
func (r *Registry) GetBot(id string) (*BotStatus, error) {
	r.botsMu.RLock()
	e, ok := r.bots[id]
	r.botsMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("bots: bot %q not found", id)
	}
	e.mu.Lock()
	status := e.BotStatus
	e.mu.Unlock()
	return &status, nil
}

// RegisterTradeBotID records that the given OANDA trade was opened by botID.
// Called by the live runner immediately after a successful PlaceMarketOrder.
func (r *Registry) RegisterTradeBotID(tradeID, botID string) {
	if tradeID == "" || botID == "" {
		return
	}
	r.tradeBotMu.Lock()
	if r.tradeBotMap == nil {
		r.tradeBotMap = make(map[string]string)
	}
	r.tradeBotMap[tradeID] = botID
	r.tradeBotMu.Unlock()
}

// LookupTradeBotID returns the bot ID that opened the given OANDA trade, or
// empty string if none was registered (e.g. trade opened outside a bot).
func (r *Registry) LookupTradeBotID(tradeID string) string {
	r.tradeBotMu.RLock()
	id := r.tradeBotMap[tradeID]
	r.tradeBotMu.RUnlock()
	return id
}

// ── stats-tracking strategy wrapper ──────────────────────────────────────

// statsTrackingStrategy wraps a LiveStrategy and updates the bot entry's
// Ticks, Opens, and Closes counters on every tick.
type statsTrackingStrategy struct {
	inner account.LiveStrategy
	entry *botEntry
}

func (w *statsTrackingStrategy) Name() string { return w.inner.Name() }

func (w *statsTrackingStrategy) Tick(ctx context.Context, price account.LivePrice, trades []account.LiveTrade) *account.LivePlan {
	plan := w.inner.Tick(ctx, price, trades)
	w.entry.mu.Lock()
	w.entry.Ticks++
	if plan != nil {
		if plan.Open != nil {
			w.entry.Opens++
		}
		w.entry.Closes += len(plan.CloseIDs)
	}
	w.entry.mu.Unlock()
	return plan
}

func newBotID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "bot-" + hex.EncodeToString(b)
}

func parseBotDuration(s string, def time.Duration) (time.Duration, error) {
	if s == "" {
		return def, nil
	}
	return time.ParseDuration(s)
}
