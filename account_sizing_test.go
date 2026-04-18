package trader

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func newOpenRequest(instrument string, side Side, entry, stop float64) *OpenRequest {
	th := NewTradeHistory(instrument)
	th.Side = side
	th.Stop = PriceFromFloat(stop)
	return &OpenRequest{
		Request: Request{
			TradeCommon: th.TradeCommon,
			Price:       PriceFromFloat(entry),
		},
	}
}

func sizedAccount(equity float64, riskPct float64) Account {
	return Account{
		ID:       "test",
		Currency: "USD",
		Balance:  MoneyFromFloat(equity),
		Equity:   MoneyFromFloat(equity),
		RiskPct:  RateFromFloat(riskPct),
	}
}

func TestSizePosition_HappyPath_EURUSD(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	req := newOpenRequest("EURUSD", Long, 1.3000, 1.2980)

	err := acct.SizePosition(req)
	require.NoError(t, err)
	assert.InDelta(t, float64(100_000), float64(req.Units), 2)
}

func TestSizePosition_HappyPath_LongVsShort(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	long := newOpenRequest("EURUSD", Long, 1.3000, 1.2980)
	short := newOpenRequest("EURUSD", Short, 1.2980, 1.3000)

	err := acct.SizePosition(long)
	require.NoError(t, err)

	err = acct.SizePosition(short)
	require.NoError(t, err)

	assert.Equal(t, long.Units, short.Units)
}

func TestSizePosition_USDJPY(t *testing.T) {
	t.Parallel()

	const usdJpy = 150.0
	acct := sizedAccount(10_000, 0.01)
	req := newOpenRequest("USDJPY", Long, 150.00, 149.50)

	err := acct.SizePosition(req)
	require.NoError(t, err)

	assert.Greater(t, int64(req.Units), int64(0))
	assert.Less(t, float64(req.Units), float64(usdJpy)*1000)
}

func TestSizePosition_RiskPctZero(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0)
	req := newOpenRequest("EURUSD", Long, 1.3000, 1.2990)

	err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "risk_pct")
}

func TestSizePosition_NegativeRiskPct(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, -0.01)
	req := newOpenRequest("EURUSD", Long, 1.3000, 1.2990)

	err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "risk_pct")
}

func TestSizePosition_EquityZero(t *testing.T) {
	t.Parallel()

	acct := Account{
		ID:      "test",
		RiskPct: RateFromFloat(0.02),
		Equity:  0,
	}
	req := newOpenRequest("EURUSD", Long, 1.3000, 1.2990)

	err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "equity")
}

func TestSizePosition_EntryZero(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	req := newOpenRequest("EURUSD", Long, 1.3000, 1.2990)
	req.Price = 0

	err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "entry")
}

func TestSizePosition_StopZero(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	req := newOpenRequest("EURUSD", Long, 1.3000, 1.2990)
	req.Stop = 0

	err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stop")
}

func TestSizePosition_EntryEqualsStop(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	price := PriceFromFloat(1.3000)
	req := newOpenRequest("EURUSD", Long, 1.3000, 1.2990)
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

func TestSizePosition_UnknownInstrument(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	req := newOpenRequest("XXXYYY", Long, 1.3000, 1.2990)

	err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "XXXYYY")
}

func TestSizePosition_UnitsTooSmall(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(1.0, 0.01)
	req := newOpenRequest("EURUSD", Long, 1.3000, 1.1000)

	err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "risk budget too small")
}

func TestSizePosition_ResultUnitsNonZero(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(50_000, 0.01)
	req := newOpenRequest("EURUSD", Long, 1.2000, 1.1950)

	err := acct.SizePosition(req)
	require.NoError(t, err)

	assert.Greater(t, int64(req.Units), int64(0), "Units must be > 0")
}
