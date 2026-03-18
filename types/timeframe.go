package types

import "strings"

type Timeframe int64

const (
	TF0   Timeframe = 0
	Ticks Timeframe = 1
	M1    Timeframe = 60
	H1    Timeframe = 3600
	D1    Timeframe = 86400
)

// func (tf Timeframe) Seconds() int64

// func (tf Timeframe) Duration() time.Duration

// func (tf Timeframe) String() string

// func ParseTimeframe(s string) (Timeframe, error)

// func (tf Timeframe) IsValid() bool

// func (tf Timeframe) ParentOf(child Timeframe) bool

// func (tf Timeframe) Ratio(child Timeframe) (int, error)

func TF(t string) Timeframe {
	t = strings.ToLower(t)

	switch t {
	default:
	case "tf0":
		return TF0

	case "ticks":
		return Ticks

	case "m1":
		return M1

	case "h1":
		return H1

	case "d1":
		return D1
	}
	return TF0
}

func (tf Timeframe) String() string {
	switch tf {
	case TF0:
		return "tf0"

	case Ticks:
		return "ticks"

	case M1:
		return "m1"

	case H1:
		return "h1"

	case D1:
		return "d1"

	default:
		return "UNKNOWN"
	}
}
