package account

import (
	"testing"

	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLotUnrealizedPL_LongAndShort verifies expected behavior for this component.
func TestLotUnrealizedPL_LongAndShort(t *testing.T) {
	t.Parallel()

	units := types.Units(1000)
	longLot := &Lot{TradeCommon: &TradeCommon{Side: types.Long, Units: units}, EntryPrice: types.PriceFromFloat(1.20000), OriginalUnits: units, RemainingUnits: units}
	shortLot := &Lot{TradeCommon: &TradeCommon{Side: types.Short, Units: units}, EntryPrice: types.PriceFromFloat(1.20000), OriginalUnits: units, RemainingUnits: units}

	// +10 pips move on 1,000 units is +1.00 in quote currency.
	upMove := types.PriceFromFloat(1.20100)
	qta := types.Rate(types.RateScale) // 1:1 conversion

	longPL, err := lotUnrealizedPNL(longLot, upMove, qta)
	require.NoError(t, err)
	assert.Equal(t, types.Money(types.MoneyScale), longPL)

	shortPL, err := lotUnrealizedPNL(shortLot, upMove, qta)
	require.NoError(t, err)
	assert.Equal(t, -types.Money(types.MoneyScale), shortPL)

	// -10 pips move on 1,000 units is -1.00 for long and +1.00 for short.
	downMove := types.PriceFromFloat(1.19900)
	longPL2, err := lotUnrealizedPNL(longLot, downMove, qta)
	require.NoError(t, err)
	assert.Equal(t, -types.Money(types.MoneyScale), longPL2)

	shortPL2, err := lotUnrealizedPNL(shortLot, downMove, qta)
	require.NoError(t, err)
	assert.Equal(t, types.Money(types.MoneyScale), shortPL2)
}

// TestLotUnrealizedPL_QuoteToAccountConversion verifies expected behavior for this component.
func TestLotUnrealizedPL_QuoteToAccountConversion(t *testing.T) {
	t.Parallel()

	units := types.Units(1000)
	lot := &Lot{TradeCommon: &TradeCommon{Side: types.Long, Units: units}, EntryPrice: types.PriceFromFloat(1.20000), OriginalUnits: units, RemainingUnits: units}
	current := types.PriceFromFloat(1.20100) // +1.00 quote currency P/L
	// quoteToAccount as Rate (scaled by RateScale)
	qta := types.Rate(int64(types.PriceFromFloat(1.500)) * int64(types.RateScale) / int64(types.PriceScale))

	pl, err := lotUnrealizedPNL(lot, current, qta)
	require.NoError(t, err)
	// +1.00 quote * 1.5 conversion = +1.50 account
	assert.Equal(t, types.Money(1_500_000), pl)
}
