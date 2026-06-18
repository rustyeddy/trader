package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// BotConfig is the payload needed to start a live strategy bot via the API.
type BotConfig struct {
	Instrument     string         `json:"instrument"`
	TickInterval   string         `json:"tick_interval"`    // e.g. "60s", "5m"
	RiskPct        float64        `json:"risk_pct"`
	MaxUnits       int64          `json:"max_units"`
	MaxPositionUSD float64        `json:"max_position_usd"`
	Strategy       StrategyConfig `json:"strategy"`
}

// BotStatus is the public view of a running or stopped bot.
type BotStatus struct {
	ID           string    `json:"id"`
	Instrument   string    `json:"instrument"`
	StrategyName string    `json:"strategy_name"`
	StartedAt    time.Time `json:"started_at"`
	Status       string    `json:"status"` // "running" | "stopped" | "error"
	Error        string    `json:"error,omitempty"`
}

// botEntry is the internal record tracking a bot goroutine.
type botEntry struct {
	BotStatus
	cancel context.CancelFunc
	done   <-chan struct{}
}

// StartBot builds and launches a live strategy bot inside the serve process.
// Returns the bot's initial status (Status="running"). The bot runs until
// StopBot is called or the parent context is cancelled.
func (s *Service) StartBot(ctx context.Context, cfg BotConfig) (*BotStatus, error) {
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

	entry := &botEntry{
		BotStatus: BotStatus{
			ID:           id,
			Instrument:   cfg.Instrument,
			StrategyName: strategy.Name(),
			StartedAt:    time.Now().UTC(),
			Status:       "running",
		},
		cancel: cancel,
		done:   done,
	}

	s.botsMu.Lock()
	if s.bots == nil {
		s.bots = make(map[string]*botEntry)
	}
	s.bots[id] = entry
	s.botsMu.Unlock()

	go func() {
		defer close(done)
		runErr := s.RunLiveStrategy(botCtx, LiveRunConfig{
			Instrument:     cfg.Instrument,
			TickInterval:   interval,
			Strategy:       strategy,
			RiskPct:        cfg.RiskPct,
			MaxUnits:       cfg.MaxUnits,
			MaxPositionUSD: cfg.MaxPositionUSD,
		})
		s.botsMu.Lock()
		if e, ok := s.bots[id]; ok {
			if runErr != nil && botCtx.Err() == nil {
				e.Status = "error"
				e.Error = runErr.Error()
			} else {
				e.Status = "stopped"
			}
		}
		s.botsMu.Unlock()
	}()

	status := entry.BotStatus
	return &status, nil
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
		out = append(out, e.BotStatus)
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

// GetBot returns the status of one bot by ID.
func (s *Service) GetBot(id string) (*BotStatus, error) {
	s.botsMu.RLock()
	defer s.botsMu.RUnlock()
	e, ok := s.bots[id]
	if !ok {
		return nil, fmt.Errorf("bots: bot %q not found", id)
	}
	status := e.BotStatus
	return &status, nil
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
