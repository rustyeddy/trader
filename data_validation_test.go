package trader

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func writeRawOandaMonthFile(t *testing.T, rawDir string, key Key, rows []string) string {
	t.Helper()

	path := monthlyCandle(rawDir, key)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))

	content := fmt.Sprintf("# schema=raw-v1 source=oanda instrument=%s tf=%s year=%d month=%02d\n",
		key.Instrument, strings.ToLower(key.TF.String()), key.Year, key.Month)
	content += "time,bid_o,bid_h,bid_l,bid_c,ask_o,ask_h,ask_l,ask_c,volume,complete\n"
	for _, row := range rows {
		content += row + "\n"
	}
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestValidateCandleData_MissingExpectedCandles(t *testing.T) {
	s := useTempStore(t)
	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, s.WriteMonthlyCandles(SourceOanda, "EURUSD", H1, start, []Candle{
		{Open: 100, High: 101, Low: 99, Close: 100, Ticks: 1},
	}))

	report, err := ValidateCandleData(context.Background(), CandleValidationRequest{
		Instruments: []string{"EURUSD"},
		Source:      SourceOanda,
		Timeframe:   H1,
		Start:       start,
		End:         start.AddDate(0, 1, 0),
	})
	require.NoError(t, err)
	require.Equal(t, 1, report.MonthsScanned)
	require.NotEmpty(t, report.Issues)
	require.Equal(t, "missing_expected_candles", report.Issues[0].Kind)
	require.Greater(t, report.Issues[0].Missing, 0)
}

func TestValidateCandleData_MissingRawOandaMonth(t *testing.T) {
	s := useTempStore(t)
	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	cs, err := newMonthlyCandleSet("EURUSD", D1, FromTime(start), PriceScale, SourceOanda)
	require.NoError(t, err)

	step := time.Duration(cs.Timeframe) * time.Second
	monthStart := time.Unix(int64(cs.Start), 0).UTC()
	for i := range cs.Candles {
		slotStart := monthStart.Add(time.Duration(i) * step)
		slotEnd := slotStart.Add(step)
		if !timeRangeMayHaveForexData(slotStart, slotEnd) {
			continue
		}
		cs.Candles[i] = Candle{Open: 100, High: 101, Low: 99, Close: 100, Ticks: 1}
		cs.SetValid(i)
	}
	require.NoError(t, s.WriteCSV(cs))

	report, err := ValidateCandleData(context.Background(), CandleValidationRequest{
		Instruments: []string{"EURUSD"},
		Source:      SourceOanda,
		Timeframe:   D1,
		Start:       start,
		End:         start.AddDate(0, 1, 0),
		IncludeRaw:  true,
	})
	require.NoError(t, err)
	require.Len(t, report.Issues, 1)
	require.Equal(t, "missing_raw_source", report.Issues[0].Kind)
}

func TestValidateCandleData_RawMismatch(t *testing.T) {
	s := useTempStore(t)
	rawDir := s.rawRoot()
	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	candles := make([]Candle, 1)
	candles[0] = Candle{Open: 100, High: 101, Low: 99, Close: 100, Ticks: 1}
	require.NoError(t, s.WriteMonthlyCandles(SourceOanda, "EURUSD", H1, start, candles))

	key := Key{Instrument: "EURUSD", Source: SourceOanda, Kind: KindCandle, TF: H1, Year: 2026, Month: 1}
	writeRawOandaMonthFile(t, rawDir, key, []string{
		"2026-01-01T01:00:00Z,1.10000,1.10100,1.09900,1.10050,1.10010,1.10110,1.09910,1.10060,100,true",
	})

	report, err := ValidateCandleData(context.Background(), CandleValidationRequest{
		Instruments: []string{"EURUSD"},
		Source:      SourceOanda,
		Timeframe:   H1,
		Start:       start,
		End:         start.AddDate(0, 1, 0),
		IncludeRaw:  true,
		RawDir:      rawDir,
	})
	require.NoError(t, err)

	kinds := map[string]bool{}
	for _, issue := range report.Issues {
		kinds[issue.Kind] = true
	}
	require.True(t, kinds["raw_complete_missing_canonical"])
}
