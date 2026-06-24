package execution

import (
	"testing"

	"github.com/rustyeddy/trader/market"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLotUnrealizedPL_LongAndShort verifies expected behavior for this component.
func TestLotUnrealizedPL_LongAndShort(t *testing.T) {
	t.Parallel()

	units := market.Units(1000)
	longLot := &Lot{TradeCommon: &TradeCommon{Side: market.Long, Units: units}, EntryPrice: market.PriceFromFloat(1.20000), OriginalUnits: units, RemainingUnits: units}
	shortLot := &Lot{TradeCommon: &TradeCommon{Side: market.Short, Units: units}, EntryPrice: market.PriceFromFloat(1.20000), OriginalUnits: units, RemainingUnits: units}

	// +10 pips move on 1,000 units is +1.00 in quote currency.
	upMove := market.PriceFromFloat(1.20100)
	qta := market.Rate(market.RateScale) // 1:1 conversion

	longPL, err := lotUnrealizedPNL(longLot, upMove, qta)
	require.NoError(t, err)
	assert.Equal(t, market.Money(market.MoneyScale), longPL)

	shortPL, err := lotUnrealizedPNL(shortLot, upMove, qta)
	require.NoError(t, err)
	assert.Equal(t, -market.Money(market.MoneyScale), shortPL)

	// -10 pips move on 1,000 units is -1.00 for long and +1.00 for short.
	downMove := market.PriceFromFloat(1.19900)
	longPL2, err := lotUnrealizedPNL(longLot, downMove, qta)
	require.NoError(t, err)
	assert.Equal(t, -market.Money(market.MoneyScale), longPL2)

	shortPL2, err := lotUnrealizedPNL(shortLot, downMove, qta)
	require.NoError(t, err)
	assert.Equal(t, market.Money(market.MoneyScale), shortPL2)
}

// TestLotUnrealizedPL_QuoteToAccountConversion verifies expected behavior for this component.
func TestLotUnrealizedPL_QuoteToAccountConversion(t *testing.T) {
	t.Parallel()

	units := market.Units(1000)
	lot := &Lot{TradeCommon: &TradeCommon{Side: market.Long, Units: units}, EntryPrice: market.PriceFromFloat(1.20000), OriginalUnits: units, RemainingUnits: units}
	current := market.PriceFromFloat(1.20100) // +1.00 quote currency P/L
	// quoteToAccount as Rate (scaled by RateScale)
	qta := market.Rate(int64(market.PriceFromFloat(1.500)) * int64(market.RateScale) / int64(market.PriceScale))

	pl, err := lotUnrealizedPNL(lot, current, qta)
	require.NoError(t, err)
	// +1.00 quote * 1.5 conversion = +1.50 account
	assert.Equal(t, market.Money(1_500_000), pl)
}
