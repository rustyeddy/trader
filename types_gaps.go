package trader

type gapKind string

const (
	GapMinor      gapKind = "minor"
	GapWeekend    gapKind = "weekend"
	GapSuspicious gapKind = "suspicious"
)

// type Gap struct {
// 	Start Timestamp
// 	End   Timestamp
// 	TF    Timeframe
// 	Kind  GapKind
// }

// func (g Gap) Count() int
// func (g Gap) Duration() int64
