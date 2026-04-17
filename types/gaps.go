package types

type GapKind string

const (
	GapMinor      GapKind = "minor"
	GapWeekend    GapKind = "weekend"
	GapSuspicious GapKind = "suspicious"
)

// type Gap struct {
// 	Start Timestamp
// 	End   Timestamp
// 	TF    Timeframe
// 	Kind  GapKind
// }

// func (g Gap) Count() int
// func (g Gap) Duration() int64
