package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/rustyeddy/trader/market"
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

// StartBot builds and launches a live strategy bot on this account inside the
// serve process. Returns the bot's initial status (Status="running"). The bot
// runs until StopBot is called or the parent context is cancelled. Bots are
// tracked in the shared Service registry, tagged with this account's ID.
func (a *Account) StartBot(ctx context.Context, cfg BotConfig) (*BotStatus, error) {
	s := a.svc
	if cfg.Instrument == "" {
		return nil, fmt.Errorf("bots: instrument is required")
	}

	interval, err := parseBotDuration(cfg.TickInterval, 60*time.Second)
	if err != nil {
		return nil, fmt.Errorf("bots: invalid tick_interval: %w", err)
	}

	strategy, err := s.BuildLiveStrategy(cfg.Strategy, cfg.Instrument)
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
			AccountID:    a.ID,
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

	s.botsMu.Lock()
	if s.bots == nil {
		s.bots = make(map[string]*botEntry)
	}
	s.bots[id] = entry
	s.botsMu.Unlock()

	go func() {
		defer close(done)
		runErr := a.RunLiveStrategy(botCtx, LiveRunConfig{
			Instrument:     cfg.Instrument,
			TickInterval:   interval,
			Strategy:       tracked,
			RiskPct:        market.RateFromFloat(cfg.RiskPct / 100.0),
			MaxUnits:       cfg.MaxUnits,
			MaxPositionUSD: cfg.MaxPositionUSD,
			BotID:          id,
		})
		now := time.Now().UTC()
		s.botsMu.Lock()
		if e, ok := s.bots[id]; ok {
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
		s.botsMu.Unlock()
	}()

	status := entry.BotStatus
	return &status, nil
}

// StartBot launches a bot on the default account. See Account.StartBot.
func (s *Service) StartBot(ctx context.Context, cfg BotConfig) (*BotStatus, error) {
	acc, err := s.DefaultAccount(ctx)
	if err != nil {
		return nil, fmt.Errorf("bots: %w", err)
	}
	return acc.StartBot(ctx, cfg)
}

// ListBots returns a snapshot of this account's bots (running and stopped).
func (a *Account) ListBots() []BotStatus {
	a.svc.botsMu.RLock()
	defer a.svc.botsMu.RUnlock()
	out := make([]BotStatus, 0)
	for _, e := range a.svc.bots {
		e.mu.Lock()
		status := e.BotStatus
		e.mu.Unlock()
		if status.AccountID == a.ID {
			out = append(out, status)
		}
	}
	return out
}

// StopBot cancels the bot with the given ID and waits for it to exit.
func (s *Service) StopBot(id string) error {
	s.botsMu.RLock()
	entry, ok := s.bots[id]
	s.botsMu.RUnlock()
	if !ok {
		return fmt.Errorf("bots: bot %q not found", id)
	}
	entry.cancel()
	<-entry.done
	return nil
}

// ListBots returns a snapshot of all known bot statuses (running and stopped).
func (s *Service) ListBots() []BotStatus {
	s.botsMu.RLock()
	defer s.botsMu.RUnlock()
	out := make([]BotStatus, 0, len(s.bots))
	for _, e := range s.bots {
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
func (s *Service) StopAllBots() {
	s.botsMu.RLock()
	entries := make([]*botEntry, 0, len(s.bots))
	for _, e := range s.bots {
		entries = append(entries, e)
	}
	s.botsMu.RUnlock()

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
func (s *Service) WaitBot(id string) error {
	s.botsMu.RLock()
	entry, ok := s.bots[id]
	s.botsMu.RUnlock()
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
func (s *Service) GetBot(id string) (*BotStatus, error) {
	s.botsMu.RLock()
	e, ok := s.bots[id]
	s.botsMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("bots: bot %q not found", id)
	}
	e.mu.Lock()
	status := e.BotStatus
	e.mu.Unlock()
	return &status, nil
}

// ── stats-tracking strategy wrapper ──────────────────────────────────────

// statsTrackingStrategy wraps a LiveStrategy and updates the bot entry's
// Ticks, Opens, and Closes counters on every tick.
type statsTrackingStrategy struct {
	inner LiveStrategy
	entry *botEntry
}

func (w *statsTrackingStrategy) Name() string { return w.inner.Name() }

func (w *statsTrackingStrategy) Tick(ctx context.Context, price LivePrice, trades []LiveTrade) *LivePlan {
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
