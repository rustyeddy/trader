package data

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// CandleStore enforces a file naming convention like:
//
//	GBPUSD-M1-2026.csv
//	GBPUSD-H1-2026.csv
//	GBPUSD-D1-all.csv
type CandleStore struct {
	Basedir string // e.g. "data/candles"
	Source  string // e.g. "dukascopy" (used as subdir by default)
}

func (s CandleStore) CandlePath(instrument, tf string, year int) string {

	instrument = normalizeInstrument(instrument)
	tf = normalizeTF(tf)

	tfLower := strings.ToLower(tf)
	instLower := strings.ToLower(instrument)

	var name string
	if year <= 0 {
		name = fmt.Sprintf("%s-%s-all.csv", instLower, tfLower)
	} else {
		name = fmt.Sprintf("%s-%s-%d.csv", instLower, tfLower, year)
	}

	return filepath.Join(
		s.Basedir,
		s.Source,
		instrument,
		tf,
		name,
	)
}

func (s CandleStore) Exists(instrument, tf string, year int) (bool, error) {
	p := s.CandlePath(instrument, tf, year)
	_, err := os.Stat(p)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// ListAvailableYears returns sorted years for which files exist for instrument+tf.
// It ignores "-all.csv".
func (s CandleStore) ListAvailableYears(instrument, tf string) ([]int, error) {
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
func (s CandleStore) LatestCompleteYear(instrument, tf string) (int, error) {
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
		ok, err := s.Exists(instrument, tf, y)
		if err != nil || !ok {
			continue
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

func (s CandleStore) baseScanDir() string {
	if s.Source != "" {
		return filepath.Join(s.Basedir, s.Source)
	}
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
