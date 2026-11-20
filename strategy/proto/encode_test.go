package proto

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/strategy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ts(s string) time.Time {
	t, err := parseTime(s)
	if err != nil {
		panic(err)
	}
	return t
}

func pr(s string) num.Price { return num.MustParsePrice(s) }

func ohlc(o, h, l, c string) marketdata.OHLC {
	return marketdata.OHLC{Open: pr(o), High: pr(h), Low: pr(l), Close: pr(c)}
}

// golden is every message of the protocol, built in Go, with the file in
// testdata/golden that holds its exact bytes.
func golden() map[string]Message {
	return map[string]Message{
		"register": &Register{Header: Header{Seq: 1}, Name: "emax-go", Version: "0.3.1", Build: "git:9f2c1ab",
			Config: json.RawMessage(`{"short":12,"long":26}`), Capabilities: []string{"bars", "warm"},
			Run: Run{Mode: ModeBacktest, Interval: "H1", Instruments: []string{"EURUSD", "GBPUSD"},
				From: ts("2024-03-01T00:00:00Z"), To: ts("2024-04-01T00:00:00Z"), WarmBars: 100}},
		"register-paper": &Register{Header: Header{Seq: 1}, Name: "emax-go", Capabilities: []string{"bars"},
			Run: Run{Mode: ModePaper, Interval: "D1", Instruments: []string{"EURUSD"}, From: ts("2024-03-01T00:00:00Z")}},
		"accepted": &Accepted{Header: Header{Seq: 1, RunID: "r-20241103-7f3a9c"}, Instruments: []string{"EURUSD", "GBPUSD"},
			Interval: "H1", WarmBars: 100, ReplyTimeoutMS: 30000},
		"bars": &Bars{Header: Header{Seq: 2, RunID: "r-20241103-7f3a9c"}, FrameAt: ts("2024-03-01T11:00:00Z"), Bars: []marketdata.Bar{
			{Instrument: "EURUSD", Time: ts("2024-03-01T10:00:00Z"), AvailableAt: ts("2024-03-01T11:00:00Z"), Volume: 2310,
				Bid: ohlc("1.08725", "1.0879", "1.0869", "1.0876"), Ask: ohlc("1.08728", "1.08794", "1.08693", "1.08764"),
				Mid: ohlc("1.087265", "1.08792", "1.086915", "1.08762")},
			{Instrument: "GBPUSD", Time: ts("2024-03-01T10:00:00Z"), AvailableAt: ts("2024-03-01T11:00:00Z"), Volume: 1984,
				Mid: ohlc("1.26412", "1.264825", "1.26372", "1.264575"), Trade: ohlc("1.2641", "1.2648", "1.2637", "1.26455")}}},
		"bars-warm": &Bars{Header: Header{Seq: 2, RunID: "r-20241103-7f3a9c"}, FrameAt: ts("2024-02-29T11:00:00Z"), Warm: true,
			Bars: []marketdata.Bar{{Instrument: "EURUSD", Time: ts("2024-02-29T10:00:00Z"), AvailableAt: ts("2024-02-29T11:00:00Z"),
				Mid: ohlc("1.08", "1.081", "1.079", "1.0805")}}},
		"orders": &Orders{Header: Header{Seq: 2, RunID: "r-20241103-7f3a9c"}, InReplyTo: 2, Orders: []strategy.Order{
			{Instrument: "EURUSD", Action: strategy.Open, Direction: strategy.Long, Type: strategy.Market, Price: pr("1.08764"),
				StopLoss: pr("1.085"), TakeProfit: pr("1.092"), Trail: pr("0.002"), Ref: "emax-entry-1"},
			{Instrument: "GBPUSD", Action: strategy.Open, Direction: strategy.Short, Type: strategy.Limit, Price: pr("1.266"),
				StopLoss: pr("1.269"), TakeProfit: pr("1.26"), Expires: ts("2024-03-01T20:00:00Z")},
			{Instrument: "GBPUSD", Action: strategy.Close, Ref: "emax-exit-7"}}},
		"orders-empty": &Orders{Header: Header{Seq: 3, RunID: "r-20241103-7f3a9c"}, InReplyTo: 3},
		"orders-amend": &Orders{Header: Header{Seq: 5, RunID: "r-20241103-7f3a9c"}, InReplyTo: 5, Orders: []strategy.Order{
			{Instrument: "EURUSD", Action: strategy.Amend, StopLoss: pr("1.08764"), Trail: pr("0.0015"), TakeProfit: pr("1.1"), Ref: "emax-breakeven-1"}}},
		"done":                  &Done{Header: Header{Seq: 812, RunID: "r-20241103-7f3a9c"}, Status: StatusCompleted},
		"error":                 &Failure{Header: Header{Seq: 3, RunID: "r-20241103-7f3a9c"}, Code: CodeInvalidOrder, Fatal: true, Message: "orders[0]: open requires stop_loss"},
		"error-before-accepted": &Failure{Header: Header{Seq: 1}, Code: CodeVersion, Fatal: true, Message: "protocol version 2 is not supported"},
	}
}

func TestEncode_MatchesTheGoldenBytes(t *testing.T) {
	for name, m := range golden() {
		t.Run(name, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("testdata", "golden", name+".json"))
			require.NoError(t, err)

			got, err := Encode(m)

			require.NoError(t, err)
			assert.Equal(t, strings.TrimRight(string(want), "\n"), string(got))
		})
	}
}

func TestEncode_EveryGoldenFileHasAMessage(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "golden", "*.json"))
	require.NoError(t, err)
	msgs := golden()
	assert.Len(t, files, len(msgs))
	for _, f := range files {
		assert.Contains(t, msgs, strings.TrimSuffix(filepath.Base(f), ".json"))
	}
}

func TestEncode_DecodeRoundTripsEveryMessage(t *testing.T) {
	for name, m := range golden() {
		t.Run(name, func(t *testing.T) {
			payload, err := Encode(m)
			require.NoError(t, err)

			back, err := Decode(payload)

			require.NoError(t, err)
			again, err := Encode(back)
			require.NoError(t, err)
			assert.Equal(t, string(payload), string(again), "decode then encode is the same bytes")
			assert.Equal(t, m.Head().Seq, back.Head().Seq)
			assert.Equal(t, m.Head().RunID, back.Head().RunID)
		})
	}
}

func TestEncode_DecodedGoldenBarsEqualTheBarsThatWereEncoded(t *testing.T) {
	want := golden()["bars"].(*Bars)
	payload, err := Encode(want)
	require.NoError(t, err)

	got, err := Decode(payload)

	require.NoError(t, err)
	assert.Equal(t, want.Bars, got.(*Bars).Bars)
	assert.Equal(t, want.FrameAt, got.(*Bars).FrameAt)
}

func TestEncode_DecodedGoldenOrdersEqualTheOrdersThatWereEncoded(t *testing.T) {
	want := golden()["orders"].(*Orders)
	payload, err := Encode(want)
	require.NoError(t, err)

	got, err := Decode(payload)

	require.NoError(t, err)
	wantOrders := want.Orders
	wantOrders[2].Type = strategy.Market // an empty type is sent, and read back, as market
	assert.Equal(t, wantOrders, got.(*Orders).Orders)
}

func TestEncode_FillsInVersionAndType(t *testing.T) {
	m := &Done{Header: Header{Seq: 1, RunID: "r"}, Status: StatusFailed, Reason: "x"}

	payload, err := Encode(m)

	require.NoError(t, err)
	assert.JSONEq(t, `{"v":1,"type":"done","seq":1,"run_id":"r","status":"failed","reason":"x"}`, string(payload))
}

func TestEncode_Refusals(t *testing.T) {
	for name, m := range map[string]Message{
		"seq 0":               &Done{Header: Header{RunID: "r"}, Status: StatusCompleted},
		"no run_id on done":   &Done{Header: Header{Seq: 1}, Status: StatusCompleted},
		"no run_id on bars":   &Bars{Header: Header{Seq: 1}},
		"another version":     &Done{Header: Header{V: 2, Seq: 1, RunID: "r"}},
		"type does not match": &Done{Header: Header{Type: TypeBars, Seq: 1, RunID: "r"}},
	} {
		_, err := Encode(m)
		assert.Error(t, err, name)
	}
}

func TestEncode_ARegisterAndAFailureNeedNoRunID(t *testing.T) {
	_, err := Encode(&Register{Header: Header{Seq: 1}, Capabilities: []string{"bars"}})
	require.NoError(t, err)
	_, err = Encode(&Failure{Header: Header{Seq: 1}, Code: CodeProtocol, Fatal: true, Message: "x"})
	require.NoError(t, err)
}

type unknownMessage struct{ Header }

func TestEncode_RefusesAMessageItDoesNotKnow(t *testing.T) {
	_, err := Encode(&unknownMessage{Header{V: 1, Type: TypeDone, Seq: 1, RunID: "r"}})

	assert.Error(t, err)
}

func TestEncode_DefaultsEmptyCollectionsAndConfig(t *testing.T) {
	payload, err := Encode(&Register{Header: Header{Seq: 1}, Name: "x"})
	require.NoError(t, err)

	assert.Contains(t, string(payload), `"config":{}`)
	assert.Contains(t, string(payload), `"capabilities":[]`)
	assert.Contains(t, string(payload), `"instruments":[]`)
	assert.NotContains(t, string(payload), `"to"`, "no end is left out")
}

func TestEncode_DoesNotEscapeHTML(t *testing.T) {
	payload, err := Encode(&Done{Header: Header{Seq: 1, RunID: "r"}, Status: StatusFailed, Reason: "a<b & c>d"})

	require.NoError(t, err)
	assert.Contains(t, string(payload), `a<b & c>d`)
}

func TestPriceText_PadsToFiveDecimalsAndNeverRounds(t *testing.T) {
	for in, want := range map[string]string{
		"1.1":        "1.10000",
		"150":        "150.00000",
		"1.08726":    "1.08726",
		"1.087265":   "1.087265",
		"1.08726512": "1.08726512",
	} {
		got := priceText(pr(in))
		assert.Equal(t, want, got, in)
		assert.True(t, pr(got).Equal(pr(in)), "%s keeps its value", in)
	}
}

func TestOptPrice_ZeroIsAbsent(t *testing.T) {
	assert.Empty(t, optPrice(num.Price{}))
	assert.Equal(t, "1.10000", optPrice(pr("1.1")))
}

func TestOhlcOf_AnAbsentSeriesIsLeftOut(t *testing.T) {
	assert.Nil(t, ohlcOf(marketdata.OHLC{}))
	assert.Equal(t, &wireOHLC{Open: "1.00000", High: "2.00000", Low: "0.50000", Close: "1.50000"}, ohlcOf(ohlc("1", "2", "0.5", "1.5")))
}

func TestWrite_ThenRead_ReturnsTheMessage(t *testing.T) {
	var buf bytes.Buffer
	m := golden()["orders"].(*Orders)

	require.NoError(t, Write(&buf, m, DefaultMaxFrame))
	got, err := Read(&buf, DefaultMaxFrame)

	require.NoError(t, err)
	assert.Equal(t, TypeOrders, got.Head().Type)
	assert.Len(t, got.(*Orders).Orders, 3)
}

func TestWrite_RefusesAMessageOverTheLimitAndAnInvalidOne(t *testing.T) {
	var buf bytes.Buffer

	assert.Equal(t, CodeProtocol, CodeOf(Write(&buf, golden()["bars"], 20)))
	assert.Error(t, Write(&buf, &Done{}, DefaultMaxFrame))
	assert.Zero(t, buf.Len())
}

func TestRead_ReturnsFrameAndDecodeErrors(t *testing.T) {
	_, err := Read(bytes.NewReader(frameOf(0, "")), 10)
	assert.Equal(t, CodeProtocol, CodeOf(err))

	_, err = Read(bytes.NewReader(frameOf(2, `[]`)), 10)
	assert.Equal(t, CodeProtocol, CodeOf(err))
}
