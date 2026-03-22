package data

import (
	"bufio"
	"encoding/binary"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/ulikunitz/xz/lzma"
)

// Store enforces a file naming convention like:
//
//	GBPUSD-M1-2026-01.csv
//	GBPUSD-H1-2026-02.csv
//	GBPUSD-D1-2026-02.csv
type Store struct {
	basedir string // e.g. "data/candles"
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
	source := normalizeSource(k.Source)
	if source == "" {
		source = "unknown"
	}
	instrument := market.NormalizeInstrument(k.Instrument)
	tf := strings.ToLower(k.TF.String())

	filename := fmt.Sprintf("%s-%04d-%02d-%s.csv",
		instrument,
		k.Year,
		k.Month,
		tf,
	)

	return filepath.Join(
		s.basedir,
		source,
		instrument,
		fmt.Sprintf("%04d", k.Year),
		fmt.Sprintf("%02d", k.Month),
		filename)
}

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
	case "d1":
		k.TF = types.D1
	default:
		return k, false
	}

	k.Month = month
	k.Year = year
	return k, true
}

func (s *Store) pathForHourlyTick(k Key) string {
	instrument := market.NormalizeInstrument(k.Instrument)

	// <basedir>/dukascopy/iiijjj/yyyy/mm/dd/hhh_ticks.bi5
	return filepath.Join(
		s.basedir,
		"dukascopy",
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
	//   n-5   -4  -3 -2    -1
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
		Instrument: market.NormalizeInstrument(inst),
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

func (s *Store) RelDir(key Key) string {
	return filepath.Join(
		market.NormalizeInstrument(key.Instrument),
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
	return filepath.Walk(s.basedir, func(path string, info os.FileInfo, err error) error {
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
			rng = types.MonthRange(key.Year, key.Month)
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

func (store *Store) writeMetadata(cs *market.CandleSet, w io.Writer) error {
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

func (store *Store) ReadCSV(key Key) (cs *market.CandleSet, err error) {
	if key.Kind != KindCandle {
		return nil, fmt.Errorf("ReadCSV only supports candle keys, got %v", key.Kind)
	}
	if key.Month < 1 || key.Month > 12 {
		return nil, fmt.Errorf("invalid candle key date: month %d out of range", key.Month)
	}
	if key.Day != 0 || key.Hour != 0 {
		return nil, fmt.Errorf("ReadCSV only supports monthly candle keys with Day==0 and Hour==0, got Day=%d Hour=%d", key.Day, key.Hour)
	}

	path := store.PathForAsset(key)

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
	cs = &market.CandleSet{
		Instrument: instName,
		Source:     "candles",
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

		highv, err := types.ParsePrice(fields[1])
		if err != nil {
			return nil, fmt.Errorf("csv %q row %d: parse high: %w", path, rowNum, err)
		}
		openv, err := types.ParsePrice(fields[2])
		if err != nil {
			return nil, fmt.Errorf("csv %q row %d: parse open: %w", path, rowNum, err)
		}
		lowv, err := types.ParsePrice(fields[3])
		if err != nil {
			return nil, fmt.Errorf("csv %q row %d: parse low: %w", path, rowNum, err)
		}
		closev, err := types.ParsePrice(fields[4])
		if err != nil {
			return nil, fmt.Errorf("csv %q row %d: parse close: %w", path, rowNum, err)
		}
		avgSpread, err := types.ParsePrice(fields[5])
		if err != nil {
			return nil, fmt.Errorf("csv %q row %d: parse avgspread: %w", path, rowNum, err)
		}
		maxSpread, err := types.ParsePrice(fields[6])
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

func (s *Store) WriteCSV(cs *market.CandleSet) error {
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
	path := s.PathForAsset(key)
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

	log.Printf("writing path: %s", path)
	return bw.Flush()
}

func (s *Store) SaveFile(key Key, r io.ReadCloser) (path string, err error) {
	dst := key.Path()
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

func (s Store) Delete(k Key) error {
	return os.Remove(k.Path())
}

func (s Store) baseScanDir() string {
	return s.basedir
}

func (s *Store) IsUsableTickFile(k Key) bool {
	p := s.PathForAsset(k)
	info, err := os.Stat(p)
	if err != nil {
		return false
	}
	return !info.IsDir() && info.Size() > 0
}

func (s *Store) OpenTickIterator(key Key) (Iterator[Tick], error) {
	if key.Kind != KindTick {
		return nil, fmt.Errorf("OpenTickIterator: not a tick key: %+v", key)
	}
	if key.TF != types.Ticks {
		return nil, fmt.Errorf("OpenTickIterator: bad timeframe for tick key: %+v", key)
	}

	if types.IsForexMarketClosed(key.Time()) {
		return nil, fmt.Errorf("OpenTickIterator: market is closed: %+v", key)
	}

	if ok := store.IsUsableTickFile(key); !ok {
		store.Delete(key)
		// return nil, fmt.Errorf("tick file not usable: %+v", key)
	}

	path := s.PathForAsset(key)
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	zr, err := lzma.NewReader(bufio.NewReaderSize(f, 1<<20))
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("lzma reader %s: %w", path, err)
	}

	baseUnixMS := types.Timemilli(time.Date(
		key.Year,
		time.Month(key.Month),
		key.Day,
		key.Hour,
		0, 0, 0, time.UTC,
	).UnixMilli())

	nextFn := func() (Tick, bool, error) {
		return readNextBI5Tick(zr, path, baseUnixMS)
	}

	closeFn := func() error {
		return f.Close()
	}
	return NewFuncIterator(nextFn, closeFn), nil
}

func readNextBI5Tick(r io.Reader, path string, baseUnixMS types.Timemilli) (Tick, bool, error) {
	const recSize = 20

	var buf [recSize]byte

	_, err := io.ReadFull(r, buf[:])
	if err == io.EOF {
		return Tick{}, false, nil
	}
	if err == io.ErrUnexpectedEOF {
		return Tick{}, false, fmt.Errorf("truncated tick record in %s", path)
	}
	if err != nil {
		return Tick{}, false, fmt.Errorf("read tick record %s: %w", path, err)
	}

	msOffset := binary.BigEndian.Uint32(buf[0:4])
	askU := binary.BigEndian.Uint32(buf[4:8])
	bidU := binary.BigEndian.Uint32(buf[8:12])

	askVol := math.Float32frombits(binary.BigEndian.Uint32(buf[12:16]))
	bidVol := math.Float32frombits(binary.BigEndian.Uint32(buf[16:20]))

	if msOffset >= 3600*1000 {
		return Tick{}, false, fmt.Errorf("bad msOffset=%d in %s (decoder misaligned?)", msOffset, path)
	}

	t := Tick{
		Timemilli: baseUnixMS + types.Timemilli(msOffset),
		Ask:       types.Price(askU * 10),
		Bid:       types.Price(bidU * 10),
		AskVol:    askVol,
		BidVol:    bidVol,
	}

	return t, true, nil
}
