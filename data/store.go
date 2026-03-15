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

// Store enforces a file naming convention like:
//
//	GBPUSD-M1-2026-01.csv
//	GBPUSD-H1-2026-02.csv
//	GBPUSD-D1-2026-02.csv
type Store struct {
	Basedir string // e.g. "data/candles"
}

func (s *Store) PathForAsset(k AssetKey) string {
	switch {
	case k.Kind == KindCandle && k.Day == 0 && k.Hour == 0:
		return s.pathForMonthlyCandle(k)

	case k.Kind == KindTick && k.Day > 0 && k.Hour >= 0:
		return s.pathForHourlyTick(k)

	default:
		panic(fmt.Sprintf("unsupported asset key for path: %+v", k))
	}
}

func (s *Store) pathForMonthlyCandle(k AssetKey) string {
	instrument := strings.ToLower(k.Instrument)
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

func (s *Store) pathForHourlyTick(k AssetKey) string {
	instrument := strings.ToLower(k.Instrument)

	return filepath.Join(
		s.Basedir,
		instrument,
		"tick",
		fmt.Sprintf("%04d", k.Year),
		fmt.Sprintf("%02d", k.Month),
		fmt.Sprintf("%02d", k.Day),
		fmt.Sprintf("%02d.bi5", k.Hour),
	)
}

func (s *Store) RelDir(key MonthKey) string {
	return filepath.Join(
		strings.ToUpper(key.Instrument),
		strings.ToUpper(key.TF.String()),
		fmt.Sprintf("%04d", key.Year),
	)
}

func (s Store) Exists(key AssetKey) (bool, error) {
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

func (store *Store) writeMetadata(cs *market.CandleSet, w io.Writer) error {
	tfstr := cs.Timeframe.String()
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

	_, err = fmt.Fprintln(w, "time;O;H;L;C;AvgSpread;MaxSpread;Ticks;Valid")
	return err
}

func (store *Store) WriteCSV(cs *market.CandleSet) error {
	if cs == nil {
		return errors.New("nil CandleSet")
	}

	// TODO Fix the filename and consistency
	path := cs.Filename() + ".csv"
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
	w.Comma = ';'
	defer w.Flush()

	step := cs.Timeframe.Int64()
	if step <= 0 {
		return fmt.Errorf("invalid Timeframe=%d", cs.Timeframe)
	}

	for i := 0; i < len(cs.Candles); i++ {
		openUnix := int64(cs.Start) + int64(i)*step
		t := time.Unix(openUnix, 0).UTC().Format(time.RFC3339)

		c := cs.Candles[i]
		valid := 1
		if len(cs.Valid) > 0 && !bitIsSet(cs.Valid, i) {
			valid = 0
		}

		rec := []string{
			t,
			formatNumber(c.Open, cs.Scale),
			formatNumber(c.High, cs.Scale),
			formatNumber(c.Low, cs.Scale),
			formatNumber(c.Close, cs.Scale),
			formatNumber(c.AvgSpread, cs.Scale),
			formatNumber(c.MaxSpread, cs.Scale),
			strconv.FormatInt(int64(c.Ticks), 10),
			strconv.Itoa(valid),
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

			ak := AssetKey{
				MonthKey: MonthKey{
					Instrument: instrument,
					Kind:       KindCandle,
					TF:         types.TF(tf),
					Year:       y,
					Month:      m,
				},
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

func normalizeTF(tf string) string {
	tf = strings.TrimSpace(strings.ToUpper(tf))
	// allow "60" etc if you ever pass seconds
	switch tf {
	case "60":
		return "M1"
	case "3600":
		return "H1"
	case "86400":
		return "D1"
	}
	return tf
}

func normalizeInstrument(sym string) string {
	sym = strings.TrimSpace(sym)
	sym = strings.ReplaceAll(sym, "_", "")
	sym = strings.ReplaceAll(sym, "/", "")
	return strings.ToUpper(sym)
}
