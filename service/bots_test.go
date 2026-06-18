package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)


func TestNewBotID_Unique(t *testing.T) {
	ids := map[string]struct{}{}
	for i := 0; i < 100; i++ {
		id := newBotID()
		assert.NotContains(t, ids, id, "duplicate bot ID")
		ids[id] = struct{}{}
		assert.Contains(t, id, "bot-")
	}
}

func TestParseBotDuration_Empty(t *testing.T) {
	d, err := parseBotDuration("", 60*time.Second)
	require.NoError(t, err)
	assert.Equal(t, 60*time.Second, d)
}

func TestParseBotDuration_Valid(t *testing.T) {
	d, err := parseBotDuration("30s", time.Minute)
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, d)
}

func TestParseBotDuration_Invalid(t *testing.T) {
	_, err := parseBotDuration("notaduration", time.Minute)
	require.Error(t, err)
}

func TestStartBot_MissingInstrument(t *testing.T) {
	svc := testService()
	_, err := svc.StartBot(context.Background(), BotConfig{
		Strategy: StrategyConfig{Kind: "pulse", Params: map[string]any{"stop_pips": 20.0, "hold_bars": 5}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "instrument")
}

func TestStartBot_UnknownStrategy(t *testing.T) {
	svc := testService()
	_, err := svc.StartBot(context.Background(), BotConfig{
		Instrument: "EUR_USD",
		Strategy:   StrategyConfig{Kind: "bogus"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown strategy kind")
}

func TestStartBot_InvalidDuration(t *testing.T) {
	svc := testService()
	_, err := svc.StartBot(context.Background(), BotConfig{
		Instrument:   "EUR_USD",
		TickInterval: "notvalid",
		Strategy:     StrategyConfig{Kind: "pulse", Params: map[string]any{"stop_pips": 20.0, "hold_bars": 5}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tick_interval")
}

func TestStopBot_NotFound(t *testing.T) {
	svc := testService()
	err := svc.StopBot("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestGetBot_NotFound(t *testing.T) {
	svc := testService()
	_, err := svc.GetBot("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestListBots_Empty(t *testing.T) {
	svc := testService()
	assert.Empty(t, svc.ListBots())
}

func TestStopAllBots_CancelsAndWaits(t *testing.T) {
	svc := testService()

	// Start two bots with long tick intervals so they sit idle in the timer wait.
	cfg := BotConfig{
		TickInterval: "24h",
		Strategy: StrategyConfig{
			Kind:   "pulse",
			Params: map[string]any{"stop_pips": 20.0, "hold_bars": 5},
		},
	}

	cfg.Instrument = "EUR_USD"
	s1, err := svc.StartBot(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "running", s1.Status)

	cfg.Instrument = "GBP_USD"
	s2, err := svc.StartBot(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "running", s2.Status)

	// StopAllBots must return without hanging, and all goroutines must be done.
	done := make(chan struct{})
	go func() {
		svc.StopAllBots()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("StopAllBots did not return within 5 seconds")
	}

	// Both bots should now be in a terminal state.
	for _, id := range []string{s1.ID, s2.ID} {
		b, err := svc.GetBot(id)
		require.NoError(t, err)
		assert.NotEqual(t, "running", b.Status, "bot %s should not be running after StopAllBots", id)
	}
}

func TestStopAllBots_AlreadyStoppedBot(t *testing.T) {
	// StopAllBots must also wait for a bot whose status flipped to "stopped"
	// before close(done) ran — i.e. it must not skip non-"running" entries.
	svc := testService()

	status, err := svc.StartBot(context.Background(), BotConfig{
		Instrument:   "EUR_USD",
		TickInterval: "24h",
		Strategy: StrategyConfig{
			Kind:   "pulse",
			Params: map[string]any{"stop_pips": 20.0, "hold_bars": 5},
		},
	})
	require.NoError(t, err)

	// Stop the bot via StopBot first so its status becomes "stopped".
	require.NoError(t, svc.StopBot(status.ID))

	b, _ := svc.GetBot(status.ID)
	assert.Equal(t, "stopped", b.Status)

	// StopAllBots should return immediately (done is already closed).
	done := make(chan struct{})
	go func() {
		svc.StopAllBots()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("StopAllBots hung on an already-stopped bot")
	}
}

func TestStartBot_RegistersAndStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	svc := testService()

	// Use pulse with a very long tick interval so the goroutine just waits.
	status, err := svc.StartBot(ctx, BotConfig{
		Instrument:   "EUR_USD",
		TickInterval: "24h",
		RiskPct:      0.5,
		Strategy: StrategyConfig{
			Kind:   "pulse",
			Params: map[string]any{"stop_pips": 20.0, "hold_bars": 5},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "running", status.Status)
	assert.Equal(t, "EUR_USD", status.Instrument)
	assert.NotEmpty(t, status.ID)

	// New fields present on initial status.
	assert.Equal(t, "pulse", status.StrategyKind)
	assert.Equal(t, "24h", status.TickInterval)
	assert.Equal(t, 0.5, status.RiskPct)
	assert.Nil(t, status.StoppedAt)

	// Listed.
	bots := svc.ListBots()
	require.Len(t, bots, 1)
	assert.Equal(t, status.ID, bots[0].ID)

	// Get by ID.
	got, err := svc.GetBot(status.ID)
	require.NoError(t, err)
	assert.Equal(t, status.ID, got.ID)

	// Stop it.
	require.NoError(t, svc.StopBot(status.ID))

	// Status should now be "stopped" with StoppedAt set.
	final, err := svc.GetBot(status.ID)
	require.NoError(t, err)
	assert.Equal(t, "stopped", final.Status)
	assert.NotNil(t, final.StoppedAt)
}

func TestRegisterAndLookupTradeBotID(t *testing.T) {
	svc := testService()

	// Unknown trade returns "".
	assert.Equal(t, "", svc.LookupTradeBotID("999"))

	svc.RegisterTradeBotID("trade-1", "bot-abc")
	svc.RegisterTradeBotID("trade-2", "bot-xyz")

	assert.Equal(t, "bot-abc", svc.LookupTradeBotID("trade-1"))
	assert.Equal(t, "bot-xyz", svc.LookupTradeBotID("trade-2"))
	assert.Equal(t, "", svc.LookupTradeBotID("trade-99"))
}

func TestRegisterTradeBotID_EmptyInputsNoOp(t *testing.T) {
	svc := testService()
	// Nil/empty inputs must not panic or pollute the map.
	svc.RegisterTradeBotID("", "bot-abc")
	svc.RegisterTradeBotID("trade-1", "")
	assert.Equal(t, "", svc.LookupTradeBotID(""))
	assert.Equal(t, "", svc.LookupTradeBotID("trade-1"))
}
