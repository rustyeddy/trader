package trader

import (
	"fmt"
	"math"
	"time"
)

// SyntheticCandleConfig holds parameters for generating synthetic candle data.
type SyntheticCandleConfig struct {
	Instrument  string    // e.g., "EURUSD"
	Timeframe   Timeframe // e.g., H1 (hourly)
	StartPrice  Price     // Starting price in scale units
	Volatility  float64   // Volatility as percentage (e.g., 0.005 = 0.5%)
	Trend       float64   // Trend as log return per candle (e.g., 0.0001 = +0.01%)
	Seed        int64     // Random seed for reproducibility
	TicksPerBar int32     // Number of ticks per candle
}

// DefaultSyntheticConfig returns a sensible default configuration for EUR/USD.
func DefaultSyntheticConfig(instrument string) SyntheticCandleConfig {
	return SyntheticCandleConfig{
		Instrument:  instrument,
		Timeframe:   H1,
		StartPrice:  Price(1080000), // 1.08000 in scale
		Volatility:  0.002,          // 0.2% volatility
		Trend:       0.00005,        // +0.005% trend per hour
		TicksPerBar: 50,
	}
}

// LinearCongruentialRandom is a simple deterministic RNG.
type LinearCongruentialRandom struct {
	state uint64
}

// NewLCRandom creates a new LCR with a seed.
func NewLCRandom(seed int64) *LinearCongruentialRandom {
	if seed <= 0 {
		seed = 42
	}
	return &LinearCongruentialRandom{state: uint64(seed)}
}

// NextGaussian returns a pseudo-random number from a normal distribution (Box-Muller).
func (r *LinearCongruentialRandom) NextGaussian() float64 {
	// Simple LCG: next = (a * prev + c) mod m
	const a = 1103515245
	const c = 12345
	const m = 2147483648 // 2^31
	r.state = (a*r.state + c) % m

	u1 := float64(r.state) / float64(m)

	r.state = (a*r.state + c) % m
	u2 := float64(r.state) / float64(m)

	// Box-Muller transform
	return math.Sqrt(-2.0*math.Log(u1)) * math.Cos(2.0*math.Pi*u2)
}

// NextUniform returns a pseudo-random number in [0, 1).
func (r *LinearCongruentialRandom) NextUniform() float64 {
	const a = 1103515245
	const c = 12345
	const m = 2147483648
	r.state = (a*r.state + c) % m
	return float64(r.state) / float64(m)
}

// GenerateSyntheticCandle generates a single candle using geometric Brownian motion.
// prevClose is the previous candle's close price.
func (cfg SyntheticCandleConfig) generateCandle(rng *LinearCongruentialRandom, prevClose Price) Candle {
	// Log-normal price changes
	trend := cfg.Trend
	volatility := cfg.Volatility
	driftTerm := trend - (volatility*volatility)/2.0

	// Generate random price change
	randomTerm := volatility * rng.NextGaussian()
	logReturn := driftTerm + randomTerm

	// Open is close of previous bar
	open := float64(prevClose)
	openPrice := prevClose

	// Generate high/low around the close
	// Close is computed from open and log return
	closePrice := Price(open * math.Exp(logReturn))

	// High and low are randomly distributed around open/close
	// High is always >= max(open, close)
	// Low is always <= min(open, close)
	highRandomness := volatility * rng.NextGaussian() * 0.5
	lowRandomness := volatility * rng.NextGaussian() * 0.5

	high := math.Max(float64(openPrice), float64(closePrice))
	low := math.Min(float64(openPrice), float64(closePrice))

	// Add additional high/low movement
	high *= (1.0 + math.Abs(highRandomness))
	low *= (1.0 - math.Abs(lowRandomness))

	highPrice := Price(high)
	lowPrice := Price(low)

	// Realistic spread (bid-ask can be 1-3 pips for major pairs)
	spread := Price(int64(rng.NextUniform()*3 + 1))

	return Candle{
		Open:      openPrice,
		High:      highPrice,
		Low:       lowPrice,
		Close:     closePrice,
		AvgSpread: spread,
		MaxSpread: spread * 2,
		Ticks:     cfg.TicksPerBar,
	}
}

// GenerateSyntheticMonthlyCandles generates a full month of synthetic OHLC data.
func (cfg SyntheticCandleConfig) GenerateSyntheticMonthlyCandles(year int, month time.Month) (*CandleSet, error) {
	cs, err := NewMonthlyCandleSet(
		cfg.Instrument,
		cfg.Timeframe,
		FromTime(time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)),
		PriceScale,
		"synthetic",
	)
	if err != nil {
		return nil, err
	}

	rng := NewLCRandom(cfg.Seed + int64(year)*12 + int64(month))
	currentPrice := cfg.StartPrice

	// Skip weekends and forex market closed times
	startTime := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	step := time.Duration(cfg.Timeframe) * time.Second

	for i := 0; i < len(cs.Candles); i++ {
		candleTime := startTime.Add(time.Duration(i) * step)

		// Skip if market is closed (weekends and typical forex market hours)
		if IsForexMarketClosed(candleTime) {
			continue
		}

		candle := cfg.generateCandle(rng, currentPrice)
		cs.Candles[i] = candle
		cs.SetValid(i)
		currentPrice = candle.Close
	}

	return cs, nil
}

// GenerateSyntheticYearlyCandles generates a full year of monthly candle sets.
func (cfg SyntheticCandleConfig) GenerateSyntheticYearlyCandles(year int) ([]*CandleSet, error) {
	var candleSets []*CandleSet
	for m := 1; m <= 12; m++ {
		cs, err := cfg.GenerateSyntheticMonthlyCandles(year, time.Month(m))
		if err != nil {
			return nil, fmt.Errorf("month %d: %w", m, err)
		}
		candleSets = append(candleSets, cs)
	}
	return candleSets, nil
}

// GenerateSyntheticYearlyAndWrite generates a year of synthetic data and writes it to CSV files.
func (cfg SyntheticCandleConfig) GenerateSyntheticYearlyAndWrite(store *Store, year int) ([]string, error) {
	candleSets, err := cfg.GenerateSyntheticYearlyCandles(year)
	if err != nil {
		return nil, err
	}

	var paths []string
	for _, cs := range candleSets {
		if err := store.WriteCSV(cs); err != nil {
			return nil, err
		}
		start := time.Unix(int64(cs.Start), 0).UTC()
		key := Key{
			Instrument: cs.Instrument,
			Source:     normalizeSource(cs.Source),
			Kind:       KindCandle,
			TF:         Timeframe(cs.Timeframe),
			Year:       start.Year(),
			Month:      int(start.Month()),
		}
		paths = append(paths, store.PathForAsset(key))
	}
	return paths, nil
}
