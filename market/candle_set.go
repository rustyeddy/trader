package market

import (
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var estNoDST = time.FixedZone("EST", -5*60*60)

const layout = "20060102 150405"

type CandleSet struct {
	*Instrument
	Start     int64 // unix seconds for candle open
	Timeframe int32
	Scale     int32
	Source    string
	Candles   []Candle
	Valid     []uint64

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

func NewCandleSet(fname string) (cs *CandleSet, err error) {
	cs = &CandleSet{
		Filepath:  fname,
		Source:    "Dukascopy",
		Timeframe: 60,
		Scale:     1_000_000,
		prev:      -1,
	}

	if err := cs.ParseFilename(cs.Filepath); err != nil {
		return nil, err
	}

	if err := cs.buildDenseFromFile(); err != nil {
		return nil, err
	}
	cs.BuildGapReport()

	return cs, nil
}

func (cs *CandleSet) Time(idx int) time.Time {
	return time.Unix(cs.Start+int64(idx)*int64(cs.Timeframe), 0).UTC()
}

func (cs *CandleSet) ParseFilename(fname string) (err error) {
	base := filepath.Base(fname)
	ext := filepath.Ext(base)
	name := base[:len(base)-len(ext)]

	parts := strings.Split(name, "-")
	if len(parts) == 1 {
		parts = strings.Split(name, "_")
	}
	fmt.Printf("PARTS: %+v\n", parts)

	var year int
	switch parts[0] {
	case "DAT":
		cs.Instrument = Instruments[parts[2]]
		cs.Timeframe, err = TFStringToSeconds(parts[3])

	default:
		cs.Instrument = Instruments[parts[0]]
		cs.Timeframe, err = TFStringToSeconds(parts[2])
		year, err = strconv.Atoi(parts[4])
		if err != nil {
			return fmt.Errorf("invalid year: %w", err)
		}

	}

	cs.Start = time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC).Unix()

	return err
}

// scanBounds finds min/max timestamps (UTC unix seconds) in one pass.
// This is robust even if the file has weird lines or isn’t strictly sorted.
func (cs *CandleSet) scanBounds() (minTs, maxTs int64, err error) {
	f, err := os.Open(cs.Filepath)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	minTs = 0
	maxTs = 0

	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "time;") || strings.HasPrefix(line, "Time;") {
			continue
		}
		parts := strings.Split(line, ";")
		if len(parts) < 6 {
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

	tf := int64(cs.Timeframe)
	start := (minTs / tf) * tf
	end := (maxTs / tf) * tf

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
		line := sc.Text()
		if strings.HasPrefix(line, "time;") || strings.HasPrefix(line, "Time;") {
			continue
		}
		parts := strings.Split(line, ";")
		if len(parts) < 6 {
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
			// keep-first policy (ignore later duplicates)
			continue
		}

		prices := make([]int32, 4)
		for i := 1; i < 5; i++ {
			if prices[i-1], err = fastPrice(parts[i]); err != nil {
				err = fmt.Errorf("failed to convert %s to int32\n", parts[i])
				break
			}
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "error %s\n", err)
			continue
		}
		candle := Candle{
			O: prices[0],
			H: prices[1],
			L: prices[2],
			C: prices[3],
		}
		cs.Candles[idx] = candle
		bitSet(cs.Valid, idx)
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

	startUnix := cs.Start + int64(startIdx)*tf
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

	tfIn := int64(cs.Timeframe) // 60
	tfOut := int64(3600)

	start := (cs.Start / tfOut) * tfOut
	end := cs.Start + int64(len(cs.Candles)-1)*tfIn
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
		hourStart := start + int64(h)*tfOut
		firstIdx := int((hourStart - cs.Start) / tfIn)

		validCount := 0
		var o, hi, lo, cl int32
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
				o = bar.O
				hi = bar.H
				lo = bar.L
				firstSet = true
			} else {
				if bar.H > hi {
					hi = bar.H
				}
				if bar.L < lo {
					lo = bar.L
				}
			}
			cl = bar.C
			validCount++
		}

		// Critical: require at least one real minute AND threshold
		if firstSet && validCount >= minValid {
			h1.Candles[h] = Candle{O: o, H: hi, L: lo, C: cl}
			bitSet(h1.Valid, h)
		}
	}

	return h1
}

func (cs *CandleSet) F(v int32) float64 {
	return float64(v) / float64(cs.Scale)
}

func (cs *CandleSet) I(f float64) int32 {
	// round to nearest scaled int
	return int32(f*float64(cs.Scale) + 0.5)
}

// size of 1 pip in *price units* (float64), e.g. EURUSD: 0.0001, USDJPY: 0.01
func (cs *CandleSet) PipSize() float64 {
	i := cs.Instrument
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

func (cs *CandleSet) Filename() string {
	fname := cs.Instrument.Name
	tfstr, err := SecondsToTFString(cs.Timeframe)
	if err != nil {
		return "unknown"
	}
	year := time.Unix(cs.Start, 0).UTC().Year()
	fname += "-" + strconv.Itoa(year)
	fname += "-" + tfstr
	return fname
}

// WriteCSV writes candles as:
// RFC3339Time;Open;High;Low;Close;Valid
// No header line is written.
func (cs *CandleSet) WriteCSV(path string) error {
	if cs == nil {
		return errors.New("nil CandleSet")
	}
	if len(cs.Candles) == 0 {
		// Still create an empty file (often useful for pipelines)
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		return f.Close()
	}

	fname := cs.Filename()
	path = path + "/" + fname + ".csv"

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Buffered writer for performance.
	bw := bufio.NewWriterSize(f, 256*1024)
	defer bw.Flush()

	w := csv.NewWriter(bw)
	w.Comma = ';'
	// We intentionally do not write a header.
	defer w.Flush()

	// Duration between candle opens, in seconds.
	step := timeframeSeconds(cs.Timeframe)
	if step <= 0 {
		return fmt.Errorf("invalid Timeframe=%d (seconds must be > 0)", cs.Timeframe)
	}

	// If Valid is provided, we’ll use it. If not (or too short), treat all as valid=1.
	hasValid := len(cs.Valid) > 0

	for i := 0; i < len(cs.Candles); i++ {
		openUnix := cs.Start + int64(i)*step
		t := time.Unix(openUnix, 0).UTC().Format(time.RFC3339)

		c := cs.Candles[i]

		valid := uint64(1)
		if hasValid && i < len(cs.Valid) {
			valid = cs.Valid[i]
		}

		rec := []string{
			t,
			formatNumber(c.O, cs.Scale),
			formatNumber(c.H, cs.Scale),
			formatNumber(c.L, cs.Scale),
			formatNumber(c.C, cs.Scale),
			strconv.FormatUint(valid, 10),
		}

		if err := w.Write(rec); err != nil {
			return err
		}
	}

	// Check any buffered error from csv.Writer.
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	return bw.Flush()
}

// timeframeSeconds converts your int32 timeframe to seconds.
// If your Timeframe is already "seconds per candle", just return int64(tf).
func timeframeSeconds(tf int32) int64 {
	// Many codebases store timeframe as seconds (e.g., 60, 300, 3600).
	// If yours uses an enum (M1/H1/D1), replace this mapping accordingly.
	return int64(tf)
}

//	func PriceToFloat(price int32, scale int32) float64 {
//		return float64(price) / math.Pow10(int(scale))
//	}
func formatNumber(price int32, scale int32) string {
	// For floats, this uses default formatting; adjust if you need fixed decimals.
	return fmt.Sprintf("%f", float64(price)/float64(scale))
}

// Optional helper: write to any io.Writer (useful for tests, pipes, gzip, etc.)
func (cs *CandleSet) WriteCSVTo(w io.Writer) error {
	if cs == nil {
		return errors.New("nil CandleSet")
	}
	cw := csv.NewWriter(w)
	cw.Comma = ';'
	defer cw.Flush()

	step := timeframeSeconds(cs.Timeframe)
	if step <= 0 {
		return fmt.Errorf("invalid Timeframe=%d (seconds must be > 0)", cs.Timeframe)
	}

	hasValid := len(cs.Valid) > 0

	for i := 0; i < len(cs.Candles); i++ {
		openUnix := cs.Start + int64(i)*step
		t := time.Unix(openUnix, 0).UTC().Format(time.RFC3339)

		c := cs.Candles[i]
		valid := uint64(1)
		if hasValid && i < len(cs.Valid) {
			valid = cs.Valid[i]
		}

		if err := cw.Write([]string{
			t,
			formatNumber(c.O, cs.Scale),
			formatNumber(c.H, cs.Scale),
			formatNumber(c.L, cs.Scale),
			formatNumber(c.C, cs.Scale),
			strconv.FormatUint(valid, 10),
		}); err != nil {
			return err
		}
	}

	cw.Flush()
	return cw.Error()
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

func (it *Iterator) Time() time.Time {
	return it.cs.Time(it.idx)
}

func (it *Iterator) StartTime() int64 {
	return it.cs.Start
}
