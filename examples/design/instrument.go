package main

var instruments = map[string]*instrument{
	"EURUSD": {
		symbol:     "EURUSD",
		base:       "EUR",
		quote:      "USD",
		pipSize:    10,             // appropriate to our price representation
		marginRate: rateTwoPercent, // 50:1 leverage
		minUnits:   1,
		unitStep:   1,
	},
}

// rate is a fixed-point ratio (a margin rate, a risk fraction) held as an
// integer scaled by rateScale, so 1% is 10_000. Like prices and money it never
// uses binary floating point.
type rate int64

// rateScale is the number of rate units in 1.0 (100%).
const rateScale int64 = 1_000_000

const (
	rateOnePercent rate = 10_000
	rateTwoPercent rate = 20_000
)

type instrument struct {
	symbol string
	base   string
	quote  string

	pipSize    price
	marginRate rate

	minUnits int64 // smallest tradable size
	maxUnits int64 // largest order size; 0 means no limit
	unitStep int64 // sizes are whole multiples of this
}

func getInstrument(symbol string) (*instrument, bool) {
	inst, ok := instruments[symbol]
	return inst, ok
}
