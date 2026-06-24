package marketdata

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/market"
)

type Key struct {
	Instrument string
	Source     string
	Kind       DataKind
	TF         market.Timeframe
	Year       int
	Month      int
	Day        int
	Hour       int
}

// compare returns:
//
//	-1 if ak < k
//	 0 if ak == k
//	 1 if ak > k
func (ak Key) compare(k Key) int {
	if ak.Source < k.Source {
		return -1
	}
	if ak.Source > k.Source {
		return 1
	}

	if ak.Instrument < k.Instrument {
		return -1
	}
	if ak.Instrument > k.Instrument {
		return 1
	}

	if ak.Kind < k.Kind {
		return -1
	}
	if ak.Kind > k.Kind {
		return 1
	}

	if ak.TF < k.TF {
		return -1
	}
	if ak.TF > k.TF {
		return 1
	}

	if ak.Year < k.Year {
		return -1
	}
	if ak.Year > k.Year {
		return 1
	}

	if ak.Month < k.Month {
		return -1
	}
	if ak.Month > k.Month {
		return 1
	}

	if ak.Day < k.Day {
		return -1
	}
	if ak.Day > k.Day {
		return 1
	}
	if ak.Hour < k.Hour {
		return -1
	}
	if ak.Hour > k.Hour {
		return 1
	}

	return 0
}

func (ak Key) before(k Key) bool {
	return ak.compare(k) < 0
}

func (ak Key) after(k Key) bool {
	return ak.compare(k) > 0
}

// Time returns the UTC time represented by the key.
// Missing fields are normalized to the earliest valid value.
// Use Validate() first when invalid keys should be rejected instead of coerced.
//
// Examples:
//
//	Year=2024, Month=0, Day=0, Hour=0 -> 2024-01-01 00:00:00 UTC
//	Year=2024, Month=5, Day=0, Hour=0 -> 2024-05-01 00:00:00 UTC
//	Year=2024, Month=5, Day=7, Hour=13 -> 2024-05-07 13:00:00 UTC
func (ak Key) Time() time.Time {
	year := ak.Year
	if year <= 0 {
		year = 1970
	}

	month := ak.Month
	if month < 1 || month > 12 {
		month = 1
	}

	day := ak.Day
	if day < 1 || day > 31 {
		day = 1
	}

	hour := ak.Hour
	if hour < 0 || hour > 23 {
		hour = 0
	}

	return time.Date(year, time.Month(month), day, hour, 0, 0, 0, time.UTC)
}

func (k Key) Validate() error {
	switch {
	case k.IsHourlyTick():
		if k.TF != market.Ticks {
			return fmt.Errorf("tick key must use Ticks timeframe, got %v", k.TF)
		}
		return validateKeyDate(k.Year, k.Month, k.Day, k.Hour)

	case k.IsMonthlyCandle():
		if k.TF <= 0 || k.TF == market.Ticks {
			return fmt.Errorf("monthly candle key must use a candle timeframe, got %v", k.TF)
		}
		return validateKeyMonth(k.Year, k.Month)

	default:
		return fmt.Errorf("unsupported key shape: %+v", k)
	}
}

func (k Key) IsMonthlyCandle() bool {
	return k.Kind == KindCandle && k.Day == 0 && k.Hour == 0
}

func (k Key) IsHourlyTick() bool {
	return k.Kind == KindTick && k.Day > 0 && k.Hour >= 0
}

func (k Key) Range() (market.TimeRange, error) {
	if err := k.Validate(); err != nil {
		return market.TimeRange{}, err
	}
	start := k.Time().UTC()

	switch {
	case k.Kind == KindTick && k.Hour >= 0:
		end := start.Add(time.Hour)
		return market.TimeRange{
			Start: market.Timestamp(start.Unix()),
			End:   market.Timestamp(end.Unix()),
			TF:    market.Ticks,
		}, nil

	case k.Kind == KindCandle && k.Day == 0 && k.Hour == 0:
		end := start.AddDate(0, 1, 0)
		return market.TimeRange{
			Start: market.Timestamp(start.Unix()),
			End:   market.Timestamp(end.Unix()),
			TF:    k.TF,
		}, nil

	default:
		return market.TimeRange{}, fmt.Errorf("unsupported key range: %+v", k)
	}
}

func RequiredTickHoursForMonth(source, instrument string, year, month int) ([]Key, error) {
	if err := validateKeyMonth(year, month); err != nil {
		return nil, err
	}
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)

	out := make([]Key, 0, 24*31)

	for t := start; t.Before(end); t = t.Add(time.Hour) {
		if market.IsForexMarketClosed(t) {
			continue
		}

		out = append(out, Key{
			Source:     source,
			Instrument: market.NormalizeInstrument(instrument),
			Kind:       KindTick,
			TF:         market.Ticks,
			Year:       t.Year(),
			Month:      int(t.Month()),
			Day:        t.Day(),
			Hour:       t.Hour(),
		})
	}

	return out, nil
}

func validateKeyMonth(year, month int) error {
	if year <= 0 {
		return fmt.Errorf("key year must be > 0, got %d", year)
	}
	if month < 1 || month > 12 {
		return fmt.Errorf("key month must be between 1 and 12, got %d", month)
	}
	return nil
}

func validateKeyDate(year, month, day, hour int) error {
	if err := validateKeyMonth(year, month); err != nil {
		return err
	}
	if day < 1 || day > 31 {
		return fmt.Errorf("key day must be between 1 and 31, got %d", day)
	}
	if hour < 0 || hour > 23 {
		return fmt.Errorf("key hour must be between 0 and 23, got %d", hour)
	}

	t := time.Date(year, time.Month(month), day, hour, 0, 0, 0, time.UTC)
	if t.Year() != year || int(t.Month()) != month || t.Day() != day || t.Hour() != hour {
		return fmt.Errorf("key date is not a valid calendar time: %04d-%02d-%02d %02d:00Z", year, month, day, hour)
	}
	return nil
}
