package account_test

import (
	"testing"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	req := account.SizeRequest{
		Instrument:     "EURUSD",
		Entry:          types.PriceFromFloat(1.3000),
		Stop:           types.PriceFromFloat(1.2980),
		QuoteToAccount: types.Rate(types.RateScale), // 1.0
	}

	res, err := acct.SizePosition(req)
	require.NoError(t, err)

	assert.InDelta(t, float64(100_000), float64(res.Units), 2)
	assert.InDelta(t, 20.0, res.StopPips, 0.01)
	assert.InDelta(t, 200.0, res.RiskAmount.Float64(), 0.01)
	assert.InDelta(t, 0.002, res.LossPerUnit.Float64(), 1e-6)
	assert.InDelta(t, 200.0, res.EstimatedLoss.Float64(), 0.50)
}

// TestSizePosition_HappyPath_LongVsShort checks that sizing is symmetric:
// a long trade (entry > stop, stop-loss below entry) and a short trade
// (entry < stop, stop-loss above entry) both produce the same unit count
// because the stop distance is taken as an absolute value.
func TestSizePosition_HappyPath_LongVsShort(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	base := account.SizeRequest{
		Instrument:     "EURUSD",
		QuoteToAccount: types.Rate(types.RateScale),
	}

	// Long: entry above stop
	long := base
	long.Entry = types.PriceFromFloat(1.3000)
	long.Stop = types.PriceFromFloat(1.2980)

	// Short: entry below stop
	short := base
	short.Entry = types.PriceFromFloat(1.2980)
	short.Stop = types.PriceFromFloat(1.3000)

	resLong, err := acct.SizePosition(long)
	require.NoError(t, err)

	resShort, err := acct.SizePosition(short)
	require.NoError(t, err)

	assert.Equal(t, resLong.Units, resShort.Units)
	assert.InDelta(t, resLong.StopPips, resShort.StopPips, 0.001)
}

// TestSizePosition_USDJPY verifies sizing with a non-unity quoteToAccount rate.
// For USDJPY in a USD account quoteToAccount ≈ 1/150.
func TestSizePosition_USDJPY(t *testing.T) {
	t.Parallel()

	const usdJpy = 150.0
	acct := sizedAccount(10_000, 0.01)
	req := account.SizeRequest{
		Instrument:     "USDJPY",
		Entry:          types.PriceFromFloat(150.00),
		Stop:           types.PriceFromFloat(149.50), // 50 pips
		QuoteToAccount: types.RateFromFloat(1.0 / usdJpy),
	}

	res, err := acct.SizePosition(req)
	require.NoError(t, err)

	assert.Greater(t, int64(res.Units), int64(0))
	assert.InDelta(t, 50.0, res.StopPips, 0.5)
	assert.InDelta(t, 100.0, res.RiskAmount.Float64(), 0.01) // 1% of 10000
}

// TestSizePosition_RiskPctZero checks that a zero risk percentage is rejected.
func TestSizePosition_RiskPctZero(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0) // RiskPct = 0
	req := account.SizeRequest{
		Instrument:     "EURUSD",
		Entry:          types.PriceFromFloat(1.3000),
		Stop:           types.PriceFromFloat(1.2990),
		QuoteToAccount: types.Rate(types.RateScale),
	}

	_, err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "risk_pct")
}

// TestSizePosition_NegativeRiskPct checks that a negative risk percentage is rejected.
func TestSizePosition_NegativeRiskPct(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, -0.01)
	req := account.SizeRequest{
		Instrument:     "EURUSD",
		Entry:          types.PriceFromFloat(1.3000),
		Stop:           types.PriceFromFloat(1.2990),
		QuoteToAccount: types.Rate(types.RateScale),
	}

	_, err := acct.SizePosition(req)
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
	req := account.SizeRequest{
		Instrument:     "EURUSD",
		Entry:          types.PriceFromFloat(1.3000),
		Stop:           types.PriceFromFloat(1.2990),
		QuoteToAccount: types.Rate(types.RateScale),
	}

	_, err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "equity")
}

// TestSizePosition_EntryZero checks that a zero entry price is rejected.
func TestSizePosition_EntryZero(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	req := account.SizeRequest{
		Instrument:     "EURUSD",
		Entry:          0,
		Stop:           types.PriceFromFloat(1.2990),
		QuoteToAccount: types.Rate(types.RateScale),
	}

	_, err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "entry")
}

// TestSizePosition_StopZero checks that a zero stop price is rejected.
func TestSizePosition_StopZero(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	req := account.SizeRequest{
		Instrument:     "EURUSD",
		Entry:          types.PriceFromFloat(1.3000),
		Stop:           0,
		QuoteToAccount: types.Rate(types.RateScale),
	}

	_, err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stop")
}

// TestSizePosition_EntryEqualsStop checks that entry == stop is rejected.
func TestSizePosition_EntryEqualsStop(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	price := types.PriceFromFloat(1.3000)
	req := account.SizeRequest{
		Instrument:     "EURUSD",
		Entry:          price,
		Stop:           price,
		QuoteToAccount: types.Rate(types.RateScale),
	}

	_, err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "differ")
}

// TestSizePosition_QuoteToAccountZero checks that a zero conversion rate is rejected.
func TestSizePosition_QuoteToAccountZero(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	req := account.SizeRequest{
		Instrument:     "EURUSD",
		Entry:          types.PriceFromFloat(1.3000),
		Stop:           types.PriceFromFloat(1.2990),
		QuoteToAccount: 0,
	}

	_, err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "quote_to_account")
}

// TestSizePosition_UnknownInstrument checks that an unrecognised instrument is rejected.
func TestSizePosition_UnknownInstrument(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	req := account.SizeRequest{
		Instrument:     "XXXYYY",
		Entry:          types.PriceFromFloat(1.3000),
		Stop:           types.PriceFromFloat(1.2990),
		QuoteToAccount: types.Rate(types.RateScale),
	}

	_, err := acct.SizePosition(req)
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
	req := account.SizeRequest{
		Instrument:     "EURUSD",
		Entry:          types.PriceFromFloat(1.3000),
		Stop:           types.PriceFromFloat(1.1000), // 2000-pip stop
		QuoteToAccount: types.Rate(types.RateScale),
	}

	_, err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "units")
}

// TestSizePosition_ResultFieldsNonZero is a broad smoke test ensuring none
// of the result fields are accidentally left at their zero value on a valid call.
func TestSizePosition_ResultFieldsNonZero(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(50_000, 0.01)
	req := account.SizeRequest{
		Instrument:     "EURUSD",
		Entry:          types.PriceFromFloat(1.2000),
		Stop:           types.PriceFromFloat(1.1950), // 50-pip stop
		QuoteToAccount: types.Rate(types.RateScale),
	}

	res, err := acct.SizePosition(req)
	require.NoError(t, err)

	assert.Greater(t, int64(res.Units), int64(0), "Units must be > 0")
	assert.Greater(t, res.StopPips, 0.0, "StopPips must be > 0")
	assert.Greater(t, int64(res.RiskAmount), int64(0), "RiskAmount must be > 0")
	assert.Greater(t, int64(res.LossPerUnit), int64(0), "LossPerUnit must be > 0")
	assert.Greater(t, int64(res.EstimatedLoss), int64(0), "EstimatedLoss must be > 0")
}
