package marketdata

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrInvalidIntervalJSON marks JSON input to Interval.UnmarshalJSON that
// does not describe a well-formed Interval.
var ErrInvalidIntervalJSON = errors.New("marketdata: invalid interval json")

// intervalWire is Interval's explicit wire shape (issue #215 review):
// Interval.String is documented as display-only and never parsed back
// ("H4" is ambiguous with, for example, a future hex-count scheme), so
// JSON encoding uses named fields instead of reusing that string, never
// treating String as a parser contract.
type intervalWire struct {
	Unit  string `json:"unit"`
	Count int    `json:"count"`
}

// unitNames/parseUnitName translate between Unit and its canonical wire
// name — full words, distinct from String's single-letter display form,
// so the two encodings can never be confused with one another.
var unitNames = map[Unit]string{
	UnitMinute: "minute",
	UnitHour:   "hour",
	UnitDay:    "day",
	UnitWeek:   "week",
}

func parseUnitName(s string) (Unit, bool) {
	for u, name := range unitNames {
		if name == s {
			return u, true
		}
	}
	return 0, false
}

// MarshalJSON implements json.Marshaler, encoding i as {"unit":
// "<name>","count":<n>}.
func (i Interval) MarshalJSON() ([]byte, error) {
	if err := i.validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidIntervalJSON, err)
	}
	name, ok := unitNames[i.unit]
	if !ok {
		return nil, fmt.Errorf("%w: unit %v has no wire name", ErrInvalidIntervalJSON, i.unit)
	}
	return json.Marshal(intervalWire{Unit: name, Count: i.count})
}

// UnmarshalJSON implements json.Unmarshaler, decoding the {"unit","count"}
// shape MarshalJSON produces. It rejects an unrecognized unit name or a
// non-positive count — the same well-formedness NewInterval itself
// requires.
func (i *Interval) UnmarshalJSON(data []byte) error {
	var w intervalWire
	if err := json.Unmarshal(data, &w); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidIntervalJSON, err)
	}
	unit, ok := parseUnitName(w.Unit)
	if !ok {
		return fmt.Errorf("%w: unrecognized unit %q", ErrInvalidIntervalJSON, w.Unit)
	}
	parsed, err := NewInterval(unit, w.Count)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidIntervalJSON, err)
	}
	*i = parsed
	return nil
}

// timeRangeWire is TimeRange's wire shape: its start/end fields are
// unexported, same reason Interval needs an explicit shape rather than
// relying on default struct-field reflection. time.Time already
// implements json.Marshaler/Unmarshaler (RFC 3339), so the wire fields
// reuse that directly rather than re-deriving a timestamp encoding.
type timeRangeWire struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// MarshalJSON implements json.Marshaler, encoding r's start/end as
// RFC 3339 timestamps (time.Time's own canonical JSON form).
func (r TimeRange) MarshalJSON() ([]byte, error) {
	return json.Marshal(timeRangeWire{Start: r.start, End: r.end})
}

// UnmarshalJSON implements json.Unmarshaler, decoding the {"start","end"}
// shape MarshalJSON produces, through the same NewTimeRange validation
// (end strictly after start) every other TimeRange construction path
// uses.
func (r *TimeRange) UnmarshalJSON(data []byte) error {
	var w timeRangeWire
	if err := json.Unmarshal(data, &w); err != nil {
		return fmt.Errorf("marketdata: invalid time range json: %w", err)
	}
	parsed, err := NewTimeRange(w.Start, w.End)
	if err != nil {
		return fmt.Errorf("marketdata: invalid time range json: %w", err)
	}
	*r = parsed
	return nil
}
