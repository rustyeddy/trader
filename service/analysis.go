package service

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/rustyeddy/trader"
)

// ParseAnalysisCSV reads a ChatGPT-generated forex analysis CSV and returns
// all rows as typed ForexAnalysis values, including "No Trade" rows.
// Callers filter with IsTradeable() / IsWatched() as needed.
//
// Expected columns (row 1 is header):
//
//	Group, Pair, Structure, Setup Bias, Trend, Volatility,
//	Support zone, Resistance Zone, Status
func ParseAnalysisCSV(r io.Reader) ([]trader.ForexAnalysis, error) {
	cr := csv.NewReader(r)
	cr.TrimLeadingSpace = true

	// Discard header row.
	if _, err := cr.Read(); err != nil {
		return nil, fmt.Errorf("analysis csv: read header: %w", err)
	}

	var out []trader.ForexAnalysis
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("analysis csv: %w", err)
		}
		if len(rec) < 8 {
			continue
		}

		supLo, supHi, err := parseZone(rec[6])
		if err != nil {
			return nil, fmt.Errorf("analysis csv: pair %s support %q: %w", rec[1], rec[6], err)
		}
		resLo, resHi, err := parseZone(rec[7])
		if err != nil {
			return nil, fmt.Errorf("analysis csv: pair %s resistance %q: %w", rec[1], rec[7], err)
		}

		out = append(out, trader.ForexAnalysis{
			Group:          strings.TrimSpace(rec[0]),
			Pair:           strings.TrimSpace(rec[1]),
			Structure:      strings.TrimSpace(rec[2]),
			SetupBias:      strings.TrimSpace(rec[3]),
			Trend:          strings.TrimSpace(rec[4]),
			Volatility:     strings.TrimSpace(rec[5]),
			SupportLow:     trader.PriceFromFloat(supLo),
			SupportHigh:    trader.PriceFromFloat(supHi),
			ResistanceLow:  trader.PriceFromFloat(resLo),
			ResistanceHigh: trader.PriceFromFloat(resHi),
			Status:         trader.AnalysisStatus(strings.TrimSpace(rec[8])),
		})
	}
	return out, nil
}

// parseZone parses a price range like "1.1570–1.1590" (en dash) or
// "1.1570-1.1590" (hyphen) and returns (low, high, nil).
func parseZone(s string) (float64, float64, error) {
	s = strings.TrimSpace(s)

	// Try en dash (–, U+2013) first, then em dash (—, U+2014), then hyphen.
	var parts []string
	for _, sep := range []string{"–", "—", "-"} {
		parts = strings.SplitN(s, sep, 2)
		if len(parts) == 2 {
			break
		}
	}
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected lo–hi format, got %q", s)
	}

	lo, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse low %q: %w", parts[0], err)
	}
	hi, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse high %q: %w", parts[1], err)
	}
	return lo, hi, nil
}
