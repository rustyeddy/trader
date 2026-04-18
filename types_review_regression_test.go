package trader

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestOrderTypeString_UnknownIsSafe(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "<unknown>", OrderType(255).String())
}

func TestOrderStatusString_Values(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "canceled", OrderCanceled.String())
	assert.Equal(t, "<unknown>", OrderStatus(255).String())
}

func TestPositionUnrealizedPL_LongAndShort(t *testing.T) {
	t.Parallel()

	longPos := &Position{TradeCommon: &TradeCommon{Side: Long, Units: 1000}, FillPrice: PriceFromFloat(1.20000)}
	shortPos := &Position{TradeCommon: &TradeCommon{Side: Short, Units: 1000}, FillPrice: PriceFromFloat(1.20000)}

	// +10 pips move on 1,000 units is +1.00 in quote currency.
	upMove := PriceFromFloat(1.20100)
	spot := PriceFromFloat(1.00000)
	assert.Equal(t, Money(MoneyScale), longPos.UnrealizedPL(upMove, spot))
	assert.Equal(t, -Money(MoneyScale), shortPos.UnrealizedPL(upMove, spot))

	// -10 pips move on 1,000 units is -1.00 for long and +1.00 for short.
	downMove := PriceFromFloat(1.19900)
	assert.Equal(t, -Money(MoneyScale), longPos.UnrealizedPL(downMove, spot))
	assert.Equal(t, Money(MoneyScale), shortPos.UnrealizedPL(downMove, spot))
}

func TestPositionUnrealizedPL_QuoteToAccountConversion(t *testing.T) {
	t.Parallel()

	pos := &Position{TradeCommon: &TradeCommon{Side: Long, Units: 1000}, FillPrice: PriceFromFloat(1.20000)}
	current := PriceFromFloat(1.20100)      // +1.00 quote currency P/L
	quoteToAccount := PriceFromFloat(1.500) // quote->account conversion

	// +1.00 quote * 1.5 conversion = +1.50 account
	assert.Equal(t, Money(1_500_000), pos.UnrealizedPL(current, quoteToAccount))
}

func TestIsFXMarketClosed_BackCompatWrapper(t *testing.T) {
	t.Parallel()

	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	// Wrapper should match canonical function behavior.
	tBerlin := time.Date(2024, 6, 8, 1, 0, 0, 0, berlin)
	assert.Equal(t, IsForexMarketClosed(tBerlin), IsFXMarketClosed(tBerlin))
}

func TestIsForexMarketClosed_NewYorkBoundaries(t *testing.T) {
	t.Parallel()

	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	assert.True(t, IsForexMarketClosed(time.Date(2024, 6, 7, 17, 0, 0, 0, ny)))
	assert.False(t, IsForexMarketClosed(time.Date(2024, 6, 9, 17, 0, 0, 0, ny)))
	assert.True(t, IsForexMarketClosed(time.Date(2024, 12, 24, 13, 0, 0, 0, ny)))
}
