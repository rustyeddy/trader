package pricing

import "time"

type Candle struct {
	Instrument string // optional but handy
	Time       time.Time

	Open  float64
	High  float64
	Low   float64
	Close float64

	Volume float64 // optional
}
