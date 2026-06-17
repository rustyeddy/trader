package trader

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type CandleValidationRequest struct {
	Instruments []string
	Source      string
	Timeframe   Timeframe
	Start       time.Time
	End         time.Time
	IncludeRaw  bool
	RawDir      string
}

type CandleValidationIssue struct {
	Kind          string   `json:"kind"`
	Severity      string   `json:"severity"`
	Source        string   `json:"source"`
	Instrument    string   `json:"instrument"`
	Timeframe     string   `json:"timeframe"`
	Year          int      `json:"year"`
	Month         int      `json:"month"`
	Path          string   `json:"path,omitempty"`
	RawPath       string   `json:"raw_path,omitempty"`
	Expected      int      `json:"expected"`
	Present       int      `json:"present"`
	Missing       int      `json:"missing"`
	SampleMissing []string `json:"sample_missing,omitempty"`
	Message       string   `json:"message"`
}

type CandleValidationReport struct {
	Source        string                  `json:"source"`
	Timeframe     string                  `json:"timeframe"`
	IncludeRaw    bool                    `json:"include_raw"`
	MonthsScanned int                     `json:"months_scanned"`
	Issues        []CandleValidationIssue `json:"issues"`
}

func (r *CandleValidationReport) IssueCount() int {
	if r == nil {
		return 0
	}
	return len(r.Issues)
}

func ValidateCandleData(ctx context.Context, req CandleValidationRequest) (*CandleValidationReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	instruments := normalizeInstruments(req.Instruments)
	if len(instruments) == 0 {
		return nil, fmt.Errorf("no instruments")
	}

	source := normalizeSource(req.Source)
	if source == "" {
		source = SourceOanda
	}

	switch req.Timeframe {
	case M1, H1, D1:
	default:
		return nil, fmt.Errorf("unsupported timeframe: %v", req.Timeframe)
	}

	start := time.Date(req.Start.UTC().Year(), req.Start.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(req.End.UTC().Year(), req.End.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	if !start.Before(end) {
		return nil, fmt.Errorf("start must be before end")
	}

	rawDir := req.RawDir
	if rawDir == "" {
		rawDir = store.rawRoot()
	}

	months := TimeRange{Start: FromTime(start), End: FromTime(end), TF: req.Timeframe}.MonthsInRange()
	report := &CandleValidationReport{
		Source:     source,
		Timeframe:  req.Timeframe.String(),
		IncludeRaw: req.IncludeRaw,
	}

	for _, instrument := range instruments {
		for _, ym := range months {
			if err := ctx.Err(); err != nil {
				return nil, err
			}

			key := Key{
				Instrument: instrument,
				Source:     source,
				Kind:       KindCandle,
				TF:         req.Timeframe,
				Year:       ym.Year,
				Month:      ym.Month,
			}
			report.MonthsScanned++

			issues, err := validateCandleMonth(key, req.IncludeRaw, rawDir)
			if err != nil {
				return nil, err
			}
			report.Issues = append(report.Issues, issues...)
		}
	}

	sort.Slice(report.Issues, func(i, j int) bool {
		a := report.Issues[i]
		b := report.Issues[j]
		if a.Instrument != b.Instrument {
			return a.Instrument < b.Instrument
		}
		if a.Year != b.Year {
			return a.Year < b.Year
		}
		if a.Month != b.Month {
			return a.Month < b.Month
		}
		return a.Kind < b.Kind
	})

	return report, nil
}

type candleCoverage struct {
	Expected      int
	Present       int
	Missing       int
	Invalid       int
	SampleMissing []string
}

func validateCandleMonth(key Key, includeRaw bool, rawDir string) ([]CandleValidationIssue, error) {
	path, err := store.PathForAsset(key)
	if err != nil {
		return nil, err
	}

	cs, err := store.ReadCSV(key)
	if err != nil {
		if os.IsNotExist(err) {
			expected := expectedOpenSlotCount(key.Year, key.Month, key.TF)
			issue := CandleValidationIssue{
				Kind:       "missing_candle_month",
				Severity:   "error",
				Source:     key.Source,
				Instrument: key.Instrument,
				Timeframe:  key.TF.String(),
				Year:       key.Year,
				Month:      key.Month,
				Path:       path,
				Expected:   expected,
				Missing:    expected,
				Message:    "canonical candle month file is missing",
			}
			if !includeRaw || key.Source != SourceOanda {
				return []CandleValidationIssue{issue}, nil
			}
			rawIssues, rawErr := compareRawOandaMonth(key, nil, rawDir)
			if rawErr != nil {
				return nil, rawErr
			}
			return append([]CandleValidationIssue{issue}, rawIssues...), nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	coverage := analyzeCandleCoverage(cs)
	issues := make([]CandleValidationIssue, 0, 4)
	if coverage.Missing > 0 {
		issues = append(issues, CandleValidationIssue{
			Kind:          "missing_expected_candles",
			Severity:      "error",
			Source:        key.Source,
			Instrument:    key.Instrument,
			Timeframe:     key.TF.String(),
			Year:          key.Year,
			Month:         key.Month,
			Path:          path,
			Expected:      coverage.Expected,
			Present:       coverage.Present,
			Missing:       coverage.Missing,
			SampleMissing: coverage.SampleMissing,
			Message:       fmt.Sprintf("%d expected candle slots are missing", coverage.Missing),
		})
	}
	if coverage.Invalid > 0 {
		issues = append(issues, CandleValidationIssue{
			Kind:       "invalid_candles",
			Severity:   "error",
			Source:     key.Source,
			Instrument: key.Instrument,
			Timeframe:  key.TF.String(),
			Year:       key.Year,
			Month:      key.Month,
			Path:       path,
			Expected:   coverage.Expected,
			Present:    coverage.Present,
			Message:    fmt.Sprintf("%d valid candle rows have invalid OHLC shape", coverage.Invalid),
		})
	}

	if includeRaw && key.Source == SourceOanda {
		rawIssues, err := compareRawOandaMonth(key, cs, rawDir)
		if err != nil {
			return nil, err
		}
		issues = append(issues, rawIssues...)
	}

	return issues, nil
}

func analyzeCandleCoverage(cs *candleSet) candleCoverage {
	if cs == nil || cs.Timeframe <= 0 {
		return candleCoverage{}
	}

	step := time.Duration(cs.Timeframe) * time.Second
	start := time.Unix(int64(cs.Start), 0).UTC()
	var cov candleCoverage
	for i := range cs.Candles {
		slotStart := start.Add(time.Duration(i) * step)
		slotEnd := slotStart.Add(step)
		if !timeRangeMayHaveForexData(slotStart, slotEnd) {
			continue
		}
		cov.Expected++
		if !cs.IsValid(i) {
			cov.Missing++
			if len(cov.SampleMissing) < 10 {
				cov.SampleMissing = append(cov.SampleMissing, slotStart.Format(time.RFC3339))
			}
			continue
		}
		cov.Present++
		if !cs.Candles[i].Validate() {
			cov.Invalid++
		}
	}
	return cov
}

func expectedOpenSlotCount(year, month int, tf Timeframe) int {
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	step := time.Duration(tf) * time.Second
	count := 0
	for slotStart := start; slotStart.Before(end); slotStart = slotStart.Add(step) {
		if timeRangeMayHaveForexData(slotStart, slotStart.Add(step)) {
			count++
		}
	}
	return count
}

type rawOandaCoverage struct {
	Path          string
	Present       map[int]struct{}
	SampleMissing []string
}

func compareRawOandaMonth(key Key, cs *candleSet, rawDir string) ([]CandleValidationIssue, error) {
	rawPath := monthlyCandle(rawDir, key)
	coverage, err := readRawOandaMonth(rawPath, key)
	if err != nil {
		if os.IsNotExist(err) {
			return []CandleValidationIssue{{
				Kind:       "missing_raw_source",
				Severity:   "warn",
				Source:     key.Source,
				Instrument: key.Instrument,
				Timeframe:  key.TF.String(),
				Year:       key.Year,
				Month:      key.Month,
				RawPath:    rawPath,
				Message:    "raw OANDA month file is missing",
			}}, nil
		}
		return nil, fmt.Errorf("read raw month %s: %w", rawPath, err)
	}

	if cs == nil {
		return nil, nil
	}

	canonical := make(map[int]struct{})
	for i := range cs.Candles {
		if cs.IsValid(i) {
			canonical[i] = struct{}{}
		}
	}

	rawOnly := diffSample(coverage.Present, canonical, key)
	canonicalOnly := diffSample(canonical, coverage.Present, key)
	issues := make([]CandleValidationIssue, 0, 2)
	if len(rawOnly) > 0 {
		issues = append(issues, CandleValidationIssue{
			Kind:          "raw_complete_missing_canonical",
			Severity:      "error",
			Source:        key.Source,
			Instrument:    key.Instrument,
			Timeframe:     key.TF.String(),
			Year:          key.Year,
			Month:         key.Month,
			RawPath:       rawPath,
			Missing:       len(rawOnly),
			SampleMissing: rawOnly,
			Message:       "raw OANDA has complete candles missing from canonical month",
		})
	}
	if len(canonicalOnly) > 0 {
		issues = append(issues, CandleValidationIssue{
			Kind:          "canonical_missing_raw_complete",
			Severity:      "error",
			Source:        key.Source,
			Instrument:    key.Instrument,
			Timeframe:     key.TF.String(),
			Year:          key.Year,
			Month:         key.Month,
			RawPath:       rawPath,
			Missing:       len(canonicalOnly),
			SampleMissing: canonicalOnly,
			Message:       "canonical month contains valid candles not backed by raw complete OANDA rows",
		})
	}
	return issues, nil
}

func diffSample(left, right map[int]struct{}, key Key) []string {
	if len(left) == 0 {
		return nil
	}
	keys := make([]int, 0)
	for idx := range left {
		if _, ok := right[idx]; ok {
			continue
		}
		keys = append(keys, idx)
	}
	sort.Ints(keys)
	limit := 10
	if len(keys) < limit {
		limit = len(keys)
	}
	out := make([]string, 0, limit)
	for _, idx := range keys {
		if len(out) >= 10 {
			break
		}
		out = append(out, candleSlotTime(key, idx).Format(time.RFC3339))
	}
	return out
}


func readRawOandaMonth(path string, key Key) (*rawOandaCoverage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	cov := &rawOandaCoverage{
		Path:    path,
		Present: make(map[int]struct{}),
	}
	monthStart := time.Date(key.Year, time.Month(key.Month), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	step := int64(key.TF)

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
		if !complete {
			continue
		}

		bidClose, err := strconv.ParseFloat(strings.TrimSpace(fields[4]), 64)
		if err != nil {
			return nil, fmt.Errorf("raw csv %q: parse bid_c: %w", path, err)
		}
		askClose, err := strconv.ParseFloat(strings.TrimSpace(fields[8]), 64)
		if err != nil {
			return nil, fmt.Errorf("raw csv %q: parse ask_c: %w", path, err)
		}
		if bidClose == 0 && askClose == 0 {
			continue
		}

		idx := int((ts.Unix() - monthStart.Unix()) / step)
		cov.Present[idx] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan raw csv %q: %w", path, err)
	}
	return cov, nil
}

func candleSlotTime(key Key, idx int) time.Time {
	start := time.Date(key.Year, time.Month(key.Month), 1, 0, 0, 0, 0, time.UTC)
	return start.Add(time.Duration(idx) * time.Duration(key.TF) * time.Second)
}
