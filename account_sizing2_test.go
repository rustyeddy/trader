package trader

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── availableMargin ──────────────────────────────────────────────────────────

func TestAvailableMargin_UsesFreeMarginWhenSet(t *testing.T) {
	acct := Account{FreeMargin: MoneyFromFloat(500), Equity: MoneyFromFloat(1000), MarginUsed: MoneyFromFloat(200)}
	assert.Equal(t, MoneyFromFloat(500), acct.availableMargin())
}

func TestAvailableMargin_ComputesFromEquityMinusMargin(t *testing.T) {
	acct := Account{FreeMargin: 0, Equity: MoneyFromFloat(1000), MarginUsed: MoneyFromFloat(200)}
	assert.Equal(t, MoneyFromFloat(800), acct.availableMargin())
}

func TestAvailableMargin_ReturnsZeroWhenOverMargin(t *testing.T) {
	acct := Account{FreeMargin: 0, Equity: MoneyFromFloat(100), MarginUsed: MoneyFromFloat(200)}
	assert.Equal(t, Money(0), acct.availableMargin())
}

// ─── minUnits ─────────────────────────────────────────────────────────────────

func TestMinUnits_ReturnsSecondArgWhenSmaller(t *testing.T) {
	assert.Equal(t, Units(3), minUnits(5, 3))
}

func TestMinUnits_ReturnsFirstArgWhenSmaller(t *testing.T) {
	assert.Equal(t, Units(3), minUnits(3, 5))
}

// ─── marginPerUnit ────────────────────────────────────────────────────────────

func TestMarginPerUnit_NilInstrument(t *testing.T) {
	acct := sizedAccount(10_000, 0.02)
	_, err := acct.marginPerUnit(nil, PriceFromFloat(1.3))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "instrument is nil")
}

func TestMarginPerUnit_InvalidMarginRate(t *testing.T) {
	acct := sizedAccount(10_000, 0.02)
	inst := &Instrument{Name: "EURUSD", MarginRate: 0, QuoteCurrency: "USD"}
	_, err := acct.marginPerUnit(inst, PriceFromFloat(1.3))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid margin rate")
}

func TestMarginPerUnit_InvalidPrice(t *testing.T) {
	acct := sizedAccount(10_000, 0.02)
	inst := GetInstrument("EURUSD")
	require.NotNil(t, inst)
	_, err := acct.marginPerUnit(inst, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid price")
}

func TestMarginPerUnit_HappyPath_EURUSD(t *testing.T) {
	acct := Account{ID: "test", Currency: "USD", Equity: MoneyFromFloat(10_000)}
	inst := GetInstrument("EURUSD")
	require.NotNil(t, inst)
	m, err := acct.marginPerUnit(inst, PriceFromFloat(1.30))
	require.NoError(t, err)
	assert.Greater(t, int64(m), int64(0))
}

// ─── unitsByMargin ────────────────────────────────────────────────────────────

func TestUnitsByMargin_NoFreeMargin(t *testing.T) {
	acct := Account{
		ID:         "test",
		Currency:   "USD",
		Equity:     MoneyFromFloat(100),
		MarginUsed: MoneyFromFloat(200),
		FreeMargin: 0,
	}
	req := makeOpenRequest("EURUSD", Long, 1.3000, 1.2990)
	_, err := acct.unitsByMargin(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "free margin")
}

func TestUnitsByMargin_UnitsTooSmall(t *testing.T) {
	// FreeMargin is 1 micro-dollar, marginPerUnit is much larger → units = 0
	acct := Account{
		ID:         "test",
		Currency:   "USD",
		Equity:     0,
		FreeMargin: 1, // 1 micro-unit of account currency
	}
	req := makeOpenRequest("EURUSD", Long, 1.3000, 1.2990)
	_, err := acct.unitsByMargin(req)
	require.Error(t, err)
	// either "free margin too small" or "free margin must be > 0"
	assert.Error(t, err)
}

// ─── lossPerUnit ─────────────────────────────────────────────────────────────

func TestLossPerUnit_ZeroStopDistance(t *testing.T) {
	acct := sizedAccount(10_000, 0.02)
	req := makeOpenRequest("EURUSD", Long, 1.3000, 1.2990)
	req.Stop = req.Price // force priceDist == 0
	_, err := acct.lossPerUnit(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "differ")
}

// ─── TradeMargin ─────────────────────────────────────────────────────────────

func TestTradeMargin_InvalidPriceZero(t *testing.T) {
	acct := sizedAccount(10_000, 0.02)
	_, err := acct.TradeMargin(1000, 0, "EURUSD")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid price")
}

func TestTradeMargin_HappyPath(t *testing.T) {
	acct := Account{ID: "test", Currency: "USD", Equity: MoneyFromFloat(10_000)}
	m, err := acct.TradeMargin(1000, PriceFromFloat(1.3), "EURUSD")
	require.NoError(t, err)
	assert.Greater(t, int64(m), int64(0))
}

// ─── SizePosition additional branches ────────────────────────────────────────

func TestSizePosition_NilAccount(t *testing.T) {
	var acct *Account
	req := makeOpenRequest("EURUSD", Long, 1.3000, 1.2990)
	err := acct.SizePosition(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "account is nil")
}

func TestSizePosition_NilRequest(t *testing.T) {
	acct := sizedAccount(10_000, 0.02)
	err := acct.SizePosition(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestSizePosition_ShortStopBelowPrice(t *testing.T) {
	acct := sizedAccount(10_000, 0.02)
	// For Short, stop must be > price; here stop < price → error
	req := makeOpenRequest("EURUSD", Short, 1.3000, 1.2990)
	err := acct.SizePosition(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "short stop")
}
