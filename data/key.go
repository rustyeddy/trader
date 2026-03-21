package data

import (
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

type Key struct {
	Instrument string
	Source     string
	Kind       DataKind
	TF         types.Timeframe
	Year       int
	Month      int
	Day        int
	Hour       int
}

func (k Key) Path() string {
	return store.PathForAsset(k)
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

func (k Key) IsMonthlyCandle() bool {
	return k.Kind == KindCandle && k.Day == 0 && k.Hour == 0
}

func (k Key) IsHourlyTick() bool {
	return k.Kind == KindTick && k.Day > 0 && k.Hour >= 0
}
func RequiredTickHoursForMonth(source, instrument string, year, month int) []Key {
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)

	out := make([]Key, 0, 24*31)

	for t := start; t.Before(end); t = t.Add(time.Hour) {
		if types.IsForexMarketClosed(t) {
			continue
		}

		out = append(out, Key{
			Source:     source,
			Instrument: market.NormalizeInstrument(instrument),
			Kind:       KindTick,
			Year:       t.Year(),
			Month:      int(t.Month()),
			Day:        t.Day(),
			Hour:       t.Hour(),
		})
	}

	return out
}
