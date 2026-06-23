package execution

import (
	"testing"

	"github.com/rustyeddy/trader/market"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeOpenRequest(instrument string, side market.Side, entry, stop float64) *OpenRequest {
	th := NewTradeHistory(instrument)
	th.Side = side
	th.Stop = market.PriceFromFloat(stop)
	return &OpenRequest{
		Request: Request{
			TradeCommon: th.TradeCommon,
			Price:       market.PriceFromFloat(entry),
		},
	}
}

func sizedAccount(equity float64, riskPct float64) Account {
	return Account{
		ID:           "test",
		Currency:     "USD",
		Balance:      market.MoneyFromFloat(equity),
		Equity:       market.MoneyFromFloat(equity),
		RiskFraction: market.RateFromFloat(riskPct),
	}
}

func TestAvailableMargin_UsesFreeMarginWhenSet(t *testing.T) {
	t.Parallel()

	acct := Account{FreeMargin: market.MoneyFromFloat(500), Equity: market.MoneyFromFloat(1000), MarginUsed: market.MoneyFromFloat(200)}
	assert.Equal(t, market.MoneyFromFloat(500), acct.availableMargin())
}

func TestAvailableMargin_ComputesFromEquityMinusMargin(t *testing.T) {
	t.Parallel()

	acct := Account{FreeMargin: 0, Equity: market.MoneyFromFloat(1000), MarginUsed: market.MoneyFromFloat(200)}
	assert.Equal(t, market.MoneyFromFloat(800), acct.availableMargin())
}

func TestAvailableMargin_ReturnsZeroWhenOverMargin(t *testing.T) {
	t.Parallel()

	acct := Account{FreeMargin: 0, Equity: market.MoneyFromFloat(100), MarginUsed: market.MoneyFromFloat(200)}
	assert.Equal(t, market.Money(0), acct.availableMargin())
}

func TestMarginRequiredPerUnit_NilInstrument(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	_, err := acct.marginRequiredPerUnit(nil, market.PriceFromFloat(1.3))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "instrument metadata is nil")
}

func TestMarginRequiredPerUnit_InvalidMarginRate(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	inst := &market.Instrument{Name: "EURUSD", MarginRate: 0, QuoteCurrency: "USD"}
	_, err := acct.marginRequiredPerUnit(inst, market.PriceFromFloat(1.3))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid margin rate")
}

func TestMarginRequiredPerUnit_InvalidPrice(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	inst := market.GetInstrument("EURUSD")
	require.NotNil(t, inst)
	_, err := acct.marginRequiredPerUnit(inst, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid price")
}

func TestMarginRequiredPerUnit_HappyPath_EURUSD(t *testing.T) {
	t.Parallel()

	acct := Account{ID: "test", Currency: "USD", Equity: market.MoneyFromFloat(10_000)}
	inst := market.GetInstrument("EURUSD")
	require.NotNil(t, inst)
	m, err := acct.marginRequiredPerUnit(inst, market.PriceFromFloat(1.30))
	require.NoError(t, err)
	assert.Greater(t, int64(m), int64(0))
}

func TestUnitsByMargin_NoFreeMargin(t *testing.T) {
	t.Parallel()

	acct := Account{
		ID:         "test",
		Currency:   "USD",
		Equity:     market.MoneyFromFloat(100),
		MarginUsed: market.MoneyFromFloat(200),
		FreeMargin: 0,
	}
	req := makeOpenRequest("EURUSD", market.Long, 1.3000, 1.2990)
	_, err := acct.unitsByMargin(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "free margin")
}

func TestUnitsByMargin_UnitsTooSmall(t *testing.T) {
	t.Parallel()

	// FreeMargin is 1 micro-dollar, marginRequiredPerUnit is much larger -> units = 0
	acct := Account{
		ID:         "test",
		Currency:   "USD",
		Equity:     0,
		FreeMargin: 1,
	}
	req := makeOpenRequest("EURUSD", market.Long, 1.3000, 1.2990)
	_, err := acct.unitsByMargin(req)
	require.Error(t, err)
	assert.Error(t, err)
}

func TestLossPerUnit_ZeroStopDistance(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	req := makeOpenRequest("EURUSD", market.Long, 1.3000, 1.2990)
	req.Stop = req.Price
	_, err := acct.lossPerUnit(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "differ")
}

func TestMarginRequired_HappyPath(t *testing.T) {
	t.Parallel()

	acct := Account{ID: "test", Currency: "USD", Equity: market.MoneyFromFloat(10_000)}
	m, err := acct.marginRequired(10_000, market.PriceFromFloat(1.1), "EURUSD")
	require.NoError(t, err)
	assert.Equal(t, market.MoneyFromFloat(220), m)
}

func TestMarginRequired_NegativeUnitsSymmetric(t *testing.T) {
	t.Parallel()

	acct := Account{ID: "test", Currency: "USD", Equity: market.MoneyFromFloat(10_000)}
	price := market.PriceFromFloat(1.2345)

	pos, err := acct.marginRequired(1000, price, "EURUSD")
	require.NoError(t, err)

	neg, err := acct.marginRequired(-1000, price, "EURUSD")
	require.NoError(t, err)

	assert.Equal(t, pos, neg)
}

func TestMarginRequired_ZeroUnits(t *testing.T) {
	t.Parallel()

	acct := Account{ID: "test", Currency: "USD", Equity: market.MoneyFromFloat(10_000)}
	m, err := acct.marginRequired(0, market.PriceFromFloat(1.5), "EURUSD")
	require.NoError(t, err)
	assert.Equal(t, market.Money(0), m)
}

func TestMarginRequired_InvalidPriceZero(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	_, err := acct.marginRequired(1000, 0, "EURUSD")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid price")
}

func TestMarginRequired_UnknownInstrument(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	_, err := acct.marginRequired(1000, market.PriceFromFloat(1.1), "XXXYYY")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown instrument")
}

func TestSizePosition_HappyPath_EURUSD(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	req := makeOpenRequest("EURUSD", market.Long, 1.3000, 1.2980)

	err := acct.SizePosition(req)
	require.NoError(t, err)
	assert.InDelta(t, float64(100_000), float64(req.Units), 2)
}

func TestSizePosition_HappyPath_LongVsShort(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	long := makeOpenRequest("EURUSD", market.Long, 1.3000, 1.2980)
	short := makeOpenRequest("EURUSD", market.Short, 1.2980, 1.3000)

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
	req := makeOpenRequest("USDJPY", market.Long, 150.00, 149.50)

	err := acct.SizePosition(req)
	require.NoError(t, err)

	assert.Greater(t, int64(req.Units), int64(0))
	assert.Less(t, float64(req.Units), float64(usdJpy)*1000)
}

func TestSizePosition_RiskPctZero(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0)
	req := makeOpenRequest("EURUSD", market.Long, 1.3000, 1.2990)

	err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "risk fraction")
}

func TestSizePosition_NegativeRiskPct(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, -0.01)
	req := makeOpenRequest("EURUSD", market.Long, 1.3000, 1.2990)

	err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "risk fraction")
}

func TestSizePosition_EquityZero(t *testing.T) {
	t.Parallel()

	acct := Account{
		ID:           "test",
		RiskFraction: market.RateFromFloat(0.02),
		Equity:       0,
	}
	req := makeOpenRequest("EURUSD", market.Long, 1.3000, 1.2990)

	err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "equity")
}

func TestSizePosition_EntryZero(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	req := makeOpenRequest("EURUSD", market.Long, 1.3000, 1.2990)
	req.Price = 0

	err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "entry")
}

func TestSizePosition_StopZero(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	req := makeOpenRequest("EURUSD", market.Long, 1.3000, 1.2990)
	req.Stop = 0

	err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stop")
}

func TestSizePosition_EntryEqualsStop(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	price := market.PriceFromFloat(1.3000)
	req := makeOpenRequest("EURUSD", market.Long, 1.3000, 1.2990)
	req.Price = price
	req.Stop = price

	err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "differ")
}

func TestSizePosition_InvalidSide(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	req := makeOpenRequest("EURUSD", 0, 1.3000, 1.2990)

	err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid side")
}

func TestSizePosition_UnknownInstrument(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	req := makeOpenRequest("XXXYYY", market.Long, 1.3000, 1.2990)

	err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "XXXYYY")
}

func TestSizePosition_UnitsTooSmall(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(1.0, 0.01)
	req := makeOpenRequest("EURUSD", market.Long, 1.3000, 1.1000)

	err := acct.SizePosition(req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "risk budget too small")
}

func TestSizePosition_ResultUnitsNonZero(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(50_000, 0.01)
	req := makeOpenRequest("EURUSD", market.Long, 1.2000, 1.1950)

	err := acct.SizePosition(req)
	require.NoError(t, err)

	assert.Greater(t, int64(req.Units), int64(0), "Units must be > 0")
}

func TestSizePosition_NilAccount(t *testing.T) {
	t.Parallel()

	var acct *Account
	req := makeOpenRequest("EURUSD", market.Long, 1.3000, 1.2990)
	err := acct.SizePosition(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "account is nil")
}

func TestSizePosition_NilRequest(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	err := acct.SizePosition(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestSizePosition_ShortStopBelowPrice(t *testing.T) {
	t.Parallel()

	acct := sizedAccount(10_000, 0.02)
	req := makeOpenRequest("EURUSD", market.Short, 1.3000, 1.2990)
	err := acct.SizePosition(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "short stop")
}
