package alpaca

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrMalformedData marks a raw Alpaca file, path, or row this package
// could not interpret — a bad file name, an unreadable file, a wrong
// column header, or a row with the wrong field count or an unparseable
// value. Mirrors stooq.ErrMalformedData exactly.
var ErrMalformedData = errors.New("alpaca: malformed data")

// meta is the file-level context resolved from a raw partition's path:
// its symbol, year, and month.
type meta struct {
	Symbol string
	Year   int
	Month  time.Month
}

// partitionFileName matches "SYMBOL-YYYY-MM-d1.csv" exactly — the
// identical convention stooq's own raw archive uses.
var partitionFileName = regexp.MustCompile(`^([A-Za-z0-9.\-]+)-(\d{4})-(\d{2})-d1\.csv$`)

// partitionPath returns the path a raw partition for (symbol, year,
// month) lives at under root: root/SYMBOL/YYYY/MM/SYMBOL-YYYY-MM-d1.csv.
func partitionPath(root, symbol string, year int, month time.Month) string {
	dir := filepath.Join(root, symbol, fmt.Sprintf("%04d", year), fmt.Sprintf("%02d", int(month)))
	base := fmt.Sprintf("%s-%04d-%02d-d1.csv", symbol, year, int(month))
	return filepath.Join(dir, base)
}

// parsePathMeta resolves path's partition metadata from its file name
// alone — stooq.parsePathMeta's identical approach.
func parsePathMeta(path string) (meta, error) {
	base := filepath.Base(path)
	m := partitionFileName.FindStringSubmatch(base)
	if m == nil {
		return meta{}, fmt.Errorf("%w: %s: file name does not match SYMBOL-YYYY-MM-d1.csv", ErrMalformedData, path)
	}
	symbol := strings.ToUpper(m[1])
	year, err := strconv.Atoi(m[2])
	if err != nil {
		return meta{}, fmt.Errorf("%w: %s: invalid year: %v", ErrMalformedData, path, err)
	}
	monthNum, err := strconv.Atoi(m[3])
	if err != nil || monthNum < 1 || monthNum > 12 {
		return meta{}, fmt.Errorf("%w: %s: invalid month", ErrMalformedData, path)
	}
	return meta{Symbol: symbol, Year: year, Month: time.Month(monthNum)}, nil
}

// verifyPathLayout reports whether path's location relative to root
// agrees with meta: exactly SYMBOL/YYYY/MM/<filename>.
func verifyPathLayout(root, path string, m meta) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrMalformedData, path, err)
	}
	want := filepath.Join(m.Symbol, fmt.Sprintf("%04d", m.Year), fmt.Sprintf("%02d", int(m.Month)), filepath.Base(path))
	if rel != want {
		return fmt.Errorf("%w: %s: expected to be filed at %s relative to root", ErrMalformedData, path, want)
	}
	return nil
}
