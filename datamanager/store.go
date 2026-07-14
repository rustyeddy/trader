package datamanager

import (
	"bufio"
	"encoding/binary"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rustyeddy/trader/log"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/ulikunitz/xz/lzma"
)

// store manages candle CSVs and raw tick files under a pair of symmetric
// directory trees that share a common root:
//
//	/srv/trading/data/
//	├── candles/<provider>/<instrument>/<year>/<month>/<filename>.csv
//	└── raw/<provider>/<instrument>/<year>/<month>/<day>/<hh>h_ticks.bi5
//
// The "candles" tree is rooted at basedir; the "raw" tree is its sibling
// (rawRoot = filepath.Dir(basedir) + "/raw").  Providers are source names
// such as "oanda" or "dukascopy".
//
// Candle filenames embed every identifying dimension so the file is
// self-describing without its path:
//
//	gbpusd-2026-01-h1.csv   (instrument-year-month-tf)
//	eurusd-2025-08-m1.csv
//	usdchf-2024-12-d1.csv
//
// Raw tick files follow the Dukascopy bi5 naming convention:
//
//	/srv/trading/data/raw/dukascopy/EURUSD/2025/01/02/13h_ticks.bi5
type store struct {
	basedir string // root of the candles tree, e.g. "/srv/trading/data/candles"

	cacheMu sync.RWMutex
	cache   map[Key]*CandleSet // process-lifetime cache of ReadCSV results, keyed by Key
}

func (s *store) PathForAsset(k Key) (string, error) {
	switch {
	case k.Kind == KindCandle && k.Day == 0 && k.Hour == 0:
		return s.pathForMonthlyCandle(k), nil

	case k.Kind == KindTick && k.Day > 0 && k.Hour >= 0:
		return s.pathForHourlyTick(k), nil

	default:
		return "", fmt.Errorf("unsupported asset key for path: %+v", k)
	}
}

// monthlyCandle builds a monthly candle path under root.
// Used for both the canonical candle tree (basedir) and the raw preservation
// tree (rawRoot) so path structure stays consistent across both.
func monthlyCandle(root string, k Key) string {
	source := normalizeSource(k.Source)
	if source == "" {
		source = "unknown"
	}
	instrument := market.NormalizeInstrument(k.Instrument)
	tf := strings.ToLower(k.TF.String())
	filename := fmt.Sprintf("%s-%04d-%02d-%s.csv", instrument, k.Year, k.Month, tf)
	return filepath.Join(root, source, instrument,
		fmt.Sprintf("%04d", k.Year), fmt.Sprintf("%02d", k.Month), filename)
}

func (s *store) pathForMonthlyCandle(k Key) string {
	return monthlyCandle(s.basedir, k)
}

// PathForMonthlyCandle returns the file path for a monthly candle CSV.
func (s *store) PathForMonthlyCandle(k Key) string {
	return s.pathForMonthlyCandle(k)
}

// RawCandlePath returns the path for a monthly candle CSV under the raw tree.
// It mirrors PathForAsset but roots in rawRoot instead of basedir.
func (s *store) RawCandlePath(k Key) (string, error) {
	if k.Kind != KindCandle || k.Day != 0 || k.Hour != 0 {
		return "", fmt.Errorf("RawCandlePath requires a monthly candle key (Day=0, Hour=0)")
	}
	return monthlyCandle(s.rawRoot(), k), nil
}

// RawRoot returns the root directory for raw source data.
// e.g. basedir=/srv/trading/data/candles → /srv/trading/data/raw
func (s *store) RawRoot() string { return s.rawRoot() }

// RawCandlePathAt returns the path for a monthly candle CSV under a custom
// raw root (e.g. when --raw-dir overrides the default).
func RawCandlePathAt(rawDir string, k Key) string { return monthlyCandle(rawDir, k) }

func parseCandlePath(path string) (k Key, ok bool) {
	p := filepath.ToSlash(path)
	parts := strings.Split(p, "/")
	if len(parts) < 5 {
		return k, false
	}

	//         n-6+       n-5        n-4       n-3    n-2       n-1
	// Expect <basedir>/<source>/<instrument>/<year>/<month>/<filename>.csv
	n := len(parts)
	k = Key{
		Instrument: market.NormalizeInstrument(parts[n-4]),
		Source:     normalizeSource(parts[n-5]),
		Kind:       KindCandle,
	}

	year, err := strconv.Atoi(parts[n-3])
	if err != nil {
		return k, false
	}
	month, err := strconv.Atoi(parts[n-2])
	if err != nil || month < 1 || month > 12 {
		return k, false
	}

	fname := strings.ToLower(strings.TrimSuffix(parts[n-1], ".csv"))

	//                0    1    2  3
	// <filename>: iiijjj-yyyy-mm-tf.csv
	//
	// e.g. EURUSD-2026-03-m1.csv
	nameParts := strings.Split(fname, "-")
	if len(nameParts) != 4 {
		return k, false
	}

	fileInst := market.NormalizeInstrument(nameParts[0])
	fileYear, err := strconv.Atoi(nameParts[1])
	if err != nil {
		return k, false
	}

	fileMonth, err := strconv.Atoi(nameParts[2])
	if err != nil || fileMonth < 1 || fileMonth > 12 {
		return k, false
	}
	fileTF := strings.ToLower(nameParts[3])
	if fileInst != k.Instrument || fileYear != year || fileMonth != month || fileTF == "" {
		return k, false
	}

	switch fileTF {
	case "m1": // XXX normalize these!!
		k.TF = types.M1
	case "h1":
		k.TF = types.H1
	case "h4":
		k.TF = types.H4
	case "d1":
		k.TF = types.D1
	default:
		return k, false
	}

	k.Month = month
	k.Year = year
	return k, true
}

// rawRoot returns the sibling "raw" directory next to basedir.
// e.g. basedir=/srv/trading/data/candles → rawRoot=/srv/trading/data/raw
func (s *store) rawRoot() string {
	return filepath.Join(filepath.Dir(s.basedir), "raw")
}

func (s *store) pathForHourlyTick(k Key) string {
	source := normalizeSource(k.Source)
	if source == "" {
		source = market.SourceDukascopy
	}
	instrument := market.NormalizeInstrument(k.Instrument)

	// <rawRoot>/<source>/<instr>/<yyyy>/<mm>/<dd>/<hh>h_ticks.bi5
	return filepath.Join(
		s.rawRoot(),
		source,
		instrument,
		fmt.Sprintf("%04d", k.Year),
		fmt.Sprintf("%02d", k.Month),
		fmt.Sprintf("%02d", k.Day),
		fmt.Sprintf("%02dh_ticks.bi5", k.Hour),
	)
}

func parseTickPath(path string) (Key, bool) {
	var k Key

	clean := filepath.ToSlash(path)

	// Example expected tail:
	//   n-6      -5   -4  -3 -2    -1
	// dukascopy/EURUSD/2025/01/02/13h_ticks.bi5
	parts := strings.Split(clean, "/")
	if len(parts) < 6 {
		return k, false
	}

	n := len(parts)
	file := parts[n-1]
	dayStr := parts[n-2]
	monthStr := parts[n-3]
	yearStr := parts[n-4]
	inst := parts[n-5]
	source := parts[n-6]

	k = Key{
		Instrument: market.NormalizeInstrument(inst),
		Source:     normalizeSource(source),
		Kind:       KindTick,
		TF:         types.Ticks,
	}

	year, err := strconv.Atoi(yearStr)
	if err != nil {
		return k, false
	}
	k.Year = year

	month, err := strconv.Atoi(monthStr)
	if err != nil {
		return k, false
	}
	k.Month = month

	day, err := strconv.Atoi(dayStr)
	if err != nil {
		return k, false
	}
	k.Day = day

	// Dukascopy commonly uses "13h_ticks.bi5"
	base := strings.ToLower(file)
	if !strings.HasSuffix(base, "h_ticks.bi5") {
		return k, false
	}

	hourStr := strings.TrimSuffix(base, "h_ticks.bi5")
	hour, err := strconv.Atoi(hourStr)
	if err != nil {
		return k, false
	}
	k.Hour = hour

	if month < 1 || month > 12 || day < 1 || day > 31 || hour < 0 || hour > 23 {
		return k, false
	}
	if parsed := time.Date(year, time.Month(month), day, hour, 0, 0, 0, time.UTC); parsed.Year() != year ||
		int(parsed.Month()) != month || parsed.Day() != day || parsed.Hour() != hour {
		return k, false
	}
	return k, true
}

func (s *store) RelDir(key Key) string {
	return filepath.Join(
		market.NormalizeInstrument(key.Instrument),
		strings.ToUpper(key.TF.String()),
		fmt.Sprintf("%04d", key.Year),
	)
}

func (s *store) Exists(key Key) (bool, error) {
	p, err := s.PathForAsset(key)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(p)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (s *store) scanFiles(inv *Inventory) error {
	// Candle CSVs live under basedir; raw .bi5 tick files live under the
	// sibling raw dir. Walk both so the inventory has full coverage.
	for _, root := range []string{s.basedir, s.rawRoot()} {
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		}
		if err := s.walkRoot(root, inv); err != nil {
			return err
		}
	}
	return nil
}

func (s *store) walkRoot(root string, inv *Inventory) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info == nil || info.IsDir() {
			return nil
		}

		var ok bool
		var rng types.TimeRange
		var descriptor string
		name := strings.ToLower(info.Name())
		var key Key
		switch {
		case strings.HasSuffix(name, ".bi5"):
			key, ok = parseTickPath(path)
			if !ok {
				return nil
			}

			start := key.Time()
			end := start.Add(time.Hour)
			descriptor = fmt.Sprintf(
				"dukascopy raw bi5 tick file %04d-%02d-%02d %02d:00Z",
				key.Year, key.Month, key.Day, key.Hour)
			rng = types.NewTimeRange(types.FromTime(start), types.FromTime(end), types.Ticks)

		case strings.HasSuffix(name, ".csv"):
			key, ok = parseCandlePath(path)
			if !ok {
				// log.Println("Failed to parse candle path ", path)
				return nil
			}
			asset := s.inspectCandleAsset(key, path, info)
			inv.Put(asset)
			return nil

		default:
			return nil
		}

		asset := Asset{
			Key:        key,
			Path:       path,
			Range:      rng,
			Exists:     true,
			Complete:   info.Size() > 0, // TODO FIX THIS - minimal heuristic only
			Size:       info.Size(),
			UpdatedAt:  info.ModTime(),
			Descriptor: descriptor,
		}
		inv.Put(asset)
		return nil
	})
}

func (s *store) inspectCandleAsset(key Key, path string, info os.FileInfo) Asset {
	asset := Asset{
		Key:        key,
		Path:       path,
		Range:      types.MonthRange(key.Year, key.Month),
		Exists:     true,
		Complete:   info.Size() > 0,
		Size:       info.Size(),
		UpdatedAt:  info.ModTime(),
		Descriptor: "Candles",
	}
	if info.Size() <= 0 {
		asset.Reason = "empty candle file"
		return asset
	}

	cs, err := s.ReadCSV(key)
	if err != nil {
		asset.Complete = false
		asset.Reason = fmt.Sprintf("read candle file: %v", err)
		return asset
	}

	missingExpected, expected := candleSetMissingExpectedSlots(cs)
	asset.Complete = missingExpected == 0
	asset.Buildable = expected > 0
	asset.MissingInputs = missingExpected
	if missingExpected > 0 {
		asset.Reason = fmt.Sprintf("%d expected candles missing", missingExpected)
	}
	return asset
}

func candleSetMissingExpectedSlots(cs *CandleSet) (missing int, expected int) {
	if cs == nil || cs.Timeframe <= 0 {
		return 0, 0
	}

	step := time.Duration(cs.Timeframe) * time.Second
	start := time.Unix(int64(cs.Start), 0).UTC()
	for i := range cs.Candles {
		slotStart := start.Add(time.Duration(i) * step)
		slotEnd := slotStart.Add(step)
		if !timeRangeMayHaveForexData(slotStart, slotEnd) {
			continue
		}
		expected++
		if !cs.IsValid(i) {
			missing++
		}
	}
	return missing, expected
}

// SlotMayHaveForexData reports whether a time slot (start inclusive, end exclusive)
// could contain forex trading activity. Exported for use by the service layer.
func SlotMayHaveForexData(start, end time.Time) bool {
	return timeRangeMayHaveForexData(start, end)
}

func timeRangeMayHaveForexData(start, end time.Time) bool {
	start = start.UTC()
	end = end.UTC()
	if !start.Before(end) {
		return false
	}

	for probe := start; probe.Before(end); probe = probe.Add(time.Hour) {
		if !market.IsForexMarketClosed(probe) {
			return true
		}
	}
	return false
}

func (s *store) writeMetadata(cs *CandleSet, w io.Writer) error {
	tfstr := types.Timeframe(cs.Timeframe).String()
	year := time.Unix(int64(cs.Start), 0).UTC().Year()

	_, err := fmt.Fprintf(w, "# schema=v1 source=%s instrument=%s tf=%s year=%d scale=%d\n",
		cs.Source, cs.Instrument, tfstr, year, cs.Scale)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintln(w, "Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags")
	return err
}

// ReadCSV returns the CandleSet for key, serving from an in-memory cache
// when possible. The cache is process-lifetime only (no persistence, no
// TTL) and deliberately excludes the current calendar month, since that
// month's data is a moving target that can change mid-process as new
// candles are downloaded and written via WriteCSV.
func (s *store) ReadCSV(key Key) (*CandleSet, error) {
	if !isCurrentMonth(key) {
		s.cacheMu.RLock()
		cached, ok := s.cache[key]
		s.cacheMu.RUnlock()
		if ok {
			return cached, nil
		}
	}

	cs, err := s.readCSVUncached(key)
	if err != nil {
		return nil, err
	}

	if !isCurrentMonth(key) {
		s.cacheMu.Lock()
		if s.cache == nil {
			s.cache = make(map[Key]*CandleSet)
		}
		s.cache[key] = cs
		s.cacheMu.Unlock()
	}
	return cs, nil
}

// isCurrentMonth reports whether key addresses the calendar month containing
// the current moment (UTC), which readCSV/WriteCSV treat as a moving target
// that must never be served from the cache.
func isCurrentMonth(key Key) bool {
	now := time.Now().UTC()
	return key.Year == now.Year() && key.Month == int(now.Month())
}

// invalidateCache drops any cached entry for key, called after WriteCSV so a
// subsequent ReadCSV in the same process sees freshly written data instead
// of a stale cache hit from before the write.
func (s *store) invalidateCache(key Key) {
	s.cacheMu.Lock()
	delete(s.cache, key)
	s.cacheMu.Unlock()
}

func (s *store) readCSVUncached(key Key) (cs *CandleSet, err error) {
	if key.Kind != KindCandle {
		return nil, fmt.Errorf("ReadCSV only supports candle keys, got %v", key.Kind)
	}
	if key.Month < 1 || key.Month > 12 {
		return nil, fmt.Errorf("invalid candle key date: month %d out of range", key.Month)
	}
	if key.Day != 0 || key.Hour != 0 {
		return nil, fmt.Errorf("ReadCSV only supports monthly candle keys with Day==0 and Hour==0, got Day=%d Hour=%d", key.Day, key.Hour)
	}

	path, err := s.PathForAsset(key)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open csv %q: %w", path, err)
	}
	defer f.Close()

	// Build CandleSet structure from key parameters.
	monthStart := time.Date(key.Year, time.Month(key.Month), 1, 0, 0, 0, 0, time.UTC)
	start := types.FromTime(monthStart)
	tf := key.TF
	step := int64(tf)

	endTime := monthStart.AddDate(0, 1, 0)
	spanSec := int64(endTime.Sub(monthStart).Seconds())
	n := int(spanSec / step)

	instName := market.NormalizeInstrument(key.Instrument)
	cs = &CandleSet{
		Instrument: instName,
		Source:     readCSVSource(key.Source),
		Start:      start,
		Timeframe:  tf,
		Scale:      types.PriceScale,
		Candles:    make([]market.Candle, n),
		Valid:      make([]uint64, (n+63)/64),
	}

	scanner := bufio.NewScanner(f)
	rowNum := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Split(line, ",")
		if looksLikeHeader(fields) {
			continue
		}
		rowNum++

		if len(fields) < 9 {
			return nil, fmt.Errorf("csv %q row %d: expected 9 fields, got %d", path, rowNum, len(fields))
		}

		ts, err := strconv.ParseInt(strings.TrimSpace(fields[0]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("csv %q row %d: parse timestamp: %w", path, rowNum, err)
		}

		offset := ts - int64(start)
		if offset < 0 || offset%step != 0 {
			return nil, fmt.Errorf("csv %q row %d: timestamp %d not aligned to timeframe %d", path, rowNum, ts, step)
		}
		idx := int(offset / step)
		if idx >= n {
			return nil, fmt.Errorf("csv %q row %d: timestamp %d out of range for month", path, rowNum, ts)
		}

		highv, err := types.ParseRawPrice(fields[1])
		if err != nil {
			return nil, fmt.Errorf("csv %q row %d: parse high: %w", path, rowNum, err)
		}
		openv, err := types.ParseRawPrice(fields[2])
		if err != nil {
			return nil, fmt.Errorf("csv %q row %d: parse open: %w", path, rowNum, err)
		}
		lowv, err := types.ParseRawPrice(fields[3])
		if err != nil {
			return nil, fmt.Errorf("csv %q row %d: parse low: %w", path, rowNum, err)
		}
		closev, err := types.ParseRawPrice(fields[4])
		if err != nil {
			return nil, fmt.Errorf("csv %q row %d: parse close: %w", path, rowNum, err)
		}
		avgSpread, err := types.ParseRawPrice(fields[5])
		if err != nil {
			return nil, fmt.Errorf("csv %q row %d: parse avgspread: %w", path, rowNum, err)
		}
		maxSpread, err := types.ParseRawPrice(fields[6])
		if err != nil {
			return nil, fmt.Errorf("csv %q row %d: parse maxspread: %w", path, rowNum, err)
		}

		ticks, err := strconv.ParseInt(strings.TrimSpace(fields[7]), 10, 32)
		if err != nil {
			return nil, fmt.Errorf("csv %q row %d: parse ticks: %w", path, rowNum, err)
		}

		flags, err := strconv.ParseUint(strings.TrimSpace(fields[8]), 0, 64)
		if err != nil {
			return nil, fmt.Errorf("csv %q row %d: parse flags: %w", path, rowNum, err)
		}

		cs.Candles[idx] = market.Candle{
			High:      highv,
			Open:      openv,
			Low:       lowv,
			Close:     closev,
			AvgSpread: avgSpread,
			MaxSpread: maxSpread,
			Ticks:     int32(ticks),
		}
		if flags&0x0001 != 0 {
			cs.SetValid(idx)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan csv %q: %w", path, err)
	}

	return cs, nil
}

func looksLikeHeader(rec []string) bool {
	if len(rec) == 0 {
		return false
	}

	h := strings.ToLower(strings.TrimSpace(rec[0]))
	return h == "timestamp" || h == "time"
}

func readCSVSource(source string) string {
	source = normalizeSource(source)
	if source == "" {
		return market.SourceCandles
	}
	return source
}

func (s *store) WriteCSV(cs *CandleSet) error {
	if cs == nil {
		return errors.New("nil CandleSet")
	}
	if cs.Instrument == "" {
		return errors.New("nil candle set instrument")
	}

	step := cs.Timeframe
	if step <= 0 {
		return fmt.Errorf("invalid candle set timeframe: %d", cs.Timeframe)
	}

	start := time.Unix(int64(cs.Start), 0).UTC()
	key := Key{
		Instrument: market.NormalizeInstrument(cs.Instrument),
		Source:     normalizeSource(cs.Source),
		Kind:       KindCandle,
		TF:         types.Timeframe(cs.Timeframe),
		Year:       start.Year(),
		Month:      int(start.Month()),
	}
	path, err := s.PathForAsset(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	bw := bufio.NewWriterSize(f, 256*1024)
	if err := s.writeMetadata(cs, bw); err != nil {
		return err
	}

	w := csv.NewWriter(bw)
	defer w.Flush()

	for i := 0; i < len(cs.Candles); i++ {
		openUnix := int64(cs.Start) + int64(i)*int64(step)

		c := cs.Candles[i]
		var flags uint64
		if len(cs.Valid) > 0 && types.BitIsSet(cs.Valid, i) {
			flags = 0x0001
		}

		rec := []string{
			strconv.FormatInt(openUnix, 10),
			strconv.FormatInt(int64(c.High), 10),
			strconv.FormatInt(int64(c.Open), 10),
			strconv.FormatInt(int64(c.Low), 10),
			strconv.FormatInt(int64(c.Close), 10),
			strconv.FormatInt(int64(c.AvgSpread), 10),
			strconv.FormatInt(int64(c.MaxSpread), 10),
			strconv.FormatInt(int64(c.Ticks), 10),
			fmt.Sprintf("0x%04x", flags),
		}
		if err := w.Write(rec); err != nil {
			return err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}

	log.Data.Debug("writing candle file", "path", path)
	if err := bw.Flush(); err != nil {
		return err
	}

	s.invalidateCache(key)
	return nil
}

func (s *store) SaveFile(key Key, r io.ReadCloser) (path string, err error) {
	if r == nil {
		return "", errors.New("nil reader")
	}
	defer r.Close()

	dst, err := s.PathForAsset(key)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
	}

	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return "", fmt.Errorf("create %s: %w", tmp, err)
	}

	// Important: flush + close BEFORE rename/stat
	n, copyErr := io.Copy(f, r)
	syncErr := f.Sync()
	closeErr := f.Close()

	if copyErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(tmp)
		if copyErr != nil {
			return "", fmt.Errorf("write %s: wrote %d bytes: %w", tmp, n, copyErr)
		}
		if syncErr != nil {
			return "", fmt.Errorf("sync %s: wrote %d bytes: %w", tmp, n, syncErr)
		}
		return "", fmt.Errorf("close %s: wrote %d bytes: %w", tmp, n, closeErr)
	}

	// Atomic move into place
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("rename %s -> %s: %w", tmp, dst, err)
	}
	return dst, nil
}

func (s *store) Delete(k Key) error {
	p, err := s.PathForAsset(k)
	if err != nil {
		return err
	}
	err = os.Remove(p)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	s.invalidateCache(k)
	return nil
}

func (s *store) baseScanDir() string {
	return s.basedir
}

func (s *store) IsUsableTickFile(k Key) bool {
	p, err := s.PathForAsset(k)
	if err != nil {
		return false
	}
	info, err := os.Stat(p)
	if err != nil {
		return false
	}
	return !info.IsDir() && info.Size() > 0
}

func (s *store) OpenTickIterator(key Key) (iterator[RawTick], error) {
	if key.Kind != KindTick {
		return nil, fmt.Errorf("OpenTickIterator: not a tick key: %+v", key)
	}
	if key.TF != types.Ticks {
		return nil, fmt.Errorf("OpenTickIterator: bad timeframe for tick key: %+v", key)
	}

	if market.IsForexMarketClosed(key.Time()) {
		return nil, fmt.Errorf("OpenTickIterator: market is closed: %+v", key)
	}

	if ok := s.IsUsableTickFile(key); !ok {
		return nil, fmt.Errorf("OpenTickIterator: tick file not usable: %+v", key)
	}

	path, err := s.PathForAsset(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	zr, err := lzma.NewReader(bufio.NewReaderSize(f, 1<<20))
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("lzma reader %s: %w", path, err)
	}

	baseUnixMS := types.TimeMillis(time.Date(
		key.Year,
		time.Month(key.Month),
		key.Day,
		key.Hour,
		0, 0, 0, time.UTC,
	).UnixMilli())

	inst := market.GetInstrument(key.Instrument)
	var priceMultiplier uint32 = 1
	if inst != nil {
		priceMultiplier = inst.DukascopyPriceMultiplier()
	}

	nextFn := func() (RawTick, bool, error) {
		return readNextBI5Tick(zr, path, baseUnixMS, priceMultiplier)
	}

	closeFn := func() error {
		return f.Close()
	}
	return newFuncIterator(nextFn, closeFn), nil
}

func readNextBI5Tick(r io.Reader, path string, baseUnixMS types.TimeMillis, priceMultiplier uint32) (RawTick, bool, error) {
	const recSize = 20

	var buf [recSize]byte

	_, err := io.ReadFull(r, buf[:])
	if err == io.EOF {
		return RawTick{}, false, nil
	}
	if err == io.ErrUnexpectedEOF {
		return RawTick{}, false, fmt.Errorf("truncated tick record in %s", path)
	}
	if err != nil {
		return RawTick{}, false, fmt.Errorf("read tick record %s: %w", path, err)
	}

	msOffset := binary.BigEndian.Uint32(buf[0:4])
	askU := binary.BigEndian.Uint32(buf[4:8])
	bidU := binary.BigEndian.Uint32(buf[8:12])

	askVol := math.Float32frombits(binary.BigEndian.Uint32(buf[12:16]))
	bidVol := math.Float32frombits(binary.BigEndian.Uint32(buf[16:20]))

	if msOffset >= 3600*1000 {
		return RawTick{}, false, fmt.Errorf("bad msOffset=%d in %s (decoder misaligned?)", msOffset, path)
	}

	t := RawTick{
		TimeMillis: baseUnixMS + types.TimeMillis(msOffset),
		Ask:        types.Price(askU * priceMultiplier),
		Bid:        types.Price(bidU * priceMultiplier),
		AskVol:     askVol,
		BidVol:     bidVol,
	}

	return t, true, nil
}
