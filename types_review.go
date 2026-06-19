package trader

import "encoding/json"

// ReviewStatus is the action classification from a ChatGPT forex review row.
type ReviewStatus string

const (
	StatusNoTrade   ReviewStatus = "No Trade"
	StatusWatchlist ReviewStatus = "Watchlist"
	StatusTradeable ReviewStatus = "Tradeable watch list"
)

// ForexReview holds one row from a ChatGPT forex review CSV.
// Price fields are stored as scaled int32 (Price) matching the rest of the
// engine; JSON output converts them back to decimal via Float64().
type ForexReview struct {
	Group          string       `json:"-"`
	Pair           string       `json:"-"`
	Structure      string       `json:"-"`
	SetupBias      string       `json:"-"`
	Trend          string       `json:"-"`
	Volatility     string       `json:"-"`
	SupportLow     Price        `json:"-"`
	SupportHigh    Price        `json:"-"`
	ResistanceLow  Price        `json:"-"`
	ResistanceHigh Price        `json:"-"`
	Status         ReviewStatus `json:"-"`
}

// forexReviewJSON is the wire representation with prices as decimals.
type forexReviewJSON struct {
	Group          string       `json:"group"`
	Pair           string       `json:"pair"`
	Structure      string       `json:"structure"`
	SetupBias      string       `json:"setup_bias"`
	Trend          string       `json:"trend"`
	Volatility     string       `json:"volatility"`
	SupportLow     float64      `json:"support_low"`
	SupportHigh    float64      `json:"support_high"`
	ResistanceLow  float64      `json:"resistance_low"`
	ResistanceHigh float64      `json:"resistance_high"`
	Status         ReviewStatus `json:"status"`
}

// MarshalJSON emits price fields as decimal floats.
func (f ForexReview) MarshalJSON() ([]byte, error) {
	return json.Marshal(forexReviewJSON{
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
func (f ForexReview) IsTradeable() bool {
	return f.Status == StatusTradeable
}

// IsWatched reports whether the row belongs on any watchlist
// (both Watchlist and Tradeable rows qualify).
func (f ForexReview) IsWatched() bool {
	return f.Status == StatusWatchlist || f.Status == StatusTradeable
}
