package account_test

import (
	"testing"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newOpenRequest(instrument string, side types.Side, entry, stop float64) *types.OpenRequest {
	th := types.NewTradeHistory(instrument)
	th.Side = side
	th.Stop = types.PriceFromFloat(stop)
	return &types.OpenRequest{
		Request: types.Request{
			TradeCommon: th.TradeCommon,
			Price:       types.PriceFromFloat(entry),
		},
	}
}

// sizedAccount returns an Account pre-configured for position-sizing tests.
func sizedAccount(equity float64, riskPct float64) account.Account {
	return account.Account{
		ID:       "test",
		Currency: "USD",
		Balance:  types.MoneyFromFloat(equity),
		Equity:   types.MoneyFromFloat(equity),
		RiskPct:  types.RateFromFloat(riskPct),
	}
}

// TestSizePosition_HappyPath_EURUSD verifies a standard EURUSD sizing calculation
// for a USD account where quoteToAccount == 1.0.
//
// With $10,000 equity, 2% risk, entry=1.3000, stop=1.2980 (20-pip stop):
//
//	riskAmount   = 10000 * 0.02 = $200
//	stopPips     = |1.3000 - 1.2980| / 0.0001 = 20
//	pipValuePerUnit = 0.0001 * 1.0 = 0.0001
//	units        = floor(200 / (20 * 0.0001)) = floor(100000) = 100000
//	lossPerUnit  = |1.3000 - 1.2980| * 1.0 = 0.0020
//	estimatedLoss = 100000 * 0.0020 = $200
func TestSizePosition_HappyPath_EURUSD(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	req := newOpenRequest("EURUSD", types.Long, 1.3000, 1.2980)

	err := acct.SizePosition(req)
	require.NoError(t, err)
	assert.InDelta(t, float64(100_000), float64(req.Units), 2)
}

// TestSizePosition_HappyPath_LongVsShort checks that sizing is symmetric:
// a long trade (entry > stop, stop-loss below entry) and a short trade
// (entry < stop, stop-loss above entry) both produce the same unit count
// because the stop distance is taken as an absolute value.
func TestSizePosition_HappyPath_LongVsShort(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	long := newOpenRequest("EURUSD", types.Long, 1.3000, 1.2980)
	short := newOpenRequest("EURUSD", types.Short, 1.2980, 1.3000)

	err := acct.SizePosition(long)
	require.NoError(t, err)

	err = acct.SizePosition(short)
	require.NoError(t, err)

	assert.Equal(t, long.Units, short.Units)
}

// TestSizePosition_USDJPY verifies sizing with a non-unity quoteToAccount rate.
// For USDJPY in a USD account quoteToAccount ≈ 1/150.
func TestSizePosition_USDJPY(t *testing.T) {
	t.Parallel()

	const usdJpy = 150.0
	acct := sizedAccount(10_000, 0.01)
	req := newOpenRequest("USDJPY", types.Long, 150.00, 149.50)

	err := acct.SizePosition(req)
	require.NoError(t, err)

	assert.Greater(t, int64(req.Units), int64(0))
	assert.Less(t, float64(req.Units), float64(usdJpy)*1000)
}

// TestSizePosition_RiskPctZero checks that a zero risk percentage is rejected.
func TestSizePosition_RiskPctZero(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0) // RiskPct = 0
	req := newOpenRequest("EURUSD", types.Long, 1.3000, 1.2990)

	err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "risk_pct")
}

// TestSizePosition_NegativeRiskPct checks that a negative risk percentage is rejected.
func TestSizePosition_NegativeRiskPct(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, -0.01)
	req := newOpenRequest("EURUSD", types.Long, 1.3000, 1.2990)

	err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "risk_pct")
}

// TestSizePosition_EquityZero checks that zero equity is rejected.
func TestSizePosition_EquityZero(t *testing.T) {
	t.Parallel()

	acct := account.Account{
		ID:      "test",
		RiskPct: types.RateFromFloat(0.02),
		Equity:  0,
	}
	req := newOpenRequest("EURUSD", types.Long, 1.3000, 1.2990)

	err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "equity")
}

// TestSizePosition_EntryZero checks that a zero entry price is rejected.
func TestSizePosition_EntryZero(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	req := newOpenRequest("EURUSD", types.Long, 1.3000, 1.2990)
	req.Price = 0

	err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "entry")
}

// TestSizePosition_StopZero checks that a zero stop price is rejected.
func TestSizePosition_StopZero(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	req := newOpenRequest("EURUSD", types.Long, 1.3000, 1.2990)
	req.Stop = 0

	err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stop")
}

// TestSizePosition_EntryEqualsStop checks that entry == stop is rejected.
func TestSizePosition_EntryEqualsStop(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	price := types.PriceFromFloat(1.3000)
	req := newOpenRequest("EURUSD", types.Long, 1.3000, 1.2990)
	req.Price = price
	req.Stop = price

	err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "differ")
}

func TestSizePosition_InvalidSide(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	req := newOpenRequest("EURUSD", 0, 1.3000, 1.2990)

	err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid side")
}

// TestSizePosition_UnknownInstrument checks that an unrecognised instrument is rejected.
func TestSizePosition_UnknownInstrument(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	req := newOpenRequest("XXXYYY", types.Long, 1.3000, 1.2990)

	err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "XXXYYY")
}

// TestSizePosition_UnitsTooSmall checks that a computed unit count that is
// at or below the instrument's MinimumTradeSize is rejected.
// With $1 equity and 1% risk, riskAmount = $0.01.  With a 2000-pip stop on
// EURUSD (pipValuePerUnit = 0.0001):
//
//	units = floor(0.01 / (2000 * 0.0001)) = floor(0.05) = 0 ≤ MinimumTradeSize(1).
func TestSizePosition_UnitsTooSmall(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(1.0, 0.01) // $1 equity, 1% risk => riskAmount = $0.01
	req := newOpenRequest("EURUSD", types.Long, 1.3000, 1.1000)

	err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "risk budget too small")
}

func TestSizePosition_ResultUnitsNonZero(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(50_000, 0.01)
	req := newOpenRequest("EURUSD", types.Long, 1.2000, 1.1950)

	err := acct.SizePosition(req)
	require.NoError(t, err)

	assert.Greater(t, int64(req.Units), int64(0), "Units must be > 0")
}
