package journal

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatTradeOrg(t *testing.T) {
	t.Parallel()

	open := time.Date(2024, 3, 15, 10, 30, 45, 0, time.UTC)
	close := time.Date(2024, 3, 15, 14, 20, 30, 0, time.UTC)

	trade := TradeRecord{
		TradeID:    "trade-12345678-abcd",
		Instrument: "EUR_USD",
		Units:      1000,
		EntryPrice: 1.08500,
		ExitPrice:  1.08750,
		OpenTime:   open,
		CloseTime:  close,
		RealizedPL: 250.00,
		Reason:     "trend-following",
	}

	result := FormatTradeOrg(trade)

	// Check heading
	assert.Contains(t, result, "** Trade: EUR_USD (trade-12)")

	// Check properties drawer
	assert.Contains(t, result, ":PROPERTIES:")
	assert.Contains(t, result, ":TRADE_ID: trade-12345678-abcd")
	assert.Contains(t, result, ":ID: trade-12345678-abcd")
	assert.Contains(t, result, ":INSTRUMENT: EUR_USD")
	assert.Contains(t, result, ":UNITS: 1000")
	assert.Contains(t, result, ":ENTRY_PRICE: 1.08500")
	assert.Contains(t, result, ":EXIT_PRICE: 1.08750")
	assert.Contains(t, result, ":OPEN_TIME: 2024-03-15T10:30:45Z")
	assert.Contains(t, result, ":CLOSE_TIME: 2024-03-15T14:20:30Z")
	assert.Contains(t, result, ":REALIZED_PL: 250.00")
	assert.Contains(t, result, ":REASON: trend-following")
	assert.Contains(t, result, ":END:")

	// Check narrative sections
	assert.Contains(t, result, "*** Thesis")
	assert.Contains(t, result, "*** Execution")
	assert.Contains(t, result, "*** Review")
}

func TestFormatTradeOrgShortID(t *testing.T) {
	t.Parallel()

	trade := TradeRecord{
		TradeID:    "short",
		Instrument: "GBP_USD",
		Units:      500,
		EntryPrice: 1.25000,
		ExitPrice:  1.25100,
		OpenTime:   time.Now(),
		CloseTime:  time.Now(),
		RealizedPL: 50.00,
		Reason:     "test",
	}

	result := FormatTradeOrg(trade)
	assert.Contains(t, result, "** Trade: GBP_USD (short)")
}

func TestFormatTradeOrgNegativePL(t *testing.T) {
	t.Parallel()

	trade := TradeRecord{
		TradeID:    "loss-trade",
		Instrument: "USD_JPY",
		Units:      2000,
		EntryPrice: 150.50,
		ExitPrice:  150.25,
		OpenTime:   time.Now(),
		CloseTime:  time.Now(),
		RealizedPL: -500.00,
		Reason:     "stop-loss",
	}

	result := FormatTradeOrg(trade)
	assert.Contains(t, result, ":REALIZED_PL: -500.00")
}

func TestFormatTradesOrg(t *testing.T) {
	t.Parallel()

	open1 := time.Date(2024, 1, 10, 9, 0, 0, 0, time.UTC)
	close1 := time.Date(2024, 1, 10, 12, 0, 0, 0, time.UTC)
	open2 := time.Date(2024, 1, 11, 10, 0, 0, 0, time.UTC)
	close2 := time.Date(2024, 1, 11, 15, 0, 0, 0, time.UTC)

	trades := []TradeRecord{
		{
			TradeID:    "trade-001",
			Instrument: "EUR_USD",
			Units:      1000,
			EntryPrice: 1.08000,
			ExitPrice:  1.08200,
			OpenTime:   open1,
			CloseTime:  close1,
			RealizedPL: 200.00,
			Reason:     "breakout",
		},
		{
			TradeID:    "trade-002",
			Instrument: "GBP_USD",
			Units:      500,
			EntryPrice: 1.25000,
			ExitPrice:  1.24800,
			OpenTime:   open2,
			CloseTime:  close2,
			RealizedPL: -100.00,
			Reason:     "reversal",
		},
	}

	result := FormatTradesOrg(trades)

	// Check both trades are present
	assert.Contains(t, result, "EUR_USD")
	assert.Contains(t, result, "GBP_USD")
	assert.Contains(t, result, "trade-001")
	assert.Contains(t, result, "trade-002")

	// Check trades are separated by blank lines
	parts := strings.Split(result, "\n\n\n")
	assert.Len(t, parts, 2, "Expected two trades separated by blank lines")
}

func TestFormatTradesOrgEmpty(t *testing.T) {
	t.Parallel()

	result := FormatTradesOrg([]TradeRecord{})
	assert.Empty(t, result)
}

func TestFormatTradesOrgSingle(t *testing.T) {
	t.Parallel()

	trade := TradeRecord{
		TradeID:    "single",
		Instrument: "USD_CAD",
		Units:      750,
		EntryPrice: 1.35000,
		ExitPrice:  1.35100,
		OpenTime:   time.Now(),
		CloseTime:  time.Now(),
		RealizedPL: 75.00,
		Reason:     "momentum",
	}

	result := FormatTradesOrg([]TradeRecord{trade})

	assert.Contains(t, result, "USD_CAD")
	assert.Contains(t, result, "single")
	// Should not have triple newlines for a single trade
	assert.NotContains(t, result, "\n\n\n")
}

func TestShortID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "long ID gets truncated",
			input:    "trade-12345678-abcdef-more-chars",
			expected: "trade-12",
		},
		{
			name:     "exactly 8 characters",
			input:    "12345678",
			expected: "12345678",
		},
		{
			name:     "less than 8 characters",
			input:    "short",
			expected: "short",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "exactly 9 characters gets truncated",
			input:    "123456789",
			expected: "12345678",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shortID(tt.input)
			assert.Equal(t, tt.expected, result)
			assert.LessOrEqual(t, len(result), 8, "shortID result should be at most 8 characters")
		})
	}
}

func TestFormatTradeOrgStructure(t *testing.T) {
	t.Parallel()

	trade := TradeRecord{
		TradeID:    "structure-test",
		Instrument: "AUD_USD",
		Units:      100,
		EntryPrice: 0.65000,
		ExitPrice:  0.65500,
		OpenTime:   time.Now(),
		CloseTime:  time.Now(),
		RealizedPL: 50.00,
		Reason:     "test",
	}

	result := FormatTradeOrg(trade)

	// Verify structural elements are in the correct order
	lines := strings.Split(result, "\n")
	require.Greater(t, len(lines), 10, "Expected at least 10 lines in formatted output")

	// First line should be the heading
	assert.True(t, strings.HasPrefix(lines[0], "** Trade:"))

	// Find properties drawer
	propertiesStart := -1
	propertiesEnd := -1
	for i, line := range lines {
		if line == ":PROPERTIES:" {
			propertiesStart = i
		}
		if line == ":END:" && propertiesStart >= 0 && propertiesEnd < 0 {
			propertiesEnd = i
			break
		}
	}

	assert.Greater(t, propertiesStart, 0, "Properties drawer should start after heading")
	assert.Greater(t, propertiesEnd, propertiesStart, "Properties drawer should have end marker")

	// Verify narrative sections come after properties
	thesisIdx := -1
	executionIdx := -1
	reviewIdx := -1
	for i, line := range lines {
		if strings.Contains(line, "*** Thesis") {
			thesisIdx = i
		}
		if strings.Contains(line, "*** Execution") {
			executionIdx = i
		}
		if strings.Contains(line, "*** Review") {
			reviewIdx = i
		}
	}

	assert.Greater(t, thesisIdx, propertiesEnd, "Thesis section should come after properties")
	assert.Greater(t, executionIdx, thesisIdx, "Execution should come after Thesis")
	assert.Greater(t, reviewIdx, executionIdx, "Review should come after Execution")
}
