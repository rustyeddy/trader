package trader

import "encoding/json"

// AnalysisStatus is the action classification from a ChatGPT forex analysis row.
type AnalysisStatus string

const (
	StatusNoTrade   AnalysisStatus = "No Trade"
	StatusWatchlist AnalysisStatus = "Watchlist"
	StatusTradeable AnalysisStatus = "Tradeable watch list"
)

// ForexAnalysis holds one row from a ChatGPT forex analysis CSV.
// Price fields are stored as scaled int32 (Price) matching the rest of the
// engine; JSON output converts them back to decimal via Float64().
type ForexAnalysis struct {
	Group          string         `json:"-"`
	Pair           string         `json:"-"`
	Structure      string         `json:"-"`
	SetupBias      string         `json:"-"`
	Trend          string         `json:"-"`
	Volatility     string         `json:"-"`
	SupportLow     Price          `json:"-"`
	SupportHigh    Price          `json:"-"`
	ResistanceLow  Price          `json:"-"`
	ResistanceHigh Price          `json:"-"`
	Status         AnalysisStatus `json:"-"`
}

// forexAnalysisJSON is the wire representation with prices as decimals.
type forexAnalysisJSON struct {
	Group          string         `json:"group"`
	Pair           string         `json:"pair"`
	Structure      string         `json:"structure"`
	SetupBias      string         `json:"setup_bias"`
	Trend          string         `json:"trend"`
	Volatility     string         `json:"volatility"`
	SupportLow     float64        `json:"support_low"`
	SupportHigh    float64        `json:"support_high"`
	ResistanceLow  float64        `json:"resistance_low"`
	ResistanceHigh float64        `json:"resistance_high"`
	Status         AnalysisStatus `json:"status"`
}

// MarshalJSON emits price fields as decimal floats.
func (f ForexAnalysis) MarshalJSON() ([]byte, error) {
	return json.Marshal(forexAnalysisJSON{
		Group:          f.Group,
		Pair:           f.Pair,
		Structure:      f.Structure,
		SetupBias:      f.SetupBias,
		Trend:          f.Trend,
		Volatility:     f.Volatility,
		SupportLow:     f.SupportLow.Float64(),
		SupportHigh:    f.SupportHigh.Float64(),
		ResistanceLow:  f.ResistanceLow.Float64(),
		ResistanceHigh: f.ResistanceHigh.Float64(),
		Status:         f.Status,
	})
}

// IsTradeable reports whether the row is an active trade candidate.
func (f ForexAnalysis) IsTradeable() bool {
	return f.Status == StatusTradeable
}

// IsWatched reports whether the row belongs on any watchlist
// (both Watchlist and Tradeable rows qualify).
func (f ForexAnalysis) IsWatched() bool {
	return f.Status == StatusWatchlist || f.Status == StatusTradeable
}
