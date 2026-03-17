package data

import (
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

var (
	store = &Store{
		Basedir: "../../tmp",
	}
)

// Store enforces a file naming convention like:
//
//	GBPUSD-M1-2026-01.csv
//	GBPUSD-H1-2026-02.csv
//	GBPUSD-D1-2026-02.csv
type Store struct {
	Basedir string // e.g. "data/candles"
}

func (s *Store) PathForAsset(k Key) string {
	switch {
	case k.Kind == KindCandle && k.Day == 0 && k.Hour == 0:
		return s.pathForMonthlyCandle(k)

	case k.Kind == KindTick && k.Day > 0 && k.Hour >= 0:
		return s.pathForHourlyTick(k)

	default:
		panic(fmt.Sprintf("unsupported asset key for path: %+v", k))
	}
}

func (s *Store) pathForMonthlyCandle(k Key) string {
	instrument := normalizeInstrument(k.Instrument)
	tf := strings.ToLower(k.TF.String())

	filename := fmt.Sprintf("%s-%s-%04d-%02d.csv",
		instrument,
		tf,
		k.Year,
		k.Month,
	)

	return filepath.Join(
		s.Basedir,
		instrument,
		tf,
		fmt.Sprintf("%04d", k.Year),
		filename,
	)
}

func (s *Store) pathForHourlyTick(k Key) string {
	instrument := normalizeInstrument(k.Instrument)

	return filepath.Join(
		s.Basedir,
		"dukascopy",
		instrument,
		fmt.Sprintf("%04d", k.Year),
		fmt.Sprintf("%02d", k.Month),
		fmt.Sprintf("%02d", k.Day),
		fmt.Sprintf("%02dh_ticks.bi5", k.Hour),
	)
}

func (s *Store) RelDir(key Key) string {
	return filepath.Join(
		normalizeInstrument(key.Instrument),
		strings.ToUpper(key.TF.String()),
		fmt.Sprintf("%04d", key.Year),
	)
}

func (s Store) Exists(key Key) (bool, error) {
	p := s.PathForAsset(key)
	_, err := os.Stat(p)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (s *Store) scanFiles(inv *Inventory) error {
	return filepath.Walk(s.Basedir, func(path string, info os.FileInfo, err error) error {
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
			rng = types.NewTimeRange(types.FromTime(start), types.FromTime(end))

		case strings.HasSuffix(name, ".csv"):
			key, ok = parseCandlePath(path)
			if !ok {
				// log.Println("Failed to parse candle path ", path)
				return nil
			}
			rng = types.YearRange(key.Year)
			descriptor = "Candles"

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

func parseTickPath(path string) (Key, bool) {
	var k Key

	clean := filepath.ToSlash(path)

	// Example expected tail:
	// EURUSD/2025/01/02/13h_ticks.bi5
	parts := strings.Split(clean, "/")
	if len(parts) < 5 {
		return k, false
	}

	n := len(parts)
	file := parts[n-1]
	dayStr := parts[n-2]
	monthStr := parts[n-3]
	yearStr := parts[n-4]
	inst := parts[n-5]

	k = Key{
		Instrument: normalizeInstrument(inst),
		Source:     normalizeSource("dukascopy"),
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
	return k, true
}

func parseCandlePath(path string) (k Key, ok bool) {
	p := filepath.ToSlash(path)
	parts := strings.Split(p, "/")
	if len(parts) < 4 {
		return k, false
	}

	// Expect .../<source>/<instrument>/<tf>/<file>.csv
	n := len(parts)
	k = Key{
		Instrument: normalizeInstrument(parts[n-3]),
		Source:     normalizeSource(parts[n-4]),
	}

	tfStr := strings.ToUpper(parts[n-2])
	switch tfStr {
	case "M1", "m1": // XXX normalize these!!
		k.TF = types.M1
	case "H1", "h1":
		k.TF = types.H1
	case "D1", "d1":
		k.TF = types.D1
	default:
		return k, false
	}

	base := strings.ToLower(strings.TrimSuffix(parts[n-1], ".csv"))

	// e.g. eurusd-m1-2026-03.csv
	nameParts := strings.Split(base, "-")
	if len(nameParts) < 4 {
		return k, false
	}

	y, err := strconv.Atoi(nameParts[len(nameParts)-1])
	if err != nil {
		return k, false
	}
	k.Year = y

	println("TODO ENSURE MONTH PARSING IS CORRECT")
	m, err := strconv.Atoi(nameParts[len(nameParts)])
	if err != nil {
		return k, false
	}
	k.Month = m
	return k, true
}

func (store *Store) writeMetadata(cs *market.CandleSet, w io.Writer) error {
	tfstr := types.Timeframe(cs.Timeframe).String()
	year := time.Unix(int64(cs.Start), 0).UTC().Year()

	_, err := fmt.Fprintf(w,
		"# schema=v1 source=%s instrument=%s tf=%s year=%d scale=%d\n",
		cs.Source,
		cs.Instrument.Name,
		tfstr,
		year,
		cs.Scale,
	)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintln(w, "Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags")
	return err
}

func (store *Store) ReadCSV(key Key) (cs *market.CandleSet, err error) {
	if key.Kind != KindCandle {
		return nil, fmt.Errorf("ReadCSV only supports candle keys, got %v", key.Kind)
	}
	if key.Month < 1 || key.Month > 12 {
		return nil, fmt.Errorf("invalid candle key date: month %d out of range", key.Month)
	}

	path := store.PathForAsset(key)

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open csv %q: %w", path, err)
	}
	defer f.Close()

	// Derive CandleSet parameters from the key.
	monthStart := time.Date(key.Year, time.Month(key.Month), 1, 0, 0, 0, 0, time.UTC)
	startTS := types.Timestamp(monthStart.Unix())
	tf := types.Timestamp(key.TF)
	if tf <= 0 {
		return nil, fmt.Errorf("invalid candle timeframe %d: must be > 0", key.TF)
	}
	// Enforce a minimum supported candle timeframe (e.g., 1 minute in seconds).
	if tf < 60 {
		return nil, fmt.Errorf("unsupported candle timeframe %d: must be at least 60 seconds", key.TF)
	}

	nSlots := 0
	if tf > 0 {
		monthEnd := monthStart.AddDate(0, 1, 0)
		spanSec := int64(monthEnd.Sub(monthStart).Seconds())
		nSlots = int(spanSec / int64(tf))
	}

	inst := market.GetInstrument(normalizeInstrument(key.Instrument))
	if inst == nil {
		inst = &market.Instrument{Name: normalizeInstrument(key.Instrument)}
	}

	cs = &market.CandleSet{
		Instrument: inst,
		Start:      startTS,
		Timeframe:  tf,
	}
	if nSlots > 0 {
		cs.Candles = make([]market.Candle, nSlots)
		cs.Valid = make([]uint64, (nSlots+63)/64)
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	rowNum := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())

		// Skip empty lines and comment lines.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Skip header line (first non-comment field is "timestamp" or "time").
		first := strings.ToLower(strings.SplitN(line, ",", 2)[0])
		if first == "timestamp" || first == "time" {
			continue
		}

		rowNum++

		parts := strings.Split(line, ",")
		if len(parts) != 9 {
			return nil, fmt.Errorf("csv %q row %d: expected 9 fields, got %d", path, rowNum, len(parts))
		}
		dataRow++

		tsUnix, parseErr := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
		if parseErr != nil {
			return nil, fmt.Errorf("csv %q row %d: parse timestamp: %w", path, rowNum, parseErr)
		}

		// Validate timestamp alignment to timeframe.
		if tf > 0 && (tsUnix-int64(startTS))%int64(tf) != 0 {
			return nil, fmt.Errorf("csv %q row %d: timestamp %d not aligned to timeframe %d", path, rowNum, tsUnix, tf)
		}

		highv, parseErr := parsePrice(strings.TrimSpace(parts[1]))
		if parseErr != nil {
			return nil, fmt.Errorf("csv %q row %d: parse high: %w", path, rowNum, parseErr)
		}

		openv, parseErr := parsePrice(strings.TrimSpace(parts[2]))
		if parseErr != nil {
			return nil, fmt.Errorf("csv %q row %d: parse open: %w", path, rowNum, parseErr)
		}

		lowv, parseErr := parsePrice(strings.TrimSpace(parts[3]))
		if parseErr != nil {
			return nil, fmt.Errorf("csv %q row %d: parse low: %w", path, rowNum, parseErr)
		}

		closev, parseErr := parsePrice(strings.TrimSpace(parts[4]))
		if parseErr != nil {
			return nil, fmt.Errorf("csv %q row %d: parse close: %w", path, rowNum, parseErr)
		}

		avgSpread, parseErr := parsePrice(strings.TrimSpace(parts[5]))
		if parseErr != nil {
			return nil, fmt.Errorf("csv %q row %d: parse avgspread: %w", path, rowNum, parseErr)
		}

		maxSpread, parseErr := parsePrice(strings.TrimSpace(parts[6]))
		if parseErr != nil {
			return nil, fmt.Errorf("csv %q row %d: parse maxspread: %w", path, rowNum, parseErr)
		}

		ticks, parseErr := strconv.ParseInt(strings.TrimSpace(parts[7]), 10, 32)
		if parseErr != nil {
			return nil, fmt.Errorf("csv %q row %d: parse ticks: %w", path, rowNum, parseErr)
		}

		flags, parseErr := strconv.ParseUint(strings.TrimSpace(parts[8]), 0, 64)
		if parseErr != nil {
			return nil, fmt.Errorf("csv %q row %d: parse flags: %w", path, rowNum, parseErr)
		}

		c := market.Candle{
			High:      highv,
			Open:      openv,
			Low:       lowv,
			Close:     closev,
			AvgSpread: avgSpread,
			MaxSpread: maxSpread,
			Ticks:     int32(ticks),
		}

		if tf > 0 && nSlots > 0 {
			idx := int((types.Timestamp(tsUnix) - startTS) / tf)
			if idx >= 0 && idx < len(cs.Candles) {
				cs.Candles[idx] = c
				if flags != 0 {
					cs.SetValid(idx)
				}
			}
		} else {
			cs.Candles = append(cs.Candles, c)
			if flags != 0 {
				// Extend Valid slice if needed.
				idx := len(cs.Candles) - 1
				needed := (idx/64 + 1)
				for len(cs.Valid) < needed {
					cs.Valid = append(cs.Valid, 0)
				}
				cs.SetValid(idx)
			}
		}
	}

	if scanErr := sc.Err(); scanErr != nil {
		return nil, fmt.Errorf("scan csv %q: %w", path, scanErr)
	}

	return cs, nil
}

func (store *Store) WriteCSV(cs *market.CandleSet) error {
	if cs == nil {
		return errors.New("nil candle set")
	}
	if cs.Instrument == nil {
		return errors.New("nil candle set instrument")
	}

	step := cs.Timeframe.Int64()
	if step <= 0 {
		return fmt.Errorf("invalid candle set timeframe: %d", cs.Timeframe)
	}

	start := time.Unix(int64(cs.Start), 0).UTC()
	key := Key{
		Instrument: normalizeInstrument(cs.Instrument.Name),
		Kind:       KindCandle,
		TF:         types.Timeframe(cs.Timeframe),
		Year:       start.Year(),
		Month:      int(start.Month()),
	}
	path := store.PathForAsset(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	bw := bufio.NewWriterSize(f, 256*1024)
	defer bw.Flush()

	if err := store.writeMetadata(cs, bw); err != nil {
		return err
	}

	w := csv.NewWriter(bw)
	defer w.Flush()

	step := cs.Timeframe.Int64()

	for i := 0; i < len(cs.Candles); i++ {
		openUnix := int64(cs.Start) + int64(i)*step

		c := cs.Candles[i]
		var flags uint64
		if len(cs.Valid) > 0 && bitIsSet(cs.Valid, i) {
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
	return bw.Flush()
}

// ListAvailableYears returns sorted years for which files exist for instrument+tf.
// It ignores "-all.csv".
func (s Store) ListAvailableYears(instrument, tf string) ([]int, error) {
	dir := s.baseScanDir()
	instrument = normalizeInstrument(instrument)
	tf = normalizeTF(tf)

	re := regexp.MustCompile(fmt.Sprintf(`^%s-%s-(\d{4})\.csv$`,
		regexp.QuoteMeta(instrument),
		regexp.QuoteMeta(tf),
	))

	years := make([]int, 0, 16)
	seen := map[int]struct{}{}

	err := fs.WalkDir(os.DirFS(dir), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		m := re.FindStringSubmatch(base)
		if len(m) != 2 {
			return nil
		}
		y, err := strconv.Atoi(m[1])
		if err != nil {
			return nil
		}
		if _, ok := seen[y]; !ok {
			seen[y] = struct{}{}
			years = append(years, y)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Ints(years)
	return years, nil
}

// LatestCompleteYear returns the latest year that *looks complete* for the given timeframe,
// based on current UTC time and the presence of the year file.
//
// Rules:
// - For current year: only considered complete if "now" is after Jan 1 of next year.
// - For past years: if file exists, it's complete.
// - For tf=D1 and you store "-all.csv", use year=0 and this function isn't needed.
func (s Store) LatestCompleteYear(instrument, tf string) (int, error) {
	years, err := s.ListAvailableYears(instrument, tf)
	if err != nil {
		return 0, err
	}
	if len(years) == 0 {
		return 0, fmt.Errorf("no candle files found for %s %s", instrument, tf)
	}

	now := time.Now().UTC()
	currentYear := now.Year()

	// walk backwards
	for i := len(years) - 1; i >= 0; i-- {
		y := years[i]
		for m := 0; m < 12; m++ {

			ak := Key{
				Instrument: normalizeInstrument(instrument),
				Kind:       KindCandle,
				TF:         types.TF(tf),
				Year:       y,
				Month:      m,
			}
			ok, err := s.Exists(ak)
			if err != nil || !ok {
				continue
			}
		}
		// Only mark current year complete if we've actually passed it.
		if y == currentYear {
			continue
		}
		// If someone has future years (unlikely), ignore them.
		if y > currentYear {
			continue
		}
		return y, nil
	}

	// If only current year exists, it's not "complete" yet.
	return 0, fmt.Errorf("no complete year available yet for %s %s (only current year present)", instrument, tf)
}

func (s Store) baseScanDir() string {
	return s.Basedir
}

//	func PriceToFloat(price int32, scale int32) float64 {
//		return float64(price) / math.Pow10(int(scale))
//	}
func formatNumber(price types.Price, scale int32) string {
	decimals := 0
	for s := scale; s > 1; s /= 10 {
		decimals++
	}
	return strconv.FormatFloat(float64(price)/float64(scale), 'f', decimals, 64)
}

func normalizeTF(tf string) string {
	tf = strings.TrimSpace(strings.ToUpper(tf))
	// allow "60" etc if you ever pass seconds
	switch tf {
	case "60":
		return "m1"
	case "3600":
		return "h1"
	case "86400":
		return "d1"
	}
	return tf
}

func normalizeInstrument(sym string) string {
	sym = strings.TrimSpace(sym)
	sym = strings.ReplaceAll(sym, "_", "")
	sym = strings.ReplaceAll(sym, "/", "")
	return strings.ToUpper(sym)
}
