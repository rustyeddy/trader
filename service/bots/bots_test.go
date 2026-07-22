package botsvc

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/brokers/oanda"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestAccount builds a session Account backed by a fake (unreachable in
// these tests — no network call is needed to construct or cache a session)
// OANDA client, matching the old testService() helper's setup.
func newTestAccount() *account.Account {
	client := &oanda.Client{BaseURL: "https://api-fxpractice.oanda.com", Token: "test"}
	return account.NewSession("test-account", client, slog.Default())
}

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
	var reg Registry
	acc := newTestAccount()
	_, err := reg.StartBotOnAccount(context.Background(), acc, BotConfig{
		Strategy: StrategyConfig{Kind: "pulse", Params: map[string]any{"stop_pips": 20.0, "hold_bars": 5}},
	}, acc.OANDA, slog.Default())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "instrument")
}

func TestStartBot_UnknownStrategy(t *testing.T) {
	var reg Registry
	acc := newTestAccount()
	_, err := reg.StartBotOnAccount(context.Background(), acc, BotConfig{
		Instrument: "EUR_USD",
		Strategy:   StrategyConfig{Kind: "bogus"},
	}, acc.OANDA, slog.Default())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown strategy kind")
}

func TestStartBot_InvalidDuration(t *testing.T) {
	var reg Registry
	acc := newTestAccount()
	_, err := reg.StartBotOnAccount(context.Background(), acc, BotConfig{
		Instrument:   "EUR_USD",
		TickInterval: "notvalid",
		Strategy:     StrategyConfig{Kind: "pulse", Params: map[string]any{"stop_pips": 20.0, "hold_bars": 5}},
	}, acc.OANDA, slog.Default())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tick_interval")
}

func TestStopBot_NotFound(t *testing.T) {
	var reg Registry
	err := reg.StopBot("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestGetBot_NotFound(t *testing.T) {
	var reg Registry
	_, err := reg.GetBot("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestListBots_Empty(t *testing.T) {
	var reg Registry
	assert.Empty(t, reg.ListBots())
}

func TestStopAllBots_CancelsAndWaits(t *testing.T) {
	var reg Registry
	acc := newTestAccount()

	// Start two bots with long tick intervals so they sit idle in the timer wait.
	cfg := BotConfig{
		TickInterval: "24h",
		Strategy: StrategyConfig{
			Kind:   "pulse",
			Params: map[string]any{"stop_pips": 20.0, "hold_bars": 5},
		},
	}

	cfg.Instrument = "EUR_USD"
	s1, err := reg.StartBotOnAccount(context.Background(), acc, cfg, acc.OANDA, slog.Default())
	require.NoError(t, err)
	assert.Equal(t, "running", s1.Status)

	cfg.Instrument = "GBP_USD"
	s2, err := reg.StartBotOnAccount(context.Background(), acc, cfg, acc.OANDA, slog.Default())
	require.NoError(t, err)
	assert.Equal(t, "running", s2.Status)

	// StopAllBots must return without hanging, and all goroutines must be done.
	done := make(chan struct{})
	go func() {
		reg.StopAllBots()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("StopAllBots did not return within 5 seconds")
	}

	// Both bots should now be in a terminal state.
	for _, id := range []string{s1.ID, s2.ID} {
		b, err := reg.GetBot(id)
		require.NoError(t, err)
		assert.NotEqual(t, "running", b.Status, "bot %s should not be running after StopAllBots", id)
	}
}

func TestStopAllBots_AlreadyStoppedBot(t *testing.T) {
	// StopAllBots must also wait for a bot whose status flipped to "stopped"
	// before close(done) ran — i.e. it must not skip non-"running" entries.
	var reg Registry
	acc := newTestAccount()

	status, err := reg.StartBotOnAccount(context.Background(), acc, BotConfig{
		Instrument:   "EUR_USD",
		TickInterval: "24h",
		Strategy: StrategyConfig{
			Kind:   "pulse",
			Params: map[string]any{"stop_pips": 20.0, "hold_bars": 5},
		},
	}, acc.OANDA, slog.Default())
	require.NoError(t, err)

	// Stop the bot via StopBot first so its status becomes "stopped".
	require.NoError(t, reg.StopBot(status.ID))

	b, _ := reg.GetBot(status.ID)
	assert.Equal(t, "stopped", b.Status)

	// StopAllBots should return immediately (done is already closed).
	done := make(chan struct{})
	go func() {
		reg.StopAllBots()
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

	var reg Registry
	acc := newTestAccount()

	// Use pulse with a very long tick interval so the goroutine just waits.
	status, err := reg.StartBotOnAccount(ctx, acc, BotConfig{
		Instrument:   "EUR_USD",
		TickInterval: "24h",
		RiskPct:      0.5,
		Strategy: StrategyConfig{
			Kind:   "pulse",
			Params: map[string]any{"stop_pips": 20.0, "hold_bars": 5},
		},
	}, acc.OANDA, slog.Default())
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
	bots := reg.ListBots()
	require.Len(t, bots, 1)
	assert.Equal(t, status.ID, bots[0].ID)

	// Get by ID.
	got, err := reg.GetBot(status.ID)
	require.NoError(t, err)
	assert.Equal(t, status.ID, got.ID)

	// Stop it.
	require.NoError(t, reg.StopBot(status.ID))

	// Status should now be "stopped" with StoppedAt set.
	final, err := reg.GetBot(status.ID)
	require.NoError(t, err)
	assert.Equal(t, "stopped", final.Status)
	assert.NotNil(t, final.StoppedAt)
}

func TestRegisterAndLookupTradeBotID(t *testing.T) {
	var reg Registry

	// Unknown trade returns "".
	assert.Equal(t, "", reg.LookupTradeBotID("999"))

	reg.RegisterTradeBotID("trade-1", "bot-abc")
	reg.RegisterTradeBotID("trade-2", "bot-xyz")

	assert.Equal(t, "bot-abc", reg.LookupTradeBotID("trade-1"))
	assert.Equal(t, "bot-xyz", reg.LookupTradeBotID("trade-2"))
	assert.Equal(t, "", reg.LookupTradeBotID("trade-99"))
}

func TestRegisterTradeBotID_EmptyInputsNoOp(t *testing.T) {
	var reg Registry
	// Nil/empty inputs must not panic or pollute the map.
	reg.RegisterTradeBotID("", "bot-abc")
	reg.RegisterTradeBotID("trade-1", "")
	assert.Equal(t, "", reg.LookupTradeBotID(""))
	assert.Equal(t, "", reg.LookupTradeBotID("trade-1"))
}

func TestRegisterTradeBotID_InitializesNilMap(t *testing.T) {
	reg := &Registry{}
	assert.NotPanics(t, func() {
		reg.RegisterTradeBotID("trade-1", "bot-abc")
	})
	assert.Equal(t, "bot-abc", reg.LookupTradeBotID("trade-1"))
}
