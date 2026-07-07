package datamanager

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// writeRawMonth preserves a candle-native provider's raw bid+ask OHLC rows
// for one month under rawDir, in the schema readRawMonthRows understands.
// This is the single place raw OANDA-style CSVs are written; DataManager's
// sync path and the CLI --repair derive path both go through it.
func writeRawMonth(rawDir string, key Key, monthStart time.Time, rows []RawCandleRow) error {
	path := monthlyCandle(rawDir, key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	monthEnd := monthStart.AddDate(0, 1, 0)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	tf := strings.ToLower(key.TF.String())
	if _, err := fmt.Fprintf(f, "# schema=raw-v1 source=%s instrument=%s tf=%s year=%d month=%02d\n",
		key.Source, key.Instrument, tf, key.Year, key.Month); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(f, "time,bid_o,bid_h,bid_l,bid_c,ask_o,ask_h,ask_l,ask_c,volume,complete"); err != nil {
		return err
	}

	for _, r := range rows {
		if !r.Time.Before(monthEnd) || r.Time.Before(monthStart) {
			continue
		}
		if _, err := fmt.Fprintf(f,
			"%s,%.5f,%.5f,%.5f,%.5f,%.5f,%.5f,%.5f,%.5f,%d,%t\n",
			r.Time.UTC().Format(time.RFC3339),
			r.BidOpen, r.BidHigh, r.BidLow, r.BidClose,
			r.AskOpen, r.AskHigh, r.AskLow, r.AskClose,
			r.Volume, r.Complete,
		); err != nil {
			return err
		}
	}
	return nil
}

// readRawMonthRows reads a raw month CSV written by writeRawMonth, returning
// every complete row within [monthStart, monthEnd). This is the single raw
// CSV parser shared by validation (coverage comparison) and canonical
// re-derivation (--repair).
func readRawMonthRows(path string, monthStart, monthEnd time.Time) ([]RawCandleRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var rows []RawCandleRow

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ",")
		if looksLikeHeader(fields) {
			continue
		}
		if len(fields) < 11 {
			return nil, fmt.Errorf("raw csv %q: expected 11 fields, got %d", path, len(fields))
		}

		ts, err := time.Parse(time.RFC3339, strings.TrimSpace(fields[0]))
		if err != nil {
			return nil, fmt.Errorf("raw csv %q: parse time: %w", path, err)
		}
		ts = ts.UTC()
		if ts.Before(monthStart) || !ts.Before(monthEnd) {
			continue
		}

		complete, err := strconv.ParseBool(strings.TrimSpace(fields[10]))
		if err != nil {
			return nil, fmt.Errorf("raw csv %q: parse complete: %w", path, err)
		}

		parseF := func(s string) (float64, error) {
			return strconv.ParseFloat(strings.TrimSpace(s), 64)
		}
		bidO, err := parseF(fields[1])
		if err != nil {
			return nil, fmt.Errorf("raw csv %q: parse bid_o: %w", path, err)
		}
		bidH, err := parseF(fields[2])
		if err != nil {
			return nil, fmt.Errorf("raw csv %q: parse bid_h: %w", path, err)
		}
		bidL, err := parseF(fields[3])
		if err != nil {
			return nil, fmt.Errorf("raw csv %q: parse bid_l: %w", path, err)
		}
		bidC, err := parseF(fields[4])
		if err != nil {
			return nil, fmt.Errorf("raw csv %q: parse bid_c: %w", path, err)
		}
		askO, err := parseF(fields[5])
		if err != nil {
			return nil, fmt.Errorf("raw csv %q: parse ask_o: %w", path, err)
		}
		askH, err := parseF(fields[6])
		if err != nil {
			return nil, fmt.Errorf("raw csv %q: parse ask_h: %w", path, err)
		}
		askL, err := parseF(fields[7])
		if err != nil {
			return nil, fmt.Errorf("raw csv %q: parse ask_l: %w", path, err)
		}
		askC, err := parseF(fields[8])
		if err != nil {
			return nil, fmt.Errorf("raw csv %q: parse ask_c: %w", path, err)
		}
		vol, err := strconv.ParseInt(strings.TrimSpace(fields[9]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("raw csv %q: parse volume: %w", path, err)
		}

		rows = append(rows, RawCandleRow{
			Time:     ts,
			BidOpen:  bidO,
			BidHigh:  bidH,
			BidLow:   bidL,
			BidClose: bidC,
			AskOpen:  askO,
			AskHigh:  askH,
			AskLow:   askL,
			AskClose: askC,
			Volume:   int(vol),
			Complete: complete,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan raw csv %q: %w", path, err)
	}
	return rows, nil
}
