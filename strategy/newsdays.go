package strategy

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
)

// LoadNewsDays reads a file of YYYY-MM-DD dates (one per line, # comments
// allowed) and returns a set of unix day numbers that strategies can use to
// block entries on high-impact news days.
func LoadNewsDays(path string) (map[int64]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("news_days_file: %w", err)
	}
	defer f.Close()

	days := map[int64]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.IndexByte(line, '#'); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		t, err := time.Parse("2006-01-02", line)
		if err != nil {
			return nil, fmt.Errorf("news_days_file: invalid date %q: %w", line, err)
		}
		days[t.UTC().Unix()/86400] = true
	}
	return days, sc.Err()
}
