package trader

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

var estNoDST = time.FixedZone("EST", -5*60*60)

const layout = "20060102 150405"

// scanBounds is an internal helper for trader type processing.
func (cs *candleSet) scanBounds() (minTs, maxTs Timestamp, err error) {
	f, err := os.Open(cs.Filepath)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") ||
			strings.HasPrefix(line, "time;") || strings.HasPrefix(line, "Time;") {
			continue
		}

		parts := strings.Split(line, ";")
		if len(parts) < 9 {
			continue
		}

		ts, e := parseToUnix(parts[0])
		if e != nil {
			continue
		}

		if minTs == 0 || ts < minTs {
			minTs = ts
		}
		if maxTs == 0 || ts > maxTs {
			maxTs = ts
		}
	}

	if err := sc.Err(); err != nil {
		return 0, 0, err
	}
	if minTs == 0 || maxTs == 0 {
		return 0, 0, fmt.Errorf("no valid timestamps found in %s", cs.Filepath)
	}
	return minTs, maxTs, nil
}

// BuildDenseFromFile allocates a dense grid covering [minTs..maxTs] at cs.Timeframe seconds,
// fills Candles and sets Valid bits when a candle exists in the file.
// Missing minutes naturally remain invalid (Valid bit = 0).

// buildDenseFromFile is an internal helper for trader type processing.
func (cs *candleSet) buildDenseFromFile() error {
	if cs.Timeframe == 0 {
		cs.Timeframe = 60
	}
	if cs.Scale == 0 {
		cs.Scale = 1_000_000
	}

	minTs, maxTs, err := cs.scanBounds()
	if err != nil {
		return err
	}

	tf := Timestamp(cs.Timeframe)
	start := Timestamp((minTs / tf) * tf)
	end := Timestamp((maxTs / tf) * tf)

	n := int((end-start)/tf) + 1

	cs.Start = start
	cs.Candles = make([]Candle, n)
	cs.Valid = make([]uint64, (n+63)/64)

	f, err := os.Open(cs.Filepath)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var duplicates int64
	var outOfRange int64
	var badLines int64

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") ||
			strings.HasPrefix(line, "time;") || strings.HasPrefix(line, "Time;") {
			continue
		}

		parts := strings.Split(line, ";")
		if len(parts) < 9 {
			badLines++
			continue
		}

		ts, e := parseToUnix(parts[0])
		if e != nil {
			badLines++
			continue
		}

		idx := int((ts - cs.Start) / tf)
		if idx < 0 || idx >= len(cs.Candles) {
			outOfRange++
			continue
		}

		if bitIsSet(cs.Valid, idx) {
			duplicates++
			continue
		}

		open, err := fastPrice(parts[1])
		if err != nil {
			badLines++
			continue
		}
		high, err := fastPrice(parts[2])
		if err != nil {
			badLines++
			continue
		}
		low, err := fastPrice(parts[3])
		if err != nil {
			badLines++
			continue
		}
		closep, err := fastPrice(parts[4])
		if err != nil {
			badLines++
			continue
		}
		avgSpread, err := fastPrice(parts[5])
		if err != nil {
			badLines++
			continue
		}
		maxSpread, err := fastPrice(parts[6])
		if err != nil {
			badLines++
			continue
		}

		ticks64, err := strconv.ParseInt(parts[7], 10, 32)
		if err != nil {
			badLines++
			continue
		}

		valid64, err := strconv.ParseUint(parts[8], 10, 64)
		if err != nil {
			badLines++
			continue
		}

		cs.Candles[idx] = Candle{
			Open:      open,
			High:      high,
			Low:       low,
			Close:     closep,
			AvgSpread: avgSpread,
			MaxSpread: maxSpread,
			Ticks:     int32(ticks64),
		}

		if valid64 != 0 {
			bitSet(cs.Valid, idx)
		}
	}

	if err := sc.Err(); err != nil {
		return err
	}

	cs.duplicates = int(duplicates)
	cs.outOfRange = int(outOfRange)
	cs.badLines = int(badLines)
	return nil
}
