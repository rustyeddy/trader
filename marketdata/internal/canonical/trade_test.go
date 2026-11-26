package canonical

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/trader/internal/store/storetest"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSnapshot = "sha256:54b64bba890d89495bc2108824ba378a3f7f92349d5021595499bbc47696ee6b"

func tradeBar(t *testing.T, y int, m time.Month, d int) marketdata.Bar {
	t.Helper()
	s, err := USEquitySession(y, m, d)
	require.NoError(t, err)
	return marketdata.Bar{Time: s.Open, AvailableAt: s.Close, Volume: 78064486,
		Trade: marketdata.OHLC{Open: px("501.028"), High: px("505.27"), Low: px("500.614"), Close: px("504.838")}}
}

// tradeFile is a synthetic March 2024 SPY file with the metadata of ADR-095's
// example.
func tradeFile(t *testing.T) File {
	t.Helper()
	last := tradeBar(t, 2024, 3, 28)
	last.Trade = marketdata.OHLC{Open: px("516.63500001"), High: px("518.018"), Low: px("516.212"), Close: px("516.498")}
	last.Volume = 0
	return File{
		Key: Key{Source: "stooq", Symbol: "SPY", Interval: D1, Year: 2024, Month: time.March},
		Meta: Meta{RawFingerprint: "sha256:month", BuilderVersion: "builder-v1", ValidatorVersion: "validator-v1",
			AvailabilityPolicy: PolicyUSEquityCloseV1, BuiltAt: utc(2026, 11, 26, 12),
			Trade: &TradeMeta{PriceBasis: PriceBasisTrade, Currency: "USD", VolumeUnit: VolumeShares,
				Adjustment: AdjustmentTotalReturn, AdjustmentEvidence: EvidenceObserved,
				SnapshotFingerprint: testSnapshot, SnapshotAsOf: "2026-09-10", Calendar: CalendarUSEquityV2}},
		Bars: []marketdata.Bar{tradeBar(t, 2024, 3, 1), last},
	}
}

const tradeText = `# schema=canonical-trade-v1 provider=stooq symbol=SPY interval=d1 year=2024 month=03
# meta {"raw_fingerprint":"sha256:month","builder_version":"builder-v1","validator_version":"validator-v1","availability_policy":"us-equity-close-v1","calendar":"us-equity-calendar-v2","built_at":"2026-11-26T12:00:00Z","bar_count":2,"bad_count":1,"first_bar":"2024-03-01T14:30:00Z","last_bar":"2024-03-28T13:30:00Z","price_basis":"trade","currency":"USD","volume_unit":"shares","adjustment":"total-return","adjustment_evidence":"observed","snapshot_fingerprint":"sha256:54b64bba890d89495bc2108824ba378a3f7f92349d5021595499bbc47696ee6b","snapshot_as_of":"2026-09-10"}
# bad time=2024-03-29T13:30:00Z reason=non-session raw_row=21
time,available_at,trade_o,trade_h,trade_l,trade_c,volume
2024-03-01T14:30:00Z,2024-03-01T21:00:00Z,501.02800000,505.27000000,500.61400000,504.83800000,78064486
2024-03-28T13:30:00Z,2024-03-28T20:00:00Z,516.63500001,518.01800000,516.21200000,516.49800000,0
`

func TestTradeFile_RoundTripsExactly(t *testing.T) {
	f := tradeFile(t)
	f.Bad = []BadRecord{{Time: minute(2024, 3, 29, 13, 30), Reason: ReasonNonSession, RawRow: 21}}

	assert.Equal(t, SchemaTrade, f.Schema())
	assert.Equal(t, tradeText, encode(t, f))

	got, err := Decode(strings.NewReader(tradeText))
	require.NoError(t, err)
	assert.Equal(t, f.Key, got.Key)
	assert.Equal(t, f.Meta, got.Meta)
	assert.Equal(t, f.Bad, got.Bad)
	require.Len(t, got.Bars, 2)
	for i, b := range got.Bars {
		want := f.Bars[i]
		want.Instrument = "SPY" // named from the key
		assert.Equal(t, want, b)
		assert.Equal(t, b.Trade, b.OHLC(), "the trade series is the preferred price")
		assert.True(t, b.Bid.IsZero() && b.Ask.IsZero() && b.Mid.IsZero(), "nothing is derived")
	}
}

func TestTradeFile_PublishesAndReads(t *testing.T) {
	storetest.Root(t)
	ctx := context.Background()
	f := tradeFile(t)

	outcome, err := Publish(ctx, f)
	require.NoError(t, err)
	assert.Equal(t, Built, outcome)
	outcome, err = Publish(ctx, f)
	require.NoError(t, err)
	assert.Equal(t, Unchanged, outcome)

	got, err := Read(ctx, f.Key)
	require.NoError(t, err)
	assert.Equal(t, *f.Meta.Trade, *got.Meta.Trade)

	next := tradeFile(t)
	next.Meta.Trade.SnapshotFingerprint = "sha256:" + strings.Repeat("a", 64)
	outcome, err = Publish(ctx, next)
	require.NoError(t, err)
	assert.Equal(t, Replaced, outcome, "another snapshot is another build")
}

func TestMetaSameBuild_ComparesTradeMeta(t *testing.T) {
	base := tradeFile(t).Meta
	assert.True(t, base.SameBuild(tradeFile(t).Meta), "equal values, distinct pointers")
	assert.False(t, base.SameBuild(Meta{RawFingerprint: base.RawFingerprint, BuilderVersion: base.BuilderVersion,
		ValidatorVersion: base.ValidatorVersion, AvailabilityPolicy: base.AvailabilityPolicy}))
	for name, change := range map[string]func(*TradeMeta){
		"basis": func(m *TradeMeta) { m.PriceBasis = "x" }, "currency": func(m *TradeMeta) { m.Currency = "EUR" },
		"unit": func(m *TradeMeta) { m.VolumeUnit = "x" }, "adjustment": func(m *TradeMeta) { m.Adjustment = AdjustmentSplit },
		"evidence": func(m *TradeMeta) { m.AdjustmentEvidence = EvidenceDocumented },
		"snapshot": func(m *TradeMeta) { m.SnapshotFingerprint = "x" }, "as of": func(m *TradeMeta) { m.SnapshotAsOf = "x" },
		"calendar": func(m *TradeMeta) { m.Calendar = "x" },
	} {
		o := tradeFile(t).Meta
		change(o.Trade)
		assert.False(t, base.SameBuild(o), name)
		assert.False(t, o.SameBuild(base), name)
	}
}

func TestTradeFile_RoundTripsEveryAdjustment(t *testing.T) {
	for _, a := range []Adjustment{AdjustmentTotalReturn, AdjustmentSplit, AdjustmentUnadjusted, AdjustmentUnknown} {
		for _, e := range []Evidence{EvidenceObserved, EvidenceDocumented} {
			f := tradeFile(t)
			f.Meta.Trade.Adjustment, f.Meta.Trade.AdjustmentEvidence = a, e
			got, err := Decode(strings.NewReader(encode(t, f)))
			require.NoError(t, err)
			assert.Equal(t, f.Meta.Trade, got.Meta.Trade)
		}
	}
}

func TestTradeFile_RequiresCompleteMetadata(t *testing.T) {
	for name, change := range map[string]func(*File){
		"interval h1":        func(f *File) { f.Key.Interval = H1 },
		"FX policy":          func(f *File) { f.Meta.AvailabilityPolicy = PolicyScheduledCloseV1 },
		"retired calendar":   func(f *File) { f.Meta.Trade.Calendar = "us-equity-calendar-v1" },
		"price basis":        func(f *File) { f.Meta.Trade.PriceBasis = "mid" },
		"no currency":        func(f *File) { f.Meta.Trade.Currency = "" },
		"bad currency":       func(f *File) { f.Meta.Trade.Currency = "usd" },
		"volume unit":        func(f *File) { f.Meta.Trade.VolumeUnit = "ticks" },
		"no adjustment":      func(f *File) { f.Meta.Trade.Adjustment = "" },
		"no evidence":        func(f *File) { f.Meta.Trade.AdjustmentEvidence = "" },
		"no snapshot":        func(f *File) { f.Meta.Trade.SnapshotFingerprint = "" },
		"short snapshot":     func(f *File) { f.Meta.Trade.SnapshotFingerprint = "sha256:abc" },
		"upper-case digest":  func(f *File) { f.Meta.Trade.SnapshotFingerprint = "sha256:" + strings.Repeat("A", 64) },
		"digest, no prefix":  func(f *File) { f.Meta.Trade.SnapshotFingerprint = strings.Repeat("a", 64) },
		"no as-of date":      func(f *File) { f.Meta.Trade.SnapshotAsOf = "" },
		"as-of not a date":   func(f *File) { f.Meta.Trade.SnapshotAsOf = "2026-02-30" },
		"no raw fingerprint": func(f *File) { f.Meta.RawFingerprint = "" },
	} {
		f := tradeFile(t)
		change(&f)
		assert.Error(t, f.Validate(), name)
	}
}

func TestTradeFile_RefusesBarsItCannotHold(t *testing.T) {
	quote := marketdata.OHLC{Open: px("1"), High: px("1"), Low: px("1"), Close: px("1")}
	for name, change := range map[string]func(*marketdata.Bar){
		"a bid":                func(b *marketdata.Bar) { b.Bid = quote },
		"an ask":               func(b *marketdata.Bar) { b.Ask = quote },
		"a mid":                func(b *marketdata.Bar) { b.Mid = quote },
		"no trade series":      func(b *marketdata.Bar) { b.Trade, b.Mid = marketdata.OHLC{}, quote },
		"high below the close": func(b *marketdata.Bar) { b.Trade.High = px("500") },
		"a zero price":         func(b *marketdata.Bar) { b.Trade.Low = px("0") },
		"negative volume":      func(b *marketdata.Bar) { b.Volume = -1 },
		"late availability":    func(b *marketdata.Bar) { b.AvailableAt = b.AvailableAt.Add(time.Second) },
		"early availability":   func(b *marketdata.Bar) { b.AvailableAt = b.AvailableAt.Add(-3 * time.Hour) },
		"the FX open": func(b *marketdata.Bar) {
			b.Time = minute(2024, 3, 1, 22, 0)
			b.AvailableAt = b.Time.Add(24 * time.Hour)
		},
		"another instrument": func(b *marketdata.Bar) { b.Instrument = "QQQ" },
		"a closed day":       func(b *marketdata.Bar) { b.Time, b.AvailableAt = minute(2024, 3, 2, 14, 30), minute(2024, 3, 2, 21, 0) },
		"another month than file": func(b *marketdata.Bar) {
			b.Time, b.AvailableAt = minute(2024, 2, 29, 14, 30), minute(2024, 2, 29, 21, 0)
		},
	} {
		f := tradeFile(t)
		change(&f.Bars[0])
		assert.ErrorIs(t, f.Validate(), ErrMalformedData, name)
	}

	f := tradeFile(t)
	f.Bars[1] = f.Bars[0]
	assert.ErrorIs(t, f.Validate(), ErrMalformedData, "repeated bar")
}

func TestTradeFile_RefusesADateOutsideTheCalendar(t *testing.T) {
	f := tradeFile(t)
	f.Key.Year, f.Key.Month = 2027, time.January
	f.Bars = f.Bars[:1]
	f.Bars[0].Time, f.Bars[0].AvailableAt = minute(2027, 1, 4, 14, 30), minute(2027, 1, 4, 21, 0)

	err := f.Validate()

	assert.ErrorIs(t, err, ErrOutsideCalendar)
	assert.ErrorIs(t, err, ErrMalformedData)
}

func TestQuoteFile_RefusesNonSession(t *testing.T) {
	f := goodFile(t)
	assert.Equal(t, Schema, f.Schema())
	f.Bad = []BadRecord{{Time: utc(2024, 1, 2, 9), Reason: ReasonNonSession, RawRow: 4}}

	assert.ErrorIs(t, f.Validate(), ErrMalformedData)
}

func TestDecode_RejectsMalformedTradeFiles(t *testing.T) {
	good := encode(t, tradeFile(t))
	quotes := encode(t, goodFile(t))
	swap := func(text, old, new string) string {
		require.Contains(t, text, old)
		return strings.Replace(text, old, new, 1)
	}
	for name, text := range map[string]string{
		"nine decimals":              swap(good, "501.02800000", "501.028000001"),
		"too few decimals":           swap(good, "501.02800000", "501.028"),
		"above the price range":      swap(good, "505.27000000", "99999999999.00000000"),
		"fractional volume":          swap(good, ",78064486", ",78064486.5"),
		"an absent trade series":     swap(good, "501.02800000,505.27000000,500.61400000,504.83800000", ",,,"),
		"a partial series":           swap(good, "501.02800000,", ","),
		"the quote schema's name":    swap(good, "schema=canonical-trade-v1", "schema=canonical-v2"),
		"the trade schema on quotes": swap(quotes, "schema=canonical-v2", "schema=canonical-trade-v1"),
		"an unknown schema":          swap(good, "schema=canonical-trade-v1", "schema=canonical-trade-v2"),
		"quote columns":              swap(good, "trade_o", "bid_o"),
		"an extra column":            swap(good, "volume\n", "volume,vwap\n"),
		"a missing meta field":       swap(good, `,"currency":"USD"`, ""),
		"an unknown meta field":      swap(good, `"currency":"USD"`, `"currency":"USD","exchange":"X"`),
		"a trade field in a quote":   swap(quotes, `"built_at"`, `"calendar":"","built_at"`),
		"a wrong bar count":          swap(good, `"bar_count":2`, `"bar_count":3`),
		"a wrong available time":     swap(good, "2024-03-01T21:00:00Z", "2024-03-01T18:00:00Z"),
		"a non-UTC time":             swap(good, "2024-03-01T14:30:00Z,", "2024-03-01T09:30:00-05:00,"),
	} {
		_, err := Decode(strings.NewReader(text))
		assert.ErrorIs(t, err, ErrMalformedData, name)
	}
}
