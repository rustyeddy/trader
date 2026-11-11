package trader

var instruments = map[string]*instrument{
	"EURUSD": {
		symbol:     "EURUSD",
		marginRate: rateTwoPercent, // 50:1 leverage
		minUnits:   1,
		unitStep:   1,
	},
	"GBPUSD": {
		symbol:     "GBPUSD",
		marginRate: rateFivePercent, // 20:1 leverage
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
	rateOnePercent  rate = 10_000
	rateTwoPercent  rate = 20_000
	rateFivePercent rate = 50_000
)

type instrument struct {
	symbol     string
	marginRate rate

	minUnits int64 // smallest tradable size
	maxUnits int64 // largest order size; 0 means no limit
	unitStep int64 // sizes are whole multiples of this
}
