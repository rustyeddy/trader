package market

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rustyeddy/trader/types"
)

type Candle struct {
	Open      types.Price
	High      types.Price
	Low       types.Price
	Close     types.Price
	AvgSpread types.Price
	MaxSpread types.Price
	Ticks     int32 // number of ticks per candle
}

func (c *Candle) IsZero() bool {
	return c.Open == 0 && c.High == 0 && c.Low == 0 && c.Close == 0 && c.Ticks == 0
}

// CandleSet contains a dense set of candles.
type CandleSet struct {
	Instrument string
	Start      types.Timestamp // unix seconds for candle open
	Timeframe  types.Timeframe
	Scale      types.Scale6
	Source     string
	Candles    []Candle
	Valid      []uint64

	Filepath   string
	Gaps       []Gap
	duplicates int
	outOfRange int
	badLines   int

	prev int64
}

type Gap struct {
	StartIdx int32  // first missing candle index
	Len      int32  // number of missing intervals
	Kind     string // weekend vs suspicious
}

type GapStats struct {
	TotalMinutes   int
	PresentMinutes int
	MissingMinutes int
	GapCount       int
	WeekendGaps    int
	SuspiciousGaps int
	LongestGap     int
	LongestGapKind string
}

var estNoDST = time.FixedZone("EST", -5*60*60)

const layout = "20060102 150405"

func NewMonthlyCandleSet(inst string, tf types.Timeframe, monthStart types.Timestamp,
	scale types.Scale6, source string) (*CandleSet, error) {
	if inst == "" {
		return nil, fmt.Errorf("blank instrument")
	}
	if tf <= 0 {
		return nil, fmt.Errorf("invalid timeframe: %d", tf)
	}

	startTime := time.Unix(int64(monthStart), 0).UTC()
	if startTime.Second() != 0 || startTime.Nanosecond() != 0 {
		return nil, fmt.Errorf("monthStart not aligned to minute boundary: %d", monthStart)
	}
	if startTime.Day() != 1 || startTime.Hour() != 0 || startTime.Minute() != 0 {
		return nil, fmt.Errorf("monthStart not aligned to start of month: %s", startTime.Format(time.RFC3339))
	}

	endTime := startTime.AddDate(0, 1, 0)
	spanSec := int64(endTime.Sub(startTime).Seconds())
	n := int(spanSec / int64(tf))
	if n <= 0 {
		return nil, fmt.Errorf("computed invalid candle count: %d", n)
	}

	return &CandleSet{
		Instrument: inst,
		Start:      monthStart,
		Timeframe:  tf,
		Scale:      scale,
		Source:     source,
		Candles:    make([]Candle, n),
		Valid:      make([]uint64, (n+63)/64),
	}, nil
}

func (cs *CandleSet) AddCandle(ts types.Timestamp, c Candle) error {
	if cs == nil {
		return fmt.Errorf("nil CandleSet")
	}
	if cs.Timeframe <= 0 {
		return fmt.Errorf("invalid timeframe: %d", cs.Timeframe)
	}
	if ts < cs.Start {
		cs.outOfRange++
		return fmt.Errorf("timestamp %d before set start %d", ts, cs.Start)
	}

	tf := types.Timestamp(cs.Timeframe)

	off := ts - cs.Start
	if off%tf != 0 {
		return fmt.Errorf("timestamp %d not aligned to timeframe %d", ts, cs.Timeframe)
	}

	idx := int(off / tf)
	if idx < 0 || idx >= len(cs.Candles) {
		cs.outOfRange++
		return fmt.Errorf("timestamp %d out of range for set starting %d", ts, cs.Start)
	}

	if cs.IsValid(idx) {
		cs.duplicates++
		// overwrite policy for now
	}

	cs.Candles[idx] = c
	cs.SetValid(idx)
	cs.prev = int64(ts)
	return nil
}

func (cs *CandleSet) Merge(src *CandleSet) error {
	if cs == nil || src == nil {
		return fmt.Errorf("nil CandleSet in merge")
	}
	if cs.Timeframe != src.Timeframe {
		return fmt.Errorf("timeframe mismatch dst=%d src=%d", cs.Timeframe, src.Timeframe)
	}
	if cs.Scale != src.Scale {
		return fmt.Errorf("scale mismatch dst=%d src=%d", cs.Scale, src.Scale)
	}
	if cs.Instrument == "" || src.Instrument == "" {
		return fmt.Errorf("nil instrument in merge")
	}

	// Adjust this comparison to whatever your Instrument identity actually is.
	if cs.Instrument != src.Instrument {
		return fmt.Errorf("instrument mismatch dst=%q src=%q", cs.Instrument, src.Instrument)
	}

	for i := range src.Candles {
		if !src.IsValid(i) {
			continue
		}
		ts := src.Start + types.Timestamp(i)*types.Timestamp(src.Timeframe)
		if err := cs.AddCandle(ts, src.Candles[i]); err != nil {
			return err
		}
	}

	return nil
}

func (cs *CandleSet) SetValid(idx int) {
	cs.Valid[idx/64] |= uint64(1) << uint(idx%64)
}

func (cs *CandleSet) IsValid(idx int) bool {
	return cs.Valid[idx/64]&(uint64(1)<<uint(idx%64)) != 0
}

func (cs *CandleSet) CountValid() int {
	n := 0
	for i := range cs.Candles {
		if cs.IsValid(i) {
			n++
		}
	}
	return n
}

func FloorToMonthUTC(ts types.Timestamp) types.Timestamp {
	t := time.Unix(int64(ts), 0).UTC()
	first := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	return types.Timestamp(first.Unix())
}

func (cs *CandleSet) Time(idx int) time.Time {
	return time.Unix(int64(cs.Start)+int64(idx)*int64(cs.Timeframe), 0).UTC()
}

func (cs *CandleSet) Timestamp(idx int) types.Timestamp {
	return types.Timestamp(int64(cs.Start) + int64(idx)*int64(cs.Timeframe))
}

func (cs *CandleSet) Filename() string {
	inst := strings.ToLower(cs.Instrument)

	tfstr := cs.Timeframe.String()
	tfstr = strings.ToLower(tfstr)

	year := time.Unix(int64(cs.Start), 0).UTC().Year()

	if tfstr == "d1" {
		return fmt.Sprintf("%s-%s-all", inst, tfstr)
	}
	return fmt.Sprintf("%s-%s-%d", inst, tfstr, year)
}

func setValid(valid []uint64, idx int) {
	valid[idx/64] |= 1 << (idx % 64)
}

func isValid(valid []uint64, idx int) bool {
	return valid[idx/64]&(1<<(idx%64)) != 0
}

func (cs *CandleSet) scanBounds() (minTs, maxTs types.Timestamp, err error) {
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

func (cs *CandleSet) buildDenseFromFile() error {
	if cs.Timeframe == 0 {
		cs.Timeframe = 60
	}
	if cs.Scale == 0 {
		// your file has 6 decimals like 1.035030
		cs.Scale = 1_000_000
	}

	minTs, maxTs, err := cs.scanBounds()
	if err != nil {
		return err
	}

	tf := types.Timestamp(cs.Timeframe)
	start := types.Timestamp((minTs / tf) * tf)
	end := types.Timestamp((maxTs / tf) * tf)

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

	if duplicates > 0 || outOfRange > 0 || badLines > 0 {
		fmt.Fprintf(os.Stderr,
			"ingest warnings: duplicates=%d outOfRange=%d badLines=%d\n",
			cs.duplicates, cs.outOfRange, cs.badLines)
	}
	return nil
}

func (cs *CandleSet) BuildGapReport() {
	cs.Gaps = cs.Gaps[:0]

	n := len(cs.Candles)
	if n == 0 {
		return
	}

	i := 0
	for i < n {
		if bitIsSet(cs.Valid, i) {
			i++
			continue
		}

		// start of gap
		start := i
		for i < n && !bitIsSet(cs.Valid, i) {
			i++
		}
		length := i - start

		kind := cs.classifyGap(start, length)
		cs.Gaps = append(cs.Gaps, Gap{
			StartIdx: int32(start),
			Len:      int32(length),
			Kind:     kind,
		})
	}
}

func (cs *CandleSet) classifyGap(startIdx, length int) string {
	tf := int64(cs.Timeframe) // seconds per bar (60 for M1, 3600 for H1)

	startUnix := int64(cs.Start) + int64(startIdx)*tf
	t := time.Unix(startUnix, 0).UTC()
	wd := t.Weekday()

	gapSeconds := int64(length) * tf
	gapMinutes := gapSeconds / 60

	// Weekend-ish if gap >= 24h and starts Fri/Sat/Sun (UTC heuristic)
	if gapMinutes >= 60*24 {
		if wd == time.Friday || wd == time.Saturday || wd == time.Sunday {
			return "weekend"
		}
		return "suspicious"
	}

	// Anything >= 10 minutes missing is worth flagging (tune as you like)
	if gapMinutes >= 10 {
		return "suspicious"
	}

	return "minor"
}

func (cs *CandleSet) Stats() GapStats {
	var s GapStats

	if len(cs.Gaps) == 0 {
		cs.BuildGapReport()
	}

	n := len(cs.Candles)
	s.TotalMinutes = n

	// count present
	for i := 0; i < n; i++ {
		if bitIsSet(cs.Valid, i) {
			s.PresentMinutes++
		}
	}

	s.MissingMinutes = n - s.PresentMinutes

	for _, g := range cs.Gaps {
		s.GapCount++
		if int(g.Len) > s.LongestGap {
			s.LongestGap = int(g.Len)
			s.LongestGapKind = g.Kind
		}
		switch g.Kind {
		case "weekend":
			s.WeekendGaps++
		case "suspicious":
			s.SuspiciousGaps++
		}
	}

	return s
}
func (cs *CandleSet) AggregateH1(minValid int) *CandleSet {
	if cs.Timeframe != 60 {
		panic("AggregateH1 requires M1 source")
	}

	// Defensive: never allow 0 (would mark empty hours valid)
	if minValid < 1 {
		minValid = 1
	}
	if minValid > 60 {
		minValid = 60
	}

	tfIn := types.Timestamp(cs.Timeframe) // 60
	tfOut := types.Timestamp(3600)

	start := (cs.Start / tfOut) * tfOut
	end := cs.Start + types.Timestamp(len(cs.Candles)-1)*tfIn
	nHours := int((end-start)/tfOut) + 1

	h1 := &CandleSet{
		Instrument: cs.Instrument,
		Start:      start,
		Timeframe:  3600,
		Scale:      cs.Scale,
		Source:     cs.Source + " H1",
		Candles:    make([]Candle, nHours),
		Valid:      make([]uint64, (nHours+63)/64),
	}

	for h := 0; h < nHours; h++ {
		hourStart := start + types.Timestamp(h)*tfOut
		firstIdx := int((hourStart - cs.Start) / tfIn)

		validCount := 0
		var o, hi, lo, cl types.Price
		firstSet := false

		for m := 0; m < 60; m++ {
			idx := firstIdx + m
			if idx < 0 || idx >= len(cs.Candles) {
				continue
			}
			if !bitIsSet(cs.Valid, idx) {
				continue
			}

			bar := cs.Candles[idx]

			if !firstSet {
				o = bar.Open
				hi = bar.High
				lo = bar.Low
				firstSet = true
			} else {
				if bar.High > hi {
					hi = bar.High
				}
				if bar.Low < lo {
					lo = bar.Low
				}
			}
			cl = bar.Close
			validCount++
		}

		// Critical: require at least one real minute AND threshold
		if firstSet && validCount >= minValid {
			h1.Candles[h] = Candle{Open: o, High: hi, Low: lo, Close: cl}
			bitSet(h1.Valid, h)
		}
	}

	return h1
}

func (cs *CandleSet) Float64(v int32) float64 {
	return float64(v) / float64(cs.Scale)
}

func (cs *CandleSet) Int32(f float64) int32 {
	// round to nearest scaled int
	return int32(f*float64(cs.Scale) + 0.5)
}

// size of 1 pip in *price units* (float64), e.g. EURUSD: 0.0001, USDJPY: 0.01
func (cs *CandleSet) PipSize() float64 {
	i, ok := Instruments[cs.Instrument]
	if !ok {
		return 0.0
	}
	return math.Pow10(i.PipLocation) // PipLocation is negative
}

// number of encoded integer units per pip, e.g. if cs.Scale=1e6 and pip=1e-4 => 100 units/pip
func (cs *CandleSet) UnitsPerPip() float64 {
	return float64(cs.Scale) * cs.PipSize()
}

// convert encoded delta (int32) to pips
func (cs *CandleSet) DeltaToPips(delta int32) float64 {
	return float64(delta) / cs.UnitsPerPip()
}

// convert pips to encoded delta (int32)
func (cs *CandleSet) PipsToDelta(pips float64) int32 {
	return int32(pips*cs.UnitsPerPip() + 0.5)
}

func (cs *CandleSet) PrintStats(f io.WriteCloser) {
	cs.BuildGapReport()
	s := cs.Stats()

	fmt.Fprintln(f, "---- CandleSet Stats ----")
	fmt.Fprintf(f, "Range: %s → %s\n",
		cs.Time(0),
		cs.Time(len(cs.Candles)-1))
	fmt.Fprintf(f, "           Total Minutes: %d\n", s.TotalMinutes)
	fmt.Fprintf(f, "         Present Minutes: %d\n", s.PresentMinutes)
	fmt.Fprintf(f, "         Missing Minutes: %d\n", s.MissingMinutes)
	fmt.Fprintf(f, "              Total Gaps: %d\n", s.GapCount)
	fmt.Fprintf(f, "            Weekend Gaps: %d\n", s.WeekendGaps)
	fmt.Fprintf(f, "         Suspicious Gaps: %d\n", s.SuspiciousGaps)
	fmt.Fprintf(f, "Longest Gap: %d minutes (%s)\n",
		s.LongestGap, s.LongestGapKind)
	fmt.Fprintln(f, "--------------------------")
}

// Aggregate builds a higher timeframe CandleSet from a lower timeframe CandleSet.
// Assumes Timeframe is in seconds (e.g., 60, 3600, 86400).
func (cs *CandleSet) Aggregate(outTF types.Timeframe) (*CandleSet, error) {
	if cs == nil {
		return nil, fmt.Errorf("nil input candleset")
	}
	if cs.Timeframe <= 0 || outTF <= 0 {
		return nil, fmt.Errorf("bad timeframe cs=%d out=%d", cs.Timeframe, outTF)
	}
	if outTF%cs.Timeframe != 0 {
		return nil, fmt.Errorf("outTF %d must be multiple of csTF %d", outTF, cs.Timeframe)
	}

	ratio := int(outTF / cs.Timeframe)
	outLen := (len(cs.Candles) + ratio - 1) / ratio

	out := &CandleSet{
		Instrument: cs.Instrument,
		Start:      cs.Start,
		Timeframe:  outTF,
		Scale:      cs.Scale,
		Source:     "candles",
		Candles:    make([]Candle, outLen),
		Valid:      make([]uint64, (outLen+63)/64),
	}

	hasValidBits := len(cs.Valid) > 0

	isValid := func(i int) bool {
		if !hasValidBits {
			return true
		}
		return (cs.Valid[i>>6] & (1 << uint(i&63))) != 0
	}
	setValid := func(i int) {
		out.Valid[i>>6] |= 1 << uint(i&63)
	}

	for oi := 0; oi < outLen; oi++ {
		start := oi * ratio
		end := start + ratio
		if end > len(cs.Candles) {
			end = len(cs.Candles)
		}

		var (
			outC      Candle
			haveAny   bool
			openSet   bool
			sumTicks  int64
			sumSpread int64 // sum(AvgSpread * Ticks)
		)

		for ii := start; ii < end; ii++ {
			c := cs.Candles[ii]
			valid := isValid(ii)

			// Skip completely empty/invalid candles.
			if !valid && c.Ticks == 0 {
				continue
			}

			if !openSet {
				outC.Open = c.Open
				outC.High = c.High
				outC.Low = c.Low
				openSet = true
			} else {
				if c.High > outC.High {
					outC.High = c.High
				}
				if c.Low < outC.Low {
					outC.Low = c.Low
				}
			}

			outC.Close = c.Close

			if c.MaxSpread > outC.MaxSpread {
				outC.MaxSpread = c.MaxSpread
			}

			t := int64(c.Ticks)
			sumTicks += t
			if t > 0 {
				sumSpread += int64(c.AvgSpread) * t
			}

			haveAny = true
		}

		if !haveAny {
			continue
		}

		outC.Ticks = int32(sumTicks)
		if sumTicks > 0 {
			outC.AvgSpread = types.Price((sumSpread + sumTicks/2) / sumTicks)
		}

		out.Candles[oi] = outC
		setValid(oi)
	}

	return out, nil
}

type Iterator struct {
	cs  *CandleSet
	idx int
}

func (cs *CandleSet) Iterator() *Iterator {
	return &Iterator{
		cs:  cs,
		idx: -1,
	}
}

func (it *Iterator) Next() bool {
	n := len(it.cs.Candles)

	for {
		it.idx++
		if it.idx >= n {
			return false
		}
		if bitIsSet(it.cs.Valid, it.idx) {
			return true
		}
	}
}

func (it *Iterator) Candle() Candle {
	return it.cs.Candles[it.idx]
}

func (it *Iterator) Index() int {
	return it.idx
}

func (it *Iterator) Timestamp() types.Timestamp {
	return it.cs.Timestamp(it.idx)
}

func (it *Iterator) Time() time.Time {
	return it.cs.Time(it.idx)
}

func (it *Iterator) StartTime() types.Timestamp {
	return it.cs.Start
}
