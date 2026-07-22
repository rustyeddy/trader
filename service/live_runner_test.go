package service

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader/account"
	"github.com/stretchr/testify/assert"
)

// stubStrategy is a minimal account.LiveStrategy for exercising
// Service.RunLiveStrategy's early validation without a real strategy.
type stubStrategy struct{ name string }

func (s *stubStrategy) Name() string { return s.name }
func (s *stubStrategy) Tick(_ context.Context, _ account.LivePrice, _ []account.LiveTrade) *account.LivePlan {
	return nil
}

// ── RunLiveStrategy — config validation ───────────────────────────────────────

func TestRunLiveStrategy_NilStrategy(t *testing.T) {
	svc := &Service{}
	err := svc.RunLiveStrategy(context.Background(), account.LiveRunConfig{
		Instrument: "EUR_USD",
		Strategy:   nil,
	})
	assert.ErrorContains(t, err, "strategy is required")
}

func TestRunLiveStrategy_EmptyInstrument(t *testing.T) {
	svc := &Service{}
	err := svc.RunLiveStrategy(context.Background(), account.LiveRunConfig{
		Strategy:   &stubStrategy{name: "stub"},
		Instrument: "",
	})
	assert.ErrorContains(t, err, "instrument is required")
}

func TestRunLiveStrategy_NoOANDA_FailsAtResolve(t *testing.T) {
	svc := &Service{} // no OANDA client
	err := svc.RunLiveStrategy(context.Background(), account.LiveRunConfig{
		Instrument: "EUR_USD",
		Strategy:   &stubStrategy{name: "stub"},
	})
	assert.ErrorContains(t, err, "OANDA")
}
